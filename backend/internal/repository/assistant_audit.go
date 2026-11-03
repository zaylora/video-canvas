package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

type assistantAuditKey struct{}

// WithAssistantAudit 将助手审计交给现有写仓储，在同一事务内提交并回填 ID。
func WithAssistantAudit(ctx context.Context, audit *model.AIAuditLog) context.Context {
	return context.WithValue(ctx, assistantAuditKey{}, audit)
}

// HasAssistantAudit 表示调用方要求原子审计，现有事后审计不应重复写入。
func HasAssistantAudit(ctx context.Context) bool { return assistantAuditFrom(ctx) != nil }

func assistantAuditFrom(ctx context.Context) *model.AIAuditLog {
	audit, _ := ctx.Value(assistantAuditKey{}).(*model.AIAuditLog)
	return audit
}

// insertAssistantAudit 锁定审批和条目，已应用返回冲突；不会提交第二次业务变更。
func insertAssistantAudit(ctx context.Context, tx *gorm.DB, created map[string]string) error {
	audit := assistantAuditFrom(ctx)
	if audit == nil {
		return nil
	}
	var detail map[string]any
	if err := json.Unmarshal(audit.DetailJSON, &detail); err != nil {
		return err
	}
	key := fmt.Sprint("assistant:", detail["approval_id"], ":", detail["index"])
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", key).Error; err != nil {
		return err
	}
	var count int64
	if err := tx.Model(&model.AIAuditLog{}).Where("detail_json->>'via' = 'assistant' AND detail_json->>'approval_id' = ? AND detail_json->>'index' = ?", detail["approval_id"], fmt.Sprint(detail["index"])).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrRevisionConflict
	}
	detail["created"] = created
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	audit.DetailJSON = model.JSONText(raw)
	return tx.Create(audit).Error
}
