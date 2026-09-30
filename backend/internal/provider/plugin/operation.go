package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginproto"
)

// redactedSecret 是 DryRun 里代替凭证明文的占位符：DryRun 不解析凭证。
const redactedSecret = "***"

// operation 是一次 Executor 调用（Submit / Query / Cancel / Check / Import / DryRun）的执行上下文，不跨调用共享。
type operation struct {
	e     *Executor
	rt    *provider.ChannelRuntime
	model *provider.ModelSnapshot // Check / Import 为 nil
	task  provider.TaskRef
	base  *url.URL // 渠道 base_url
	hosts []string // 请求与产物可访问的主机：插件 allowedHosts ∪ base_url 主机
	trace *provider.Trace
	red   redactor

	input map[string]any    // 规范化后的输入（媒体字段是 asset id）；Check / Import 为 nil
	media map[string]uint64 // 已填写的媒体字段 → asset id，文件引用只能指向这里

	dryRun       bool
	secret       string
	secretLoaded bool
	asset        *provider.Output // 最近一次 binary 响应落库的素材，产物 {type:"asset"} 用它补全
}

// newOperation 校验渠道 base_url 并组装执行上下文。
func (e *Executor) newOperation(ctx context.Context, rt *provider.ChannelRuntime, ms *provider.ModelSnapshot, task provider.TaskRef) (*operation, error) {
	base, err := url.Parse(rt.Channel.BaseURL)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil {
		return nil, terminalErr(codeInvalidSnapshot, fmt.Sprintf("渠道 %s 的 base_url 不合法", rt.Channel.Key), nil)
	}
	hosts := append(append([]string(nil), rt.Plugin.Meta.AllowedHosts...), strings.ToLower(base.Hostname()))
	return &operation{e: e, rt: rt, model: ms, task: task, base: base, hosts: hosts, trace: provider.TraceFrom(ctx)}, nil
}

// setInput 登记规范化后的输入与其中已填写的媒体字段。
func (o *operation) setInput(input map[string]any) {
	o.input = input
	o.media = map[string]uint64{}
	if o.model == nil {
		return
	}
	for _, name := range modelcfg.MediaFieldNames(o.model.InputSchema) {
		// 任务输入是落库 JSON 解码来的，素材 id 是 float64，不能只认 uint64
		if id, ok := modelcfg.AsAssetID(input[name]); ok {
			o.media[name] = id
		}
	}
}

// wantsCredentials 判断 ctx 里是否放 credentials：只有插件声明 auth: custom 且渠道显式开启 allow_credentials。
func (o *operation) wantsCredentials() bool {
	return o.rt.Plugin.Meta.Auth.Type == pluginmeta.AuthCustom && o.rt.Channel.AllowCredentials
}

// needsSecretUpfront 判断是否在调任何钩子之前就取 Key：meta 层面的注入方式需要 Key，或要放进 ctx.credentials。
// 提前取的好处是 Key 未设置时不白跑钩子；请求级 auth 临时要 Key 时再按需取。
func (o *operation) needsSecretUpfront() bool {
	switch o.rt.Plugin.Meta.Auth.Type {
	case pluginmeta.AuthBearer, pluginmeta.AuthHeader, pluginmeta.AuthQuery:
		return true
	}
	return o.wantsCredentials()
}

// loadSecret 取渠道 Key 并登记脱敏；DryRun 不解析凭证，一律用 ***。
func (o *operation) loadSecret(ctx context.Context) (string, error) {
	if o.secretLoaded {
		return o.secret, nil
	}
	if o.dryRun {
		o.secret, o.secretLoaded = redactedSecret, true
		return o.secret, nil
	}
	if o.e.opts.Secrets == nil {
		return "", terminalErr(codeHostMisconfigured, "宿主未配置凭证解析器", nil)
	}
	s, err := o.e.opts.Secrets.Get(ctx, model.ChannelSecretName(o.rt.Channel.Key))
	if err != nil || s == "" {
		// 凭证取不到是配置问题，重试没有意义；错误信息只带渠道名，不带任何明文
		return "", terminalErr(codeSecretUnavailable, fmt.Sprintf("渠道 %s 的 Key 未设置或无法读取", o.rt.Channel.Key), redactError(err, o.red.str))
	}
	o.secret, o.secretLoaded = s, true
	o.red.add(s)
	return s, nil
}

// hookCtx 是传给钩子的 ctx（契约 §3），字段驼峰。
type hookCtx struct {
	Task        *hookTask        `json:"task,omitempty"`
	Model       *hookModel       `json:"model,omitempty"`
	Input       json.RawMessage  `json:"input,omitempty"`
	Channel     hookChannel      `json:"channel"`
	Prepared    json.RawMessage  `json:"prepared,omitempty"` // 只在 buildSubmitRequest 里出现（没有时是 null）
	Credentials *hookCredentials `json:"credentials,omitempty"`
	Now         int64            `json:"now"`
}

type hookTask struct {
	ID             uint64          `json:"id"`
	ProviderTaskID string          `json:"providerTaskId"`
	State          json.RawMessage `json:"state"`
}

type hookModel struct {
	Key           string         `json:"key"`
	Kind          string         `json:"kind"`
	UpstreamModel string         `json:"upstreamModel"`
	Params        map[string]any `json:"params"`
}

type hookChannel struct {
	BaseURL  string         `json:"baseUrl"`
	Settings map[string]any `json:"settings"`
}

type hookCredentials struct {
	APIKey string `json:"apiKey"`
}

// buildCtx 组装钩子 ctx。任务类操作带 task / model / input；Check / Import 只有 channel、credentials、now。
// 媒体字段换成文件引用字符串 "input:<字段名>"：插件永远看不到 asset id，只能通过文件引用让宿主注入内容。
func (o *operation) buildCtx(ctx context.Context) (*hookCtx, error) {
	settings := o.rt.Channel.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	hc := &hookCtx{Channel: hookChannel{BaseURL: o.rt.Channel.BaseURL, Settings: settings}, Now: o.e.opts.Now().Unix()}
	if o.wantsCredentials() {
		key, err := o.loadSecret(ctx)
		if err != nil {
			return nil, err
		}
		hc.Credentials = &hookCredentials{APIKey: key}
	}
	if o.model == nil {
		return hc, nil
	}
	params := o.model.Params
	if params == nil {
		params = map[string]any{}
	}
	hc.Model = &hookModel{Key: o.model.Key, Kind: o.model.Kind, UpstreamModel: o.model.UpstreamModel, Params: params}
	hc.Task = &hookTask{ID: o.task.ID, ProviderTaskID: o.task.ProviderTaskID, State: nonEmptyJSON(o.task.State)}
	in := make(map[string]any, len(o.input))
	for k, v := range o.input {
		if _, isMedia := o.media[k]; isMedia {
			v = fileRefPrefix + k
		}
		in[k] = v
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, terminalErr(codeInvalidInput, "编码任务输入失败", err)
	}
	hc.Input = raw
	return hc, nil
}

// nonEmptyJSON 把空 RawMessage 规范成 nil（编码为 null）：空切片直接编码会得到非法 JSON。
func nonEmptyJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// errHookMissing 表示插件没有导出这个钩子；可选钩子据此走缺省行为，必需钩子由 require 转成插件级失败。
var errHookMissing = errors.New("插件没有实现该钩子")

// call 调用一个钩子，返回它的返回值 JSON（钩子返回 undefined / null 时是 "null"）。
// 第一个参数是 ctx，编码后不得超过 1MB；runner 的各种失败按契约 §8 映射成 *provider.Error，钩子缺失返回 errHookMissing。
func (o *operation) call(ctx context.Context, hook string, args ...any) (json.RawMessage, error) {
	if !o.e.loader.mayHave(o.rt.Plugin.SHA256, hook) {
		return nil, errHookMissing
	}
	encoded := make([]json.RawMessage, 0, len(args))
	for _, a := range args {
		b, err := json.Marshal(a)
		if err != nil {
			return nil, terminalErr(provider.CodePluginError, fmt.Sprintf("编码钩子 %s 的参数失败", hook), err)
		}
		encoded = append(encoded, b)
	}
	if len(encoded) > 0 && len(encoded[0]) > pluginproto.MaxPayloadBytes {
		return nil, terminalErr(provider.CodePluginError, fmt.Sprintf("钩子 %s 的 ctx 编码后超过 %d 字节", hook, pluginproto.MaxPayloadBytes), nil)
	}
	start := time.Now()
	resp, err := o.e.loader.call(ctx, o.rt, hook, encoded, o.e.opts.HookTimeout)
	out := &hookOutcome{duration: time.Since(start)}
	defer func() { recordHook(o.trace, &o.red, hook, encoded, out) }()
	if err != nil {
		out.err = o.runnerError(hook, err)
		return nil, out.err
	}
	out.result, out.logs = resp.Result, resp.Logs
	if resp.Error != nil {
		if resp.Error.Code == pluginproto.CodeHookMissing {
			out.err = errHookMissing
			return nil, errHookMissing
		}
		out.err = pluginFaultf("插件钩子 %s 执行失败（%s）：%s", hook, resp.Error.Code, o.red.str(resp.Error.Message))
		return nil, out.err
	}
	if len(resp.Result) == 0 {
		return json.RawMessage("null"), nil
	}
	return resp.Result, nil
}

// require 调用必需的钩子：插件没有实现它是插件级失败。
func (o *operation) require(ctx context.Context, hook string, args ...any) (json.RawMessage, error) {
	raw, err := o.call(ctx, hook, args...)
	if errors.Is(err, errHookMissing) {
		return nil, pluginFaultf("插件没有实现钩子 %s", hook)
	}
	return raw, err
}

// runnerError 把 runner 客户端返回的错误映射成分类错误（契约 §8）。
func (o *operation) runnerError(hook string, err error) error {
	var pe *provider.Error
	if errors.As(err, &pe) {
		return err // 装载阶段已经分类过（代码仓库、哈希核对、装载失败）
	}
	cause := redactError(err, o.red.str)
	switch {
	case errors.Is(err, ErrRunnerUnavailable):
		return &provider.Error{Class: provider.ClassRetryable, Code: provider.CodeRunnerUnavailable, Message: "plugin-runner 不可用", Cause: cause}
	case errors.Is(err, ErrRunnerCrashed):
		return &provider.Error{Class: provider.ClassRetryable, Code: provider.CodeRunnerCrashed, Message: fmt.Sprintf("调用钩子 %s 时 plugin-runner 崩溃", hook), Cause: cause, PluginFault: true}
	}
	return retryableErr("", fmt.Sprintf("调用钩子 %s 失败", hook), cause)
}

// isNull 判断钩子返回值是否是 null（钩子返回 undefined / null）。
func isNull(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null"
}
