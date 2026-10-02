package service

import (
	"context"
	"errors"
	"fmt"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// CheckDelete 删除预检：返回阻断删除渠道的原因（blockers 为空数组表示可以删）。渠道不存在返回 ErrChannelNotFound。
// 阻断原因：被模型引用（channel_models，列出模型 key 与展示名）、被进行中的任务引用（active_tasks）。
// 结果只给界面展示，真正能否删除以 Delete 在事务内的判断为准。
func (s *AIChannelService) CheckDelete(ctx context.Context, key string) (*model.DeleteCheck, error) {
	// 1. 统计引用；渠道不存在翻译成 404
	refs, err := s.repo.CountChannelRefs(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrChannelNotFound
	}
	if err != nil {
		return nil, err
	}
	// 2. 组装阻断原因
	return &model.DeleteCheck{Blockers: channelBlockers(refs)}, nil
}

// Delete 硬删除渠道，连同它在 ai_secrets 里的 Key。仍被模型（最新草稿或已发布版本）或非终态任务引用时返回 ErrChannelInUse（409，文案带数量）；
// 渠道不存在返回 ErrChannelNotFound。删除后写审计、清掉本实例的 Key 明文缓存并刷新 Registry。
func (s *AIChannelService) Delete(ctx context.Context, actorID uint64, key string) error {
	// 1. 先查一次引用给出清楚的原因；真正的删除在仓储事务里锁住渠道行后重新统计，这里的结果可能过期
	refs, err := s.repo.CountChannelRefs(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrChannelNotFound
	}
	if err != nil {
		return err
	}
	if refs.InUse() {
		return channelInUse(key, refs)
	}
	// 2. 事务内加锁复查并删除；并发下刚被新模型 / 新任务引用时仓储返回 ErrInUse 与当时的引用
	refs, err = s.repo.DeleteChannel(ctx, key)
	if errors.Is(err, repository.ErrInUse) {
		return channelInUse(key, refs)
	}
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrChannelNotFound
	}
	if err != nil {
		return err
	}
	// 3. 审计（不含 Key）；Key 行已删，清掉本实例缓存里的明文；刷新 Registry 让渠道运行时立即消失
	aiAudit(ctx, s.audit, actorID, model.AuditChannelDelete, model.AuditTargetChannel, key, nil)
	s.secrets.ForgetSecret(model.ChannelSecretName(key))
	s.notifyChanged(ctx, "channel delete "+key)
	return nil
}

// channelBlockers 把渠道的引用情况转成阻断原因列表（永不为 nil）；模型没有展示名时用 key。
func channelBlockers(refs repository.ChannelRefs) []model.DeleteBlocker {
	out := []model.DeleteBlocker{}
	if len(refs.Models) > 0 {
		list := make([]model.DeleteRef, 0, len(refs.Models))
		for _, m := range refs.Models {
			name := m.Label
			if name == "" {
				name = m.Key
			}
			list = append(list, model.DeleteRef{Key: m.Key, Name: name})
		}
		out = append(out, model.DeleteBlocker{
			Kind: model.BlockerChannelModels, Message: fmt.Sprintf("%d 个模型在用这个渠道", len(refs.Models)), Refs: list,
		})
	}
	if refs.ActiveTasks > 0 {
		out = append(out, model.DeleteBlocker{
			Kind: model.BlockerActiveTasks, Message: fmt.Sprintf("%d 个进行中的任务在用这个渠道", refs.ActiveTasks), Refs: []model.DeleteRef{},
		})
	}
	return out
}

// channelInUse 生成“渠道仍被引用”的 409，文案带模型数与任务数。
func channelInUse(key string, refs repository.ChannelRefs) error {
	return errcode.ErrChannelInUse.WithMsg(fmt.Sprintf("渠道 %s 被 %d 个模型、%d 个进行中的任务使用，无法删除",
		key, len(refs.Models), refs.ActiveTasks))
}
