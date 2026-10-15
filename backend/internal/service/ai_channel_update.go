package service

import (
	"context"
	"encoding/json"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// Update 更新渠道（不传的字段不改）。headers 传了就整体替换。
// 修改会写审计：channel.update 记被改的字段名；enabled 的变化另记一条带前后值的审计。
func (s *AIChannelService) Update(ctx context.Context, in ChannelUpdateInput) (*ChannelView, error) {
	// 1. 取当前渠道
	cur, err := s.load(ctx, in.Key)
	if err != nil {
		return nil, err
	}
	// 2. 套用修改并校验
	next := *cur
	problems, err := applyUpdateFields(&next, in)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, invalidChannel(problems)
	}
	// 3. 写库并记审计；渠道在中途被删按“不合法”处理，让运维刷新后重试
	next.UpdatedBy = in.ActorID
	if err := s.repo.UpdateChannel(ctx, &next); errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrChannelInvalid.WithMsg("渠道已被删除，请刷新后重试")
	} else if err != nil {
		return nil, err
	}
	s.auditUpdate(ctx, in.ActorID, cur, &next)
	s.notifyChanged(ctx, "channel update "+cur.Key)
	return s.view(ctx, &next)
}

// applyUpdateFields 把更新参数里传了的普通字段套到 next 上，返回校验问题；编码失败才返回 error。
func applyUpdateFields(next *model.AIChannel, in ChannelUpdateInput) ([]string, error) {
	var problems []string
	if in.Name != nil {
		name, msg := checkChannelName(*in.Name)
		problems = appendIf(problems, msg)
		next.Name = name
	}
	if in.BaseURL != nil {
		baseURL, msg := checkBaseURL(*in.BaseURL)
		problems = appendIf(problems, msg)
		next.BaseURL = baseURL
	}
	if in.Headers != nil {
		headers, msg := checkChannelHeaders(in.Headers)
		problems = appendIf(problems, msg)
		raw, err := json.Marshal(headers)
		if err != nil {
			return nil, err
		}
		next.HeadersJSON = model.JSONText(raw)
	}
	if in.RateLimit != nil {
		problems = appendIf(problems, checkRateLimit(*in.RateLimit))
		raw, err := json.Marshal(*in.RateLimit)
		if err != nil {
			return nil, err
		}
		next.RateLimitJSON = model.JSONText(raw)
	}
	if in.Enabled != nil {
		next.Enabled = *in.Enabled
	}
	return problems, nil
}

// auditUpdate 为一次更新写审计：启停单独一条（带前后值），其余变化汇总成一条 channel.update（只记字段名；请求头的值不记）。
func (s *AIChannelService) auditUpdate(ctx context.Context, actor uint64, before, after *model.AIChannel) {
	if changed := changedFields(before, after); len(changed) > 0 {
		aiAudit(ctx, s.audit, actor, model.AuditChannelUpdate, model.AuditTargetChannel, after.Key, map[string]any{"fields": changed})
	}
	if before.Enabled != after.Enabled {
		aiAudit(ctx, s.audit, actor, model.AuditChannelEnable, model.AuditTargetChannel, after.Key,
			map[string]any{"from": before.Enabled, "to": after.Enabled})
	}
}

// changedFields 列出普通字段里发生变化的（启停另有专门的审计，不在这里）。
func changedFields(before, after *model.AIChannel) []string {
	var changed []string
	if before.Name != after.Name {
		changed = append(changed, "name")
	}
	if before.BaseURL != after.BaseURL {
		changed = append(changed, "base_url")
	}
	if !jsonEqual(before.HeadersJSON, after.HeadersJSON) {
		changed = append(changed, "headers")
	}
	if !jsonEqual(before.RateLimitJSON, after.RateLimitJSON) {
		changed = append(changed, "rate_limit")
	}
	return changed
}
