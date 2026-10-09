package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/repository"
)

// SetSecret 设置渠道 Key（存 ai_secrets，名字固定为 channel:<key>），只写。审计只记“谁在什么时候改了哪个渠道的 Key”，不含值。
func (s *AIChannelService) SetSecret(ctx context.Context, actorID uint64, key, value string) error {
	// 1. 渠道必须存在：否则会往 ai_secrets 里写出没有主人的凭证
	if _, err := s.load(ctx, key); err != nil {
		return err
	}
	// 2. 加密入库（空值、超长、没有主密钥等错误由凭证服务给出）
	if err := s.secrets.SetSecret(ctx, model.ChannelSecretName(key), value, actorID); err != nil {
		return err
	}
	// 3. 审计：detail 故意为空
	aiAudit(ctx, s.audit, actorID, model.AuditChannelSecret, model.AuditTargetChannel, key, nil)
	return nil
}

// Check 连通性检查：调插件的 buildCheckRequest 发一次请求，确认地址与 Key 可用。
// 上游不通、插件没实现该钩子都是正常的检查结果（ok=false + 原因），不是 HTTP 错误；
// 需要 Key 而没设置（409）、runner 不可用（503）、插件本身出错（502）才是错误。
func (s *AIChannelService) Check(ctx context.Context, key string) (*provider.CheckResult, error) {
	// 1. 渠道运行时，与 Key 是否就绪
	rt, err := s.runtime(ctx, key)
	if err != nil {
		return nil, err
	}
	// 2. 执行检查并翻译结果
	res, err := s.ops.Check(ctx, rt)
	return checkOutcome(res, err, func(text string) string { return s.redactMsg(ctx, key, text) })
}

// CheckDraft 保存前的连通性检查：用表单里还没保存的地址、插件版本、设置与 Key 发一次检查请求，不写库、不写审计。
// 目的是让“检查通过才能保存”成立——已保存的渠道才能走 Check，而新建渠道和改过地址 / Key 的渠道都还没落库。
// Key 的来源：填了草稿 Key 就用它（明文只在这次请求里，不落库、不进日志，结果文本里也会脱敏）；
// 没填则只有编辑已有渠道（ExistingKey）且已保存过 Key 时才用已保存的，否则 409（50015）。
func (s *AIChannelService) CheckDraft(ctx context.Context, in ChannelCheckDraftInput) (*provider.CheckResult, error) {
	// 1. 与新建渠道同一套校验：地址、插件版本（必须启用）、按插件声明校验并补全 settings
	var problems []string
	baseURL, msg := checkBaseURL(in.BaseURL)
	problems = appendIf(problems, msg)
	target, msg, err := s.resolveVersion(ctx, in.PluginKey, in.PluginVersion, true)
	if err != nil {
		return nil, err
	}
	problems = appendIf(problems, msg)
	var settings map[string]any
	if target != nil {
		var issues []modelcfg.Issue
		settings, issues = pluginmeta.ValidateSettingValues(target.meta.ChannelSettings, in.Settings)
		problems = append(problems, formatSettingIssues("settings", issues)...)
	}
	if len(problems) > 0 {
		return nil, invalidChannel(problems)
	}
	// 2. 确定 Key 的来源：插件不要宿主注入鉴权就不需要 Key；否则草稿 Key 优先，其次已保存的 Key
	secret := strings.TrimSpace(in.Secret)
	needsKey := target.meta.Auth.Type != pluginmeta.AuthNone
	if needsKey && secret == "" {
		set := false
		if in.ExistingKey != "" {
			if set, err = s.secrets.SecretIsSet(ctx, model.ChannelSecretName(in.ExistingKey)); err != nil {
				return nil, err
			}
		}
		if !set {
			return nil, errcode.ErrChannelSecretUnset.WithMsg("请先填写 API Key 再检查")
		}
	}
	// 3. 用草稿拼一个只存在于内存里的渠道运行时；key 取已有渠道的（用于解析已保存的 Key），新建时用占位名
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("编码渠道 settings 失败：%w", err)
	}
	key := in.ExistingKey
	if key == "" {
		key = "draft"
	}
	rt, err := provider.RuntimeOf(&model.AIChannel{
		Key: key, PluginKey: in.PluginKey, PluginVersionID: target.version.ID, BaseURL: baseURL,
		TrustedInternal: in.TrustedInternal, AllowCredentials: in.AllowCredentials,
		SettingsJSON: model.JSONText(settingsJSON),
	}, target.version)
	if err != nil {
		return nil, errcode.ErrChannelInvalid.WithMsg("渠道配置无法解析：" + err.Error())
	}
	// 4. 执行检查：有草稿 Key 走 CheckDraft，否则让宿主按渠道 key 取已保存的 Key
	if needsKey && secret != "" {
		res, err := s.ops.CheckDraft(ctx, rt, secret)
		return checkOutcome(res, err, func(text string) string { return aiRedactString(text, []string{secret}) })
	}
	res, err := s.ops.Check(ctx, rt)
	return checkOutcome(res, err, func(text string) string { return s.redactMsg(ctx, in.ExistingKey, text) })
}

// checkOutcome 把宿主的检查返回翻译成接口结果：Executor 在“请求发出去但失败”时同时返回结果（带原因）与错误，
// 这种情况和插件没实现钩子一样都是正常的检查结果（ok=false）；runner 不可用、插件本身出错才是错误。redact 用来抹掉 Key 明文。
func checkOutcome(res *provider.CheckResult, err error, redact func(string) string) (*provider.CheckResult, error) {
	switch {
	case errors.Is(err, provider.ErrCheckUnsupported):
		return &provider.CheckResult{OK: false, Message: "插件不支持连通性检查"}, nil
	case provider.CodeOf(err) == provider.CodeRunnerUnavailable:
		return nil, errcode.ErrRunnerUnavailable
	case err != nil && res != nil:
		return &provider.CheckResult{OK: false, Message: redact(res.Message), DurationMs: res.DurationMs}, nil
	case err != nil:
		return nil, errcode.ErrPluginOpFailed.WithMsg(redact(err.Error()))
	}
	res.Message = redact(res.Message)
	return res, nil
}

// Import 从渠道导入模型草稿：args 按插件 meta.import.args 校验后交给插件的导入钩子。结果只用来预填编辑器，不落库。
func (s *AIChannelService) Import(ctx context.Context, key string, args map[string]any) (*ChannelImportResult, error) {
	// 1. 渠道运行时，与 Key 是否就绪
	rt, err := s.runtime(ctx, key)
	if err != nil {
		return nil, err
	}
	// 2. 参数按声明校验（没有声明参数的插件只接受空对象）
	var schema pluginmeta.SettingSchema
	if rt.Plugin.Meta.Import != nil {
		schema = rt.Plugin.Meta.Import.Args
	}
	normalized, issues := pluginmeta.ValidateSettingValues(schema, args)
	if len(issues) > 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("导入参数不合法：" + strings.Join(formatSettingIssues("args", issues), "；"))
	}
	// 3. 调插件
	drafts, err := s.ops.Import(ctx, rt, normalized)
	switch {
	case errors.Is(err, provider.ErrImportUnsupported):
		return nil, errcode.ErrPluginOpFailed.WithMsg("插件不支持导入模型")
	case provider.CodeOf(err) == provider.CodeRunnerUnavailable:
		return nil, errcode.ErrRunnerUnavailable
	case err != nil:
		return nil, errcode.ErrPluginOpFailed.WithMsg(s.redactMsg(ctx, key, err.Error()))
	}
	if drafts == nil {
		drafts = []provider.ModelDraft{}
	}
	return &ChannelImportResult{Drafts: drafts}, nil
}

// runtime 组装渠道运行时（渠道 + 它固定的插件版本），并在插件需要宿主注入鉴权时确认 Key 已设置（ErrChannelSecretUnset）。
func (s *AIChannelService) runtime(ctx context.Context, key string) (*provider.ChannelRuntime, error) {
	ch, err := s.load(ctx, key)
	if err != nil {
		return nil, err
	}
	ver, err := s.plugins.GetVersionHead(ctx, ch.PluginVersionID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrChannelInvalid.WithMsg(fmt.Sprintf("渠道 %q 固定的插件版本不存在", key))
	}
	if err != nil {
		return nil, err
	}
	rt, err := provider.RuntimeOf(ch, ver)
	if err != nil {
		return nil, errcode.ErrChannelInvalid.WithMsg("渠道配置无法解析：" + err.Error())
	}
	if rt.Plugin.Meta.Auth.Type != pluginmeta.AuthNone {
		set, err := s.secrets.SecretIsSet(ctx, model.ChannelSecretName(key))
		if err != nil {
			return nil, err
		}
		if !set {
			return nil, errcode.ErrChannelSecretUnset.WithMsg(fmt.Sprintf("渠道 %q 的 Key 尚未设置", key))
		}
	}
	return rt, nil
}

// redactMsg 把渠道 Key 明文从要返回给管理员的文本里抹掉。宿主已经脱敏，这里是防御：取不到 Key（没设置、没主密钥）就没有可泄露的明文。
func (s *AIChannelService) redactMsg(ctx context.Context, key, text string) string {
	secret, err := s.secrets.Get(ctx, model.ChannelSecretName(key))
	if err != nil || secret == "" {
		return text
	}
	return aiRedactString(text, []string{secret})
}
