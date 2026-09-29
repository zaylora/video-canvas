package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
)

// httpResult 是一次 HTTP 调用的结果。
type httpResult struct {
	Status int
	Raw    []byte
	JSON   any // 响应不是 JSON 时为 nil
}

// transportErr 是传输层错误：请求没有拿到完整响应。connected 表示连接已经建立
// （请求可能已经被平台收到），这是区分“可以安全重试”和“结果未知”的依据。
type transportErr struct {
	err       error
	connected bool
}

func (e *transportErr) Error() string { return e.err.Error() }
func (e *transportErr) Unwrap() error { return e.err }

// exchange 是一次成功（success 表达式为真）的调用结果，带着响应阶段的渲染上下文供后续 extract 使用。
type exchange struct {
	Result *httpResult
	RC     *dsl.RenderContext // 已填入 Resp / Status
	rec    *stepRecorder
}

// execParams 是 exec 的入参。
type execParams struct {
	provider *dsl.ProviderConfig
	name     string // upload / submit / query / cancel
	step     string // Trace 步骤名
	op       *dsl.Operation
	rc       *dsl.RenderContext
	file     *uploadPart // 仅上传
	// submit 为 true 时，连接建立后的失败（读超时、连接被重置）按“结果未知”处理
	submit bool
}

// opLabel 是操作的中文名，用在错误信息里。
var opLabel = map[string]string{"upload": "上传素材", "submit": "提交任务", "query": "查询任务", "cancel": "取消任务"}

// exec 执行一个操作：渲染请求 → 加鉴权 → 限流 → 发送 → 判定 success。
// 失败时返回已分类的 *provider.Error；成功时返回 exchange，调用方继续做 extract（并负责调用 ex.rec.done）。
// 出错路径上 exec 自己会结束 Trace 记录。
func (e *engine) exec(ctx context.Context, p execParams) (*exchange, error) {
	// 1. 取凭证。凭证只在这里取，只用于加鉴权和脱敏，不进表达式上下文，也不进日志
	secret := ""
	if p.provider.Auth.Type != dsl.AuthNone && p.provider.Auth.Type != "" {
		s, err := e.secret(ctx, p.provider.Auth.Secret)
		if err != nil {
			return nil, err
		}
		secret = s
	}
	rec := newStepRecorder(ctx, p.step, secret)
	fail := func(err error) (*exchange, error) {
		rec.fail(err.Error())
		rec.done()
		return nil, err
	}

	// 2. 渲染请求并加鉴权
	b, err := buildRequest(p.provider, p.op, p.rc)
	if err != nil {
		return fail(newErr(provider.ClassTerminal, "", opLabel[p.name]+"：请求模板渲染失败："+redactMsg(err.Error(), secret), nil))
	}
	if err := applyAuth(b, p.provider.Auth, secret); err != nil {
		return fail(newErr(provider.ClassTerminal, "", opLabel[p.name]+"："+err.Error(), nil))
	}

	// 3. 限流 + 发送
	res, terr := e.send(ctx, p, b, secret, rec)
	if terr != nil {
		return fail(e.transportToError(ctx, p, terr, secret))
	}

	// 4. 判定 success：cancel 可以不写 success，此时按 2xx 判断
	rc := *p.rc
	rc.Resp, rc.Status = res.JSON, res.Status
	var ok bool
	if strings.TrimSpace(p.op.Success) == "" {
		ok = res.Status >= 200 && res.Status < 300
	} else if ok, err = dsl.EvalBool(p.op.Success, &rc); err != nil {
		return fail(newErr(provider.ClassTerminal, "", opLabel[p.name]+"：success 表达式求值失败："+redactMsg(err.Error(), secret), nil))
	}
	if !ok {
		return fail(e.classify(p, &rc, res, secret))
	}
	return &exchange{Result: res, RC: &rc, rec: rec}, nil
}

// secret 通过 SecretResolver 取凭证明文；取不到按 terminal 处理（配置问题，重试没有意义）。
func (e *engine) secret(ctx context.Context, name string) (string, error) {
	if e.opts.Secrets == nil {
		return "", newErr(provider.ClassTerminal, "", "引擎未配置凭证解析器", nil)
	}
	s, err := e.opts.Secrets.Get(ctx, name)
	if err != nil || s == "" {
		// 日志里只写凭证的名字，不写任何明文
		logger.Error("读取平台凭证失败", zap.String("secret", name), zap.Error(err))
		return "", newErr(provider.ClassTerminal, "secret_unavailable", fmt.Sprintf("平台凭证 %q 未设置或无法读取", name), nil)
	}
	return s, nil
}

// send 限流后发出请求并读取（带上限的）响应体。
func (e *engine) send(ctx context.Context, p execParams, b *builtRequest, secret string, rec *stepRecorder) (*httpResult, *transportErr) {
	// 1. 限流：并发名额 + 令牌桶。等待也受调用方 ctx 约束
	lim := e.limiters.get(p.provider.Key, p.provider.RateLimit.RPS, p.provider.RateLimit.MaxConcurrency)
	release, err := lim.acquire(ctx)
	if err != nil {
		return nil, &transportErr{err: fmt.Errorf("等待限流失败：%w", err)}
	}
	defer release()

	// 2. 每个操作的超时，默认 30s
	timeout := p.op.Timeout.D()
	if timeout <= 0 {
		timeout = e.opts.DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 3. 用 httptrace 记录“连接是否已建立”，读超时判断要用
	var connected atomic.Bool
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { connected.Store(true) },
	})

	body, err := encodeBody(b, p.file)
	if err != nil {
		return nil, &transportErr{err: err}
	}
	req, err := http.NewRequestWithContext(ctx, b.Method, b.URL.String(), body.reader)
	if err != nil {
		if c, ok := body.reader.(io.Closer); ok { // multipart 管道没被消费时要关掉，否则写入协程会一直阻塞
			_ = c.Close()
		}
		return nil, &transportErr{err: err}
	}
	if body.reader != nil && body.length >= 0 {
		req.ContentLength = body.length
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "video-canvas-engine/1")
	for k, v := range b.Headers {
		req.Header.Set(k, v)
	}
	if body.contentType != "" {
		req.Header.Set("Content-Type", body.contentType)
	}

	rec.request(b.Method, b.URL.String(), flattenHeaders(req.Header), body.preview)

	// 4. 发送。allowed_hosts / 内网 IP / 重定向都由客户端里的防护层校验
	start := time.Now()
	client := newClient(e.transport, p.provider.AllowedHosts, e.opts.MaxRedirects)
	resp, err := client.Do(req)
	if err != nil {
		return nil, &transportErr{err: err, connected: connected.Load()}
	}
	defer resp.Body.Close()

	raw, err := readLimited(resp.Body, e.opts.MaxResponseBytes)
	if err != nil {
		// 响应体读取失败（超时 / 超限）：连接必然已建立
		return nil, &transportErr{err: err, connected: true}
	}
	rec.response(resp.StatusCode, raw)
	logger.Debug("平台调用完成",
		zap.String("provider", p.provider.Key), zap.String("op", p.name),
		zap.String("host", b.URL.Hostname()), zap.String("path", b.URL.Path),
		zap.Int("status", resp.StatusCode), zap.Duration("cost", time.Since(start)))

	res := &httpResult{Status: resp.StatusCode, Raw: raw}
	if len(raw) > 0 {
		if v, err := decodeJSON(raw); err == nil {
			res.JSON = v
		}
	}
	return res, nil
}

// flattenHeaders 把请求头压平成 map，给 Trace 用。
func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		out[k] = strings.Join(vs, ", ")
	}
	return out
}

// readLimited 读取至多 max 字节；超出返回 ErrResponseTooLarge。
func readLimited(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%w（超过 %d 字节）", ErrResponseTooLarge, max)
	}
	return b, nil
}

// transportToError 把传输层错误转成分类错误：
//   - 触发 SSRF 防护（域名不在白名单、内网地址、重定向过多）、响应超限：terminal，重试没有意义；
//   - 提交操作里连接已建立之后的失败（读超时、连接被重置）：submit_unknown，平台可能已经建了任务，不能盲目重试；
//   - 其余（连接失败、DNS 失败、限流等待被取消、幂等操作超时）：retryable。
//
// 错误文案会脱敏：*url.Error 会把完整 URL（query 鉴权时含凭证）带进错误信息。
func (e *engine) transportToError(ctx context.Context, p execParams, terr *transportErr, secret string) error {
	msg := redactMsg(terr.err.Error(), secret)
	label := opLabel[p.name]
	cause := errors.New(msg)
	switch {
	case errors.Is(terr.err, ErrBlockedAddress), errors.Is(terr.err, ErrHostNotAllowed),
		errors.Is(terr.err, ErrTooManyRedirects), errors.Is(terr.err, ErrResponseTooLarge):
		return newErr(provider.ClassTerminal, "ssrf_blocked", label+"被安全策略拒绝："+msg, cause)
	case p.submit && terr.connected:
		return newErr(provider.ClassSubmitUnknown, "", label+"：请求已发出但没有收到完整响应，结果未知："+msg, cause)
	}
	return newErr(provider.ClassRetryable, "", label+"：网络错误："+msg, cause)
}

// classify 用 error_rules 给失败的响应分类：按顺序取第一条 when 为真的；
// 都不匹配时 429 / 5xx 视为 retryable，其余 terminal。
// 规则求值出错按“不匹配”处理并记警告（运营写错的规则不能让引擎整个失败）。
func (e *engine) classify(p execParams, rc *dsl.RenderContext, res *httpResult, secret string) error {
	class := provider.ClassTerminal
	if res.Status == http.StatusTooManyRequests || res.Status >= 500 {
		class = provider.ClassRetryable
	}
	for i, r := range p.provider.ErrorRules {
		ok, err := dsl.EvalBool(r.When, rc)
		if err != nil {
			logger.Warn("error_rules 求值失败，按不匹配处理",
				zap.String("provider", p.provider.Key), zap.Int("rule", i), zap.Error(err))
			continue
		}
		if ok {
			class = provider.ErrorClass(r.Class)
			break
		}
	}

	// 平台给的错误码 / 文案（如果操作里配置了 extract.error_code / error_message）
	code := fmt.Sprintf("http_%d", res.Status)
	msg := ""
	if expr, ok := p.op.Extract["error_code"]; ok {
		if v, err := dsl.EvalExpr(expr, rc); err == nil && v != nil {
			if s := dsl.Stringify(v); s != "" {
				code = s
			}
		}
	}
	if expr, ok := p.op.Extract["error_message"]; ok {
		if v, err := dsl.EvalExpr(expr, rc); err == nil && v != nil {
			msg = dsl.Stringify(v)
		}
	}
	if msg == "" {
		msg = snippet(res.Raw)
	}
	full := fmt.Sprintf("%s失败（HTTP %d）", opLabel[p.name], res.Status)
	if msg != "" {
		full += "：" + msg
	}
	return newErr(class, code, redactMsg(full, secret), nil)
}

// snippet 取响应体开头一小段用于错误信息。
func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	return truncate(s, 300)
}

func newErr(class provider.ErrorClass, code, msg string, cause error) *provider.Error {
	return &provider.Error{Class: class, Code: code, Message: msg, Cause: cause}
}

// redactMsg 把凭证明文从文本里抹掉。
func redactMsg(s string, secret string) string {
	if secret == "" {
		return s
	}
	return dsl.Redact(s, secret).(string)
}
