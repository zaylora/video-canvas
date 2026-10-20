package repository

import (
	"context"
	"errors"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// SlotResult 是一次名额申请的结果。
type SlotResult string

// 名额申请的各种结果。
const (
	SlotAcquired    SlotResult = "acquired"     // 已拿到（或本来就持有）用户名额和渠道名额
	SlotWaitUser    SlotResult = "wait_user"    // 用户的并发名额已满，继续排队
	SlotWaitChannel SlotResult = "wait_channel" // 渠道的“最大同时生成数”已满，或前面还有更早的等待者
	SlotGone        SlotResult = "gone"         // 任务不存在或已不是 pending（被取消、超时了），不用再申请
)

// SlotRequest 是一次名额申请的参数。
type SlotRequest struct {
	TaskID           uint64
	UserLimit        int       // 该用户生效的并发上限（单用户覆盖 > 系统默认）；试跑任务忽略
	DefaultUserLimit int       // 系统默认并发上限：推算排在前面的等待者里谁还有空闲名额时，对没有单独覆盖的用户用它
	Now              time.Time // 当前时间：已超过截止时间的等待者不参与排名
	Deadline         time.Time // 拿到名额后任务的新截止时间：生成时长从拿到名额的这一刻重新计算，排队时间不吃生成时间
}

// slotLockSQL 是名额事务的两把建议锁：先用户后渠道，加锁顺序固定，避免死锁。
// 用户锁让同一用户并发的申请串行（用户名额不会被穿透），渠道锁让同一渠道并发的申请串行（渠道名额不会被穿透）。
const (
	userSlotLockSQL    = `SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`
	channelSlotLockSQL = `SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`
)

// 占名额的状态列表（SQL IN 列表），要和 model.ActiveTaskStatuses 保持一致。
const (
	// userHoldStatuses：占用户名额的状态（含 finalizing：转存时用户仍算“进行中”）
	userHoldStatuses = `('pending','queued','running','finalizing')`
	// channelHoldStatuses：占渠道名额的状态（不含 finalizing：上游已经出结果，不再占用上游的生成能力）
	channelHoldStatuses = `('pending','queued','running')`
)

// channelLimitSQL 读渠道当前的“最大同时生成数”，缺省、为 0 或渠道记录不存在都表示不限。
// 取当前配置而不是快照：运营调整上限后，对还在排队的任务立即生效。
const channelLimitSQL = `
SELECT COALESCE(NULLIF(rate_limit_json->>'max_running', '')::int, 0)
FROM ai_channels WHERE key = ?`

// channelAheadSQL 数渠道上排在我前面、并且“现在真能拿到名额”的等待者：
// 同渠道、仍在 pending 且没拿到名额、没超时、(created_at, id) 更小，并且它自己的用户还有空闲名额。
// 最后一个条件很关键：排在前面但自己用户名额已满的任务暂时拿不了名额，不能占着渠道名额不放、挡住别的用户。
// 试跑任务不受用户上限约束，视为一定有空闲。
const channelAheadSQL = `
SELECT COUNT(*) FROM generation_tasks w
LEFT JOIN users u ON u.id = w.user_id
WHERE w.provider = @provider AND w.status = 'pending' AND w.slot_acquired_at IS NULL
  AND w.deadline_at >= @now
  AND (w.created_at, w.id) < (@created, @id)
  AND (w.is_test OR (
        SELECT COUNT(*) FROM generation_tasks o
        WHERE o.user_id = w.user_id AND o.is_test = FALSE AND o.slot_acquired_at IS NOT NULL
          AND o.status IN ` + userHoldStatuses + `
      ) < COALESCE(NULLIF(u.max_active_tasks, 0), @default_limit))`

// AcquireSlot 为 pending 任务申请“用户名额 + 渠道名额”，两个都有才算拿到，并在同一个事务里写 slot_acquired_at 与新的 deadline_at。
//   - 用户名额：该用户已占名额（slot_acquired_at 非空且非终态）的正式任务数 < UserLimit；试跑任务不看用户上限；
//   - 渠道名额：渠道已占名额（pending / queued / running）数 + 排在前面的有效等待者数 < 渠道 max_running，先到先得；
//   - 事务先拿用户锁再拿渠道锁，保证并发申请时占用数不会超过上限。
//
// 任务已持有名额返回 SlotAcquired（幂等，不改任何字段）；任务不存在或已不是 pending 返回 SlotGone。
// 没拿到名额不改任何数据，调用方稍后重试。
func (r *GenerationTaskRepository) AcquireSlot(ctx context.Context, req SlotRequest) (SlotResult, error) {
	result := SlotGone
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 读任务并锁行：同一个任务的重复申请（例如 job 重试）在这里串行
		t, early, err := lockSlotTask(tx, req.TaskID)
		if err != nil || early != "" {
			result = early
			return err
		}
		// 2. 先用户锁、后渠道锁（试跑任务只锁渠道）
		if err := lockSlotScopes(tx, t); err != nil {
			return err
		}
		// 3. 用户名额、渠道名额，任何一个不够就等
		if wait, err := slotWait(tx, t, req); err != nil || wait != "" {
			result = wait
			return err
		}
		// 4. 两个名额都有：占用，并把生成时长从现在起重新计算
		err = tx.Model(&model.GenerationTask{}).
			Where("id = ? AND status = ? AND slot_acquired_at IS NULL", t.ID, model.TaskPending).
			Updates(map[string]any{"slot_acquired_at": req.Now, "deadline_at": req.Deadline, "updated_at": req.Now}).Error
		result = SlotAcquired
		return err
	})
	if err != nil {
		return "", err
	}
	return result, nil
}

// lockSlotTask 读出任务并锁行。返回的 early 非空表示不用继续申请：任务不存在或已不是 pending 返回 SlotGone，已持有名额返回 SlotAcquired。
func lockSlotTask(tx *gorm.DB, id uint64) (t *model.GenerationTask, early SlotResult, err error) {
	t = &model.GenerationTask{}
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id", "user_id", "provider", "status", "is_test", "created_at", "slot_acquired_at").
		Where("id = ?", id).Take(t).Error
	switch {
	case errors.Is(translate(err), ErrNotFound):
		return nil, SlotGone, nil
	case err != nil:
		return nil, "", err
	case t.Status != model.TaskPending:
		return nil, SlotGone, nil
	case t.SlotAcquiredAt != nil:
		return nil, SlotAcquired, nil
	}
	return t, "", nil
}

// lockSlotScopes 按固定顺序（先用户后渠道）拿事务级建议锁；试跑任务不占用户名额，所以只锁渠道。
func lockSlotScopes(tx *gorm.DB, t *model.GenerationTask) error {
	if !t.IsTest {
		if err := tx.Exec(userSlotLockSQL, "vc_slot_user:"+strconv.FormatUint(t.UserID, 10)).Error; err != nil {
			return err
		}
	}
	return tx.Exec(channelSlotLockSQL, "vc_slot_channel:"+t.Provider).Error
}

// slotWait 检查用户名额与渠道名额：都够返回空串；用户名额满返回 SlotWaitUser，渠道名额满（或前面还有更早的等待者）返回 SlotWaitChannel。
func slotWait(tx *gorm.DB, t *model.GenerationTask, req SlotRequest) (SlotResult, error) {
	if !t.IsTest {
		var held int
		err := tx.Raw(`SELECT COUNT(*) FROM generation_tasks WHERE user_id = ? AND is_test = FALSE AND slot_acquired_at IS NOT NULL AND status IN `+userHoldStatuses, t.UserID).Scan(&held).Error
		if err != nil {
			return "", err
		}
		if held >= req.UserLimit {
			return SlotWaitUser, nil
		}
	}

	// 渠道名额：上限为 0（或渠道记录不存在）不限
	var limit int
	if err := tx.Raw(channelLimitSQL, t.Provider).Scan(&limit).Error; err != nil || limit <= 0 {
		return "", err
	}
	var held, ahead int
	if err := tx.Raw(`SELECT COUNT(*) FROM generation_tasks WHERE provider = ? AND slot_acquired_at IS NOT NULL AND status IN `+channelHoldStatuses, t.Provider).Scan(&held).Error; err != nil {
		return "", err
	}
	err := tx.Raw(channelAheadSQL, map[string]any{
		"provider": t.Provider, "now": req.Now, "created": t.CreatedAt, "id": t.ID, "default_limit": req.DefaultUserLimit,
	}).Scan(&ahead).Error
	if err != nil {
		return "", err
	}
	if held+ahead >= limit {
		return SlotWaitChannel, nil
	}
	return "", nil
}
