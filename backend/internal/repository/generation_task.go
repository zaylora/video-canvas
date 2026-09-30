package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
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
	// EnsureCredit 账户不存在时以 initial 惰性创建（INSERT … ON CONFLICT DO NOTHING），已存在则什么都不做。
	EnsureCredit(ctx context.Context, userID uint64, initial int) error
	// LockCredit 读取积分账户并加行锁（SELECT … FOR UPDATE），账户不存在返回 ErrNotFound。
	LockCredit(ctx context.Context, userID uint64) (*model.UserCredit, error)
	// AddCredit 对账户做增量更新：balance += dBalance，frozen += dFrozen。账户不存在返回 ErrNotFound。
	AddCredit(ctx context.Context, userID uint64, dBalance, dFrozen int) error
	// InsertLedger 写一条流水（ON CONFLICT DO NOTHING）。inserted=false 表示 (task_id, type) 已存在，即已处理过。
	InsertLedger(ctx context.Context, entry *model.CreditLedger) (inserted bool, err error)
	// InsertTask 插入任务（ON CONFLICT DO NOTHING）。inserted=false 表示命中了幂等唯一索引，任务没有被写入。
	InsertTask(ctx context.Context, t *model.GenerationTask) (inserted bool, err error)
	// UpdateIf 是状态迁移的 CAS：UPDATE … WHERE id=? AND status IN (from) … RETURNING *。
	// bumpVersion 为 true 时 version+1（状态或进度对用户可见地变化了）。没命中返回 ErrStateConflict。
	UpdateIf(ctx context.Context, id uint64, from []string, fields map[string]any, bumpVersion bool) (*model.GenerationTask, error)
}

// GenerationTaskRepository 是生成任务与积分的数据访问层，只做 SQL，不含业务规则。
type GenerationTaskRepository struct {
	db *gorm.DB
}

// NewGenerationTaskRepository 创建生成任务与积分仓储。
func NewGenerationTaskRepository(db *gorm.DB) *GenerationTaskRepository {
	return &GenerationTaskRepository{db: db}
}

var _ GenerationTaskTx = (*GenerationTaskRepository)(nil)

// WithTx 在一个数据库事务里执行 fn：fn 返回错误则整体回滚，否则提交。
// 传给 fn 的是绑定到该事务的仓储，fn 里的所有读写都在同一个事务中。
func (r *GenerationTaskRepository) WithTx(ctx context.Context, fn func(tx GenerationTaskTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&GenerationTaskRepository{db: tx})
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

// EnsureCredit 账户不存在时以 initial 惰性创建，已存在则不改动（INSERT … ON CONFLICT DO NOTHING）。
func (r *GenerationTaskRepository) EnsureCredit(ctx context.Context, userID uint64, initial int) error {
	acc := &model.UserCredit{UserID: userID, Balance: initial, UpdatedAt: time.Now()}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(acc).Error
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
func (r *GenerationTaskRepository) InsertTask(ctx context.Context, t *model.GenerationTask) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(t)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// UpdateIf 状态迁移的 CAS：只有任务当前状态在 from 里才更新，并通过 RETURNING 拿回更新后的整行。
// 没有命中（不存在或状态已变）返回 ErrStateConflict。fields 不会被修改。
func (r *GenerationTaskRepository) UpdateIf(ctx context.Context, id uint64, from []string, fields map[string]any, bumpVersion bool) (*model.GenerationTask, error) {
	set := make(map[string]any, len(fields)+1)
	for k, v := range fields {
		set[k] = v
	}
	if bumpVersion {
		set["version"] = gorm.Expr("version + 1")
	}
	var t model.GenerationTask
	res := r.db.WithContext(ctx).Model(&t).Clauses(clause.Returning{}).
		Where("id = ? AND status IN ?", id, from).Updates(set)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrStateConflict
	}
	return &t, nil
}

// claimDueSQL 领取到期任务：先在 MATERIALIZED CTE 里 FOR UPDATE SKIP LOCKED 选出并锁住，再统一写租约。
// 用 CTE 而不是 IN (子查询)：后者在某些执行计划下会锁住超过 LIMIT 的行；SKIP LOCKED 保证并发领取者互不阻塞、不重复。
const claimDueSQL = `
WITH due AS MATERIALIZED (
	SELECT id FROM generation_tasks
	WHERE status IN ('pending','queued','running','finalizing')
	  AND next_poll_at <= @now
	  AND (lease_until IS NULL OR lease_until < @now)
	ORDER BY next_poll_at
	LIMIT @limit
	FOR UPDATE SKIP LOCKED
)
UPDATE generation_tasks t
SET lease_until = @lease, updated_at = @now
FROM due
WHERE t.id = due.id
RETURNING t.*`

// ClaimDue 领取最多 limit 个到期（next_poll_at <= now）且租约已过期（或无租约）的非终态任务，
// 并把租约写成 now+lease。返回的是写租约后的整行；两个并发调用不会领到同一个任务。
func (r *GenerationTaskRepository) ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]model.GenerationTask, error) {
	var tasks []model.GenerationTask
	err := r.db.WithContext(ctx).Raw(claimDueSQL,
		map[string]any{"now": now, "limit": limit, "lease": now.Add(lease)},
	).Scan(&tasks).Error
	return tasks, err
}

// ExtendLease 续租：仅对非终态任务把 lease_until 推到 until，返回是否续上（任务已终态则为 false）。
func (r *GenerationTaskRepository) ExtendLease(ctx context.Context, id uint64, until time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.GenerationTask{}).
		Where("id = ? AND status IN ?", id, activeStatuses()).
		Update("lease_until", until)
	return res.RowsAffected > 0, res.Error
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
	Balance       int   // user_credits.balance
	Frozen        int   // user_credits.frozen
	FreezeSum     int64 // 流水 freeze 合计
	SettleSum     int64 // 流水 settle 合计
	RefundSum     int64 // 流水 refund 合计
	ActiveCredits int64 // 非终态正式任务冻结的积分合计
}

// FrozenDiff 冻结额与流水推算值（freeze - settle - refund）的差，应为 0。
func (c CreditReconcile) FrozenDiff() int64 {
	return int64(c.Frozen) - (c.FreezeSum - c.SettleSum - c.RefundSum)
}

// ActiveDiff 冻结额与进行中任务积分合计的差，应为 0。
func (c CreditReconcile) ActiveDiff() int64 { return int64(c.Frozen) - c.ActiveCredits }

// BalanceDiff 余额与“初始积分 - 已结算合计”的差，应为 0。
func (c CreditReconcile) BalanceDiff(initial int) int64 {
	return int64(c.Balance) - (int64(initial) - c.SettleSum)
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
		  COALESCE(SUM(amount) FILTER (WHERE type = 'refund'), 0) AS refund_sum
		FROM credit_ledger WHERE user_id = ?`, userID).Row().Scan(&out.FreezeSum, &out.SettleSum, &out.RefundSum)
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
