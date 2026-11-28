package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// PruneHistory 清理过期的协作历史，保留活跃会话的恢复锚点、未派发事件和收据。
func (r *CanvasCollaborationRepository) PruneHistory(ctx context.Context, cutoff time.Time, limit int) error {
	limit = max(1, min(limit, 1000))
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(pruneCollaborationEvents, cutoff, cutoff, limit).Error; err != nil {
			return err
		}
		if err := tx.Exec(pruneCollaborationCheckpoints, cutoff, cutoff, limit).Error; err != nil {
			return err
		}
		return tx.Exec(pruneCollaborationReceipts, cutoff, limit).Error
	})
}

const pruneCollaborationEvents = `WITH expired AS (
	SELECT event.canvas_id, event.revision FROM canvas_collaboration_events event
	JOIN canvas_projects canvas ON canvas.id = event.canvas_id
	WHERE event.created_at < ? AND (canvas.deleted_at IS NOT NULL OR NOT canvas.collaboration_active
		OR event.epoch <> canvas.collaboration_epoch OR (event.published_at IS NOT NULL AND EXISTS (
			SELECT 1 FROM canvas_collaboration_checkpoints anchor WHERE anchor.canvas_id = event.canvas_id
			AND anchor.epoch = event.epoch AND anchor.revision > event.revision AND anchor.created_at < ?)))
	ORDER BY event.created_at, event.canvas_id, event.revision LIMIT ? FOR UPDATE OF event SKIP LOCKED
) DELETE FROM canvas_collaboration_events event USING expired
	WHERE event.canvas_id = expired.canvas_id AND event.revision = expired.revision`

const pruneCollaborationCheckpoints = `WITH expired AS (
	SELECT checkpoint.canvas_id, checkpoint.epoch, checkpoint.revision FROM canvas_collaboration_checkpoints checkpoint
	JOIN canvas_projects canvas ON canvas.id = checkpoint.canvas_id
	WHERE checkpoint.created_at < ? AND (canvas.deleted_at IS NOT NULL OR NOT canvas.collaboration_active
		OR checkpoint.epoch <> canvas.collaboration_epoch OR EXISTS (
			SELECT 1 FROM canvas_collaboration_checkpoints anchor WHERE anchor.canvas_id = checkpoint.canvas_id
			AND anchor.epoch = checkpoint.epoch AND anchor.revision > checkpoint.revision AND anchor.created_at < ?))
	ORDER BY checkpoint.created_at, checkpoint.canvas_id, checkpoint.revision LIMIT ? FOR UPDATE OF checkpoint SKIP LOCKED
) DELETE FROM canvas_collaboration_checkpoints checkpoint USING expired
	WHERE checkpoint.canvas_id = expired.canvas_id AND checkpoint.epoch = expired.epoch AND checkpoint.revision = expired.revision`

const pruneCollaborationReceipts = `WITH expired AS (
	SELECT receipt.canvas_id, receipt.actor_id, receipt.op_id FROM canvas_collaboration_receipts receipt
	JOIN canvas_projects canvas ON canvas.id = receipt.canvas_id
	WHERE receipt.created_at < ? AND (canvas.deleted_at IS NOT NULL OR NOT canvas.collaboration_active
		OR receipt.result->>'epoch' <> canvas.collaboration_epoch::text)
	ORDER BY receipt.created_at, receipt.canvas_id, receipt.actor_id, receipt.op_id LIMIT ? FOR UPDATE OF receipt SKIP LOCKED
) DELETE FROM canvas_collaboration_receipts receipt USING expired
	WHERE receipt.canvas_id = expired.canvas_id AND receipt.actor_id = expired.actor_id AND receipt.op_id = expired.op_id`
