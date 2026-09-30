package plugin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/netguard"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginproto"
)

const (
	defaultHookTimeout      = 200 * time.Millisecond
	defaultRequestTimeout   = 30 * time.Second
	defaultMaxTimeout       = 120 * time.Second
	defaultMaxResponseBytes = 5 << 20
	defaultMaxInlineBytes   = 10 << 20
	defaultMaxRequestBody   = 1 << 20
	defaultMaxDownloadBytes = 2 << 30
	defaultDownloadTimeout  = 15 * time.Minute
	defaultMaxRedirects     = 3
	defaultUserAgent        = "video-canvas-plugin-host/1"
)

// Options 是插件宿主的运行参数。
type Options struct {
	Runner  RunnerClient
	Codes   CodeStore
	Secrets provider.SecretResolver
	Assets  provider.AssetStore
	Saver   provider.AssetSaver
	Now     func() time.Time

	HookTimeout         time.Duration
	DefaultTimeout      time.Duration
	MaxTimeout          time.Duration
	MaxResponseBytes    int64
	MaxInlineBytes      int64
	MaxRequestBodyBytes int64
	MaxDownloadBytes    int64
	DownloadTimeout     time.Duration
	MaxRedirects        int

	// 测试和本机开发可注入 SSRF 解析与拨号策略。
	IPAllowed func(net.IP) bool
	Resolver  netguard.Resolver
	Dial      func(context.Context, string, string) (net.Conn, error)
}

func (o *Options) applyDefaults() {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.HookTimeout <= 0 {
		o.HookTimeout = defaultHookTimeout
	}
	if o.DefaultTimeout <= 0 {
		o.DefaultTimeout = defaultRequestTimeout
	}
	if o.MaxTimeout <= 0 {
		o.MaxTimeout = defaultMaxTimeout
	}
	if o.MaxResponseBytes <= 0 {
		o.MaxResponseBytes = defaultMaxResponseBytes
	}
	if o.MaxInlineBytes <= 0 {
		o.MaxInlineBytes = defaultMaxInlineBytes
	}
	if o.MaxRequestBodyBytes <= 0 {
		o.MaxRequestBodyBytes = defaultMaxRequestBody
	}
	if o.MaxDownloadBytes <= 0 {
		o.MaxDownloadBytes = defaultMaxDownloadBytes
	}
	if o.DownloadTimeout <= 0 {
		o.DownloadTimeout = defaultDownloadTimeout
	}
	if o.MaxRedirects <= 0 {
		o.MaxRedirects = defaultMaxRedirects
	}
}

// Executor 是协议插件的唯一宿主实现。
type Executor struct {
	opts     Options
	loader   *loader
	limiters *limiterSet

	// 出站 Transport 按 trusted_internal 维度各缓存一个，复用连接池。
	// SSRF 防护不受影响：域名白名单在每次请求的 hostGuardRT 里检查（http.Client 每次按白名单新建，很轻），
	// IP 校验在共享 Transport 的 DialContext 里对每条新连接执行。
	transportMu sync.Mutex
	transports  [2]*http.Transport // 0 = 普通渠道，1 = trusted_internal
}

var (
	_ provider.Executor  = (*Executor)(nil)
	_ provider.PluginOps = (*Executor)(nil)
)

// New 创建可并发复用的插件宿主。
func New(opts Options) *Executor {
	opts.applyDefaults()
	e := &Executor{opts: opts, limiters: newLimiterSet()}
	e.loader = newLoader(opts.Runner, opts.Codes)
	return e
}

// Submit 执行准备钩子、提交请求和提交结果解析。
func (e *Executor) Submit(ctx context.Context, snap *provider.Snapshot, in provider.SubmitInput) (*provider.SubmitResult, error) {
	if snap == nil {
		return nil, terminalErr(codeInvalidSnapshot, "缺少任务配置快照", nil)
	}
	rt := snap.Runtime()
	ep, ok := rt.Plugin.Meta.Endpoint(snap.Model.Kind)
	if !ok {
		return nil, terminalErr(codeUnsupportedKind, "插件不支持模型种类 "+snap.Model.Kind, nil)
	}
	if ep.Mode != pluginmeta.ModeSync && ep.Mode != pluginmeta.ModeAsync {
		return nil, terminalErr(codeInvalidSnapshot, "插件 endpoint mode 不合法", nil)
	}
	o, err := e.newOperation(ctx, rt, &snap.Model, in.Task)
	if err != nil {
		return nil, err
	}
	o.setInput(in.Input)
	if err := o.validateInput(ctx); err != nil {
		return nil, err
	}
	if o.needsSecretUpfront() {
		if _, err := o.loadSecret(ctx); err != nil {
			return nil, err
		}
	}

	if len(in.Task.Prepared) == 0 && e.loader.mayHave(rt.Plugin.SHA256, pluginproto.HookBuildPrepareRequests) {
		prepared, err := o.prepare(ctx)
		if err != nil {
			return nil, err
		}
		if len(prepared) > 0 {
			if in.OnPrepared != nil {
				if err := in.OnPrepared(ctx, prepared); err != nil {
					return nil, retryableErr(codePreparePersist, "保存准备阶段结果失败", redactError(err, o.red.str))
				}
			}
			o.task.Prepared = prepared
		}
	}

	hc, err := o.buildCtx(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := o.require(ctx, pluginproto.HookBuildSubmitRequest, hc)
	if err != nil {
		return nil, err
	}
	req, err := o.buildRequest(raw)
	if err != nil {
		return nil, err
	}
	resp, err := o.execute(ctx, req, "submit")
	if err != nil {
		return nil, err
	}
	parsed, err := o.require(ctx, pluginproto.HookParseSubmitResponse, hc, resp)
	if err != nil {
		return nil, err
	}
	return o.parseSubmitResult(parsed, ep.Mode)
}

// Query 查询一次异步任务并解析统一结果。
func (e *Executor) Query(ctx context.Context, snap *provider.Snapshot, task provider.TaskRef) (*provider.QueryResult, error) {
	if snap == nil {
		return nil, terminalErr(codeInvalidSnapshot, "缺少任务配置快照", nil)
	}
	o, err := e.newOperation(ctx, snap.Runtime(), &snap.Model, task)
	if err != nil {
		return nil, err
	}
	if o.needsSecretUpfront() {
		if _, err := o.loadSecret(ctx); err != nil {
			return nil, err
		}
	}
	hc, err := o.buildCtx(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := o.require(ctx, pluginproto.HookBuildQueryRequest, hc)
	if err != nil {
		return nil, err
	}
	req, err := o.buildRequest(raw)
	if err != nil {
		return nil, err
	}
	resp, err := o.execute(ctx, req, "query")
	if err != nil {
		return nil, err
	}
	parsed, err := o.require(ctx, pluginproto.HookParseQueryResponse, hc, resp)
	if err != nil {
		return nil, err
	}
	return o.parseQueryResult(parsed)
}

// Cancel 尽力调用插件取消钩子；没有实现时交给任务服务软取消。
func (e *Executor) Cancel(ctx context.Context, snap *provider.Snapshot, task provider.TaskRef) error {
	if snap == nil {
		return terminalErr(codeInvalidSnapshot, "缺少任务配置快照", nil)
	}
	o, err := e.newOperation(ctx, snap.Runtime(), &snap.Model, task)
	if err != nil {
		return err
	}
	if !e.loader.mayHave(snap.Plugin.SHA256, pluginproto.HookBuildCancelRequest) {
		return provider.ErrCancelUnsupported
	}
	if o.needsSecretUpfront() {
		if _, err := o.loadSecret(ctx); err != nil {
			return err
		}
	}
	hc, err := o.buildCtx(ctx)
	if err != nil {
		return err
	}
	raw, err := o.call(ctx, pluginproto.HookBuildCancelRequest, hc)
	if errors.Is(err, errHookMissing) {
		return provider.ErrCancelUnsupported
	}
	if err != nil {
		return err
	}
	req, err := o.buildRequest(raw)
	if err != nil {
		return err
	}
	_, err = o.execute(ctx, req, "cancel")
	return err
}

// Download 下载插件声明的产物地址。
func (e *Executor) Download(ctx context.Context, snap *provider.Snapshot, rawURL string) (*provider.Download, error) {
	if snap == nil {
		return nil, terminalErr(codeInvalidSnapshot, "缺少任务配置快照", nil)
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, terminalErr(provider.CodeSSRFBlocked, "产物地址不合法", err)
	}
	allowed := append([]string(nil), snap.Plugin.Meta.AllowedHosts...)
	// base_url 在渠道创建期校验过，但快照可能来自老数据；解析失败按快照不合法处理，不能解引用 nil
	base, err := url.Parse(snap.Channel.BaseURL)
	if err != nil || base.Hostname() == "" {
		return nil, terminalErr(codeInvalidSnapshot, fmt.Sprintf("渠道 %s 的 base_url 不合法", snap.Channel.Key), err)
	}
	allowed = append(allowed, strings.ToLower(base.Hostname()))
	if err := netguard.CheckURLAllowed(allowed, u.Scheme, u.Hostname(), u.User != nil); err != nil {
		return nil, &provider.Error{Class: provider.ClassTerminal, Code: provider.CodeSSRFBlocked, Message: "产物地址不允许", Cause: redactError(err, func(s string) string { return s })}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, retryableErr(codeDownloadFailed, "构造产物下载请求失败", err)
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	client := e.client(allowed, snap.Channel.TrustedInternal)
	dctx, cancel := context.WithTimeout(ctx, e.opts.DownloadTimeout)
	defer cancel()
	req = req.WithContext(dctx)
	resp, err := client.Do(req)
	if err != nil {
		return nil, retryableErr(codeDownloadFailed, "下载产物失败", redactError(err, func(s string) string { return s }))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, terminalErr(fmt.Sprintf("%s%d", codeHTTPPrefix, resp.StatusCode), "下载产物返回非成功状态", nil)
	}
	name := fileNameFromDisposition(resp.Header.Get("Content-Disposition"))
	return &provider.Download{Body: resp.Body, ContentType: resp.Header.Get("Content-Type"), Size: resp.ContentLength, FileName: name}, nil
}

// Check 执行渠道连通性检查。
func (e *Executor) Check(ctx context.Context, rt *provider.ChannelRuntime) (*provider.CheckResult, error) {
	if rt == nil {
		return nil, terminalErr(codeInvalidSnapshot, "缺少渠道运行时", nil)
	}
	o, err := e.newOperation(ctx, rt, nil, provider.TaskRef{})
	if err != nil {
		return nil, err
	}
	if !e.loader.mayHave(rt.Plugin.SHA256, pluginproto.HookBuildCheckRequest) {
		return nil, provider.ErrCheckUnsupported
	}
	if o.needsSecretUpfront() {
		if _, err := o.loadSecret(ctx); err != nil {
			return nil, err
		}
	}
	start := time.Now()
	hc, err := o.buildCtx(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := o.call(ctx, pluginproto.HookBuildCheckRequest, hc)
	if err != nil {
		return nil, err
	}
	req, err := o.buildRequest(raw)
	if err != nil {
		return nil, err
	}
	if _, err := o.execute(ctx, req, "check"); err != nil {
		return &provider.CheckResult{Message: err.Error(), DurationMs: time.Since(start).Milliseconds()}, err
	}
	return &provider.CheckResult{OK: true, Message: "连通性检查成功", DurationMs: time.Since(start).Milliseconds()}, nil
}

// Import 从渠道导入模型草稿。
func (e *Executor) Import(ctx context.Context, rt *provider.ChannelRuntime, args map[string]any) ([]provider.ModelDraft, error) {
	if rt == nil {
		return nil, terminalErr(codeInvalidSnapshot, "缺少渠道运行时", nil)
	}
	o, err := e.newOperation(ctx, rt, nil, provider.TaskRef{})
	if err != nil {
		return nil, err
	}
	if !e.loader.mayHave(rt.Plugin.SHA256, pluginproto.HookBuildImportRequest) ||
		!e.loader.mayHave(rt.Plugin.SHA256, pluginproto.HookParseImportResponse) {
		return nil, provider.ErrImportUnsupported
	}
	if o.needsSecretUpfront() {
		if _, err := o.loadSecret(ctx); err != nil {
			return nil, err
		}
	}
	hc, err := o.buildCtx(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := o.call(ctx, pluginproto.HookBuildImportRequest, hc, args)
	if err != nil {
		return nil, err
	}
	req, err := o.buildRequest(raw)
	if err != nil {
		return nil, err
	}
	resp, err := o.execute(ctx, req, "import")
	if err != nil {
		return nil, err
	}
	raw, err = o.call(ctx, pluginproto.HookParseImportResponse, hc, resp, args)
	if err != nil {
		return nil, err
	}
	// 契约里草稿的字段是驼峰（upstreamModel / inputSchema），与 provider.ModelDraft 的蛇形 JSON 标签不同，
	// 直接解码会让这两个字段静默丢失，所以先按契约解码再转换。
	var wire []struct {
		UpstreamModel string               `json:"upstreamModel"`
		Kind          string               `json:"kind"`
		Label         string               `json:"label"`
		Params        map[string]any       `json:"params"`
		InputSchema   modelcfg.InputSchema `json:"inputSchema"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, pluginFaultf("导入模型结果不是数组：%s", err.Error())
	}
	drafts := make([]provider.ModelDraft, 0, len(wire))
	for _, w := range wire {
		drafts = append(drafts, provider.ModelDraft{
			UpstreamModel: w.UpstreamModel, Kind: w.Kind, Label: w.Label, Params: w.Params, InputSchema: w.InputSchema,
		})
	}
	for i := range drafts {
		if issues := modelcfg.ValidateInputSchema(drafts[i].InputSchema); len(issues) > 0 {
			return nil, pluginFaultf("导入模型 %d 的 inputSchema 不合规", i+1)
		}
	}
	return drafts, nil
}

// DryRun 执行请求构造钩子但不发送网络请求，返回脱敏后的请求描述。
func (e *Executor) DryRun(ctx context.Context, snap *provider.Snapshot, input map[string]any) (any, error) {
	if snap == nil {
		return nil, terminalErr(codeInvalidSnapshot, "缺少任务配置快照", nil)
	}
	o, err := e.newOperation(ctx, snap.Runtime(), &snap.Model, provider.TaskRef{})
	if err != nil {
		return nil, err
	}
	o.dryRun = true
	o.setInput(input)
	if err := o.validateInput(ctx); err != nil {
		return nil, err
	}
	hc, err := o.buildCtx(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := o.require(ctx, pluginproto.HookBuildSubmitRequest, hc)
	if err != nil {
		return nil, err
	}
	req, err := o.buildRequest(raw)
	if err != nil {
		return nil, err
	}
	return e.describeRequest(req), nil
}

func (o *operation) validateInput(ctx context.Context) error {
	if o.model == nil {
		return nil
	}
	_, issues := modelcfg.ValidateInput(o.model.InputSchema, o.input)
	if len(issues) > 0 {
		return terminalErr(codeInvalidInput, "任务输入不符合模型 input_schema", nil)
	}
	for _, id := range o.media {
		if o.e.opts.Assets == nil {
			return terminalErr(codeHostMisconfigured, "宿主未配置素材存储", nil)
		}
		if _, err := o.e.opts.Assets.Get(ctx, o.task.UserID, id); err != nil {
			return terminalErr(codeAssetUnavailable, "任务输入素材不可用", redactError(err, o.red.str))
		}
	}
	return nil
}

func (o *operation) prepare(ctx context.Context) (json.RawMessage, error) {
	hc, err := o.buildCtx(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := o.call(ctx, pluginproto.HookBuildPrepareRequests, hc)
	if errors.Is(err, errHookMissing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var descs []json.RawMessage
	if err := json.Unmarshal(raw, &descs); err != nil || len(descs) > 8 {
		return nil, pluginFault("准备请求必须是最多 8 个请求描述组成的数组")
	}
	resps := make([]any, 0, len(descs))
	for _, raw := range descs {
		req, err := o.buildRequest(raw)
		if err != nil {
			return nil, err
		}
		resp, err := o.execute(ctx, req, "prepare")
		if err != nil {
			return nil, err
		}
		resps = append(resps, resp)
	}
	raw, err = o.require(ctx, pluginproto.HookParsePrepareResponse, hc, resps)
	if err != nil {
		return nil, err
	}
	if len(raw) > pluginproto.MaxStateBytes {
		return nil, pluginFault("准备阶段结果超过 64KB")
	}
	return raw, nil
}

type hookResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body,omitempty"`
	Text    string            `json:"text,omitempty"`
	Asset   *assetResponse    `json:"asset,omitempty"`
}

type assetResponse struct {
	ID   uint64 `json:"id"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

func (o *operation) execute(ctx context.Context, b *builtRequest, phase string) (hookResponse, error) {
	requestCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	release, err := o.e.limit(requestCtx, o.rt.Channel)
	if err != nil {
		return hookResponse{}, retryableErr("", "渠道限流等待被取消", err)
	}
	defer release()
	req, cleanup, err := o.httpRequest(requestCtx, b)
	if err != nil {
		return hookResponse{}, err
	}
	defer cleanup()
	resp, err := o.e.client(o.hosts, o.rt.Channel.TrustedInternal).Do(req)
	if err != nil {
		return hookResponse{}, o.requestError(phase, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, o.e.opts.MaxResponseBytes+1))
		if readErr != nil {
			return hookResponse{}, o.requestError(phase, readErr)
		}
		h := responseFromBytes(resp, raw, responseJSON)
		return hookResponse{}, o.classifyHTTPError(ctx, h)
	}
	if b.responseType == responseBinary {
		if o.e.opts.Saver == nil {
			return hookResponse{}, terminalErr(codeHostMisconfigured, "宿主未配置生成素材存储", nil)
		}
		asset, url, err := o.e.opts.Saver.SaveGenerated(requestCtx, provider.SaveGeneratedInput{
			UserID: o.task.UserID, TaskID: o.task.ID, Kind: o.modelKind(),
			MimeType: resp.Header.Get("Content-Type"), FileName: fileNameFromDisposition(resp.Header.Get("Content-Disposition")),
			Body: resp.Body, MaxBytes: o.e.opts.MaxDownloadBytes,
		})
		if err != nil {
			return hookResponse{}, retryableErr("", "保存二进制响应失败", redactError(err, o.red.str))
		}
		o.asset = &provider.Output{Type: provider.OutputAsset, AssetID: asset.ID, AssetURL: url, MediaType: o.modelKind(), Mime: asset.MimeType, DurationMs: asset.DurationMs, Width: asset.Width, Height: asset.Height}
		return hookResponse{Status: resp.StatusCode, Headers: responseHeaders(resp.Header), Asset: &assetResponse{ID: asset.ID, Mime: asset.MimeType, Size: asset.ByteSize}}, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, o.e.opts.MaxResponseBytes+1))
	if err != nil {
		return hookResponse{}, o.requestError(phase, err)
	}
	if int64(len(raw)) > o.e.opts.MaxResponseBytes {
		return hookResponse{}, pluginFault("上游响应体超过大小上限")
	}
	return responseFromBytes(resp, raw, b.responseType), nil
}

func (o *operation) requestError(phase string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) && phase == "submit" {
		return &provider.Error{Class: provider.ClassSubmitUnknown, Code: string(provider.ClassSubmitUnknown), Message: "提交请求结果未知", Cause: redactError(err, o.red.str)}
	}
	return retryableErr("", "上游请求失败", redactError(err, o.red.str))
}

func (o *operation) classifyHTTPError(ctx context.Context, resp hookResponse) error {
	hc, err := o.buildCtx(ctx)
	if err != nil {
		return err
	}
	if o.e.loader.mayHave(o.rt.Plugin.SHA256, pluginproto.HookClassifyError) {
		raw, callErr := o.call(ctx, pluginproto.HookClassifyError, hc, resp)
		if callErr == nil && !isNull(raw) {
			var c struct {
				Class   provider.ErrorClass `json:"class"`
				Code    string              `json:"code"`
				Message string              `json:"message"`
			}
			if json.Unmarshal(raw, &c) == nil && validErrorClass(c.Class) {
				return &provider.Error{Class: c.Class, Code: c.Code, Message: c.Message}
			}
		}
	}
	text := strings.ToLower(resp.Text)
	if strings.Contains(text, "balance") || strings.Contains(text, "insufficient") ||
		strings.Contains(text, "quota") || strings.Contains(text, "credit") ||
		strings.Contains(text, "余额") || strings.Contains(text, "额度") {
		return &provider.Error{Class: provider.ClassProviderBalance, Code: fmt.Sprintf("%s%d", codeHTTPPrefix, resp.Status), Message: "上游余额或额度不足"}
	}
	if resp.Status == http.StatusTooManyRequests || resp.Status >= 500 {
		return retryableErr(fmt.Sprintf("%s%d", codeHTTPPrefix, resp.Status), "上游暂时不可用", nil)
	}
	return terminalErr(fmt.Sprintf("%s%d", codeHTTPPrefix, resp.Status), "上游拒绝请求", nil)
}

func (o *operation) parseSubmitResult(raw json.RawMessage, mode string) (*provider.SubmitResult, error) {
	var v struct {
		ProviderTaskID json.RawMessage `json:"providerTaskId"`
		State          json.RawMessage `json:"state"`
		Immediate      json.RawMessage `json:"immediate"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, pluginFaultf("parseSubmitResponse 返回值不合规：%s", err.Error())
	}
	out := &provider.SubmitResult{State: stateOrNil(v.State)}
	if len(v.ProviderTaskID) > 0 && !isNull(v.ProviderTaskID) {
		var id string
		if err := json.Unmarshal(v.ProviderTaskID, &id); err == nil {
			out.ProviderTaskID = id
		} else {
			var number json.Number
			dec := json.NewDecoder(bytes.NewReader(v.ProviderTaskID))
			dec.UseNumber()
			if err := dec.Decode(&number); err != nil {
				return nil, pluginFault("providerTaskId 必须是字符串或数字")
			}
			out.ProviderTaskID = number.String()
		}
	}
	if !isNull(v.Immediate) {
		q, err := o.parseQueryResult(v.Immediate)
		if err != nil {
			return nil, err
		}
		if q.Status != provider.StatusSucceeded && q.Status != provider.StatusFailed {
			return nil, pluginFault("sync endpoint 的 immediate.status 只能是 succeeded 或 failed")
		}
		out.Immediate = q
	}
	if len(out.State) > pluginproto.MaxStateBytes {
		return nil, pluginFault("插件 state 超过 64KB")
	}
	if mode == pluginmeta.ModeAsync && out.Immediate == nil && out.ProviderTaskID == "" {
		return nil, pluginFault("async endpoint 必须返回 providerTaskId 或 immediate")
	}
	if mode == pluginmeta.ModeSync && out.Immediate == nil {
		return nil, pluginFault("sync endpoint 必须返回 immediate")
	}
	return out, nil
}

func (o *operation) parseQueryResult(raw json.RawMessage) (*provider.QueryResult, error) {
	var v struct {
		Status   string            `json:"status"`
		Progress *float64          `json:"progress"`
		Outputs  []provider.Output `json:"outputs"`
		Error    *struct {
			Class   provider.ErrorClass `json:"class"`
			Code    string              `json:"code"`
			Message string              `json:"message"`
		} `json:"error"`
		State        json.RawMessage `json:"state"`
		ProviderCost *float64        `json:"providerCost"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, pluginFaultf("统一结果不合规：%s", err.Error())
	}
	switch v.Status {
	case provider.StatusQueued, provider.StatusRunning, provider.StatusSucceeded, provider.StatusFailed:
	default:
		return nil, pluginFault("统一结果的 status 不合法")
	}
	if v.Progress != nil {
		if *v.Progress < 0 {
			*v.Progress = 0
		}
		if *v.Progress > 100 {
			*v.Progress = 100
		}
	}
	out := &provider.QueryResult{Status: v.Status, Progress: floatProgress(v.Progress), Outputs: v.Outputs, State: stateOrNil(v.State), ProviderCost: v.ProviderCost}
	if len(out.State) > pluginproto.MaxStateBytes {
		return nil, pluginFault("统一结果 state 超过 64KB")
	}
	if v.Status == provider.StatusFailed {
		if v.Error == nil || !validErrorClass(v.Error.Class) {
			return nil, pluginFault("failed 结果必须包含合法 error")
		}
		out.ErrorClass, out.ErrorCode, out.ErrorMessage = v.Error.Class, v.Error.Code, v.Error.Message
	}
	if v.Status == provider.StatusSucceeded {
		if len(v.Outputs) == 0 {
			return nil, pluginFault("succeeded 结果必须包含产物")
		}
		if err := o.validateOutputs(v.Outputs); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (o *operation) validateOutputs(outputs []provider.Output) error {
	for i := range outputs {
		out := &outputs[i]
		if out.MediaType == "" {
			out.MediaType = o.modelKind()
		}
		switch out.Type {
		case provider.OutputText:
			if o.modelKind() != modelcfg.KindText {
				return pluginFaultf("第 %d 个 text 产物不属于文本模型", i+1)
			}
			if len(out.Text) > 256<<10 {
				return pluginFault("文本产物超过 256KB")
			}
		case provider.OutputURL:
			if o.modelKind() == modelcfg.KindText || out.URL == "" {
				return pluginFault("url 产物不合规")
			}
			u, err := url.Parse(out.URL)
			if err != nil {
				return pluginFault("url 产物地址不合法")
			}
			if err := netguard.CheckURLAllowed(o.hosts, u.Scheme, u.Hostname(), u.User != nil); err != nil {
				return &provider.Error{Class: provider.ClassTerminal, Code: provider.CodeSSRFBlocked, Message: "产物地址不允许", Cause: redactError(err, o.red.str), PluginFault: true}
			}
		case provider.OutputAsset:
			if o.asset == nil || out.AssetID == 0 {
				return pluginFault("asset 产物没有对应的二进制响应")
			}
			out.AssetID, out.AssetURL = o.asset.AssetID, o.asset.AssetURL
			out.Mime, out.MediaType = o.asset.Mime, o.asset.MediaType
			out.DurationMs, out.Width, out.Height = o.asset.DurationMs, o.asset.Width, o.asset.Height
		default:
			return pluginFault("产物 type 不合法")
		}
	}
	return nil
}

func (o *operation) modelKind() string {
	if o.model == nil {
		return ""
	}
	return o.model.Kind
}

// limit 按渠道 key 取限流器并占用名额，参数取快照里冻结的 rate_limit（rps、max_concurrency，0 表示不限）。
// 以渠道而不是主机名为单位：同一个网关下的多个渠道各有各的配额，同一渠道的所有请求（提交、轮询、准备阶段）共享一份。
func (e *Executor) limit(ctx context.Context, ch provider.ChannelSnapshot) (func(), error) {
	return e.limiters.get(ch.Key, ch.RateLimit.RPS, ch.RateLimit.MaxConcurrency).acquire(ctx)
}

// client 返回绑定白名单的客户端；底层 Transport 按 trusted 维度缓存复用。
func (e *Executor) client(allowed []string, trusted bool) *http.Client {
	return netguard.NewClient(e.transport(trusted), allowed, e.opts.MaxRedirects)
}

// transport 懒创建并缓存带 SSRF 拨号校验的 Transport。
func (e *Executor) transport(trusted bool) *http.Transport {
	idx := 0
	if trusted {
		idx = 1
	}
	e.transportMu.Lock()
	defer e.transportMu.Unlock()
	if e.transports[idx] == nil {
		cfg := &netguard.Config{IPAllowed: e.opts.IPAllowed, Resolver: e.opts.Resolver, Dial: e.opts.Dial, MaxRedirects: e.opts.MaxRedirects}
		if trusted && cfg.IPAllowed == nil {
			cfg.IPAllowed = func(net.IP) bool { return true }
		}
		cfg.ApplyDefaults()
		e.transports[idx] = netguard.NewTransport(cfg)
	}
	return e.transports[idx]
}

func responseHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, values := range h {
		if len(values) > 0 {
			out[strings.ToLower(k)] = values[0]
		}
	}
	return out
}

func responseFromBytes(resp *http.Response, raw []byte, typ string) hookResponse {
	out := hookResponse{Status: resp.StatusCode, Headers: responseHeaders(resp.Header), Text: truncate(string(raw), pluginproto.MaxPayloadBytes)}
	switch typ {
	case responseJSON:
		if json.Valid(raw) {
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			_ = dec.Decode(&out.Body)
		}
	case responseText:
		out.Body = string(raw)
	}
	return out
}

func floatProgress(v *float64) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

func stateOrNil(raw json.RawMessage) json.RawMessage {
	if isNull(raw) {
		return nil
	}
	return raw
}

func validErrorClass(c provider.ErrorClass) bool {
	switch c {
	case provider.ClassRetryable, provider.ClassTerminal, provider.ClassModeration, provider.ClassProviderBalance:
		return true
	default:
		return false
	}
}

func fileNameFromDisposition(raw string) string {
	_, params, err := mime.ParseMediaType(raw)
	if err == nil && params["filename"] != "" {
		return params["filename"]
	}
	return ""
}

func (e *Executor) describeRequest(b *builtRequest) any {
	out := map[string]any{"method": b.method, "url": b.url.String(), "headers": b.headers, "responseType": b.responseType}
	switch b.bodyKind {
	case bodyJSON:
		out["json"] = b.jsonBody
	case bodyForm:
		fields := map[string]string{}
		for _, f := range b.formBody {
			fields[f.name] = f.value
		}
		out["form"] = fields
	case bodyMultipart:
		out["multipart"] = map[string]any{"fields": b.formBody, "parts": b.parts}
	}
	return out
}

func (o *operation) resolveRef(ctx context.Context, ref fileRef) (any, error) {
	if o.e.opts.Assets == nil {
		return nil, terminalErr(codeHostMisconfigured, "宿主未配置素材存储", nil)
	}
	id, ok := o.media[ref.field]
	if !ok {
		return nil, pluginFaultf("文件引用 input:%s 不是任务输入中的媒体字段", ref.field)
	}
	f, err := o.e.opts.Assets.Open(ctx, o.task.UserID, id)
	if err != nil {
		return nil, terminalErr(codeAssetUnavailable, "打开任务输入素材失败", redactError(err, o.red.str))
	}
	defer f.Body.Close()
	switch ref.as {
	case "url":
		return f.URL, nil
	case "base64", "dataUrl":
		data, err := io.ReadAll(io.LimitReader(f.Body, o.e.opts.MaxInlineBytes+1))
		if err != nil {
			return nil, retryableErr(codeAssetUnavailable, "读取任务输入素材失败", redactError(err, o.red.str))
		}
		if int64(len(data)) > o.e.opts.MaxInlineBytes {
			return nil, terminalErr(codeMediaTooLarge, "内联素材超过大小上限", nil)
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		if ref.as == "dataUrl" {
			return "data:" + f.Asset.MimeType + ";base64," + encoded, nil
		}
		return encoded, nil
	default:
		return nil, pluginFault("文件引用 as 不合法")
	}
}

func (o *operation) httpRequest(ctx context.Context, b *builtRequest) (*http.Request, func(), error) {
	var body io.Reader
	var contentType string
	var cleanup = func() {}
	switch b.bodyKind {
	case bodyJSON:
		tree, err := o.resolveTree(ctx, b.jsonBody)
		if err != nil {
			return nil, cleanup, err
		}
		raw, err := json.Marshal(tree)
		if err != nil {
			return nil, cleanup, pluginFault("请求 JSON 编码失败")
		}
		body, contentType = bytes.NewReader(raw), "application/json"
	case bodyForm:
		values := url.Values{}
		for _, f := range b.formBody {
			v := f.value
			if f.ref != nil {
				resolved, err := o.resolveRef(ctx, *f.ref)
				if err != nil {
					return nil, cleanup, err
				}
				v = fmt.Sprint(resolved)
			}
			values.Set(f.name, v)
		}
		body, contentType = strings.NewReader(values.Encode()), "application/x-www-form-urlencoded"
	case bodyMultipart:
		pr, pw := io.Pipe()
		mw := multipart.NewWriter(pw)
		done := make(chan error, 1)
		go func() {
			err := func() error {
				for _, f := range b.formBody {
					v := f.value
					if f.ref != nil {
						resolved, err := o.resolveRef(ctx, *f.ref)
						if err != nil {
							return err
						}
						v = fmt.Sprint(resolved)
					}
					if err := mw.WriteField(f.name, v); err != nil {
						return err
					}
				}
				for _, p := range b.parts {
					if err := o.writePart(ctx, mw, p); err != nil {
						return err
					}
				}
				return mw.Close()
			}()
			_ = pw.CloseWithError(err)
			done <- err
		}()
		body, contentType = pr, mw.FormDataContentType()
		cleanup = func() {
			_ = pr.Close()
			select {
			case <-done:
			default:
			}
		}
	default:
		body = nil
	}
	req, err := http.NewRequestWithContext(ctx, b.method, b.url.String(), body)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	for k, v := range b.headers {
		req.Header.Set(k, v)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if err := o.injectAuth(req, b); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	return req, cleanup, nil
}

func (o *operation) injectAuth(req *http.Request, b *builtRequest) error {
	if b.auth.Type == pluginmeta.AuthNone || b.auth.Type == pluginmeta.AuthCustom {
		return nil
	}
	key, err := o.loadSecret(req.Context())
	if err != nil {
		return err
	}
	switch b.auth.Type {
	case pluginmeta.AuthBearer:
		req.Header.Set("Authorization", "Bearer "+key)
	case pluginmeta.AuthHeader:
		req.Header.Set(b.auth.Name, key)
	case pluginmeta.AuthQuery:
		q := req.URL.Query()
		q.Set(b.auth.Name, key)
		req.URL.RawQuery = q.Encode()
	default:
		return pluginFault("鉴权类型不合法")
	}
	return nil
}

func (o *operation) resolveTree(ctx context.Context, v any) (any, error) {
	switch t := v.(type) {
	case *fileRef:
		return o.resolveRef(ctx, *t)
	case []any:
		out := make([]any, len(t))
		for i := range t {
			v, err := o.resolveTree(ctx, t[i])
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, v := range t {
			resolved, err := o.resolveTree(ctx, v)
			if err != nil {
				return nil, err
			}
			out[k] = resolved
		}
		return out, nil
	default:
		return v, nil
	}
}

func (o *operation) writePart(ctx context.Context, mw *multipart.Writer, p partSpec) error {
	if o.e.opts.Assets == nil {
		return terminalErr(codeHostMisconfigured, "宿主未配置素材存储", nil)
	}
	id := o.media[p.field]
	f, err := o.e.opts.Assets.Open(ctx, o.task.UserID, id)
	if err != nil {
		return terminalErr(codeAssetUnavailable, "打开任务输入素材失败", redactError(err, o.red.str))
	}
	defer f.Body.Close()
	filename := p.filename
	if filename == "" {
		filename = f.Asset.FileName
	}
	if filename == "" {
		filename = "upload"
	}
	part, err := mw.CreateFormFile(p.name, filename)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, f.Body)
	return err
}
