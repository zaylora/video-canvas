//go:build legacy

package engine

import (
	"context"
	"sync"
	"time"

	"video-canvas/internal/provider/dsl"
)

// traceBodyLimit 是 Trace 里请求 / 响应体保留的最大字节数，超出部分截断。
const traceBodyLimit = 8 * 1024

// Trace 按步骤记录一次执行里的原始请求、响应和提取结果，给管理端试跑面板展示。
// 里面所有内容都已脱敏：凭证明文（含 URL 转义形式）会被替换成 ***。
type Trace struct {
	mu    sync.Mutex
	Steps []TraceStep `json:"steps"`
}

// TraceStep 是一个步骤：上传（upload:<字段名>）/ submit / query / cancel。
type TraceStep struct {
	Name       string         `json:"name"`
	Request    *TraceRequest  `json:"request,omitempty"`
	Response   *TraceResponse `json:"response,omitempty"`
	Extract    map[string]any `json:"extract,omitempty"` // 表达式提取出的结果
	Error      string         `json:"error,omitempty"`
	DurationMs int64          `json:"duration_ms"`
}

// TraceRequest 是发出的请求（脱敏后）。
type TraceRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// TraceResponse 是收到的响应（脱敏、截断后）。
type TraceResponse struct {
	Status    int    `json:"status"`
	Body      string `json:"body,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type traceKey struct{}

// WithTrace 返回带 Trace 的 ctx：把它交给 Submit / Query / Cancel，执行过程会记录到返回的 Trace 里。
func WithTrace(ctx context.Context) (context.Context, *Trace) {
	t := &Trace{}
	return context.WithValue(ctx, traceKey{}, t), t
}

func traceFrom(ctx context.Context) *Trace {
	t, _ := ctx.Value(traceKey{}).(*Trace)
	return t
}

// Snapshot 返回当前步骤列表的副本，可在并发执行时安全读取。
func (t *Trace) Snapshot() []TraceStep {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]TraceStep(nil), t.Steps...)
}

func (t *Trace) add(s TraceStep) {
	t.mu.Lock()
	t.Steps = append(t.Steps, s)
	t.mu.Unlock()
}

// stepRecorder 是引擎内部用的单步记录器，trace 为 nil 时所有方法都是空操作。
type stepRecorder struct {
	trace   *Trace
	step    TraceStep
	start   time.Time
	secrets []string
}

func newStepRecorder(ctx context.Context, name string, secrets ...string) *stepRecorder {
	return &stepRecorder{trace: traceFrom(ctx), step: TraceStep{Name: name}, start: time.Now(), secrets: secrets}
}

func (r *stepRecorder) request(method, url string, headers map[string]string, body string) {
	if r.trace == nil {
		return
	}
	hs := dsl.Redact(headers, r.secrets...).(map[string]string)
	r.step.Request = &TraceRequest{
		Method:  method,
		URL:     dsl.Redact(url, r.secrets...).(string),
		Headers: hs,
		Body:    truncate(dsl.Redact(body, r.secrets...).(string), traceBodyLimit),
	}
}

func (r *stepRecorder) response(status int, body []byte) {
	if r.trace == nil {
		return
	}
	s := dsl.Redact(string(body), r.secrets...).(string)
	r.step.Response = &TraceResponse{Status: status, Body: truncate(s, traceBodyLimit), Truncated: len(s) > traceBodyLimit}
}

func (r *stepRecorder) extract(name string, v any) {
	if r.trace == nil {
		return
	}
	if r.step.Extract == nil {
		r.step.Extract = map[string]any{}
	}
	r.step.Extract[name] = dsl.Redact(v, r.secrets...)
}

func (r *stepRecorder) fail(msg string) {
	if r.trace == nil {
		return
	}
	r.step.Error = dsl.Redact(msg, r.secrets...).(string)
}

// done 结束记录并写入 Trace。
func (r *stepRecorder) done() {
	if r.trace == nil {
		return
	}
	r.step.DurationMs = time.Since(r.start).Milliseconds()
	r.trace.add(r.step)
}

// truncate 按字节截断，并避免截在 UTF-8 字符中间。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "…（已截断）"
}
