package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// CanvasComposeEnqueuer 把成片合成 job 放进当前业务事务（真实实现是 provider/queue 的 Runtime）。
type CanvasComposeEnqueuer interface {
	// EnqueueCanvasComposeTx 在 tx 所在事务里为任务入队。
	EnqueueCanvasComposeTx(ctx context.Context, tx *sql.Tx, jobID uint64) error
}

// CanvasComposeRepository 是「画布成片合成」用到的存储：画布归属与 CAS 写回、素材登记、异步任务。
// 形状照抄 StoryboardRepository，不引入时间线或序列这类新的画布概念。
type CanvasComposeRepository struct {
	db  *gorm.DB
	enq CanvasComposeEnqueuer
}

// NewCanvasComposeRepository 创建成片合成仓储。
func NewCanvasComposeRepository(db *gorm.DB) *CanvasComposeRepository {
	return &CanvasComposeRepository{db: db}
}

// SetEnqueuer 在依赖装配时绑定事务队列（队列运行时晚于仓储创建）。
func (r *CanvasComposeRepository) SetEnqueuer(e CanvasComposeEnqueuer) { r.enq = e }

// CanvasComposeTx 是合成任务在事务里要用的原语。
type CanvasComposeTx interface {
	// CreateComposeJob 插入一条 queued 任务，并在同一事务里入队。
	// 同画布同节点已有非终态任务时返回 ErrComposeJobBusy（靠部分唯一索引约束）。
	CreateComposeJob(ctx context.Context, job *model.CanvasComposeJob) error
	// UpdateComposeJob 原子更新任务字段。
	UpdateComposeJob(ctx context.Context, jobID uint64, patch map[string]any) error
	// ComposeJobForUpdate 加行锁读一条任务（worker 执行时用）。
	ComposeJobForUpdate(ctx context.Context, jobID uint64) (*model.CanvasComposeJob, error)
	// CreateAsset 登记成片素材行；与任务终态、画布写回在同一个事务里。
	CreateAsset(ctx context.Context, asset *model.Asset) error
	// UpdateCanvas 按 user_id + revision 写回画布，冲突返回 ErrRevisionConflict。
	UpdateCanvas(ctx context.Context, userID, canvasID, revision uint64, payload []byte) error
}

// ErrComposeJobBusy 表示同画布同节点已经有一条非终态的合成任务。
var ErrComposeJobBusy = errors.New("canvas compose job busy")

// WithTx 只通过此入口开启事务；事务内只用 tx 参数。
func (r *CanvasComposeRepository) WithTx(ctx context.Context, fn func(CanvasComposeTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&canvasComposeTx{db: tx, enq: r.enq})
	})
}

// canvasComposeTx 是 CanvasComposeTx 的实现；enq 让它能在同一事务里入队。
type canvasComposeTx struct {
	db  *gorm.DB
	enq CanvasComposeEnqueuer
}

// GetCanvas 实现 service.CanvasReader：不存在或不是该用户的画布返回 ErrNotFound。
func (r *CanvasComposeRepository) GetCanvas(ctx context.Context, userID, canvasID uint64) (*model.CanvasProject, error) {
	return NewCanvasProjectRepository(r.db).GetByID(ctx, userID, canvasID)
}

// CanvasOwned 确认画布存在且属于这个用户；不是返回 ErrNotFound。
func (r *CanvasComposeRepository) CanvasOwned(ctx context.Context, userID, canvasID uint64) error {
	_, err := r.GetCanvas(ctx, userID, canvasID)
	return err
}

// ComposeJobByID 在事务外读任务（断线补查）；不存在返回 ErrNotFound。
func (r *CanvasComposeRepository) ComposeJobByID(ctx context.Context, userID, jobID uint64) (*model.CanvasComposeJob, error) {
	var job model.CanvasComposeJob
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", jobID, userID).First(&job).Error; err != nil {
		return nil, translate(err)
	}
	return &job, nil
}

// ActiveComposeJob 查某个节点上未结束的任务（弹窗打开或刷新页面时恢复用）；不存在返回 ErrNotFound。
func (r *CanvasComposeRepository) ActiveComposeJob(ctx context.Context, userID, canvasID uint64, nodeID string) (*model.CanvasComposeJob, error) {
	var job model.CanvasComposeJob
	q := r.db.WithContext(ctx).
		Where("user_id = ? AND canvas_id = ? AND node_id = ? AND status IN ?", userID, canvasID, nodeID,
			[]string{model.ComposeJobQueued, model.ComposeJobRunning}).
		Order("id DESC").
		First(&job)
	if err := q.Error; err != nil {
		return nil, translate(err)
	}
	return &job, nil
}

// LastComposeJob 读某个节点最近一次的终态任务，用于判断成片是否过期；不存在返回 ErrNotFound。
func (r *CanvasComposeRepository) LastComposeJob(ctx context.Context, userID, canvasID uint64, nodeID string) (*model.CanvasComposeJob, error) {
	var job model.CanvasComposeJob
	q := r.db.WithContext(ctx).
		Where("user_id = ? AND canvas_id = ? AND node_id = ? AND status = ?", userID, canvasID, nodeID, model.ComposeJobSucceeded).
		Order("id DESC").
		First(&job)
	if err := q.Error; err != nil {
		return nil, translate(err)
	}
	return &job, nil
}

// CreateComposeJob 插入任务行（必须在事务里，之后入队同一事务）。
func (t *canvasComposeTx) CreateComposeJob(ctx context.Context, job *model.CanvasComposeJob) error {
	now := time.Now()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	if err := t.db.WithContext(ctx).Create(job).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrComposeJobBusy
		}
		return err
	}
	if t.enq == nil {
		return errors.New("合成队列未绑定")
	}
	sqlTx, ok := t.db.Statement.ConnPool.(*sql.Tx)
	if !ok {
		return errors.New("CreateComposeJob 必须在事务里调用")
	}
	return t.enq.EnqueueCanvasComposeTx(ctx, sqlTx, job.ID)
}

// UpdateComposeJob 更新任务字段。
func (t *canvasComposeTx) UpdateComposeJob(ctx context.Context, jobID uint64, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	patch["updated_at"] = time.Now()
	return t.db.WithContext(ctx).Model(&model.CanvasComposeJob{}).Where("id = ?", jobID).Updates(patch).Error
}

// ComposeJobForUpdate 事务里加行锁读任务。
func (t *canvasComposeTx) ComposeJobForUpdate(ctx context.Context, jobID uint64) (*model.CanvasComposeJob, error) {
	var job model.CanvasComposeJob
	if err := forUpdate(t.db.WithContext(ctx), true).Where("id = ?", jobID).First(&job).Error; err != nil {
		return nil, translate(err)
	}
	return &job, nil
}

// CreateAsset 登记成片素材行。
func (t *canvasComposeTx) CreateAsset(ctx context.Context, asset *model.Asset) error {
	return t.db.WithContext(ctx).Create(asset).Error
}

// UpdateCanvas 复用画布现有 user_id + revision 乐观锁，冲突返回 ErrRevisionConflict。
func (t *canvasComposeTx) UpdateCanvas(ctx context.Context, userID, canvasID, revision uint64, payload []byte) error {
	_, err := NewCanvasProjectRepository(t.db).Update(ctx, userID, canvasID, revision, map[string]any{"payload_json": datatypes.JSON(payload)})
	return err
}
