package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// StoryboardSplitEnqueuer 把拆分镜 job 放进当前业务事务（真实实现是 provider/queue 的 Runtime）。
type StoryboardSplitEnqueuer interface {
	// EnqueueStoryboardSplitTx 在 tx 所在事务里为任务入队。
	EnqueueStoryboardSplitTx(ctx context.Context, tx *sql.Tx, jobID uint64) error
}

// StoryboardRepository 是画布分镜表「从剧本拆分镜」用到的存储：确认画布归属、积分原语，以及异步任务。
type StoryboardRepository struct {
	db  *gorm.DB
	enq StoryboardSplitEnqueuer
}

// NewStoryboardRepository 创建分镜表仓储。
func NewStoryboardRepository(db *gorm.DB) *StoryboardRepository { return &StoryboardRepository{db: db} }

// SetEnqueuer 在依赖装配时绑定事务队列（队列运行时晚于仓储创建）。
func (r *StoryboardRepository) SetEnqueuer(e StoryboardSplitEnqueuer) { r.enq = e }

// StoryboardTx 是拆分镜在事务里要用的原语。
type StoryboardTx interface {
	EnsureCredit(ctx context.Context, userID uint64, initial int) error
	LockCredit(ctx context.Context, userID uint64) (*model.UserCredit, error)
	AddCredit(ctx context.Context, userID uint64, balance, frozen int) error
	InsertLedger(ctx context.Context, e *model.CreditLedger) (bool, error)
	// CreateSplitJob 插入一条 pending 任务，并在同一事务里入队。
	// 同画布同节点已有非终态任务时返回 ErrSplitJobBusy（靠部分唯一索引约束）。
	CreateSplitJob(ctx context.Context, job *model.StoryboardSplitJob) error
	// UpdateSplitJob 原子更新任务的终态及其产物（成功的 shots、credits、usage，失败的 error）。
	UpdateSplitJob(ctx context.Context, jobID uint64, patch map[string]any) error
	// SplitJobForUpdate 加行锁读一条任务（worker 执行时用）。
	SplitJobForUpdate(ctx context.Context, jobID uint64) (*model.StoryboardSplitJob, error)
}

// ErrSplitJobBusy 表示同画布同节点已经有一条非终态的拆分镜任务。
var ErrSplitJobBusy = errors.New("storyboard split job busy")

// WithTx 只通过此入口开启事务；事务内只用 tx 参数。
func (r *StoryboardRepository) WithTx(ctx context.Context, fn func(StoryboardTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(&StoryboardRepository{db: tx, enq: r.enq}) })
}

// CanvasOwned 确认画布存在且属于这个用户；不是返回 ErrNotFound。
func (r *StoryboardRepository) CanvasOwned(ctx context.Context, userID, canvasID uint64) error {
	_, err := (&CanvasProjectRepository{db: r.db}).GetByID(ctx, userID, canvasID)
	return err
}

// EnsureCredit 没有账户时按新账户初始积分建立，复用已有的初始化和流水逻辑。
func (r *StoryboardRepository) EnsureCredit(ctx context.Context, userID uint64, initial int) error {
	return insertInitialCredit(ctx, r.db, userID, initial)
}

// LockCredit 读取积分账户并加行锁。
func (r *StoryboardRepository) LockCredit(ctx context.Context, userID uint64) (*model.UserCredit, error) {
	return (&GenerationTaskRepository{db: r.db}).LockCredit(ctx, userID)
}

// AddCredit 原子更新余额和冻结额。
func (r *StoryboardRepository) AddCredit(ctx context.Context, userID uint64, balance, frozen int) error {
	return (&GenerationTaskRepository{db: r.db}).AddCredit(ctx, userID, balance, frozen)
}

// InsertLedger 写一条积分流水。
func (r *StoryboardRepository) InsertLedger(ctx context.Context, e *model.CreditLedger) (bool, error) {
	return (&GenerationTaskRepository{db: r.db}).InsertLedger(ctx, e)
}

// SplitJobByID 在事务外读任务（断线补查）；不存在返回 ErrNotFound。
func (r *StoryboardRepository) SplitJobByID(ctx context.Context, userID, jobID uint64) (*model.StoryboardSplitJob, error) {
	var job model.StoryboardSplitJob
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", jobID, userID).First(&job).Error; err != nil {
		return nil, translate(err)
	}
	return &job, nil
}

// SplitJobByRequest 查询发起人自己的拆分镜请求，尚未受理返回 ErrNotFound。
func (r *StoryboardRepository) SplitJobByRequest(ctx context.Context, userID uint64, key string) (*model.StoryboardSplitJob, error) {
	var job model.StoryboardSplitJob
	if err := r.db.WithContext(ctx).Where("user_id = ? AND request_key = ?", userID, key).Take(&job).Error; err != nil {
		return nil, translate(err)
	}
	return &job, nil
}

// ActiveSplitJob 查某个节点上未结束的任务（弹窗刚打开时恢复用）；不存在返回 ErrNotFound。
func (r *StoryboardRepository) ActiveSplitJob(ctx context.Context, userID, canvasID uint64, nodeID string) (*model.StoryboardSplitJob, error) {
	var job model.StoryboardSplitJob
	q := r.db.WithContext(ctx).
		Where("user_id = ? AND canvas_id = ? AND node_id = ? AND status IN ?", userID, canvasID, nodeID, []string{model.SplitJobPending, model.SplitJobRunning}).
		Order("id DESC").
		First(&job)
	if err := q.Error; err != nil {
		return nil, translate(err)
	}
	return &job, nil
}

// CreateSplitJob 实现 StoryboardTx：插入任务行（必须在事务里，之后入队同一事务）。
func (r *StoryboardRepository) CreateSplitJob(ctx context.Context, job *model.StoryboardSplitJob) error {
	now := time.Now()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	db := r.db.WithContext(ctx)
	project, err := guardSplitCanvas(db, job)
	if err != nil {
		return err
	}
	if job.BudgetRequest != nil {
		if job.MaxCredits == nil {
			return ErrCanvasCollaborationInvalidBatch
		}
		job.BudgetGroupID, err = reserveCanvasGenerationBudget(db, job.BudgetRequest, job.CanvasID, job.UserID, job.CollaborationEpoch, *job.MaxCredits)
		if err != nil {
			return err
		}
	}
	if err := db.Create(job).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrSplitJobBusy
		}
		return err
	}
	if project != nil {
		if err := bindCollaborationSplit(db, project, job); err != nil {
			return err
		}
	}
	if r.enq == nil {
		return errors.New("拆分镜队列未绑定")
	}
	tx, ok := r.db.Statement.ConnPool.(*sql.Tx)
	if !ok {
		return errors.New("CreateSplitJob 必须在事务里调用")
	}
	return r.enq.EnqueueStoryboardSplitTx(ctx, tx, job.ID)
}

// UpdateSplitJob 更新任务字段。
func (r *StoryboardRepository) UpdateSplitJob(ctx context.Context, jobID uint64, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	patch["updated_at"] = time.Now()
	return r.db.WithContext(ctx).Model(&model.StoryboardSplitJob{}).Where("id = ?", jobID).Updates(patch).Error
}

// SplitJobForUpdate 事务里加行锁读任务。
func (r *StoryboardRepository) SplitJobForUpdate(ctx context.Context, jobID uint64) (*model.StoryboardSplitJob, error) {
	var job model.StoryboardSplitJob
	if err := forUpdate(r.db.WithContext(ctx), true).Where("id = ?", jobID).First(&job).Error; err != nil {
		return nil, translate(err)
	}
	return &job, nil
}
