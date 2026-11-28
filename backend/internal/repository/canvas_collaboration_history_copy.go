package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// CopyHistoricalVersion 在当前所有者的个人画布中恢复指定确认版本。
func (r *CanvasCollaborationRepository) CopyHistoricalVersion(ctx context.Context, owner, canvas, epoch, revision uint64, opID string, prepare func(*model.CanvasProject) (*model.CanvasProject, error)) (*model.CanvasProject, error) {
	if !collaborationOpID.MatchString(opID) || len(opID) > 55 {
		return nil, ErrCanvasCollaborationInvalidBatch
	}
	receipt := model.CanvasCollaborationReceipt{CanvasID: canvas, ActorID: owner, OpID: "history:" + opID, RequestHash: fmt.Sprintf("version/%d/%d", epoch, revision)}
	var result *model.CanvasProject
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var source model.CanvasProject
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ? AND drama_id IS NULL", canvas, owner).Take(&source).Error; err != nil {
			return translate(err)
		}
		if !source.CollaborationActive || epoch != source.CollaborationEpoch {
			return ErrCanvasCollaborationSession
		}
		previous, err := readHistoricalCopy(tx, receipt)
		if err != nil || previous != nil {
			result = previous
			return err
		}
		if err := loadHistoricalGraph(tx, &source, epoch, revision); err != nil {
			return err
		}
		result, err = prepare(&source)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.Returning{}).Create(result).Error; err != nil {
			return err
		}
		receipt.Result, err = json.Marshal(result.ID)
		if err != nil {
			return err
		}
		return tx.Create(&receipt).Error
	})
	return result, err
}

func readHistoricalCopy(tx *gorm.DB, request model.CanvasCollaborationReceipt) (*model.CanvasProject, error) {
	var receipt model.CanvasCollaborationReceipt
	err := tx.Where("canvas_id = ? AND actor_id = ? AND op_id = ?", request.CanvasID, request.ActorID, request.OpID).Take(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if receipt.RequestHash != request.RequestHash {
		return nil, ErrCanvasCollaborationOpID
	}
	var copiedID uint64
	if err := json.Unmarshal(receipt.Result, &copiedID); err != nil {
		return nil, err
	}
	var copied model.CanvasProject
	err = tx.Where("id = ? AND user_id = ?", copiedID, request.ActorID).Take(&copied).Error
	return &copied, translate(err)
}
