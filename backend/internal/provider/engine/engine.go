// Package engine 是 provider.Executor 的唯一实现：通用声明式引擎。
//
// 它按快照里冻结的 Provider / Model 配置执行：渲染请求 → 发 HTTP → 用表达式提取字段 → 映射状态，
// 不含任何平台专属代码。安全边界（域名白名单、内网 IP 拦截、DNS rebinding 防护、响应体上限、超时、限流）
// 都在这里，见 guard.go。凭证只通过 SecretResolver 在鉴权环节取用，不进表达式上下文，不进日志和 Trace。
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
)

// 默认值。
const (
	DefaultOperationTimeout = 30 * time.Second
	DefaultMaxResponseBytes = 5 << 20 // 5MB：平台 API 响应的大小上限
	DefaultMaxDownloadBytes = 2 << 30 // 2GiB：结果文件下载的大小上限
	DefaultDownloadTimeout  = 15 * time.Minute
	DefaultMaxRedirects     = 3
)

// Options 是引擎的构造参数。
type Options struct {
	Secrets provider.SecretResolver // 必填：解析凭证明文
	Assets  provider.AssetStore     // 提交带媒体输入的任务时必填

	Now func() time.Time // 可选，默认 time.Now，测试里用来固定 ctx.now

	// 以下是 SSRF 防护的注入点。生产环境全部留空：默认拒绝内网、使用系统 DNS。

	// AllowPrivate 为 true 时放行所有 IP（含回环 / 内网），只用于测试里让 httptest 服务器落在 127.0.0.1。
	AllowPrivate bool
	// IPCheck 自定义 IP 校验，返回 true 表示允许连接；设置后优先于默认规则（也优先于 AllowPrivate）。
	IPCheck func(net.IP) bool
	// Resolver 自定义域名解析，测试里用来模拟 DNS rebinding。
	Resolver Resolver
	// DialContext 自定义底层拨号（收到的地址一定是已经校验过的 IP:端口），测试里用来记录拨号目标。
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)

	DefaultTimeout   time.Duration // 操作默认超时，默认 30s；operation.timeout 优先
	MaxResponseBytes int64         // 平台 API 响应体上限，默认 5MB
	MaxDownloadBytes int64         // 结果下载上限，默认 2GiB
	DownloadTimeout  time.Duration // 结果下载总超时，默认 15 分钟
	MaxRedirects     int           // 最大重定向次数，默认 3
}

func (o *Options) applyDefaults() {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.DefaultTimeout <= 0 {
		o.DefaultTimeout = DefaultOperationTimeout
	}
	if o.MaxResponseBytes <= 0 {
		o.MaxResponseBytes = DefaultMaxResponseBytes
	}
	if o.MaxDownloadBytes <= 0 {
		o.MaxDownloadBytes = DefaultMaxDownloadBytes
	}
	if o.DownloadTimeout <= 0 {
		o.DownloadTimeout = DefaultDownloadTimeout
	}
	if o.MaxRedirects <= 0 {
		o.MaxRedirects = DefaultMaxRedirects
	}
}

type engine struct {
	opts      Options
	transport http.RoundTripper
	limiters  *limiterSet
}

// New 创建引擎。返回值可被多个 goroutine 并发使用；引擎无状态，只缓存连接池和每个平台的限流器。
func New(opts Options) provider.Executor {
	opts.applyDefaults()
	cfg := &guardConfig{
		resolver:     opts.Resolver,
		dial:         opts.DialContext,
		maxRedirects: opts.MaxRedirects,
	}
	switch {
	case opts.IPCheck != nil:
		cfg.ipAllowed = opts.IPCheck
	case opts.AllowPrivate:
		cfg.ipAllowed = func(net.IP) bool { return true }
	}
	cfg.applyDefaults()
	return &engine{opts: opts, transport: newGuardTransport(cfg), limiters: newLimiterSet()}
}

// ---------- 上下文构造 ----------

// baseContext 构造表达式上下文里各阶段共用的部分。
func (e *engine) baseContext(snap *dsl.Snapshot, input, files map[string]any, task provider.TaskRef, webhookURL string, mapping any) *dsl.RenderContext {
	params := snap.Model.Params
	if params == nil {
		params = map[string]any{}
	}
	if input == nil {
		input = map[string]any{}
	}
	if files == nil {
		files = map[string]any{}
	}
	now := e.opts.Now()
	var webhook any // 没有回调地址时是 nil：body 里的 ${ ctx.webhook_url } 会渲染成 null
	if webhookURL != "" {
		webhook = webhookURL
	}
	return &dsl.RenderContext{
		Input: input,
		Files: files,
		Model: map[string]any{"params": params, "mapping": mapping},
		Task:  map[string]any{"id": task.ID, "provider_task_id": task.ProviderTaskID},
		Ctx: map[string]any{
			"webhook_url": webhook,
			"now":         now.UTC().Format(time.RFC3339),
			"now_unix":    now.Unix(),
		},
	}
}

func checkSnapshot(snap *dsl.Snapshot) error {
	if snap == nil {
		return newErr(provider.ClassTerminal, "", "任务缺少配置快照", nil)
	}
	return nil
}

// decodeJSON 解码响应 JSON：整数保持 int（大整数不丢精度），其余数字 float64。
func decodeJSON(raw []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return dsl.NormalizeJSON(v), nil
}

// ---------- Submit ----------

// Submit 提交任务：先处理媒体输入（上传或取签名 URL），再渲染 mapping 与 submit 请求，发送并提取平台任务 id。
func (e *engine) Submit(ctx context.Context, snap *dsl.Snapshot, in provider.SubmitInput) (string, error) {
	// 1. 快照与提交操作必须存在
	if err := checkSnapshot(snap); err != nil {
		return "", err
	}
	p := &snap.Provider
	if p.Operations.Submit == nil {
		return "", newErr(provider.ClassTerminal, "", "平台没有配置提交操作", nil)
	}

	// 2. 防御性再校验一次输入（API 层已经校验过）。规范化后媒体字段是 uint64 的 asset id
	input, ferrs := dsl.ValidateInput(snap.Model.InputSchema, in.Input)
	if len(ferrs) > 0 {
		return "", newErr(provider.ClassTerminal, "invalid_input", "任务输入不合法："+ferrs[0].Message, nil)
	}

	// 3. 处理媒体字段：平台有 upload 操作就上传并取回引用，否则用自有存储的签名 URL
	files, err := e.resolveMedia(ctx, snap, in, input)
	if err != nil {
		return "", err
	}

	// 4. mapping 先按 {input, files, model.params} 渲染，结果作为 model.mapping 供 submit 模板使用
	rc := e.baseContext(snap, input, files, in.Task, in.WebhookURL, nil)
	mapping, err := dsl.RenderValue(snap.Model.Mapping, rc)
	if err != nil {
		return "", newErr(provider.ClassTerminal, "", "渲染 mapping 失败："+err.Error(), nil)
	}
	rc.Model["mapping"] = mapping

	// 5. 发送 submit 请求
	ex, err := e.exec(ctx, execParams{provider: p, name: "submit", step: "submit", op: p.Operations.Submit, rc: rc, submit: true})
	if err != nil {
		return "", err
	}
	defer ex.rec.done()

	// 6. 提取平台任务 id：拿不到说明 success 判定与提取路径对不上，属于配置问题，按 terminal 处理
	idExpr := p.Operations.Submit.Extract["provider_task_id"]
	v, err := dsl.EvalExpr(idExpr, ex.RC)
	if err != nil {
		ex.rec.fail(err.Error())
		return "", newErr(provider.ClassTerminal, "", "提取 provider_task_id 失败："+err.Error(), nil)
	}
	ex.rec.extract("provider_task_id", v)
	id := dsl.Stringify(v)
	if id == "" {
		msg := "提交成功但未能提取到平台任务 id：" + snippet(ex.Result.Raw)
		ex.rec.fail(msg)
		return "", newErr(provider.ClassTerminal, "", msg, nil)
	}
	return id, nil
}

// resolveMedia 处理输入里的所有媒体字段，返回 files（字段名 -> 上传引用或签名 URL）。
func (e *engine) resolveMedia(ctx context.Context, snap *dsl.Snapshot, in provider.SubmitInput, input map[string]any) (map[string]any, error) {
	files := map[string]any{}
	names := dsl.MediaFieldNames(snap.Model.InputSchema)
	if len(names) == 0 {
		return files, nil
	}
	p := &snap.Provider
	for _, name := range names {
		v, ok := input[name]
		if !ok {
			continue // 可选媒体字段没有填
		}
		id, _ := v.(uint64)
		if e.opts.Assets == nil {
			return nil, newErr(provider.ClassTerminal, "", "引擎未配置素材存储", nil)
		}
		// 素材归属由 AssetStore 校验（asset.user_id == task.user_id）
		f, err := e.opts.Assets.Open(ctx, in.Task.UserID, id)
		if err != nil {
			return nil, newErr(provider.ClassTerminal, "asset_unavailable", fmt.Sprintf("素材 %d 无法读取：%v", id, err), nil)
		}
		if p.Operations.Upload == nil {
			_ = f.Body.Close()
			if f.URL == "" {
				return nil, newErr(provider.ClassTerminal, "asset_unavailable", fmt.Sprintf("素材 %d 没有可供平台访问的地址", id), nil)
			}
			files[name] = f.URL
			continue
		}
		ref, err := e.upload(ctx, snap, in, input, name, f)
		if err != nil {
			return nil, err
		}
		files[name] = ref
	}
	return files, nil
}

// upload 用 provider 的 upload 操作上传一份素材，返回 extract.ref 提取出的引用。
func (e *engine) upload(ctx context.Context, snap *dsl.Snapshot, in provider.SubmitInput, input map[string]any, field string, f *provider.AssetFile) (any, error) {
	defer f.Body.Close()
	p := &snap.Provider
	op := p.Operations.Upload

	fileName, mimeType, kind := "file", "application/octet-stream", ""
	if f.Asset != nil {
		if f.Asset.FileName != "" {
			fileName = f.Asset.FileName
		}
		if f.Asset.MimeType != "" {
			mimeType = f.Asset.MimeType
		}
		kind = f.Asset.Kind
	}
	rc := e.baseContext(snap, input, nil, in.Task, in.WebhookURL, nil)
	rc.Upload = map[string]any{"field": field, "kind": kind, "file_name": fileName, "mime_type": mimeType}

	ex, err := e.exec(ctx, execParams{
		provider: p, name: "upload", step: "upload:" + field, op: op, rc: rc,
		file: &uploadPart{FileName: fileName, MimeType: mimeType, Body: f.Body},
	})
	if err != nil {
		return nil, err
	}
	defer ex.rec.done()

	ref, err := dsl.EvalExpr(op.Extract["ref"], ex.RC)
	if err != nil {
		ex.rec.fail(err.Error())
		return nil, newErr(provider.ClassTerminal, "", "提取上传引用失败："+err.Error(), nil)
	}
	ex.rec.extract("ref", ref)
	if ref == nil || dsl.Stringify(ref) == "" {
		msg := "上传成功但未能提取到文件引用：" + snippet(ex.Result.Raw)
		ex.rec.fail(msg)
		return nil, newErr(provider.ClassTerminal, "", msg, nil)
	}
	return ref, nil
}

// ---------- Query ----------

// Query 查询一次任务：映射成统一状态；succeeded 时对产物执行 model.output.select。
func (e *engine) Query(ctx context.Context, snap *dsl.Snapshot, task provider.TaskRef) (*provider.QueryResult, error) {
	// 1. 快照、查询操作、平台任务 id 都必须存在
	if err := checkSnapshot(snap); err != nil {
		return nil, err
	}
	p := &snap.Provider
	op := p.Operations.Query
	if op == nil {
		return nil, newErr(provider.ClassTerminal, "", "平台没有配置查询操作", nil)
	}
	if task.ProviderTaskID == "" {
		return nil, newErr(provider.ClassTerminal, "", "任务还没有平台任务 id，无法查询", nil)
	}

	// 2. 渲染并发送查询请求。失败（HTTP 层）由 error_rules 分类后作为错误返回，由 worker 决定重试还是失败
	rc := e.baseContext(snap, nil, nil, task, "", nil)
	ex, err := e.exec(ctx, execParams{provider: p, name: "query", step: "query", op: op, rc: rc})
	if err != nil {
		return nil, err
	}
	defer ex.rec.done()

	// 3. 提取状态并映射成统一状态。平台状态不在 status_map 里时用 _default，并告警
	statusVal, err := dsl.EvalExpr(op.Extract["status"], ex.RC)
	if err != nil {
		ex.rec.fail(err.Error())
		return nil, newErr(provider.ClassTerminal, "", "提取 status 失败："+err.Error(), nil)
	}
	ex.rec.extract("status", statusVal)
	rawStatus := dsl.Stringify(statusVal)
	status, usedDefault := dsl.MapStatus(p, rawStatus)
	if usedDefault {
		logger.Warn("平台状态不在 status_map 里，按 _default 处理",
			zap.String("provider", p.Key), zap.String("status", rawStatus), zap.String("mapped", status))
	}

	res := &provider.QueryResult{Status: status}
	res.Progress = e.extractProgress(op, ex)
	res.ProviderCost = e.extractCost(op, ex)
	res.ErrorCode = e.extractString(op, ex, "error_code")
	res.ErrorMessage = e.extractString(op, ex, "error_message")

	switch status {
	case provider.StatusSucceeded:
		// 4. 成功：提取产物并按 output.select 筛选
		outputs, err := e.selectOutputs(snap, op, ex)
		if err != nil {
			ex.rec.fail(err.Error())
			return nil, err
		}
		if len(outputs) == 0 {
			// 平台说成功但没有可用产物：交给 worker 当失败处理比“成功但什么都没有”更安全
			res.Status = provider.StatusFailed
			res.ErrorClass = provider.ClassTerminal
			if res.ErrorMessage == "" {
				res.ErrorMessage = "平台返回成功，但没有匹配 output.select 的产物"
			}
			ex.rec.fail(res.ErrorMessage)
			return res, nil
		}
		res.Outputs = outputs
	case provider.StatusFailed:
		// 5. 失败：用 error_rules 给出分类（规则里可以用 resp 里的错误文案）
		res.ErrorClass = e.failedClass(p, ex.RC)
		if res.ErrorMessage == "" {
			res.ErrorMessage = "平台任务失败"
		}
	}
	return res, nil
}

// failedClass 对“任务本身失败”的响应用 error_rules 分类；没有规则命中时按 terminal。
func (e *engine) failedClass(p *dsl.ProviderConfig, rc *dsl.RenderContext) provider.ErrorClass {
	for i, r := range p.ErrorRules {
		ok, err := dsl.EvalBool(r.When, rc)
		if err != nil {
			logger.Warn("error_rules 求值失败，按不匹配处理", zap.String("provider", p.Key), zap.Int("rule", i), zap.Error(err))
			continue
		}
		if ok {
			return provider.ErrorClass(r.Class)
		}
	}
	return provider.ClassTerminal
}

// extractString 求值可选的字符串提取项；没配置、求值失败或为 nil 都得到空串。
func (e *engine) extractString(op *dsl.Operation, ex *exchange, key string) string {
	expr, ok := op.Extract[key]
	if !ok {
		return ""
	}
	v, err := dsl.EvalExpr(expr, ex.RC)
	if err != nil {
		logger.Warn("提取可选字段失败", zap.String("field", key), zap.Error(err))
		return ""
	}
	ex.rec.extract(key, v)
	return dsl.Stringify(v)
}

// extractProgress 提取进度并限制在 0–100；平台不提供或无法解析时为 nil。
func (e *engine) extractProgress(op *dsl.Operation, ex *exchange) *int {
	expr, ok := op.Extract["progress"]
	if !ok {
		return nil
	}
	v, err := dsl.EvalExpr(expr, ex.RC)
	if err != nil || v == nil {
		return nil
	}
	f, ok := toNumber(v)
	if !ok {
		return nil
	}
	n := int(f)
	if n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}
	ex.rec.extract("progress", n)
	return &n
}

// extractCost 提取平台侧成本（只用于对账日志）。
func (e *engine) extractCost(op *dsl.Operation, ex *exchange) *float64 {
	expr, ok := op.Extract["provider_cost"]
	if !ok {
		return nil
	}
	v, err := dsl.EvalExpr(expr, ex.RC)
	if err != nil || v == nil {
		return nil
	}
	f, ok := toNumber(v)
	if !ok {
		return nil
	}
	ex.rec.extract("provider_cost", f)
	return &f
}

// toNumber 把数字或数字字符串转成 float64；nil、布尔与无法解析的值返回 false。
func toNumber(v any) (float64, bool) {
	switch v.(type) {
	case nil, bool:
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(dsl.Stringify(v)), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// selectOutputs 提取 outputs，按 model.output.select 筛选并转成 []provider.Output。
func (e *engine) selectOutputs(snap *dsl.Snapshot, op *dsl.Operation, ex *exchange) ([]provider.Output, error) {
	expr := op.Extract["outputs"]
	raw, err := dsl.EvalExpr(expr, ex.RC)
	if err != nil {
		return nil, newErr(provider.ClassTerminal, "", "提取 outputs 失败："+err.Error(), nil)
	}
	list, _ := raw.([]any)
	ex.rec.extract("outputs", raw)

	// select 缺省时取全部产物
	sel := strings.TrimSpace(snap.Model.Output.Select)
	filtered := any(list)
	if sel != "" {
		rc := *ex.RC
		rc.Outputs = list
		rc.Model = map[string]any{"params": snap.Model.Params}
		filtered, err = dsl.EvalExpr(sel, &rc)
		if err != nil {
			return nil, newErr(provider.ClassTerminal, "", "执行 output.select 失败："+err.Error(), nil)
		}
	}
	items, ok := filtered.([]any)
	if !ok && filtered != nil {
		return nil, newErr(provider.ClassTerminal, "", "output.select 的结果必须是数组", nil)
	}
	var out []provider.Output
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		o := provider.Output{URL: dsl.Stringify(m["url"]), Type: dsl.Stringify(m["type"]), Node: dsl.Stringify(m["node"]), Text: dsl.Stringify(m["text"])}
		if o.URL == "" && o.Text == "" {
			continue
		}
		out = append(out, o)
	}
	return out, nil
}

// ---------- Cancel ----------

// Cancel 尽力取消；平台没有 cancel 操作时返回 provider.ErrCancelUnsupported，由调用方走软取消。
func (e *engine) Cancel(ctx context.Context, snap *dsl.Snapshot, task provider.TaskRef) error {
	if err := checkSnapshot(snap); err != nil {
		return err
	}
	p := &snap.Provider
	op := p.Operations.Cancel
	if op == nil {
		return provider.ErrCancelUnsupported
	}
	rc := e.baseContext(snap, nil, nil, task, "", nil)
	ex, err := e.exec(ctx, execParams{provider: p, name: "cancel", step: "cancel", op: op, rc: rc})
	if err != nil {
		return err
	}
	ex.rec.done()
	return nil
}

// ---------- Download ----------

// Download 下载平台产物。只允许 provider.allowed_hosts，并校验内网 IP、重定向与 DNS rebinding；
// 不带平台鉴权（结果 URL 通常是 CDN 的公开或预签名地址，把平台凭证发过去只会增加泄露面）。
// 返回的 Body 在读取超过 MaxDownloadBytes 时报错；调用方负责 Close。
func (e *engine) Download(ctx context.Context, snap *dsl.Snapshot, rawURL string) (*provider.Download, error) {
	// 1. 校验 URL：协议、域名白名单
	if err := checkSnapshot(snap); err != nil {
		return nil, err
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, newErr(provider.ClassTerminal, "", "下载地址不合法", nil)
	}
	allowed := snap.Provider.AllowedHosts
	if err := checkURLAllowed(allowed, u.Scheme, u.Hostname(), u.User != nil); err != nil {
		return nil, newErr(provider.ClassTerminal, "ssrf_blocked", "下载被安全策略拒绝："+err.Error(), err)
	}

	// 2. 发请求（重定向、DNS 解析后的 IP 由防护层继续校验）
	dctx, cancel := context.WithTimeout(ctx, e.opts.DownloadTimeout)
	req, err := http.NewRequestWithContext(dctx, http.MethodGet, u.String(), nil)
	if err != nil {
		cancel()
		return nil, newErr(provider.ClassTerminal, "", "下载地址不合法", err)
	}
	req.Header.Set("User-Agent", "video-canvas-engine/1")
	resp, err := newClient(e.transport, allowed, e.opts.MaxRedirects).Do(req)
	if err != nil {
		cancel()
		return nil, e.downloadError(err)
	}

	// 3. 状态码：429 / 5xx 可以重试；其余（403 / 404，结果 URL 24 小时后失效）不可重试
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		class := provider.ClassTerminal
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			class = provider.ClassRetryable
		}
		return nil, newErr(class, fmt.Sprintf("http_%d", resp.StatusCode), fmt.Sprintf("下载失败（HTTP %d）", resp.StatusCode), nil)
	}

	// 4. 大小上限：声明的长度超限直接拒绝；未声明或谎报则在读取时截断报错
	max := e.opts.MaxDownloadBytes
	if resp.ContentLength > max {
		_ = resp.Body.Close()
		cancel()
		return nil, newErr(provider.ClassTerminal, "too_large", fmt.Sprintf("结果文件过大（%d 字节，上限 %d）", resp.ContentLength, max), ErrResponseTooLarge)
	}
	return &provider.Download{
		Body:        &limitedBody{rc: resp.Body, remain: max, max: max, cancel: cancel},
		ContentType: resp.Header.Get("Content-Type"),
		Size:        resp.ContentLength,
		FileName:    downloadFileName(resp, u),
	}, nil
}

// downloadError 把下载时的传输错误分类：防护拒绝是 terminal，其余网络错误可重试。
func (e *engine) downloadError(err error) error {
	if isGuardError(err) {
		return newErr(provider.ClassTerminal, "ssrf_blocked", "下载被安全策略拒绝："+err.Error(), err)
	}
	return newErr(provider.ClassRetryable, "", "下载失败："+err.Error(), err)
}

// limitedBody 限制下载体的最大读取量，并在 Close 时释放超时 ctx。
type limitedBody struct {
	rc     io.ReadCloser
	remain int64
	max    int64
	cancel context.CancelFunc
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.remain <= 0 {
		// 已经读满上限：再多读一个字节确认是否真的超限
		var one [1]byte
		if n, _ := b.rc.Read(one[:]); n > 0 {
			return 0, fmt.Errorf("%w（超过 %d 字节）", ErrResponseTooLarge, b.max)
		}
		return 0, io.EOF
	}
	if int64(len(p)) > b.remain {
		p = p[:b.remain]
	}
	n, err := b.rc.Read(p)
	b.remain -= int64(n)
	return n, err
}

func (b *limitedBody) Close() error {
	err := b.rc.Close()
	b.cancel()
	return err
}

// downloadFileName 优先取 Content-Disposition 里的文件名，其次取 URL 路径最后一段。
func downloadFileName(resp *http.Response, u *url.URL) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil && params["filename"] != "" {
			return path.Base(params["filename"])
		}
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" {
		return ""
	}
	return base
}
