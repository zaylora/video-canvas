package repository

import (
	"context"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// PendingNotices 读取仍可重放且尚未确认派发的协作版本。
func (r *CanvasCollaborationRepository) PendingNotices(ctx context.Context, limit int) ([]model.CanvasCollaborationEvent, error) {
	var events []model.CanvasCollaborationEvent
	err := r.db.WithContext(ctx).Model(&model.CanvasCollaborationEvent{}).
		Select("canvas_id", "revision", "epoch", "created_at").Where("published_at IS NULL").
		Where(`EXISTS (SELECT 1 FROM canvas_projects p WHERE p.id = canvas_collaboration_events.canvas_id
		AND p.deleted_at IS NULL AND p.collaboration_active AND p.collaboration_epoch = canvas_collaboration_events.epoch)`).
		Order("created_at, canvas_id, revision").Limit(min(max(limit, 1), 100)).Find(&events).Error
	return events, translate(err)
}

// AcknowledgeNotice 在相同代数和版本上确认派发，同时保留内容以供对账。
func (r *CanvasCollaborationRepository) AcknowledgeNotice(ctx context.Context, canvas, epoch, revision uint64) error {
	return translate(r.db.WithContext(ctx).Model(&model.CanvasCollaborationEvent{}).
		Where("canvas_id = ? AND epoch = ? AND revision = ? AND published_at IS NULL", canvas, epoch, revision).
		UpdateColumn("published_at", gorm.Expr("NOW()")).Error)
}
