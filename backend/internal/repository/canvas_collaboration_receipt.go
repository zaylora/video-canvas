package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// CanvasCollaborationReceiptStatus 是操作结果摘要，仅包含对账需要的信息。
type CanvasCollaborationReceiptStatus struct {
	Epoch    uint64 `json:"epoch"`
	Revision uint64 `json:"revision,omitempty"`
	OpID     string `json:"opId"`
	Result   string `json:"result"`
}

// ReceiptStatus 核对当前编辑权限与会话代数，查询调用者自己的操作结果。
// 画布不可访问时返回 ErrNotFound，会话已变更时返回 ErrCanvasCollaborationSession。
func (r *CanvasCollaborationRepository) ReceiptStatus(ctx context.Context, actorID, canvasID, epoch uint64, opID string) (*CanvasCollaborationReceiptStatus, error) {
	if !collaborationOpID.MatchString(opID) || epoch == 0 {
		return nil, ErrCanvasCollaborationInvalidBatch
	}
	result := &CanvasCollaborationReceiptStatus{Epoch: epoch, OpID: opID, Result: "missing"}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var project model.CanvasProject
		if err := tx.Select("id", "user_id", "collaboration_epoch", "collaboration_active").
			Where("id = ? AND drama_id IS NULL", canvasID).Take(&project).Error; err != nil {
			return translate(err)
		}
		role, err := collaborationRole(tx, &project, actorID)
		if err != nil {
			return err
		}
		if role != model.CanvasRoleOwner && role != model.CanvasRoleEditor {
			return ErrNotFound
		}
		if !project.CollaborationActive || project.CollaborationEpoch != epoch {
			return ErrCanvasCollaborationSession
		}
		if err := checkCanvasAutomation(tx, &project, actorID, role, false); err != nil {
			return err
		}
		return loadCollaborationReceiptStatus(tx, actorID, canvasID, result)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

func loadCollaborationReceiptStatus(tx *gorm.DB, actorID, canvasID uint64, status *CanvasCollaborationReceiptStatus) error {
	var saved model.CanvasCollaborationReceipt
	err := tx.Select("result").Where("canvas_id = ? AND actor_id = ? AND op_id = ?", canvasID, actorID, status.OpID).Take(&saved).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var receipt CanvasCollaborationCommit
	if err := json.Unmarshal(saved.Result, &receipt); err != nil {
		return err
	}
	if receipt.Epoch != status.Epoch {
		return ErrCanvasCollaborationSession
	}
	status.Revision = receipt.Revision
	status.Result = "applied"
	if len(receipt.Conflicts) > 0 {
		status.Result = "conflict"
	}
	return nil
}
