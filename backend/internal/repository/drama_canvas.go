package repository

import (
	"context"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// DramaCanvasTx 是拆解写画布事务内可用的原语，所有方法都绑定同一数据库事务。
type DramaCanvasTx interface {
	// JobForUpdate 先锁所属项目再锁用户的具体任务，供服务检查取消状态及步骤。
	JobForUpdate(ctx context.Context, userID, canvasID, jobID uint64) (*model.DramaJob, error)
	// MarkCanvasCommitted 把已提交标记合并进任务 output，保留核心写入的 result/cost。
	MarkCanvasCommitted(ctx context.Context, userID, jobID uint64, marker []byte) error
	// EpisodesForUpdate 按用户、短剧、画布和集号锁定项目及集，不存在返回 ErrNotFound。
	EpisodesForUpdate(ctx context.Context, userID, dramaID, canvasID uint64, nos []int) ([]model.DramaEpisode, error)
	// CheckCanvasRevision 锁定当前用户画布并检查 revision，即使没有图差异也不能提交旧快照的集锁。
	CheckCanvasRevision(ctx context.Context, userID, canvasID, revision uint64) error
	// UpdateCanvas 按用户、画布和 revision 写入，冲突返回 ErrRevisionConflict，不存在返回 ErrNotFound。
	UpdateCanvas(ctx context.Context, userID, canvasID, revision uint64, payload []byte) error
	// LockEpisodes 按用户、短剧、集号锁集并递增各集 revision。
	LockEpisodes(ctx context.Context, userID, dramaID uint64, nos []int) error
}

// DramaCanvasRepository 为公共画布写入口提供带用户条件的查询与原子事务。
type DramaCanvasRepository struct{ db *gorm.DB }

// NewDramaCanvasRepository 创建短剧画布仓储。
func NewDramaCanvasRepository(db *gorm.DB) *DramaCanvasRepository {
	return &DramaCanvasRepository{db: db}
}

// GetCanvas 按 user_id + id 读取画布，不存在或不是该用户的画布返回 ErrNotFound。
func (r *DramaCanvasRepository) GetCanvas(ctx context.Context, userID, canvasID uint64) (*model.CanvasProject, error) {
	return NewCanvasProjectRepository(r.db).GetByID(ctx, userID, canvasID)
}

// WithTx 原子提交锁集与画布，任何错误（包括 CAS 冲突）都回滚整次尝试。
func (r *DramaCanvasRepository) WithTx(ctx context.Context, fn func(DramaCanvasTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(&dramaCanvasTx{db: tx}) })
}

type dramaCanvasTx struct{ db *gorm.DB }

// EpisodesForUpdate 在已锁定项目和任务的事务中按用户、短剧和集号锁集。
func (t *dramaCanvasTx) EpisodesForUpdate(ctx context.Context, userID, dramaID, canvasID uint64, nos []int) ([]model.DramaEpisode, error) {
	var episodes []model.DramaEpisode
	err := t.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("drama_id = ? AND user_id = ? AND no IN ?", dramaID, userID, nos).Order("no ASC").Find(&episodes).Error
	return episodes, err
}

// UpdateCanvas 复用画布现有 user_id + revision 乐观锁，返回 ErrNotFound 或 ErrRevisionConflict。
func (t *dramaCanvasTx) UpdateCanvas(ctx context.Context, userID, canvasID, revision uint64, payload []byte) error {
	_, err := NewCanvasProjectRepository(t.db).Update(ctx, userID, canvasID, revision, map[string]any{"payload_json": datatypes.JSON(payload)})
	return err
}

// LockEpisodes 只更新该用户该短剧的指定集，locked_at 与 revision 同事务保存。
func (t *dramaCanvasTx) LockEpisodes(ctx context.Context, userID, dramaID uint64, nos []int) error {
	return t.db.WithContext(ctx).Model(&model.DramaEpisode{}).
		Where("drama_id = ? AND user_id = ? AND no IN ?", dramaID, userID, nos).
		Updates(map[string]any{"locked_at": time.Now(), "revision": gorm.Expr("revision + 1")}).Error
}

// CheckCanvasRevision 按 user_id + id 锁画布，版本改变返回 ErrRevisionConflict，不存在返回 ErrNotFound。
func (t *dramaCanvasTx) CheckCanvasRevision(ctx context.Context, userID, canvasID, revision uint64) error {
	var canvas model.CanvasProject
	if err := t.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", canvasID, userID).First(&canvas).Error; err != nil {
		return translate(err)
	}
	if canvas.Revision != revision {
		return ErrRevisionConflict
	}
	return nil
}

// JobForUpdate 按用户画布查所属未删除短剧，再按用户、短剧和任务 ID 加行锁，不存在返回 ErrNotFound。
func (t *dramaCanvasTx) JobForUpdate(ctx context.Context, userID, canvasID, jobID uint64) (*model.DramaJob, error) {
	var project model.DramaProject
	if err := t.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND canvas_project_id = ?", userID, canvasID).First(&project).Error; err != nil {
		return nil, translate(err)
	}
	var job model.DramaJob
	if err := t.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND user_id = ? AND drama_id = ?", jobID, userID, project.ID).First(&job).Error; err != nil {
		return nil, translate(err)
	}
	return &job, nil
}

// MarkCanvasCommitted 限定用户、任务和 running 状态，合并 JSONB 字段，不存在返回 ErrNotFound。
func (t *dramaCanvasTx) MarkCanvasCommitted(ctx context.Context, userID, jobID uint64, marker []byte) error {
	res := t.db.WithContext(ctx).Model(&model.DramaJob{}).Where("id = ? AND user_id = ? AND status = ?", jobID, userID, "running").
		Update("output", gorm.Expr("COALESCE(output, '{}'::jsonb) || ?::jsonb", string(marker)))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
