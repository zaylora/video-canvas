package service

import (
	"context"
	"time"

	"video-canvas/internal/pkg/idcodec"
)

// CanvasTaskSummary 是拆分镜和角色音色的协作公开投影。
type CanvasTaskSummary struct {
	ID        string    `json:"id"`
	NodeID    string    `json:"nodeId"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Username  string    `json:"username,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TaskSummaries 返回编辑成员共同可见的任务状态，详情和账单由原任务接口按发起人授权提供。
func (s *CanvasCollaborationService) TaskSummaries(ctx context.Context, actor, canvas, epoch uint64, limit int) ([]CanvasTaskSummary, error) {
	rows, err := s.repo.TaskSummaries(ctx, actor, canvas, epoch, max(1, min(100, limit)))
	if err != nil {
		return nil, collaborationError(err)
	}
	items := make([]CanvasTaskSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, CanvasTaskSummary{ID: idcodec.Encode(row.ID), NodeID: row.NodeID, Kind: row.Kind,
			Status: row.Status, Username: row.Username, UpdatedAt: row.UpdatedAt})
	}
	return items, nil
}
