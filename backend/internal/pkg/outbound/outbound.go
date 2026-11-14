// Package outbound 是统一出站 HTTP 客户端：除文本模型（走 llmgateway）和供应商插件（走插件宿主）以外，
// 用户运行里的外部 HTTP 调用都从这里拿客户端。每个请求在 app.log 打一行 ext.call，在 payload.log 记请求和响应全文，
// 并把调用计数累计到当前运行上（设计见 docs/design/统一服务器日志 6.12.3）。架构测试禁止在别处自己构造 http.Client。
package outbound

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/pkg/logger"
)

// Client 返回带记录的客户端：复制 base 的设置（超时、重定向策略），把传输层包一层。base 为 nil 时用默认设置。
// purpose 是默认用途（如 agent.web）；业务用 logger.WithCall 声明了调用时以声明的为准。
func Client(purpose string, base *http.Client) *http.Client {
	c := &http.Client{}
	if base != nil {
		*c = *base
	}
	c.Transport = Transport(purpose, c.Transport)
	return c
}

// Transport 给只能传 RoundTripper 的地方用（如第三方 SDK）。base 为 nil 时用 http.DefaultTransport。
func Transport(purpose string, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{purpose: purpose, base: base}
}

type transport struct {
	purpose string
	base    http.RoundTripper
}

// RoundTrip 实现 http.RoundTripper：发请求前取请求体副本，响应体在调用方读完（或关闭）时记日志，不改变请求和响应本身。
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	call := logger.CallOf(ctx)
	if call.ID == "" {
		call.ID = logger.NewCallID()
	}
	if call.Purpose == "" {
		call.Purpose = t.purpose
	}
	if rid := logger.RequestIDOf(ctx); rid != "" && req.Header.Get("X-Request-ID") == "" {
		req = req.Clone(ctx)
		req.Header.Set("X-Request-ID", rid)
	}
	reqBody, reqBytes := requestBody(req)
	rec := &record{call: call, req: req, reqBody: reqBody, reqBytes: reqBytes, start: time.Now()}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		rec.finish(nil, nil, 0, err)
		return nil, err
	}
	resp.Body = &capture{ReadCloser: resp.Body, rec: rec, resp: resp, text: isText(resp.Header.Get("Content-Type"))}
	return resp, nil
}

// record 是一次调用要记的东西，在响应读完时写出。
type record struct {
	call     logger.Call
	req      *http.Request
	reqBody  string
	reqBytes int64
	start    time.Time
	once     sync.Once
}

// finish 写 app.log 的 ext.call 和 payload.log 的请求、响应全文，只写一次。
func (r *record) finish(resp *http.Response, body []byte, n int64, err error) {
	r.once.Do(func() {
		ctx := r.req.Context()
		latency := time.Since(r.start).Milliseconds()
		result := "ok"
		if err != nil || (resp != nil && resp.StatusCode >= 400) {
			result = "failed"
		}
		fields := []zap.Field{
			logger.Event(logger.EventExtCall),
			zap.String("call_id", r.call.ID),
			zap.String("purpose", r.call.Purpose),
			zap.String("method", r.req.Method),
			zap.String("host", r.req.URL.Host),
			zap.String("path", r.req.URL.Path),
			zap.Int64("request_bytes", r.reqBytes),
			zap.Int64("latency_ms", latency),
			zap.String("result", result),
		}
		if resp != nil {
			fields = append(fields, zap.Int("status", resp.StatusCode), zap.Int64("response_bytes", n))
		}
		if err != nil {
			fields = append(fields, zap.String("error", err.Error()))
		}
		fields = append(fields, r.call.Fields...)
		if result == "ok" {
			logger.Ctx(ctx).Info("外部调用", fields...)
		} else {
			logger.Ctx(ctx).Warn("外部调用", fields...)
		}
		run := logger.RunOf(ctx)
		run.AddExt()
		run.NoteCall(r.call.ID)

		if !logger.PayloadEnabled() {
			return
		}
		p := logger.PayloadRecord{CallID: r.call.ID, Kind: "http", Purpose: r.call.Purpose, LatencyMs: latency,
			Request: &logger.PayloadRequest{Method: r.req.Method, URL: r.req.URL.String(), Headers: flatten(r.req.Header), Body: r.reqBody}}
		if resp != nil {
			p.Response = &logger.PayloadResponse{Status: resp.StatusCode, Headers: flatten(resp.Header), Body: responseBody(resp, body, n)}
		}
		if err != nil {
			p.Error = err.Error()
		}
		logger.Payload(ctx, p)
	})
}

// capture 包住响应体：文本响应边读边留一份（最多 MaxPayloadField），读到结尾或关闭时写日志。
type capture struct {
	io.ReadCloser
	rec  *record
	resp *http.Response
	text bool
	buf  bytes.Buffer
	n    int64
}

func (c *capture) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.n += int64(n)
	if c.text && c.buf.Len() < logger.MaxPayloadField {
		c.buf.Write(p[:min(n, logger.MaxPayloadField-c.buf.Len())])
	}
	if err == io.EOF {
		c.done(nil)
	} else if err != nil {
		c.done(err)
	}
	return n, err
}

func (c *capture) Close() error {
	err := c.ReadCloser.Close()
	c.done(nil)
	return err
}

func (c *capture) done(err error) { c.rec.finish(c.resp, c.buf.Bytes(), c.n, err) }

// requestBody 取请求体副本：文本类型且不超过上限时原文，否则只记类型和大小。不消耗原请求体。
func requestBody(req *http.Request) (string, int64) {
	if req.Body == nil || req.Body == http.NoBody {
		return "", 0
	}
	n := req.ContentLength
	ct := req.Header.Get("Content-Type")
	if req.GetBody == nil || !isText(ct) || n > logger.MaxPayloadField {
		return binaryNote(ct, n), max(n, 0)
	}
	rc, err := req.GetBody()
	if err != nil {
		return "", max(n, 0)
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, logger.MaxPayloadField))
	return string(b), int64(len(b))
}

// responseBody 返回响应体要记的内容：文本原文，其他只记类型和大小。
func responseBody(resp *http.Response, body []byte, n int64) string {
	if isText(resp.Header.Get("Content-Type")) {
		return string(body)
	}
	return binaryNote(resp.Header.Get("Content-Type"), n)
}

func binaryNote(contentType string, n int64) string {
	if contentType == "" {
		contentType = "unknown"
	}
	return fmt.Sprintf("<binary %s %d bytes>", contentType, n)
}

// isText 判断内容类型是不是文本（JSON、XML、HTML、表单、纯文本）；没写类型的按文本处理。
func isText(contentType string) bool {
	if contentType == "" {
		return true
	}
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return strings.HasPrefix(mt, "text/") || strings.HasSuffix(mt, "json") || strings.HasSuffix(mt, "xml") ||
		mt == "application/x-www-form-urlencoded" || mt == "application/javascript"
}

// flatten 把多值请求头压成一个值（取第一个），交给 logger 遮鉴权头。
func flatten(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}
