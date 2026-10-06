package llmgateway_test

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/provider/netguard"
)

func streamOnce(t *testing.T, g *llmgateway.Gateway, tg llmgateway.Target) (*llmgateway.Result, error) {
	t.Helper()
	return g.Stream(ctxTimeout(t, 5*time.Second), tg, llmgateway.Request{Messages: []llmgateway.Message{userMsg("x")}}, func(llmgateway.Event) {})
}

func TestStream_HTTPErrorsAreClassified(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		retryable bool
		wantMsg   string
	}{
		{"Key 无效", 401, `{"error":{"message":"Incorrect API key provided: sk-secret-key"}}`, false, "Key"},
		{"没有权限", 403, `{"error":{"message":"forbidden"}}`, false, "权限"},
		{"限流", 429, `{"error":{"message":"rate limit"}}`, true, "频繁"},
		{"上游故障", 500, `bad gateway`, true, "上游"},
		{"网关超时", 504, ``, true, "上游"},
		{"请求不合法", 400, `{"error":{"message":"max_tokens is too large","code":"invalid_request"}}`, false, "max_tokens is too large"},
		{"模型不存在", 404, `{"error":{"message":"The model gpt-up does not exist"}}`, false, "does not exist"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := streamOnce(t, gateway(), target(up))
			var ue *llmgateway.UpstreamError
			if !errors.As(err, &ue) {
				t.Fatalf("应返回 UpstreamError: %v", err)
			}
			if ue.Status != tc.status || ue.Retryable != tc.retryable || !strings.Contains(ue.Message, tc.wantMsg) {
				t.Errorf("err=%+v", ue)
			}
			if strings.Contains(err.Error(), "sk-secret-key") || strings.Contains(ue.Message, "sk-secret-key") {
				t.Errorf("错误信息里不能出现渠道 Key: %v", err)
			}
		})
	}
}

func TestStream_ErrorMessageIsTruncated(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"` + strings.Repeat("长", 5000) + `"}}`))
	})
	_, err := streamOnce(t, gateway(), target(up))
	var ue *llmgateway.UpstreamError
	if !errors.As(err, &ue) || len([]rune(ue.Message)) > 400 {
		t.Errorf("上游的长报错要截断，别把几 KB 塞给用户: %d", len([]rune(ue.Message)))
	}
}

func TestStream_TruncatedStreamKeepsPartialResult(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, chunk(`{"content":"说到一半"}`, "")) // 没有 finish_reason、没有 [DONE]，连接直接关闭
	})
	res, err := streamOnce(t, gateway(), target(up))
	if !errors.Is(err, llmgateway.ErrStreamTruncated) {
		t.Fatalf("应返回 ErrStreamTruncated: %v", err)
	}
	if res == nil || res.Text != "说到一半" {
		t.Errorf("要保留已收到的部分: %+v", res)
	}
}

func TestStream_IdleTimeout(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, chunk(`{"content":"开头"}`, ""))
		select { // 之后一声不吭
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})
	g := gateway(func(o *llmgateway.Options) { o.IdleTimeout = 150 * time.Millisecond })
	start := time.Now()
	res, err := streamOnce(t, g, target(up))
	if !errors.Is(err, llmgateway.ErrIdleTimeout) {
		t.Fatalf("应返回 ErrIdleTimeout: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("空闲超时应很快触发，实际 %v", time.Since(start))
	}
	if res == nil || res.Text != "开头" {
		t.Errorf("res=%+v", res)
	}
}

func TestStream_BlocksInternalAddressesUnlessTrusted(t *testing.T) {
	var hits atomic.Int32
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		hits.Add(1)
		sse(w, chunk(`{"content":"x"}`, "stop"), "[DONE]")
	})
	tg := target(up)
	tg.TrustedInternal = false // httptest 在回环地址上，普通渠道不能访问
	_, err := streamOnce(t, gateway(), tg)
	if err == nil || !netguard.IsGuardError(err) {
		t.Fatalf("普通渠道访问回环地址应被 SSRF 防护拒绝: %v", err)
	}
	if hits.Load() != 0 {
		t.Error("被拒绝时不能发出请求")
	}
}

func TestStream_DoesNotFollowRedirects(t *testing.T) {
	var target2Hits atomic.Int32
	other := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) { target2Hits.Add(1) })
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		http.Redirect(w, r, other.srv.URL+"/steal", http.StatusTemporaryRedirect) // 带着 Key 的请求不能被转走
	})
	_, err := streamOnce(t, gateway(), target(up))
	if err == nil {
		t.Fatal("重定向应被当作错误")
	}
	if target2Hits.Load() != 0 {
		t.Error("不能跟随重定向：Authorization 头会跟着发到别处")
	}
}

func TestStream_InvalidTargets(t *testing.T) {
	bad := []struct {
		name string
		mut  func(*llmgateway.Target)
	}{
		{"协议不是 http(s)", func(tg *llmgateway.Target) { tg.BaseURL = "ftp://example.com" }},
		{"带用户名密码", func(tg *llmgateway.Target) { tg.BaseURL = "https://user:pass@example.com" }},
		{"没有主机", func(tg *llmgateway.Target) { tg.BaseURL = "https://" }},
		{"没有 Key", func(tg *llmgateway.Target) { tg.APIKey = "" }},
		{"没有上游模型名", func(tg *llmgateway.Target) { tg.UpstreamModel = "" }},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			tg := llmgateway.Target{ChannelKey: "c", BaseURL: "https://example.com", APIKey: "k", UpstreamModel: "m"}
			tc.mut(&tg)
			_, err := streamOnce(t, gateway(), tg)
			if !errors.Is(err, llmgateway.ErrInvalidTarget) {
				t.Errorf("应返回 ErrInvalidTarget: %v", err)
			}
		})
	}
	t.Run("没有消息", func(t *testing.T) {
		_, err := gateway().Stream(ctxTimeout(t, time.Second), llmgateway.Target{ChannelKey: "c", BaseURL: "https://example.com", APIKey: "k", UpstreamModel: "m"}, llmgateway.Request{}, func(llmgateway.Event) {})
		if !errors.Is(err, llmgateway.ErrInvalidRequest) {
			t.Errorf("err=%v", err)
		}
	})
}

func TestStream_ChannelConcurrencyLimit(t *testing.T) {
	var cur, peak atomic.Int32
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(80 * time.Millisecond)
		cur.Add(-1)
		sse(w, chunk(`{"content":"ok"}`, "stop"), "[DONE]")
	})
	g := gateway()
	tg := target(up)
	tg.MaxConcurrency = 2
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := streamOnce(t, g, tg); err != nil {
				t.Errorf("err=%v", err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() > 2 {
		t.Errorf("同一渠道同时在途不能超过 max_concurrency=2，实际峰值 %d", peak.Load())
	}
	if peak.Load() < 2 {
		t.Errorf("限制不应过严，峰值只有 %d", peak.Load())
	}
}

func TestStream_WaitingForSlotHonorsCancel(t *testing.T) {
	release := make(chan struct{})
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		<-release
		sse(w, chunk(`{"content":"ok"}`, "stop"), "[DONE]")
	})
	g := gateway()
	tg := target(up)
	tg.MaxConcurrency = 1
	go func() { _, _ = streamOnce(t, g, tg) }()
	time.Sleep(100 * time.Millisecond) // 让第一个占住名额

	ctx := ctxTimeout(t, 150*time.Millisecond)
	_, err := g.Stream(ctx, tg, llmgateway.Request{Messages: []llmgateway.Message{userMsg("x")}}, func(llmgateway.Event) {})
	if err == nil || ctx.Err() == nil {
		t.Errorf("排队等名额时取消应立即返回: %v", err)
	}
	close(release)
}
