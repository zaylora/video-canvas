package llmgateway_test

import (
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
)

// hang 让上游一声不吭，直到调用方断开（或兜底超时）。
func hang(r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-time.After(3 * time.Second):
	}
}

// 上游一个字节都没发就超时：还没有任何内容交给调用方，网关自动再发一次，第二次正常就当作成功。
func TestStreamRaw_RetriesOnceWhenUpstreamStaysSilent(t *testing.T) {
	var hits atomic.Int32
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if hits.Add(1) == 1 {
			hang(r)
			return
		}
		sse(w, chunk(`{"content":"好了"}`, "stop"), "[DONE]")
	})
	g := gateway(func(o *llmgateway.Options) { o.IdleTimeout = 150 * time.Millisecond })
	res, got, err := streamRaw(t, g, target(up), piBody, 0)
	if err != nil {
		t.Fatalf("第二次成功就不该报错: %v", err)
	}
	if res.Text != "好了" {
		t.Errorf("text=%q", res.Text)
	}
	if n := len(up.requests()); n != 2 {
		t.Errorf("应发 2 次请求，实际 %d", n)
	}
	if got == "" {
		t.Error("成功后的响应行要转给调用方")
	}
}

// 重试只有一次：两次都不响应就放弃，调用方拿到 ErrIdleTimeout。
func TestStreamRaw_GivesUpAfterOneRetry(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) { hang(r) })
	g := gateway(func(o *llmgateway.Options) { o.IdleTimeout = 100 * time.Millisecond })
	_, _, err := streamRaw(t, g, target(up), piBody, 0)
	if !errors.Is(err, llmgateway.ErrIdleTimeout) {
		t.Fatalf("应返回 ErrIdleTimeout: %v", err)
	}
	if n := len(up.requests()); n != 2 {
		t.Errorf("首次加一次重试共 2 次请求，实际 %d", n)
	}
}

// 已经有内容交给调用方（响应流已经开始）之后卡住：不能重试，否则内容会重复、用量会重复结算。
func TestStreamRaw_DoesNotRetryAfterBytesArrived(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, chunk(`{"content":"开头"}`, ""))
		hang(r)
	})
	g := gateway(func(o *llmgateway.Options) { o.IdleTimeout = 150 * time.Millisecond })
	res, _, err := streamRaw(t, g, target(up), piBody, 0)
	if !errors.Is(err, llmgateway.ErrIdleTimeout) {
		t.Fatalf("应返回 ErrIdleTimeout: %v", err)
	}
	if res == nil || res.Text != "开头" {
		t.Errorf("已收到的部分要保留: %+v", res)
	}
	if n := len(up.requests()); n != 1 {
		t.Errorf("响应流开始后不能重试，实际 %d 次请求", n)
	}
}

// 非流式入口（Stream）同样适用：没有事件交出去之前可以重试。
func TestStream_RetriesOnceWhenUpstreamStaysSilent(t *testing.T) {
	var hits atomic.Int32
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if hits.Add(1) == 1 {
			hang(r)
			return
		}
		sse(w, chunk(`{"content":"好了"}`, "stop"), "[DONE]")
	})
	g := gateway(func(o *llmgateway.Options) { o.IdleTimeout = 150 * time.Millisecond })
	res, err := streamOnce(t, g, target(up))
	if err != nil || res.Text != "好了" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// 渠道目标上的 IdleTimeout（来自模型配置）覆盖网关默认值；没设（0）时仍用网关默认。
func TestStream_TargetIdleTimeoutOverridesGatewayDefault(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) { hang(r) })
	g := gateway() // 网关默认 2 秒
	tg := target(up)
	tg.IdleTimeout = 100 * time.Millisecond
	start := time.Now()
	_, err := streamOnce(t, g, tg)
	if !errors.Is(err, llmgateway.ErrIdleTimeout) {
		t.Fatalf("应返回 ErrIdleTimeout: %v", err)
	}
	// 两次尝试各 100ms；若覆盖没生效，至少要 4 秒（2 秒 × 2 次）
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Errorf("模型上的超时没生效，耗时 %v", d)
	}
}
