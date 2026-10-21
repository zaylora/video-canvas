package agent_test

import (
	"context"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
	. "video-canvas/internal/service/agent"
)

// 模型调用要留下用途、渠道、上游模型、耗时、首字耗时和结束原因，事后才能分析慢在哪、为什么被截断。
func TestProxyModel_RecordsCallMeta(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"拆分镜"}]}`)

	t.Run("对话调用：渠道、上游模型、结束原因、首字耗时和总耗时", func(t *testing.T) {
		b := newBridgeEnv(t)
		b.str.script = func(_ context.Context, emit func(llmgateway.Event), raw func([]byte)) (*llmgateway.Result, error) {
			*b.now = b.now.Add(300 * time.Millisecond) // 等了 300ms 才收到第一行
			emit(llmgateway.Event{Type: llmgateway.EventText, Text: "好"})
			raw([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n"))
			*b.now = b.now.Add(500 * time.Millisecond) // 之后又流了 500ms
			raw([]byte("data: [DONE]\n\n"))
			return &llmgateway.Result{Text: "好", FinishReason: "length", Usage: llmgateway.Usage{InputTokens: 10, OutputTokens: 5}}, nil
		}
		tok, _ := b.token(t, "all")
		if err := b.br.ProxyModel(ctx, tok, body, func([]byte) {}); err != nil {
			t.Fatal(err)
		}
		in := b.bill.settled[0]
		if in.Purpose != "main" || in.ChannelKey != "ch1" || in.UpstreamModel != "claude-up" || in.StopReason != "length" {
			t.Errorf("用途、渠道、上游模型、结束原因：%+v", in)
		}
		if in.TTFTMs != 300 || in.LatencyMs != 800 {
			t.Errorf("首字耗时应为 300，总耗时 800：ttft=%d latency=%d", in.TTFTMs, in.LatencyMs)
		}
	})

	t.Run("摘要调用：用途记为 summary", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		if err := b.br.ProxyModel(WithModelPurpose(ctx, ModelPurposeSummary), tok, body, func([]byte) {}); err != nil {
			t.Fatal(err)
		}
		if got := b.bill.settled[0].Purpose; got != "summary" {
			t.Errorf("purpose=%q", got)
		}
	})

	t.Run("上游一个字都没返回：首字耗时为 0，结束原因为空", func(t *testing.T) {
		b := newBridgeEnv(t)
		b.str.script = func(_ context.Context, _ func(llmgateway.Event), _ func([]byte)) (*llmgateway.Result, error) {
			*b.now = b.now.Add(2 * time.Second)
			return nil, context.DeadlineExceeded
		}
		tok, _ := b.token(t, "all")
		_ = b.br.ProxyModel(ctx, tok, body, func([]byte) {})
		in := b.bill.settled[0]
		if in.TTFTMs != 0 || in.StopReason != "" || in.LatencyMs != 2000 {
			t.Errorf("失败的调用也要记耗时：%+v", in)
		}
	})
}
