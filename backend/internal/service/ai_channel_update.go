package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/repository"
)

// Update 更新渠道（不传的字段不改）。改 plugin_version 即“升级插件后切换渠道”：新版本必须存在，且插件启用；
// settings 会按目标版本的 channelSettings 重新校验（切版本时即使没传 settings，也校验现有取值）。
// 修改会写审计：channel.update 记被改的字段名与版本变化；trusted_internal / allow_credentials / enabled 的变化各记一条带前后值的审计。
func (s *AIChannelService) Update(ctx context.Context, in ChannelUpdateInput) (*ChannelView, error) {
	// 1. 取当前渠道与它现在固定的版本
	cur, err := s.load(ctx, in.Key)
	if err != nil {
		return nil, err
	}
	curVer, err := s.plugins.GetVersionHead(ctx, cur.PluginVersionID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	// 2. 目标版本：传了 plugin_version 就解析它（限本渠道的插件），否则沿用当前版本
	target, problems, err := s.updateTarget(ctx, cur, curVer, in.PluginVersion)
	if err != nil {
		return nil, err
	}
	next := *cur
	versionChanged := target != nil && target.version != nil && target.version.ID != cur.PluginVersionID
	if versionChanged {
		next.PluginVersionID = target.version.ID
	}

	// 3. 其他字段与 settings
	problems = append(problems, applyUpdateFields(&next, in)...)
	settingsProblems, err := applyUpdateSettings(&next, cur, in.Settings, target, versionChanged)
	if err != nil {
		return nil, err
	}
	if problems = append(problems, settingsProblems...); len(problems) > 0 {
		return nil, invalidChannel(problems)
	}

	// 4. 写库并记审计；渠道或版本在中途被删按“不合法”处理，让运维刷新后重试
	next.UpdatedBy = in.ActorID
	if err := s.repo.UpdateChannel(ctx, &next); errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrChannelInvalid.WithMsg("渠道或它要固定的插件版本已被删除，请刷新后重试")
	} else if err != nil {
		return nil, err
	}
	s.auditUpdate(ctx, in.ActorID, cur, &next, curVer, target)
	s.notifyChanged(ctx, "channel update "+cur.Key)
	version := ""
	if target != nil && target.version != nil {
		version = target.version.Version
	}
	return s.view(ctx, &next, version)
}

// updateTarget 解析更新后渠道固定的插件版本。传了 plugin_version 就解析它，问题作为 problems 返回（与其他字段的问题一起报）；
// 没传就沿用当前版本（当前版本行已丢失时返回空的 resolvedVersion，后面需要按声明校验 settings 时才会报错）。
// 只有真的切版本时才要求插件启用：插件停用后运维仍要能改渠道（比如停用渠道、改地址）。
func (s *AIChannelService) updateTarget(ctx context.Context, cur *model.AIChannel, curVer *model.AIPluginVersion, want *string) (*resolvedVersion, []string, error) {
	if want != nil {
		requireEnabled := curVer == nil || *want != curVer.Version
		target, msg, err := s.resolveVersion(ctx, cur.PluginKey, *want, requireEnabled)
		return target, appendIf(nil, msg), err
	}
	target := &resolvedVersion{}
	if curVer != nil {
		// meta 损坏时不拦截无关的修改（改名、停用），只有需要按声明校验 settings 时才会报错
		target.version = curVer
		if meta, err := pluginmeta.Parse(curVer.MetaJSON); err == nil {
			target.meta = meta
		}
	}
	return target, nil, nil
}

// applyUpdateFields 把请求里给出的简单字段应用到 next，返回校验问题。
func applyUpdateFields(next *model.AIChannel, in ChannelUpdateInput) []string {
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
	if in.TrustedInternal != nil {
		next.TrustedInternal = *in.TrustedInternal
	}
	if in.AllowCredentials != nil {
		next.AllowCredentials = *in.AllowCredentials
	}
	if in.Enabled != nil {
		next.Enabled = *in.Enabled
	}
	if in.RateLimit != nil {
		problems = appendIf(problems, checkRateLimit(*in.RateLimit))
		if b, err := json.Marshal(*in.RateLimit); err == nil {
			next.RateLimitJSON = model.JSONText(b)
		}
	}
	return problems
}

// applyUpdateSettings 处理 settings：传了就整体替换并校验；没传但切了版本，校验现有取值是否仍符合新版本的声明；都没有就不动。
// 校验通过后把补全默认值的结果写进 next。target 为 nil 说明版本解析已经有问题，由调用方一并报出，这里不重复。
func applyUpdateSettings(next, cur *model.AIChannel, given map[string]any, target *resolvedVersion, versionChanged bool) ([]string, error) {
	if target == nil || (given == nil && !versionChanged) {
		return nil, nil
	}
	if target.meta == nil {
		return []string{"读不到渠道固定的插件版本的 meta，无法校验 settings，请在 plugin_version 里指定一个可用的版本"}, nil
	}
	values := given
	if values == nil {
		values = decodeObject(cur.SettingsJSON)
	}
	normalized, issues := pluginmeta.ValidateSettingValues(target.meta.ChannelSettings, values)
	if len(issues) > 0 {
		return formatSettingIssues("settings", issues), nil
	}
	b, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("编码渠道 settings 失败：%w", err)
	}
	next.SettingsJSON = model.JSONText(b)
	return nil, nil
}

// auditUpdate 为一次更新写审计：安全相关开关与启停各一条（带前后值），其余变化汇总成一条 channel.update（只记字段名与版本号）。
func (s *AIChannelService) auditUpdate(ctx context.Context, actor uint64, before, after *model.AIChannel, oldVer *model.AIPluginVersion, target *resolvedVersion) {
	if changed := changedFields(before, after); len(changed) > 0 {
		detail := map[string]any{"fields": changed}
		if before.PluginVersionID != after.PluginVersionID {
			if oldVer != nil {
				detail["from_version"] = oldVer.Version
			}
			if target != nil && target.version != nil {
				detail["to_version"] = target.version.Version
			}
		}
		aiAudit(ctx, s.audit, actor, model.AuditChannelUpdate, model.AuditTargetChannel, after.Key, detail)
	}
	if before.TrustedInternal != after.TrustedInternal {
		aiAudit(ctx, s.audit, actor, model.AuditChannelTrusted, model.AuditTargetChannel, after.Key,
			map[string]any{"from": before.TrustedInternal, "to": after.TrustedInternal})
	}
	if before.AllowCredentials != after.AllowCredentials {
		aiAudit(ctx, s.audit, actor, model.AuditChannelCred, model.AuditTargetChannel, after.Key,
			map[string]any{"from": before.AllowCredentials, "to": after.AllowCredentials})
	}
	if before.Enabled != after.Enabled {
		aiAudit(ctx, s.audit, actor, model.AuditChannelEnable, model.AuditTargetChannel, after.Key,
			map[string]any{"from": before.Enabled, "to": after.Enabled})
	}
}

// changedFields 列出普通字段里发生变化的（安全开关与启停另有专门的审计，不在这里）。
func changedFields(before, after *model.AIChannel) []string {
	var changed []string
	if before.Name != after.Name {
		changed = append(changed, "name")
	}
	if before.BaseURL != after.BaseURL {
		changed = append(changed, "base_url")
	}
	if before.PluginVersionID != after.PluginVersionID {
		changed = append(changed, "plugin_version")
	}
	if !jsonEqual(before.SettingsJSON, after.SettingsJSON) {
		changed = append(changed, "settings")
	}
	if !jsonEqual(before.RateLimitJSON, after.RateLimitJSON) {
		changed = append(changed, "rate_limit")
	}
	return changed
}
