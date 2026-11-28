package repository

import (
	"context"
	"strconv"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// CreateAutomationKey records a secret hash after checking the issuing member and epoch.
func (r *CanvasCollaborationRepository) CreateAutomationKey(ctx context.Context, key *model.CanvasAutomationKey) error {
	return r.WithTx(ctx, func(tx *gorm.DB) error {
		project, _, err := lockEditableCollaboration(tx, key.CanvasID, key.UserID, key.Epoch)
		if err != nil {
			return err
		}
		if key.Scope == model.CanvasAutomationGenerate {
			if err := requireAutomationGenerationPermission(tx, project, key.UserID); err != nil {
				return err
			}
		}
		var count int64
		if err := activeAutomationKeys(tx.Model(key)).Where("canvas_id = ? AND user_id = ?", key.CanvasID, key.UserID).Count(&count).Error; err != nil {
			return err
		}
		if count >= 16 {
			return ErrCanvasCollaborationInvalidBatch
		}
		return tx.Create(key).Error
	})
}

// AutomationKeys lists the caller's credentials for a canvas without returning secrets.
func (r *CanvasCollaborationRepository) AutomationKeys(ctx context.Context, actor, canvas uint64) ([]model.CanvasAutomationKey, error) {
	keys := make([]model.CanvasAutomationKey, 0)
	err := r.db.WithContext(ctx).Omit("token_hash").Where("user_id = ? AND canvas_id = ?", actor, canvas).Order("id DESC").Limit(100).Find(&keys).Error
	return keys, err
}

// RevokeAutomationKey serializes credential revocation with in-flight edits.
func (r *CanvasCollaborationRepository) RevokeAutomationKey(ctx context.Context, actor, canvas, id uint64) error {
	return r.WithTx(ctx, func(tx *gorm.DB) error {
		if _, err := lockCollaborationProject(tx, canvas); err != nil {
			return err
		}
		return tx.Model(&model.CanvasAutomationKey{}).Where("id = ? AND user_id = ? AND canvas_id = ? AND revoked_at IS NULL", id, actor, canvas).
			Update("revoked_at", time.Now()).Error
	})
}

// AutomationKey authenticates the hashed bearer token on its bound canvas.
func (r *CanvasCollaborationRepository) AutomationKey(ctx context.Context, hash string, canvas uint64) (model.CanvasAutomationKey, error) {
	var key model.CanvasAutomationKey
	err := activeAutomationKeys(r.db.WithContext(ctx)).Omit("token_hash").Where("token_hash = ? AND canvas_id = ?", hash, canvas).Take(&key).Error
	return key, translate(err)
}

func activeAutomationKeys(query *gorm.DB) *gorm.DB {
	return query.Where("revoked_at IS NULL AND expires_at > clock_timestamp()")
}

func automationForbiddenChange(change CanvasCollaborationChange, key model.CanvasAutomationKey) bool {
	switch change.Action {
	case "", "update":
		return (key.Scope != model.CanvasAutomationGenerate && change.Fields[collaborationGenerationInputField] != nil) ||
			change.Fields[collaborationStoryboardField] != nil || change.Fields[collaborationSceneField] != nil
	case "create":
		return false
	case collaborationShotAction:
		edit, valid := readCollaborationShotEdit(change.Value)
		return !valid || edit.Action == "rm"
	case collaborationDirectorAction:
		edit, valid := readCollaborationDirectorEdit(change.Value)
		return !valid || edit.Action == "rm"
	default:
		return true
	}
}

func checkCanvasAutomation(tx *gorm.DB, project *model.CanvasProject, actor uint64, role string, edit bool) error {
	key, scoped := model.CanvasAutomationFromContext(tx.Statement.Context)
	if !scoped {
		return nil
	}
	if key.CanvasID != project.ID || key.UserID != actor || key.Epoch != project.CollaborationEpoch ||
		(role != model.CanvasRoleEditor && role != model.CanvasRoleOwner) || (edit && !key.CanEdit()) {
		return ErrNotFound
	}
	var saved model.CanvasAutomationKey
	return translate(activeAutomationKeys(tx).Select("id").Where("id = ? AND EXISTS (SELECT 1 FROM users WHERE id = ? AND status = ? AND token_version = ? AND deleted_at IS NULL)",
		key.ID, actor, model.UserStatusActive, key.TokenVersion).Take(&saved).Error)
}

func requireAutomationGenerationPermission(tx *gorm.DB, project *model.CanvasProject, actor uint64) error {
	allowed, err := canGenerateInCollaboration(tx, project, actor)
	if err == nil && !allowed {
		return ErrNotFound
	}
	return err
}

// AuthorizeAutomationGeneration rechecks the paid key and member permissions under the canvas lock.
func (r *CanvasCollaborationRepository) AuthorizeAutomationGeneration(ctx context.Context, key model.CanvasAutomationKey) error {
	return r.WithTx(ctx, func(tx *gorm.DB) error {
		if key.Scope != model.CanvasAutomationGenerate || key.BudgetCredits == nil {
			return ErrNotFound
		}
		project, _, err := lockEditableCollaboration(tx, key.CanvasID, key.UserID, key.Epoch)
		if err != nil {
			return err
		}
		return requireAutomationGenerationPermission(tx, project, key.UserID)
	})
}

func automationTaskBudget(tx *gorm.DB, key model.CanvasAutomationKey, task *model.GenerationTask) (*model.CanvasGenerationBudgetRequest, error) {
	if key.Scope != model.CanvasAutomationGenerate || task.IsTest || task.CanvasProjectID == nil ||
		key.CanvasID != *task.CanvasProjectID || key.UserID != task.UserID || key.Epoch != task.CollaborationEpoch {
		return nil, ErrNotFound
	}
	var saved model.CanvasAutomationKey
	err := activeAutomationKeys(tx).Select("budget_credits").Where("id = ? AND canvas_id = ? AND user_id = ? AND epoch = ? AND scope = ? AND EXISTS (SELECT 1 FROM users WHERE id = ? AND status = ? AND token_version = ? AND deleted_at IS NULL)",
		key.ID, key.CanvasID, key.UserID, key.Epoch, model.CanvasAutomationGenerate, key.UserID, model.UserStatusActive, key.TokenVersion).Take(&saved).Error
	if err != nil {
		return nil, translate(err)
	}
	if saved.BudgetCredits == nil || *saved.BudgetCredits <= 0 {
		return nil, ErrNotFound
	}
	return &model.CanvasGenerationBudgetRequest{Key: model.CanvasAutomationBudgetPrefix + strconv.FormatUint(key.ID, 10), Limit: saved.BudgetCredits}, nil
}
