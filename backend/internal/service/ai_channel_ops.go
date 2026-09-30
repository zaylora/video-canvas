package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
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
	// 2. 执行检查。Executor 在“请求发出去但失败”时同时返回结果（带原因）与错误：这种情况按检查结果返回
	res, err := s.ops.Check(ctx, rt)
	switch {
	case errors.Is(err, provider.ErrCheckUnsupported):
		return &provider.CheckResult{OK: false, Message: "插件不支持连通性检查"}, nil
	case provider.CodeOf(err) == provider.CodeRunnerUnavailable:
		return nil, errcode.ErrRunnerUnavailable
	case err != nil && res != nil:
		return &provider.CheckResult{OK: false, Message: s.redactMsg(ctx, key, res.Message), DurationMs: res.DurationMs}, nil
	case err != nil:
		return nil, errcode.ErrPluginOpFailed.WithMsg(s.redactMsg(ctx, key, err.Error()))
	}
	res.Message = s.redactMsg(ctx, key, res.Message)
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
