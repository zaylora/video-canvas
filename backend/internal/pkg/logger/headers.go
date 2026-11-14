package logger

import (
	"context"
	"net/http"

	"go.uber.org/zap"
)

// 跨进程传递关联字段的请求头：Go 调 plugin-runner、Agent 服务时带上，对方的日志用同一组编号。
// 都是可选头：对方不认识也不影响调用本身。
const (
	HeaderRequestID = "X-Request-ID"
	HeaderRunKind   = "X-Run-Kind"
	HeaderRunID     = "X-Run-Id"
	HeaderCallID    = "X-Call-Id"
)

// InjectHeaders 把 ctx 上的请求编号、运行和上游调用编号写进出站请求头；没有的不写，已有的不覆盖。
func InjectHeaders(ctx context.Context, h http.Header) {
	set := func(k, v string) {
		if v != "" && h.Get(k) == "" {
			h.Set(k, v)
		}
	}
	set(HeaderRequestID, RequestIDOf(ctx))
	if r := RunOf(ctx); r != nil {
		set(HeaderRunKind, r.ref.Kind)
		set(HeaderRunID, r.ref.ID)
	}
	set(HeaderCallID, CallOf(ctx).ID)
}

// FromHeaders 在接收方还原关联字段：之后 Ctx(ctx) 打的每一行都带 request_id、run_kind / run_id 和 call_id。
func FromHeaders(ctx context.Context, h http.Header) context.Context {
	if id := h.Get(HeaderRequestID); id != "" {
		ctx = WithRequest(ctx, id)
	}
	if kind, id := h.Get(HeaderRunKind), h.Get(HeaderRunID); kind != "" && id != "" {
		ctx, _ = BeginRun(ctx, RunRef{Kind: kind, ID: id})
	}
	if id := h.Get(HeaderCallID); id != "" {
		ctx = context.WithValue(ctx, callKey{}, Call{ID: id})
		ctx = WithFields(ctx, zap.String("call_id", id)) // 接收方没有自己的调用行，直接带在每行上
	}
	return ctx
}
