package agent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"video-canvas/internal/pkg/configbody"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// adminCheckedDraft 只缓存哈希与展示用结论，草稿代码不会写入插件库或缓存。
type adminCheckedDraft struct {
	SHA   string
	Check map[string]any
}

// toolAdminListPlugins 将最新版本的非凭证元数据投影给模型，帮助优先复用现有插件。
func (b *AgentBridge) toolAdminListPlugins(ctx context.Context, _ *toolCall) (*toolOut, error) {
	s := b.d.Admin
	rows, err := s.d.Plugins.ListPlugins(ctx)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, p := range rows {
		vs, err := s.d.Plugins.ListVersions(ctx, p.Key)
		if err != nil {
			return nil, err
		}
		item := map[string]any{"key": p.Key, "name": p.Name, "builtin": p.Source == "builtin", "enabled": p.Enabled}
		if len(vs) > 0 {
			m := &provider.PluginMeta{}
			err := json.Unmarshal(vs[0].MetaJSON, m)
			if err != nil {
				return nil, err
			}
			kinds := []string{}
			for k := range m.Endpoints {
				kinds = append(kinds, k)
			}
			slices.Sort(kinds)
			item["vendor"] = m.Vendor
			item["kind"] = kinds
			item["auth"] = m.Auth
			item["endpoints"] = m.Endpoints
			item["latest_version"] = vs[0].Version
		}
		out = append(out, item)
	}
	return adminToolOut(out)
}

// pluginVersion 只从指定插件的版本头选取完整代码，默认读最新版本，内置插件也可作样例。
func (s *AdminAssistantService) pluginVersion(ctx context.Context, key, version string) (*provider.PluginMeta, string, string, error) {
	vs, err := s.d.Plugins.ListVersions(ctx, key)
	if err != nil {
		return nil, "", "", err
	}
	for _, v := range vs {
		if version != "" && version != "latest" && v.Version != version {
			continue
		}
		full, err := s.d.Plugins.GetVersion(ctx, v.ID)
		if err != nil {
			return nil, "", "", err
		}
		meta := &provider.PluginMeta{}
		err = json.Unmarshal(full.MetaJSON, meta)
		return meta, full.Code, full.Version, err
	}
	return nil, "", "", repository.ErrNotFound
}

// toolAdminGetPlugin 读取已有插件完整代码，不能读到渠道凭证。
func (b *AgentBridge) toolAdminGetPlugin(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var req struct {
		Key     string `json:"key"`
		Version string `json:"version"`
	}
	if json.Unmarshal(tc.args, &req) != nil || req.Key == "" {
		return nil, fail("需要插件 key")
	}
	m, code, version, err := b.d.Admin.pluginVersion(ctx, req.Key, req.Version)
	if err != nil {
		return nil, err
	}
	return adminToolOut(map[string]any{"key": req.Key, "version": version, "meta": m, "code": code})
}

// claimAttempt 使用持久化计数，token 为空表示一次新的调用；工具任务重放保留原次数。
func (s *AdminAssistantService) claimAttempt(ctx context.Context, runID uint64, kind, key string) (int, error) {
	if s.d.Attempts == nil {
		return 0, errcode.ErrAgentUnavailable
	}
	token := IdempotencyKeyFrom(ctx)
	if token == "" {
		token = rand.Text()
	}
	return s.d.Attempts.ClaimAdminAttempt(ctx, runID, kind, key, token)
}

// toolAdminPluginCheck 先预检确定 meta.key，再由 Go 原子计次，只有前 3 次允许执行钩子。
func (b *AgentBridge) toolAdminPluginCheck(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var in provider.PluginDraftInput
	if json.Unmarshal(tc.args, &in) != nil || len(in.Code) == 0 || len(in.Code) > provider.MaxPluginDraftBytes {
		return nil, fail("code 必须是 1–512KB 的插件源码")
	}
	s := b.d.Admin
	pre, err := s.d.Checker.Precheck(ctx, in.Code)
	if err != nil {
		return nil, err
	}
	var header struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(pre.Meta, &header)
	if header.Key == "" {
		header.Key = "__invalid__"
	}
	attempt, err := s.claimAttempt(ctx, tc.run.ID, "plugin_check", header.Key)
	if err != nil {
		return nil, err
	}
	out := &provider.PluginDraftResult{Issues: []string{}, Logs: []string{}, Meta: pre.Meta, Attempt: attempt, AttemptsLeft: 3 - attempt}
	if attempt == 0 {
		out.Attempt = 3
		out.AttemptsLeft = 0
		out.Issues = append(out.Issues, "已达自动修复上限")
		return adminPluginCheckOut(out)
	}
	for _, issue := range pre.Issues {
		out.Issues = append(out.Issues, issue.Path+"："+issue.Message)
	}
	if pre.OK {
		out, err = s.d.Checker.CheckDraft(ctx, in, pre)
		if err != nil {
			return nil, err
		}
		out.Attempt = attempt
		out.AttemptsLeft = 3 - attempt
	}
	// 1. 样例可能含文档里的鉴权值；对请求、解析、日志和异常整体脱敏后才发给 Agent。
	redacted := redactAdminValue(out, in.CtxSample, in.Responses)
	raw, _ := json.Marshal(redacted)
	_ = json.Unmarshal(raw, &out)
	check := map[string]any{"ok": out.OK, "issues": out.Issues}
	if out.Request != nil {
		check["request"] = map[string]any{"method": out.Request["method"], "url": out.Request["url"]}
	}
	s.checkMu.Lock()
	s.checks[fmt.Sprintf("%d:%s", tc.run.ID, header.Key)] = adminCheckedDraft{SHA: configbody.SHA256([]byte(in.Code)), Check: check}
	s.checkMu.Unlock()
	return adminPluginCheckOut(out)
}

// sensitiveAdminField 限定凭证通道，普通配置不能携带鉴权值。
func sensitiveAdminField(key string) bool {
	k := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	return strings.Contains(k, "secret") || strings.Contains(k, "password") || strings.Contains(k, "token") || strings.Contains(k, "authorization") || strings.Contains(k, "apikey") || k == "key" || k == "credentials" || k == "cookie"
}

// redactAdminValue 收集样例中的凭证值，并对结果整体替换，含 URL 和 JSON 转义形式。
func redactAdminValue(value any, sources ...any) any {
	secrets := []string{}
	for _, src := range sources {
		raw, _ := json.Marshal(src)
		var v any
		_ = json.Unmarshal(raw, &v)
		collectAdminSecrets(v, false, &secrets)
	}
	raw, _ := json.Marshal(value)
	var v any
	_ = json.Unmarshal(raw, &v)
	return scrubAdminValue(v, secrets)
}

// collectAdminSecrets 不只遮字段，还提取值，避免同一个凭证被钩子写进日志和错误。
func collectAdminSecrets(v any, sensitive bool, secrets *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			collectAdminSecrets(e, sensitive || sensitiveAdminField(k), secrets)
		}
	case []any:
		for _, e := range x {
			collectAdminSecrets(e, sensitive, secrets)
		}
	case string:
		if sensitive && x != "" {
			*secrets = append(*secrets, x)
		}
	}
}

// scrubAdminValue 只保留业务 key，遮盖鉴权字段，并递归清理文本中的凭证。
func scrubAdminValue(v any, secrets []string) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if sensitiveAdminField(k) && k != "key" {
				x[k] = "••••"
			} else {
				x[k] = scrubAdminValue(e, secrets)
			}
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = scrubAdminValue(e, secrets)
		}
		return x
	case string:
		masked, _ := modelcfg.Redact(x, secrets...).(string)
		return strings.ReplaceAll(masked, "***", "••••")
	default:
		return v
	}
}

// adminPluginCheckOut 提供模型全文和结构化 JSON，事件只给一行摘要。
func adminPluginCheckOut(out *provider.PluginDraftResult) (*toolOut, error) {
	text, err := jsonOut(out)
	return &toolOut{content: text, data: out, isError: !out.OK, summary: fmt.Sprintf("插件验证：通过=%t，第 %d/3 次，剩余 %d 次", out.OK, out.Attempt, out.AttemptsLeft)}, err
}

// forgetDraftChecks 运行结束后清理内存检查摘要；持久化计次仍保留以防旧运行重放绕过上限。
func (s *AdminAssistantService) forgetDraftChecks(runID uint64) {
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	prefix := strconv.FormatUint(runID, 10) + ":"
	for key := range s.checks {
		if strings.HasPrefix(key, prefix) {
			delete(s.checks, key)
		}
	}
}
