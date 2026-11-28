package repository

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"
)

// CanvasTaskSummaryRow 只包含当前协作成员有权读取的任务字段。
type CanvasTaskSummaryRow struct {
	ID        uint64
	NodeID    string
	Kind      string
	Status    string
	Username  string
	UpdatedAt time.Time
}

const canvasTaskSummariesSQL = `
SELECT task.id, task.node_id, task.kind, task.status, task.updated_at, COALESCE(u.username, '') AS username
FROM (
    SELECT id, node_id, 'storyboard-split' AS kind, status, user_id, updated_at
    FROM storyboard_split_jobs
    WHERE canvas_id = @canvas AND collaboration_epoch = @epoch
    UNION ALL
    SELECT id, substring(node_id FROM 7), 'character-voice', status, user_id, updated_at
    FROM generation_tasks
    WHERE canvas_project_id = @canvas AND collaboration_epoch = @epoch AND kind = 'audio' AND node_id LIKE 'voice:%' AND NOT is_test
) AS task
JOIN canvas_collaboration_records AS node ON node.canvas_id = @canvas AND node.kind = 'node' AND node.record_id = task.node_id AND NOT node.deleted
LEFT JOIN users u ON u.id = task.user_id AND u.deleted_at IS NULL
ORDER BY (task.status IN ('pending', 'queued', 'running', 'finalizing')) DESC, task.updated_at DESC, task.kind, task.id DESC
LIMIT @limit`

// TaskSummaries 读取当前协作代数内近期的拆分镜和角色音色摘要，正在进行的任务优先。
func (r *CanvasCollaborationRepository) TaskSummaries(ctx context.Context, actor, canvas, epoch uint64, limit int) ([]CanvasTaskSummaryRow, error) {
	rows := make([]CanvasTaskSummaryRow, 0)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := readCollaborationEditor(tx, actor, canvas, epoch); err != nil {
			return err
		}
		return tx.Raw(canvasTaskSummariesSQL, sql.Named("canvas", canvas), sql.Named("epoch", epoch), sql.Named("limit", limit)).Scan(&rows).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return rows, err
}
