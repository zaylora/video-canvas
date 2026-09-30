package service

import (
	"context"
	"encoding/json"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
)

// AIAuditWriter 是审计日志的写入依赖，由 repository.AIChannelRepository 实现。
type AIAuditWriter interface {
	// InsertAudit 写一条审计日志（只增不改不删）。
	InsertAudit(ctx context.Context, log *model.AIAuditLog) error
}

// RegistryNotifier 在插件 / 渠道变更后让 Registry 内存立即对齐，由 AIConfigService 实现。
type RegistryNotifier interface {
	// NotifyChanged 刷新本实例的 Registry 内存并广播失效，reason 只用于日志。
	NotifyChanged(ctx context.Context, reason string)
}

// NotifyChanged 实现 RegistryNotifier：插件与渠道服务在改动提交后调用它，让新任务立即按新配置解析模型。
func (s *AIConfigService) NotifyChanged(ctx context.Context, reason string) {
	s.notifyChanged(ctx, reason)
}

// aiAudit 写一条审计日志。detail 由调用方保证不含任何凭证（只放版本号、sha256、开关前后值等）。
// 审计写入失败不回滚已经提交的业务变更（变更已生效，回滚不了），但必须留下 Error 日志让运维补记：
// 这是故意的取舍——为了写一条日志让整个运维操作失败，比偶尔丢一条审计更糟。
func aiAudit(ctx context.Context, w AIAuditWriter, actorID uint64, action, targetType, targetKey string, detail map[string]any) {
	if w == nil {
		return
	}
	entry := &model.AIAuditLog{ActorID: actorID, Action: action, TargetType: targetType, TargetKey: targetKey}
	if len(detail) > 0 {
		// detail 都是字符串 / 数字 / 布尔，编码不会失败；万一失败就不带 detail 写日志
		if b, err := json.Marshal(detail); err == nil {
			entry.DetailJSON = model.JSONText(b)
		}
	}
	if err := w.InsertAudit(ctx, entry); err != nil {
		logger.Error("写审计日志失败", zap.Error(err), zap.String("action", action),
			zap.String("target", targetKey), zap.Uint64("actor_id", actorID))
	}
}
