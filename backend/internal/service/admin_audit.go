package service

import (
	"context"
	"encoding/json"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
)

// AdminAuditWriter 是后台运营审计日志的读写依赖，由 repository.AdminAuditRepository 实现。
type AdminAuditWriter interface {
	// Insert 写一条审计日志（只增不改不删）。
	Insert(ctx context.Context, l *model.AdminAuditLog) error
	// RecentForUser 返回针对某用户的最近 limit 条审计，按时间倒序，带操作人用户名。
	RecentForUser(ctx context.Context, userID uint64, limit int) ([]model.AdminAuditView, error)
}

// adminAudit 写一条审计。detail 由调用方保证不含密码、密钥。
// 写失败不回滚已提交的业务变更（变更已生效，回滚不了），但留 Error 日志让运维补记：
// 为了一条审计让整个运维操作失败，比偶尔丢一条审计更糟。
func adminAudit(ctx context.Context, w AdminAuditWriter, actorID uint64, action, targetType string, targetID uint64, detail map[string]any) {
	if w == nil {
		return
	}
	entry := &model.AdminAuditLog{ActorID: actorID, Action: action, TargetType: targetType, TargetID: targetID}
	if len(detail) > 0 {
		// detail 都是字符串 / 数字 / 布尔，编码不会失败；万一失败就不带 detail 写日志
		if b, err := json.Marshal(detail); err == nil {
			entry.DetailJSON = model.JSONText(b)
		}
	}
	if err := w.Insert(ctx, entry); err != nil {
		logger.Error("写后台审计失败", zap.Error(err), zap.String("action", action), zap.Uint64("actor_id", actorID))
	}
}
