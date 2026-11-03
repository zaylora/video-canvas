package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginproto"
)

// NewDraftChecker 复用 runner 的上传预检与钩子执行，整个流程不创建 HTTP 上游请求。
func NewDraftChecker(c RunnerClient) provider.PluginDraftChecker {
	return &draftChecker{prechecker: prechecker{c: c}}
}

type draftChecker struct{ prechecker }

// CheckDraft 使用占位凭证运行样例钩子，凭证从来不从数据库读取。
func (p *draftChecker) CheckDraft(ctx context.Context, in provider.PluginDraftInput, pre *provider.PrecheckResult) (*provider.PluginDraftResult, error) {
	out := &provider.PluginDraftResult{OK: true, Meta: pre.Meta, Issues: []string{}, Logs: []string{}, Parsed: map[string]json.RawMessage{}}
	loaded, err := p.c.Load(ctx, pre.SHA256, in.Code)
	if err != nil {
		return nil, err
	}
	if !loaded.OK {
		out.OK = false
		out.Issues = append(out.Issues, "插件装载失败："+loaded.Error.Message)
		return out, nil
	}
	m, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		return nil, err
	}
	sample := draftSample(in.CtxSample, m)
	input, _ := sample["input"].(map[string]any)
	_, hookInput, counts, err := draftMediaInput(input)
	if err != nil {
		out.OK = false
		out.Issues = append(out.Issues, err.Error())
		return out, nil
	}
	sample["input"] = hookInput
	out.Coverage = &provider.PluginDraftCoverage{InputMedia: counts, CheckedHooks: []string{}}
	// 静态检查仅用占位凭证，custom 鉴权也不读取或传递真实渠道 Key。
	placeholder := strings.Repeat("•", 4)
	sample["credentials"] = map[string]any{"apiKey": placeholder, "secretKey": placeholder}
	raw, err := json.Marshal(sample)
	if err != nil {
		return nil, err
	}
	call := func(hook string, extra json.RawMessage) json.RawMessage {
		args := []json.RawMessage{raw}
		if len(extra) > 0 {
			args = append(args, extra)
		}
		return p.callDraftHook(ctx, pre.SHA256, hook, args, out)
	}
	request := call(pluginproto.HookBuildSubmitRequest, nil)
	if len(request) > 0 {
		out.Request, err = renderDraftRequest(request, sample, m)
		if err != nil {
			out.OK = false
			out.Issues = append(out.Issues, "提交请求不合规："+err.Error())
		}
	}
	for _, stage := range []struct{ name, hook string }{{"submit", pluginproto.HookParseSubmitResponse}, {"query", pluginproto.HookParseQueryResponse}} {
		if response := in.Responses[stage.name]; len(response) > 0 && slices.Contains(pre.Hooks, stage.hook) {
			out.Parsed[stage.name] = call(stage.hook, response)
		}
	}
	return out, nil
}

// draftSample 为缺省 ctx 提供确定性的最小样例，有自定义样例时保留其结构。
func draftSample(sample map[string]any, m *pluginmeta.Meta) map[string]any {
	if sample != nil {
		cloned := make(map[string]any, len(sample))
		for k, v := range sample {
			cloned[k] = v
		}
		return cloned
	}
	kind := ""
	keys := make([]string, 0, len(m.Endpoints))
	for k := range m.Endpoints {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if len(keys) > 0 {
		kind = keys[0]
	}
	settings := map[string]any{}
	for _, e := range m.ChannelSettings {
		if e.Default != nil {
			settings[e.Name] = e.Default
		}
	}
	return map[string]any{"input": map[string]any{"prompt": "静态验证", "op": "t2v"}, "model": map[string]any{"kind": kind, "upstreamModel": "sample", "settings": map[string]any{}}, "channel": map[string]any{"baseUrl": "https://example.invalid", "settings": settings}, "task": map[string]any{"id": "sample", "providerTaskId": "sample"}, "assets": map[string]any{}}
}

// callDraftHook 共享 runner 的超时和大小限制，汇总最多 50 条日志及钩子异常。
func (p *draftChecker) callDraftHook(ctx context.Context, sha, hook string, args []json.RawMessage, out *provider.PluginDraftResult) json.RawMessage {
	res, err := p.c.Call(ctx, sha, hook, args, 0)
	if err != nil {
		out.Issues = append(out.Issues, "钩子 "+hook+" 执行失败")
		out.OK = false
		return nil
	}
	for _, log := range res.Logs {
		if len(out.Logs) < pluginproto.MaxLogsPerCall {
			out.Logs = append(out.Logs, log)
		}
	}
	if res.Error != nil {
		out.Issues = append(out.Issues, fmt.Sprintf("钩子 %s：%s", hook, res.Error.Message))
		out.OK = false
		return nil
	}
	if out.Coverage != nil {
		out.Coverage.CheckedHooks = append(out.Coverage.CheckedHooks, hook)
	}
	return res.Result
}

// draftMediaInput 把文档中的媒体占位值转换成生产钩子的 input: 引用；登记虚拟素材 ID，不下载或读取任何真实素材。
func draftMediaInput(input map[string]any) (map[string]any, map[string]any, map[string]int, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, nil, nil, err
	}
	normalized := map[string]any{}
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, nil, nil, err
	}
	if normalized == nil {
		normalized = map[string]any{}
	}
	hook := make(map[string]any, len(normalized))
	for key, value := range normalized {
		hook[key] = value
	}
	counts := map[string]int{}
	for _, kind := range modelcfg.MediaKinds {
		counts[kind.Key] = 0
		value, exists := normalized[kind.Key]
		if !exists {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			return nil, nil, nil, fmt.Errorf("样例 input.%s 必须是媒体占位值数组", kind.Key)
		}
		ids, refs := make([]any, len(items)), make([]string, len(items))
		for i, item := range items {
			ref := fmt.Sprintf("input:%s.%d", kind.Key, i)
			if err := validateDraftMediaSlot(item, ref); err != nil {
				return nil, nil, nil, fmt.Errorf("样例 input.%s[%d]：%w", kind.Key, i, err)
			}
			ids[i], refs[i] = uint64(i+1), ref
		}
		normalized[kind.Key], hook[kind.Key], counts[kind.Key] = ids, refs, len(items)
	}
	return normalized, hook, counts, nil
}

// validateDraftMediaSlot 检查占位值类型和显式引用下标，不解析网络地址或真实素材。
func validateDraftMediaSlot(item any, ref string) error {
	if text, ok := item.(string); ok {
		if strings.TrimSpace(text) == "" || (strings.HasPrefix(text, "input:") && text != ref) {
			return fmt.Errorf("媒体引用无效")
		}
		return nil
	}
	if _, ok := modelcfg.AsAssetID(item); !ok {
		return fmt.Errorf("必须是非空占位值或素材 ID")
	}
	return nil
}

// renderDraftRequest 使用宿主相同的 URL、请求头和正文校验，既不解析真实密钥也不创建 HTTP 请求。
func renderDraftRequest(raw json.RawMessage, sample map[string]any, meta *pluginmeta.Meta) (map[string]any, error) {
	channel, _ := sample["channel"].(map[string]any)
	baseURL, _ := channel["baseUrl"].(string)
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil {
		return nil, fmt.Errorf("样例 ctx.channel.baseUrl 无效")
	}
	opts := Options{}
	opts.applyDefaults()
	o := &operation{e: &Executor{opts: opts}, base: base, model: &provider.ModelSnapshot{}, rt: &provider.ChannelRuntime{Plugin: provider.PluginSnapshot{Meta: *meta}}}
	input, _ := sample["input"].(map[string]any)
	normalized, _, _, err := draftMediaInput(input)
	if err != nil {
		return nil, err
	}
	o.setInput(normalized)
	built, err := o.buildRequest(raw)
	if err != nil {
		return nil, err
	}
	headers := built.headers
	if sameOrigin(built.url, base) {
		switch built.auth.Type {
		case pluginmeta.AuthBearer:
			headers["Authorization"] = "••••"
		case pluginmeta.AuthHeader:
			headers[built.auth.Name] = "••••"
		case pluginmeta.AuthQuery:
			q := built.url.Query()
			q.Set(built.auth.Name, "••••")
			built.url.RawQuery = q.Encode()
		}
	}
	body := any(nil)
	switch built.bodyKind {
	case bodyJSON:
		body = describeTree(built.jsonBody)
	case bodyForm:
		body = describeFields(built.formBody)
	case bodyMultipart:
		body = map[string]any{"fields": describeFields(built.formBody), "parts": describeParts(built.parts)}
	}
	return map[string]any{"method": built.method, "url": built.url.String(), "headers": headers, "body": body}, nil
}
