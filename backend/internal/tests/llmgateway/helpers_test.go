package llmgateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
)

// upstream 是一个假的 OpenAI 兼容上游：记下收到的请求，按脚本回放响应。
type upstream struct {
	srv     *httptest.Server
	mu      sync.Mutex
	reqs    []capturedReq
	handler func(w http.ResponseWriter, r *http.Request, body []byte)
}

type capturedReq struct {
	Path   string
	Auth   string
	Header http.Header
	Body   map[string]any
}

func newUpstream(t *testing.T, h func(w http.ResponseWriter, r *http.Request, body []byte)) *upstream {
	t.Helper()
	u := &upstream{handler: h}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		u.mu.Lock()
		u.reqs = append(u.reqs, capturedReq{Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Header: r.Header.Clone(), Body: parsed})
		u.mu.Unlock()
		u.handler(w, r, body)
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *upstream) requests() []capturedReq {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]capturedReq(nil), u.reqs...)
}

// sse 把若干 data 块写成 SSE 流；每块写完立即 flush，模拟逐段到达。
func sse(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	f, _ := w.(http.Flusher)
	for _, c := range chunks {
		fmt.Fprintf(w, "data: %s\n\n", c)
		if f != nil {
			f.Flush()
		}
	}
}

// chunk 生成一个 chat.completion.chunk 的 JSON。
func chunk(delta string, finish string) string {
	fin := "null"
	if finish != "" {
		fin = fmt.Sprintf("%q", finish)
	}
	return fmt.Sprintf(`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":%s,"finish_reason":%s}]}`, delta, fin)
}

func usageChunk(in, out, cached int) string {
	return fmt.Sprintf(`{"id":"c1","choices":[],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d,"prompt_tokens_details":{"cached_tokens":%d}}}`, in, out, in+out, cached)
}

// target 指向假上游；渠道默认「可信内网」，因为 httptest 监听在回环地址。
func target(u *upstream) llmgateway.Target {
	return llmgateway.Target{ChannelKey: "ch1", BaseURL: u.srv.URL, APIKey: "sk-secret-key", UpstreamModel: "gpt-up", TrustedInternal: true}
}

func gateway(opts ...func(*llmgateway.Options)) *llmgateway.Gateway {
	o := llmgateway.Options{IdleTimeout: 2 * time.Second}
	for _, f := range opts {
		f(&o)
	}
	return llmgateway.New(o)
}

func userMsg(text string) llmgateway.Message {
	return llmgateway.Message{Role: llmgateway.RoleUser, Text: text}
}

// collect 返回一个收集事件的回调。
func collect() (func(llmgateway.Event), func() []llmgateway.Event) {
	var mu sync.Mutex
	var evs []llmgateway.Event
	return func(e llmgateway.Event) {
			mu.Lock()
			defer mu.Unlock()
			evs = append(evs, e)
		}, func() []llmgateway.Event {
			mu.Lock()
			defer mu.Unlock()
			return append([]llmgateway.Event(nil), evs...)
		}
}

func ctxTimeout(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}
