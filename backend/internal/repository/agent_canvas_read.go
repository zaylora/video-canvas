package repository

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// RecordCanvasReads 按用户、画布、会话、运行记录已返回详情的节点，重复记录忽略；数据库错误原样返回。
func (r *AgentRepository) RecordCanvasReads(ctx context.Context, rows []model.AgentCanvasRead) error {
	return insertCanvasReads(r.db.WithContext(ctx), rows)
}

// ListCanvasReads 按用户、画布、会话、运行查询已读节点 id；没有记录返回空列表，数据库错误原样返回。
func (r *AgentRepository) ListCanvasReads(ctx context.Context, run *model.AgentRun) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.AgentCanvasRead{}).
		Where("user_id = ? AND canvas_id = ? AND session_id = ? AND run_id = ?", run.UserID, run.CanvasID, run.SessionID, run.ID).
		Pluck("node_id", &ids).Error
	return ids, err
}

// insertCanvasReads 支持独立读取和画布写入事务共用的幂等插入；空列表不访问数据库。
func insertCanvasReads(db *gorm.DB, rows []model.AgentCanvasRead) error {
	if len(rows) == 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}
