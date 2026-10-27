package llmgateway_test

import (
	"errors"
	"net/http"
	"testing"

	"video-canvas/internal/llmgateway"
)

func TestGemini_EndpointAndAuthHeaders(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, model, wantPath string
	}{
		{"渠道前缀", "/antigravity", "gemini-3.8-flash", "/antigravity/v1beta/models/gemini-3.8-flash:streamGenerateContent"},
		{"前缀末尾斜线", "/antigravity///", "gemini-3.8-flash", "/antigravity/v1beta/models/gemini-3.8-flash:streamGenerateContent"},
		{"无前缀", "", "gemini-3.8-flash", "/v1beta/models/gemini-3.8-flash:streamGenerateContent"},
		{"模型名转义", "/antigravity", "team/gemini flash?#", "/antigravity/v1beta/models/team%2Fgemini%20flash%3F%23:streamGenerateContent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
				if got := r.URL.EscapedPath(); got != tc.wantPath {
					t.Errorf("请求路径=%q，期望 %q", got, tc.wantPath)
				}
				if got := r.URL.RawQuery; got != "alt=sse" {
					t.Errorf("请求查询参数=%q，期望 alt=sse", got)
				}
				sse(w, `{"candidates":[{"content":{"parts":[{"text":"好"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1}}`)
			})
			tg := target(up)
			tg.Format = llmgateway.FormatGemini
			tg.BaseURL += tc.prefix
			tg.APIKey = "k-123"
			tg.UpstreamModel = tc.model
			tg.Headers = map[string]string{
				"x-goog-api-key": "evil",
				"Authorization":  "evil",
				"Content-Type":   "text/plain",
				"Accept":         "application/json",
				"X-Custom":       "1",
			}
			// 本任务只验证请求侧；响应翻译尚未接入，允许现有解析器报告断流。
			_, _, err := streamRaw(t, gateway(), tg, `{"messages":[{"role":"user","content":"hi"}]}`, 0)
			if err != nil && !errors.Is(err, llmgateway.ErrStreamTruncated) {
				t.Fatal(err)
			}
			reqs := up.requests()
			if len(reqs) != 1 {
				t.Fatalf("应向上游发送一次请求，实际 %d 次", len(reqs))
			}
			if reqs[0].Body["contents"] == nil || reqs[0].Body["messages"] != nil {
				t.Errorf("Gemini 请求必须翻译为 contents: %v", reqs[0].Body)
			}
			for name, want := range map[string]string{
				"x-goog-api-key": "k-123",
				"Authorization":  "Bearer k-123",
				"Content-Type":   "application/json",
				"Accept":         "text/event-stream",
				"X-Custom":       "1",
			} {
				if got := reqs[0].Header.Get(name); got != want {
					t.Errorf("请求头 %s=%q，期望 %q", name, got, want)
				}
			}
		})
	}
}

func TestOpenAI_EndpointUnchanged(t *testing.T) {
	for _, format := range []string{"", llmgateway.FormatOpenAI} {
		t.Run("format="+format, func(t *testing.T) {
			up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
				if r.URL.RawQuery != "" {
					t.Errorf("OpenAI 请求不应新增查询参数: %s", r.URL.RawQuery)
				}
				sse(w, chunk(`{"content":"好"}`, "stop"), "[DONE]")
			})
			tg := target(up)
			tg.Format = format
			if _, _, err := streamRaw(t, gateway(), tg, `{"messages":[{"role":"user","content":"hi"}]}`, 0); err != nil {
				t.Fatal(err)
			}
			reqs := up.requests()
			if len(reqs) != 1 {
				t.Fatalf("应向上游发送一次请求，实际 %d 次", len(reqs))
			}
			got := reqs[0]
			if got.Path != "/v1/chat/completions" || got.Auth != "Bearer sk-secret-key" {
				t.Errorf("OpenAI 路径或鉴权改变: %+v", got)
			}
			if _, exists := got.Header[http.CanonicalHeaderKey("x-goog-api-key")]; exists {
				t.Error("OpenAI 请求不应新增 x-goog-api-key 头")
			}
		})
	}
}
