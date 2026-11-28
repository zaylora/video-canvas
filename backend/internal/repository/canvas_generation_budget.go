package repository

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// ErrCanvasGenerationBudget is raised when the run cannot reserve more credits.
var ErrCanvasGenerationBudget = errors.New("累计积分上限不足")

func reserveCanvasGenerationBudget(tx *gorm.DB, request *model.CanvasGenerationBudgetRequest, canvas, user, epoch uint64, reserve int) (*uint64, error) {
	if request == nil {
		return nil, nil
	}
	_, scoped := model.CanvasAutomationFromContext(tx.Statement.Context)
	if strings.HasPrefix(request.Key, model.CanvasAutomationBudgetPrefix) != scoped {
		return nil, ErrCanvasCollaborationInvalidBatch
	}
	if epoch == 0 || canvas == 0 || (!scoped && !collaborationOpID.MatchString(request.Key)) || request.Limit == nil || *request.Limit < 0 || reserve < 0 {
		return nil, ErrCanvasCollaborationInvalidBatch
	}
	budget := model.CanvasGenerationBudget{Canvas: canvas, User: user, Epoch: epoch, Key: request.Key, Maximum: *request.Limit}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&budget)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("canvas = ? AND \"user\" = ? AND epoch = ? AND key = ?", canvas, user, epoch, request.Key).Take(&budget).Error; err != nil {
			return nil, err
		}
	}
	if budget.Maximum != *request.Limit {
		return nil, ErrCanvasCollaborationOpID
	}
	used, err := canvasBudgetUsage(tx, budget.ID)
	if err != nil {
		return nil, err
	}
	if reserve > budget.Maximum || used > int64(budget.Maximum-reserve) {
		return nil, ErrCanvasGenerationBudget
	}
	return &budget.ID, nil
}

func canvasBudgetUsage(tx *gorm.DB, id uint64) (int64, error) {
	const query = `SELECT COALESCE(SUM(used), 0)::bigint FROM (
		SELECT CASE WHEN t.status IN ('succeeded','failed','canceled','expired') THEN COALESCE(l.amount, 0) ELSE t.credits END AS used
		FROM generation_tasks t LEFT JOIN credit_ledger l ON l.task_id = t.id AND l.type = 'settle'
		WHERE t.budget_group_id = ?
		UNION ALL
		SELECT CASE WHEN status IN ('pending','running') THEN COALESCE(max_credits,0) ELSE credits END AS used
		FROM storyboard_split_jobs WHERE budget_group_id = ?
	) totals`
	var used int64
	err := tx.Raw(query, id, id).Scan(&used).Error
	return used, err
}
