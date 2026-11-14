// Package llmlog 是文本模型网关的日志观察者：每次请求尝试在 app.log 打一行 llm.call，在 payload.log 记请求和回复全文，
// 并把用量累计到当前运行上。所有文本模型调用（短剧、营销、Agent、预检、转写）都经过网关，所以都会被记录。
package llmlog

import (
	"context"
	"errors"
	"net/http"
	"unicode/utf8"

	"go.uber.org/zap"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/pkg/logger"
)

// 调用结果。
const (
	resultOK       = "ok"
	resultFailed   = "failed"
	resultTimeout  = "timeout"
	resultCanceled = "canceled"
)

// Observer 实现 llmgateway.Observer。
type Observer struct{}

// OnCall 实现 llmgateway.Observer。调用的 ID 和用途取业务用 logger.WithCall 声明的；没声明时生成一个。
func (Observer) OnCall(ctx context.Context, rec llmgateway.CallRecord) {
	call := logger.CallOf(ctx)
	if call.ID == "" {
		call.ID = logger.NewCallID()
	}
	result, errType := classify(rec.Err)

	// 1. app.log：元数据
	fields := []zap.Field{
		logger.Event(logger.EventLLMCall),
		zap.String("call_id", call.ID),
		zap.Int("net_attempt", rec.Attempt),
		zap.String("channel", rec.ChannelKey),
		zap.String("upstream_model", rec.UpstreamModel),
		zap.Int("request_bytes", len(rec.Body)),
		zap.Int64("latency_ms", rec.Latency.Milliseconds()),
		zap.Int64("ttft_ms", rec.TTFT.Milliseconds()),
		zap.String("result", result),
	}
	if call.Purpose != "" {
		fields = append(fields, zap.String("purpose", call.Purpose))
	}
	if rec.Format != "" {
		fields = append(fields, zap.String("format", rec.Format))
	}
	in, out := 0, 0
	if res := rec.Result; res != nil {
		in, out = res.Usage.InputTokens, res.Usage.OutputTokens
		fields = append(fields,
			zap.Int("response_chars", utf8.RuneCountInString(res.Text)),
			zap.Int("input_tokens", in),
			zap.Int("output_tokens", out),
			zap.Int("cached_tokens", res.Usage.CachedTokens),
			zap.Bool("usage_missing", res.UsageMissing),
		)
		if res.FinishReason != "" {
			fields = append(fields, zap.String("finish_reason", res.FinishReason))
		}
	}
	if rec.Status != 0 {
		fields = append(fields, zap.Int("upstream_status", rec.Status))
	}
	if rec.Err != nil {
		fields = append(fields, zap.String("error_type", errType), zap.String("error", rec.Err.Error()))
	}
	fields = append(fields, call.Fields...)
	log := logger.Ctx(ctx)
	if result == resultOK || result == resultCanceled {
		log.Info("模型调用", fields...)
	} else {
		log.Warn("模型调用", fields...)
	}

	// 2. 运行汇总：调用次数、用量、最后一次调用
	run := logger.RunOf(ctx)
	run.AddLLM(in, out)
	run.NoteCall(call.ID)

	// 3. payload.log：请求体全文和拼装后的回复
	if !logger.PayloadEnabled() {
		return
	}
	resp := &logger.PayloadResponse{Status: rec.Status, ErrorBody: rec.ErrorBody}
	if res := rec.Result; res != nil {
		resp.Text, resp.Thinking = res.Text, res.Thinking
		if len(res.ToolCalls) > 0 {
			resp.ToolCalls = res.ToolCalls
		}
	}
	p := logger.PayloadRecord{CallID: call.ID, Kind: "llm", Purpose: call.Purpose,
		Request:   &logger.PayloadRequest{Method: http.MethodPost, URL: rec.Endpoint, Body: string(rec.Body)},
		Response:  resp,
		LatencyMs: rec.Latency.Milliseconds(),
		Extra:     []zap.Field{zap.Int("net_attempt", rec.Attempt)},
	}
	if rec.Err != nil {
		p.Error = rec.Err.Error()
	}
	logger.Payload(ctx, p)
}

// classify 把错误归成调用结果和错误类型（与 run.finish 的 error_type 同一套取值）。
func classify(err error) (result, errType string) {
	var ue *llmgateway.UpstreamError
	switch {
	case err == nil:
		return resultOK, ""
	case errors.Is(err, context.Canceled):
		return resultCanceled, "canceled"
	case errors.Is(err, llmgateway.ErrIdleTimeout), errors.Is(err, context.DeadlineExceeded):
		return resultTimeout, "timeout"
	case errors.As(err, &ue):
		return resultFailed, "upstream"
	}
	return resultFailed, "network"
}
