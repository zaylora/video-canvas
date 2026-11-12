package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// StepTx 是环节协议层（绑定、试跑）事务里用到的数据库原语。
type StepTx interface {
	// LockStep 取环节级事务锁（pg advisory xact lock），同一环节的发起试跑、设为生效串行执行；事务结束自动释放。
	LockStep(ctx context.Context, stepKey string) error
	// ActiveTrials 统计环节在 since 之后创建、仍是 queued / running 的试跑数；更早的视为卡死，不算进行中。
	ActiveTrials(ctx context.Context, stepKey string, since time.Time) (int64, error)
	// InsertTrial 插入试跑，回填主键。
	InsertTrial(ctx context.Context, t *model.StepTrial) error
	// UpdateTrial 按 id 更新试跑的可变列，不存在返回 ErrNotFound。
	UpdateTrial(ctx context.Context, id uint64, fields map[string]any) error
	// Trial 按 id 读取试跑，lock 为真时加行锁；不存在返回 ErrNotFound。
	Trial(ctx context.Context, id uint64, lock bool) (*model.StepTrial, error)
	// GetBinding 读取环节绑定（带技能名），没有记录返回 ErrNotFound（等于内置）。
	GetBinding(ctx context.Context, stepKey string) (*StepBindingRow, error)
	// EnqueueTrial 在当前业务事务里为试跑入队 River job，回滚时 job 也不存在。
	EnqueueTrial(ctx context.Context, trialID uint64) error
	// SaveBinding 写入环节绑定（不存在则插入，存在则整行更新）。
	SaveBinding(ctx context.Context, b *model.StepBinding) error
	// InsertHistory 追加一条绑定历史。
	InsertHistory(ctx context.Context, h *model.StepBindingHistory) error
	// EnsureCredit 惰性建立积分账户。
	EnsureCredit(ctx context.Context, userID uint64, initial int) error
	// LockCredit 锁定积分账户，避免并发超扣。
	LockCredit(ctx context.Context, userID uint64) (*model.UserCredit, error)
	// AddCredit 增减余额和冻结额。
	AddCredit(ctx context.Context, userID uint64, balance, frozen int) error
	// InsertLedger 幂等插入积分流水，已存在返回 inserted=false。
	InsertLedger(ctx context.Context, e *model.CreditLedger) (bool, error)
}

// StepEnqueuer 把试跑放进当前业务事务（真实实现是 provider/queue 的 Runtime）。
type StepEnqueuer interface {
	// EnqueueStepTrialTx 在 tx 所在事务里为试跑入队。
	EnqueueStepTrialTx(ctx context.Context, tx *sql.Tx, trialID uint64) error
}

// StepBindingRow 是绑定加上技能名：技能被整个删除时 SkillName 为空，调用方据此退回内置。
type StepBindingRow struct {
	model.StepBinding
	SkillName string
}

// StepHistoryRow 是绑定历史加上操作人用户名。
type StepHistoryRow struct {
	model.StepBindingHistory
	ActorName string
}

// StepRepository 是环节绑定、绑定历史和试跑的数据访问。
type StepRepository struct {
	db  *gorm.DB
	enq StepEnqueuer
}

// NewStepRepository 创建环节仓储。
func NewStepRepository(db *gorm.DB) *StepRepository { return &StepRepository{db: db} }

// SetEnqueuer 在依赖装配时绑定事务队列（队列运行时晚于仓储创建）。
func (r *StepRepository) SetEnqueuer(e StepEnqueuer) { r.enq = e }

// WithTx 只通过此入口开启环节事务；事务内只用 tx 参数。
func (r *StepRepository) WithTx(ctx context.Context, fn func(StepTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(&StepRepository{db: tx, enq: r.enq}) })
}

// bindingQuery 是读绑定的查询：左连技能表取技能名，技能被删时名字为空。
func (r *StepRepository) bindingQuery(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Table("step_bindings AS b").
		Select("b.*, s.name AS skill_name").
		Joins("LEFT JOIN agent_skills s ON s.id = b.skill_id")
}

// GetBinding 读取环节绑定；没有记录返回 ErrNotFound（等于内置）。
func (r *StepRepository) GetBinding(ctx context.Context, stepKey string) (*StepBindingRow, error) {
	var row StepBindingRow
	res := r.bindingQuery(ctx).Where("b.step_key = ?", stepKey).Limit(1).Scan(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &row, nil
}

// ListBindings 返回全部环节的绑定，环节列表页用。
func (r *StepRepository) ListBindings(ctx context.Context) ([]StepBindingRow, error) {
	var rows []StepBindingRow
	if err := r.bindingQuery(ctx).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Trial 读取试跑，不存在返回 ErrNotFound。
func (r *StepRepository) Trial(ctx context.Context, id uint64, lock bool) (*model.StepTrial, error) {
	q := r.db.WithContext(ctx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var t model.StepTrial
	if err := q.Where("id = ?", id).Take(&t).Error; err != nil {
		return nil, translate(err)
	}
	return &t, nil
}

// LastTrials 返回每个环节最近一次已结束（succeeded / failed）的试跑，按 step_key 索引。
func (r *StepRepository) LastTrials(ctx context.Context) (map[string]model.StepTrial, error) {
	var rows []model.StepTrial
	err := r.db.WithContext(ctx).Raw(
		`SELECT DISTINCT ON (step_key) * FROM step_trials WHERE status IN (?, ?) ORDER BY step_key, created_at DESC, id DESC`,
		model.StepTrialSucceeded, model.StepTrialFailed).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]model.StepTrial, len(rows))
	for _, t := range rows {
		out[t.StepKey] = t
	}
	return out, nil
}

// History 返回环节的绑定历史，新的在前，最多 limit 条，带操作人用户名（用户被删时为空）。
func (r *StepRepository) History(ctx context.Context, stepKey string, limit int) ([]StepHistoryRow, error) {
	var rows []StepHistoryRow
	err := r.db.WithContext(ctx).Table("step_binding_history AS h").
		Select("h.*, u.username AS actor_name").
		Joins("LEFT JOIN users u ON u.id = h.actor_id").
		Where("h.step_key = ?", stepKey).Order("h.created_at DESC, h.id DESC").Limit(limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// StartTrial 把 queued 的试跑置为 running 并返回最新行；已经结束（succeeded / failed）返回 ErrStateConflict。
// running 允许再次进入：job 被救回重试时接着跑。
func (r *StepRepository) StartTrial(ctx context.Context, id uint64) (*model.StepTrial, error) {
	res := r.db.WithContext(ctx).Model(&model.StepTrial{}).
		Where("id = ? AND status IN (?, ?)", id, model.StepTrialQueued, model.StepTrialRunning).
		Update("status", model.StepTrialRunning)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		if _, err := r.Trial(ctx, id, false); err != nil {
			return nil, err
		}
		return nil, ErrStateConflict
	}
	return r.Trial(ctx, id, false)
}

// LockStep 见 StepTx。
func (r *StepRepository) LockStep(ctx context.Context, stepKey string) error {
	return r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "step:"+stepKey).Error
}

// ActiveTrials 见 StepTx。
func (r *StepRepository) ActiveTrials(ctx context.Context, stepKey string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.StepTrial{}).
		Where("step_key = ? AND status IN (?, ?) AND created_at > ?", stepKey, model.StepTrialQueued, model.StepTrialRunning, since).
		Count(&n).Error
	return n, err
}

// InsertTrial 见 StepTx。
func (r *StepRepository) InsertTrial(ctx context.Context, t *model.StepTrial) error {
	return r.db.WithContext(ctx).Create(t).Error
}

// UpdateTrial 见 StepTx。
func (r *StepRepository) UpdateTrial(ctx context.Context, id uint64, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.StepTrial{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// EnqueueTrial 见 StepTx。必须在业务事务里调用：入队用同一个 sql.Tx，才能和试跑行一起提交或回滚。
func (r *StepRepository) EnqueueTrial(ctx context.Context, trialID uint64) error {
	if r.enq == nil {
		return errors.New("环节试跑队列未配置")
	}
	tx, ok := r.db.Statement.ConnPool.(*sql.Tx)
	if !ok {
		return errors.New("环节试跑入队必须处于业务事务")
	}
	return r.enq.EnqueueStepTrialTx(ctx, tx, trialID)
}

// SaveBinding 见 StepTx。
func (r *StepRepository) SaveBinding(ctx context.Context, b *model.StepBinding) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "step_key"}},
		UpdateAll: true,
	}).Create(b).Error
}

// InsertHistory 见 StepTx。
func (r *StepRepository) InsertHistory(ctx context.Context, h *model.StepBindingHistory) error {
	return r.db.WithContext(ctx).Create(h).Error
}

// EnsureCredit 复用已有账户初始化及流水逻辑。
func (r *StepRepository) EnsureCredit(ctx context.Context, userID uint64, initial int) error {
	return insertInitialCredit(ctx, r.db, userID, initial)
}

// LockCredit 读取并加行锁保护账户。
func (r *StepRepository) LockCredit(ctx context.Context, userID uint64) (*model.UserCredit, error) {
	return (&GenerationTaskRepository{db: r.db}).LockCredit(ctx, userID)
}

// AddCredit 通过已有积分原语原子更新。
func (r *StepRepository) AddCredit(ctx context.Context, userID uint64, balance, frozen int) error {
	return (&GenerationTaskRepository{db: r.db}).AddCredit(ctx, userID, balance, frozen)
}

// InsertLedger 复用幂等积分流水写入。
func (r *StepRepository) InsertLedger(ctx context.Context, e *model.CreditLedger) (bool, error) {
	return (&GenerationTaskRepository{db: r.db}).InsertLedger(ctx, e)
}
