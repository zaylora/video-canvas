package provider

import (
	"context"
	"encoding/json"
	"sync"
)

// TraceBodyLimit 是追踪里请求 / 响应体 / 钩子输入输出保留的最大字节数，超出部分截断。
const TraceBodyLimit = 8 * 1024

// Trace 按步骤记录一次执行里的插件钩子调用与 HTTP 请求 / 响应，给管理端试跑面板展示（test-run 任务会持久化到 trace_json）。
// 写入方（宿主）负责脱敏：凭证明文（含 URL 转义形式）必须在 Add 之前替换成 ***，Trace 本身不做任何处理。
type Trace struct {
	mu    sync.Mutex
	steps []TraceStep
}

// TraceStep 是一个步骤：一次钩子调用（kind=hook）或一次 HTTP 请求（kind=http）。
type TraceStep struct {
	Name       string         `json:"name"`               // 如 submit / query / prepare:0 / cancel / check / import
	Kind       string         `json:"kind"`               // hook | http
	Hook       *TraceHook     `json:"hook,omitempty"`     // kind=hook
	Request    *TraceRequest  `json:"request,omitempty"`  // kind=http
	Response   *TraceResponse `json:"response,omitempty"` // kind=http
	Error      string         `json:"error,omitempty"`
	DurationMs int64          `json:"duration_ms"`
}

// TraceHook 是一次钩子调用：名字、输入（ctx 等参数，JSON）、输出（返回值，JSON）、utils.log 的输出。
type TraceHook struct {
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output json.RawMessage `json:"output,omitempty"`
	Logs   []string        `json:"logs,omitempty"`
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

// WithTrace 返回带 Trace 的 ctx：把它交给 Executor 的调用，宿主会把执行过程记录到返回的 Trace 里。
func WithTrace(ctx context.Context) (context.Context, *Trace) {
	t := &Trace{}
	return context.WithValue(ctx, traceKey{}, t), t
}

// TraceFrom 取 ctx 里的 Trace；没有返回 nil（此时宿主不记录）。
func TraceFrom(ctx context.Context) *Trace {
	t, _ := ctx.Value(traceKey{}).(*Trace)
	return t
}

// Add 追加一个步骤，并发安全。对 nil 接收者是空操作。
func (t *Trace) Add(s TraceStep) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.steps = append(t.steps, s)
	t.mu.Unlock()
}

// Steps 返回当前步骤列表的副本，可在并发执行时安全读取。
func (t *Trace) Steps() []TraceStep {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]TraceStep(nil), t.steps...)
}
