package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
)

// ErrStateConflict 状态迁移的 CAS 没有命中：任务不存在，或当前状态已不在期望的 from 集合里
// （被别的流程抢先迁移了）。调用方应当把它当作“已被处理”，而不是失败。
var ErrStateConflict = errors.New("task state conflict")

// GenerationTaskTx 是任务与积分的 SQL 原语。同一个实现既可以在事务外单独使用，
// 也会在 WithTx 里绑定到事务上传给回调；service 用它们组合出“状态迁移 + 积分”的事务。
type GenerationTaskTx interface {
	// FindByIdempotencyKey 按 (user_id, idempotency_key) 查任务，不存在返回 ErrNotFound。
	FindByIdempotencyKey(ctx context.Context, userID uint64, key string) (*model.GenerationTask, error)
	// CountActive 统计用户非终态的正式任务数（不含 is_test）。
	CountActive(ctx context.Context, userID uint64) (int64, error)
	// EnsureCredit 账户不存在时以 initial 惰性创建（INSERT … ON CONFLICT DO NOTHING）并在同一事务写一条 initial 流水，已存在则什么都不做。
	EnsureCredit(ctx context.Context, userID uint64, initial int) error
	// LockCredit 读取积分账户并加行锁（SELECT … FOR UPDATE），账户不存在返回 ErrNotFound。
	LockCredit(ctx context.Context, userID uint64) (*model.UserCredit, error)
	// AddCredit 对账户做增量更新：balance += dBalance，frozen += dFrozen。账户不存在返回 ErrNotFound。
	AddCredit(ctx context.Context, userID uint64, dBalance, dFrozen int) error
	// InsertLedger 写一条流水（ON CONFLICT DO NOTHING）。inserted=false 表示 (task_id, type) 已存在，即已处理过。
	InsertLedger(ctx context.Context, entry *model.CreditLedger) (inserted bool, err error)
	// InsertTask 插入任务（ON CONFLICT DO NOTHING）。inserted=false 表示命中了幂等唯一索引，任务没有被写入。
	InsertTask(ctx context.Context, t *model.GenerationTask) (inserted bool, err error)
	// EnqueueTask 在当前事务里把任务放进调度队列，必须在 WithTx 的回调里调用；没有配置队列（单元测试）时什么都不做。
	// 任务行、积分冻结和队列里的 job 因此在同一个事务里提交或回滚。
	EnqueueTask(ctx context.Context, t *model.GenerationTask) error
	// EnqueueAgentNotify 在当前事务里为 Agent 发起的任务入队“任务结束了”的通知（job 与任务终态同一个事务提交或回滚）。
	// 没有配置通知队列（Agent 未启用、单元测试）时什么都不做；必须在 WithTx 的回调里调用。
	EnqueueAgentNotify(ctx context.Context, t *model.GenerationTask) error
	// UpdateIf 是状态迁移的 CAS：UPDATE … WHERE id=? AND status IN (from) … RETURNING *。
	// bumpVersion 为 true 时 version+1（状态或进度对用户可见地变化了）。没命中返回 ErrStateConflict。
	UpdateIf(ctx context.Context, id uint64, from []string, fields map[string]any, bumpVersion bool) (*model.GenerationTask, error)
}

// TaskEnqueuer 在一个已经打开的数据库事务里，为任务放进调度队列（真实实现在 provider/queue，基于 River）。
type TaskEnqueuer interface {
	// EnqueueTx 用 sqlTx 所在的事务为任务入队：事务回滚时 job 也不存在。
	// status 决定进哪个阶段的队列；runAt 为零表示立即执行。
	EnqueueTx(ctx context.Context, sqlTx *sql.Tx, taskID uint64, status string, runAt time.Time) error
	// EnqueueAgentNotifyTx 用 sqlTx 所在的事务为 Agent 入队任务结束的通知。
	EnqueueAgentNotifyTx(ctx context.Context, sqlTx *sql.Tx, taskID, agentRunID uint64) error
}

// GenerationTaskRepository 是生成任务与积分的数据访问层，只做 SQL，不含业务规则。
type GenerationTaskRepository struct {
	db  *gorm.DB
	enq TaskEnqueuer // 为 nil 时 EnqueueTask 什么都不做
}

// NewGenerationTaskRepository 创建生成任务与积分仓储。
func NewGenerationTaskRepository(db *gorm.DB) *GenerationTaskRepository {
	return &GenerationTaskRepository{db: db}
}

// SetEnqueuer 设置事务内入队的实现。在组装依赖时调用一次，之后只读。
func (r *GenerationTaskRepository) SetEnqueuer(e TaskEnqueuer) { r.enq = e }

// EnqueueTask 在当前事务里为任务入队。没有配置队列时直接返回；不在事务里调用是编程错误，返回错误而不是悄悄丢任务。
func (r *GenerationTaskRepository) EnqueueTask(ctx context.Context, t *model.GenerationTask) error {
	if r.enq == nil {
		return nil
	}
	sqlTx, ok := r.db.Statement.ConnPool.(*sql.Tx)
	if !ok {
		return errors.New("EnqueueTask 必须在事务里调用")
	}
	return r.enq.EnqueueTx(ctx, sqlTx, t.ID, t.Status, t.NextPollAt)
}

// EnqueueAgentNotify 在当前事务里为 Agent 入队任务结束的通知。没有配置队列时直接返回；不在事务里调用是编程错误。
func (r *GenerationTaskRepository) EnqueueAgentNotify(ctx context.Context, t *model.GenerationTask) error {
	if r.enq == nil {
		return nil
	}
	sqlTx, ok := r.db.Statement.ConnPool.(*sql.Tx)
	if !ok {
		return errors.New("EnqueueAgentNotify 必须在事务里调用")
	}
	return r.enq.EnqueueAgentNotifyTx(ctx, sqlTx, t.ID, t.AgentRunID)
}

var _ GenerationTaskTx = (*GenerationTaskRepository)(nil)

// WithTx 在一个数据库事务里执行 fn：fn 返回错误则整体回滚，否则提交。
// 传给 fn 的是绑定到该事务的仓储，fn 里的所有读写都在同一个事务中。
func (r *GenerationTaskRepository) WithTx(ctx context.Context, fn func(tx GenerationTaskTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&GenerationTaskRepository{db: tx, enq: r.enq})
	})
}

// activeStatuses 是非终态列表，用于 IN 查询。
func activeStatuses() []string { return model.ActiveTaskStatuses }

// GetByID 按 id + user_id 查询任务（含 is_test，由 service 决定是否对外暴露），查不到或不属于该用户返回 ErrNotFound。
func (r *GenerationTaskRepository) GetByID(ctx context.Context, userID, id uint64) (*model.GenerationTask, error) {
	var t model.GenerationTask
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&t).Error; err != nil {
		return nil, translate(err)
	}
	return &t, nil
}

// GetByIDAny 只按 id 查询，供系统内部（worker、webhook）使用，不做归属过滤。不存在返回 ErrNotFound。
func (r *GenerationTaskRepository) GetByIDAny(ctx context.Context, id uint64) (*model.GenerationTask, error) {
	var t model.GenerationTask
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&t).Error; err != nil {
		return nil, translate(err)
	}
	return &t, nil
}

// ListByIDs 批量查询该用户的正式任务（排除 is_test），别人的任务查不到，按 id 升序。
func (r *GenerationTaskRepository) ListByIDs(ctx context.Context, userID uint64, ids []uint64) ([]model.GenerationTask, error) {
	var list []model.GenerationTask
	if len(ids) == 0 {
		return list, nil
	}
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND is_test = FALSE AND id IN ?", userID, ids).
		Order("id ASC").Find(&list).Error
	return list, err
}

// ListBriefByIDs 按 id 批量查询任务，不带用户条件、不排除 is_test：给后台素材库按素材的 task_id 带出提示词与模型名用。
// 只取 id、model_id、input_json、config_snapshot 四列（其余列如 trace_json、provider_result 可能很大，这里用不到）；
// 不存在的 id 缺席，顺序不保证；ids 为空返回 nil。
func (r *GenerationTaskRepository) ListBriefByIDs(ctx context.Context, ids []uint64) ([]model.GenerationTask, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []model.GenerationTask
	err := r.db.WithContext(ctx).Select("id", "model_id", "input_json", "config_snapshot").Where("id IN ?", ids).Find(&list).Error
	return list, err
}

// ListActive 查询该用户所有非终态的正式任务（排除 is_test），按 id 升序。
func (r *GenerationTaskRepository) ListActive(ctx context.Context, userID uint64) ([]model.GenerationTask, error) {
	var list []model.GenerationTask
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND is_test = FALSE AND status IN ?", userID, activeStatuses()).
		Order("id ASC").Find(&list).Error
	return list, err
}

// FindByIdempotencyKey 按 (user_id, idempotency_key) 查任务，不存在返回 ErrNotFound。
func (r *GenerationTaskRepository) FindByIdempotencyKey(ctx context.Context, userID uint64, key string) (*model.GenerationTask, error) {
	var t model.GenerationTask
	err := r.db.WithContext(ctx).Where("user_id = ? AND idempotency_key = ?", userID, key).First(&t).Error
	if err != nil {
		return nil, translate(err)
	}
	return &t, nil
}

// CountActive 统计用户非终态的正式任务数（不含 is_test）。
func (r *GenerationTaskRepository) CountActive(ctx context.Context, userID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GenerationTask{}).
		Where("user_id = ? AND is_test = FALSE AND status IN ?", userID, activeStatuses()).
		Count(&n).Error
	return n, err
}

// CountAllActive 统计全站非终态的正式任务数（不含 is_test），升级前提示会中断多少任务。
func (r *GenerationTaskRepository) CountAllActive(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GenerationTask{}).
		Where("is_test = FALSE AND status IN ?", activeStatuses()).
		Count(&n).Error
	return n, err
}

// EnsureCredit 账户不存在时以 initial 惰性创建，已存在则不改动（INSERT … ON CONFLICT DO NOTHING）。
func (r *GenerationTaskRepository) EnsureCredit(ctx context.Context, userID uint64, initial int) error {
	return insertInitialCredit(ctx, r.db, userID, initial)
}

// GetCredit 读取积分账户（不加锁），不存在返回 ErrNotFound。
func (r *GenerationTaskRepository) GetCredit(ctx context.Context, userID uint64) (*model.UserCredit, error) {
	var c model.UserCredit
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&c).Error; err != nil {
		return nil, translate(err)
	}
	return &c, nil
}

// LockCredit 读取积分账户并加行锁（SELECT … FOR UPDATE）；必须在事务里调用才有意义。不存在返回 ErrNotFound。
func (r *GenerationTaskRepository) LockCredit(ctx context.Context, userID uint64) (*model.UserCredit, error) {
	var c model.UserCredit
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).First(&c).Error
	if err != nil {
		return nil, translate(err)
	}
	return &c, nil
}

// AddCredit 对账户做增量更新（balance += dBalance, frozen += dFrozen），由数据库原子完成。账户不存在返回 ErrNotFound。
func (r *GenerationTaskRepository) AddCredit(ctx context.Context, userID uint64, dBalance, dFrozen int) error {
	res := r.db.WithContext(ctx).Model(&model.UserCredit{}).Where("user_id = ?", userID).Updates(map[string]any{
		"balance":    gorm.Expr("balance + ?", dBalance),
		"frozen":     gorm.Expr("frozen + ?", dFrozen),
		"updated_at": time.Now(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// InsertLedger 写一条积分流水。用 ON CONFLICT DO NOTHING 而不是捕获唯一冲突：
// PostgreSQL 里冲突错误会让整个事务进入 aborted 状态，后面的语句全部失败；
// DO NOTHING 则不影响事务，调用方通过 inserted 判断“是否已处理过”。
func (r *GenerationTaskRepository) InsertLedger(ctx context.Context, entry *model.CreditLedger) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(entry)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// InsertTask 插入任务。命中 (user_id, idempotency_key) 唯一索引时 DO NOTHING，inserted=false，事务不会被污染。
// 任务编号 task_ref 由主键编码得到，所以先从序列取号定下主键，再和任务一起写入（调用方已指定 id 时沿用）。
func (r *GenerationTaskRepository) InsertTask(ctx context.Context, t *model.GenerationTask) (bool, error) {
	if t.ID == 0 {
		var id uint64
		if err := r.db.WithContext(ctx).
			Raw("SELECT nextval(pg_get_serial_sequence(?, 'id'))", t.TableName()).Scan(&id).Error; err != nil {
			return false, err
		}
		t.ID = id
	}
	t.TaskRef = idcodec.Encode(t.ID)
	// 任务与 created 事件同事务写入：命中幂等唯一索引（没写入任务）时不产生事件
	var inserted bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(t)
		if res.Error != nil {
			return res.Error
		}
		inserted = res.RowsAffected > 0
		if !inserted {
			return nil
		}
		return insertTaskEvent(tx, &model.GenerationTaskEvent{
			TaskID: t.ID, Event: model.TaskEventCreated, ToStatus: t.Status,
		})
	})
	if err != nil {
		return false, err
	}
	return inserted, nil
}

// EventDetailKey 是 UpdateIf 的 fields 里的保留键：值是 map[string]any，会被取出来写进本次状态迁移产生的事件的 detail_json，
// 不会被当成列更新。没有状态变化（不产生事件）时忽略它。
const EventDetailKey = "$event_detail"

// UpdateIf 状态迁移的 CAS：只有任务当前状态在 from 里才更新，并通过 RETURNING 拿回更新后的整行。
// 没有命中（不存在或状态已变）返回 ErrStateConflict。fields 不会被修改。
// fields 里带 status 且状态真正变化时，在同一事务里写一条任务事件（见 taskEventFor）：
// from 只有一个状态时它就是迁移前状态；有多个且需要判断时，先 SELECT … FOR UPDATE 读出真实的迁移前状态，
// 这样 from_status 不会因为并发而写错，也不会为每次轮询多一次查询。
func (r *GenerationTaskRepository) UpdateIf(ctx context.Context, id uint64, from []string, fields map[string]any, bumpVersion bool) (*model.GenerationTask, error) {
	set := make(map[string]any, len(fields)+1)
	var detail map[string]any
	for k, v := range fields {
		if k == EventDetailKey {
			detail, _ = v.(map[string]any)
			continue
		}
		set[k] = v
	}
	if bumpVersion {
		set["version"] = gorm.Expr("version + 1")
	}
	to, hasStatus := set["status"].(string)

	var t model.GenerationTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		prev := ""
		if len(from) == 1 {
			prev = from[0]
		}
		if hasStatus && len(from) > 1 && needPrevStatus(from, to) {
			if err := tx.Raw("SELECT status FROM generation_tasks WHERE id = ? FOR UPDATE", id).Scan(&prev).Error; err != nil {
				return err
			}
		}
		res := tx.Model(&t).Clauses(clause.Returning{}).
			Where("id = ? AND status IN ?", id, from).Updates(set)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrStateConflict
		}
		if !hasStatus {
			return nil
		}
		event, ok := taskEventFor(prev, t.Status)
		if !ok {
			return nil
		}
		ev := &model.GenerationTaskEvent{TaskID: id, Event: event, FromStatus: prev, ToStatus: t.Status}
		if event == model.TaskEventSubmitted && t.ProviderTaskID != "" && detail == nil {
			detail = map[string]any{"provider_task_id_set": true}
		}
		if len(detail) > 0 {
			raw, err := json.Marshal(detail)
			if err != nil {
				return fmt.Errorf("编码任务事件详情失败：%w", err)
			}
			ev.DetailJSON = datatypes.JSON(raw)
		}
		return insertTaskEvent(tx, ev)
	})
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// needPrevStatus 判断多个 from 的情况下是否需要读出真实的迁移前状态：
// 只有可能产生事件（进入 finalizing / 终态，或 from 里含 pending 而目标是 queued / running）时才需要。
func needPrevStatus(from []string, to string) bool {
	switch to {
	case model.TaskFinalizing, model.TaskSucceeded, model.TaskFailed, model.TaskCanceled, model.TaskExpired:
		return true
	case model.TaskQueued, model.TaskRunning:
		return slices.Contains(from, model.TaskPending)
	}
	return false
}

// taskEventFor 决定一次状态迁移要记哪个事件：
// pending → queued / running 记 submitted；进入 finalizing 与四个终态各记同名事件；
// queued ↔ running（轮询得来）、状态没变、回到 pending 都不记，避免噪声。
func taskEventFor(prev, to string) (string, bool) {
	if prev == to {
		return "", false
	}
	switch to {
	case model.TaskQueued, model.TaskRunning:
		if prev == model.TaskPending {
			return model.TaskEventSubmitted, true
		}
	case model.TaskFinalizing:
		return model.TaskEventFinalizing, true
	case model.TaskSucceeded:
		return model.TaskEventSucceeded, true
	case model.TaskFailed:
		return model.TaskEventFailed, true
	case model.TaskCanceled:
		return model.TaskEventCanceled, true
	case model.TaskExpired:
		return model.TaskEventExpired, true
	}
	return "", false
}

// insertTaskEvent 写一条任务事件，必须在调用方的事务里调用（和状态迁移一起提交或回滚）。
func insertTaskEvent(tx *gorm.DB, ev *model.GenerationTaskEvent) error {
	return tx.Create(ev).Error
}

// ListEvents 返回任务的状态时间线（id 升序即发生顺序）；上线前的老任务没有事件，返回空切片。
func (r *GenerationTaskRepository) ListEvents(ctx context.Context, taskID uint64) ([]model.GenerationTaskEvent, error) {
	evs := []model.GenerationTaskEvent{}
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("id ASC").Find(&evs).Error
	return evs, err
}

// TouchByProviderTask 把某个平台任务对应的非终态任务的 next_poll_at 设为 now（webhook 用），返回命中行数。
func (r *GenerationTaskRepository) TouchByProviderTask(ctx context.Context, provider, providerTaskID string, now time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.GenerationTask{}).
		Where("provider = ? AND provider_task_id = ? AND status IN ?", provider, providerTaskID, activeStatuses()).
		Updates(map[string]any{"next_poll_at": now, "updated_at": now})
	return res.RowsAffected, res.Error
}

// CreditReconcile 是一个用户的积分对账结果：账户余额与流水、进行中任务三方互相校验。
type CreditReconcile struct {
	Balance        int   // user_credits.balance
	Frozen         int   // user_credits.frozen
	FreezeSum      int64 // 流水 freeze 合计
	SettleSum      int64 // 流水 settle 合计
	RefundSum      int64 // 流水 refund 合计
	InitialSum     int64 // 流水 initial 合计（积分账户创建时的初始积分）
	AdminAdjustSum int64 // 流水 admin_adjust 合计（管理员调整，带正负号）
	RechargeSum    int64 // 流水 recharge 合计（支付充值入账）
	ActiveCredits  int64 // 非终态正式任务冻结的积分合计
}

// FrozenDiff 冻结额与流水推算值（freeze - settle - refund）的差，应为 0。
func (c CreditReconcile) FrozenDiff() int64 {
	return int64(c.Frozen) - (c.FreezeSum - c.SettleSum - c.RefundSum)
}

// ActiveDiff 冻结额与进行中任务积分合计的差，应为 0。
func (c CreditReconcile) ActiveDiff() int64 { return int64(c.Frozen) - c.ActiveCredits }

// BalanceDiff 余额与流水推算值的差，应为 0：余额 = Σinitial + Σadmin_adjust + Σrecharge - Σsettle。
// 初始积分、管理员调整和充值都以流水记账，所以不再需要调用方传入初始积分；freeze / refund 只动冻结额，不影响余额。
func (c CreditReconcile) BalanceDiff() int64 {
	return int64(c.Balance) - (c.InitialSum + c.AdminAdjustSum + c.RechargeSum - c.SettleSum)
}

// Reconcile 汇总用户的流水与账户，用于对账；账户不存在返回 ErrNotFound。
func (r *GenerationTaskRepository) Reconcile(ctx context.Context, userID uint64) (*CreditReconcile, error) {
	acc, err := r.GetCredit(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := &CreditReconcile{Balance: acc.Balance, Frozen: acc.Frozen}
	err = r.db.WithContext(ctx).Raw(`
		SELECT
		  COALESCE(SUM(amount) FILTER (WHERE type = 'freeze'), 0) AS freeze_sum,
		  COALESCE(SUM(amount) FILTER (WHERE type = 'settle'), 0) AS settle_sum,
		  COALESCE(SUM(amount) FILTER (WHERE type = 'refund'), 0) AS refund_sum,
		  COALESCE(SUM(amount) FILTER (WHERE type = 'initial'), 0) AS initial_sum,
		  COALESCE(SUM(amount) FILTER (WHERE type = 'admin_adjust'), 0) AS admin_adjust_sum,
		  COALESCE(SUM(amount) FILTER (WHERE type = 'recharge'), 0) AS recharge_sum
		FROM credit_ledger WHERE user_id = ?`, userID).Row().Scan(&out.FreezeSum, &out.SettleSum, &out.RefundSum, &out.InitialSum, &out.AdminAdjustSum, &out.RechargeSum)
	if err != nil {
		return nil, err
	}
	err = r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(credits), 0) FROM generation_tasks
		WHERE user_id = ? AND is_test = FALSE AND status IN ?`, userID, activeStatuses()).Row().Scan(&out.ActiveCredits)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SaveTrace 写入试跑任务的执行追踪（trace_json，已脱敏）。只更新 is_test 任务（WHERE id=? AND is_test=TRUE），
// 不改状态、不 bump version；任务不存在或不是试跑任务返回 ErrNotFound。
func (r *GenerationTaskRepository) SaveTrace(ctx context.Context, id uint64, trace datatypes.JSON) error {
	res := r.db.WithContext(ctx).Model(&model.GenerationTask{}).
		Where("id = ? AND is_test = TRUE", id).Update("trace_json", trace)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
