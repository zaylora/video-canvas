package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// ClaimAdminAttempt 按 run + kind + target 串行分配 1–3；同 token 幂等重放，满额返回 0。
func (r *AdminAssistantRepository) ClaimAdminAttempt(ctx context.Context, runID uint64, kind, target, token string) (int, error) {
	attempt := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		key := fmt.Sprintf("admin-attempt:%d:%s:%s", runID, kind, target)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
			return err
		}
		var rows []model.AgentAdminAttempt
		if err := tx.Where("run_id = ? AND kind = ? AND target = ?", runID, kind, target).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if row.Token == token {
				attempt = row.Attempt
				return nil
			}
		}
		if len(rows) >= 3 {
			return nil
		}
		attempt = len(rows) + 1
		return tx.Create(&model.AgentAdminAttempt{RunID: runID, Kind: kind, Target: target, Token: token, Attempt: attempt}).Error
	})
	return attempt, err
}
