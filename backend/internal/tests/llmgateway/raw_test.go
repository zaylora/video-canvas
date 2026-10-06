package llmgateway_test

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
)

// piBody 是 pi 的 openai-completions 提供方实际会发来的请求体（含我们不想放行的字段）。
const piBody = `{
  "model": "agent-model",
  "messages": [
    {"role": "system", "content": "你是画布助手"},
    {"role": "user", "content": [{"type": "text", "text": "看这张图"}, {"type": "image_url", "image_url": {"url": "data:image/png;base64,AAAA"}}]}
  ],
  "stream": true,
  "stream_options": {"include_usage": false},
  "store": false,
  "max_completion_tokens": 999999,
  "logit_bias": {"50256": -100},
  "user": "someone",
  "n": 5,
  "tools": [{"type": "function", "function": {"name": "canvas_get_state", "description": "读画布", "parameters": {"type": "object", "properties": {}}}}],
  "temperature": 0.3
}`

func streamRaw(t *testing.T, g *llmgateway.Gateway, tg llmgateway.Target, body string, maxTokens int) (*llmgateway.Result, string, error) {
	t.Helper()
	var buf bytes.Buffer
	res, err := g.StreamRaw(ctxTimeout(t, 5*time.Second), tg, []byte(body), llmgateway.RawLimits{MaxTokens: maxTokens},
		func(llmgateway.Event) {}, func(line []byte) { buf.Write(line) })
	return res, buf.String(), err
}

func TestStreamRaw_RewritesAndRestrictsTheBody(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, chunk(`{"content":"好"}`, "stop"), usageChunk(10, 2, 0), "[DONE]")
	})
	tg := target(up)
	if _, _, err := streamRaw(t, gateway(), tg, piBody, 8192); err != nil {
		t.Fatal(err)
	}
	got := up.requests()[0].Body
	if got["model"] != "gpt-up" {
		t.Errorf("模型名必须强制成渠道的上游模型，不能信请求体里的: %v", got["model"])
	}
	if got["stream"] != true {
		t.Errorf("stream=%v", got["stream"])
	}
	if so, _ := got["stream_options"].(map[string]any); so["include_usage"] != true {
		t.Errorf("用量选项必须强制打开（计费靠它），即使请求里写了 false: %v", got["stream_options"])
	}
	if got["max_tokens"] != float64(8192) {
		t.Errorf("max_completion_tokens 超过上限应收紧并改名为 max_tokens: %v", got["max_tokens"])
	}
	for _, k := range []string{"store", "max_completion_tokens", "logit_bias", "user", "n"} {
		if _, has := got[k]; has {
			t.Errorf("字段 %q 不在白名单里，不应转发", k)
		}
	}
	if got["temperature"] != 0.3 {
		t.Errorf("白名单里的字段应保留: temperature=%v", got["temperature"])
	}
	msgs := got["messages"].([]any)
	user := msgs[1].(map[string]any)["content"].([]any)
	if len(msgs) != 2 || user[1].(map[string]any)["image_url"].(map[string]any)["url"] != "data:image/png;base64,AAAA" {
		t.Errorf("messages 应原样保留（含图片分段）: %v", msgs)
	}
	if tools := got["tools"].([]any); len(tools) != 1 {
		t.Errorf("tools 应原样保留: %v", got["tools"])
	}
	if up.requests()[0].Auth != "Bearer sk-secret-key" {
		t.Errorf("鉴权必须用渠道 Key，不能用请求里带的: %q", up.requests()[0].Auth)
	}
}

func TestStreamRaw_MaxTokensCap(t *testing.T) {
	cases := []struct {
		name string
		body string
		cap  int
		want any
	}{
		{"没写：补上上限", `{"messages":[{"role":"user","content":"x"}]}`, 4096, float64(4096)},
		{"写得比上限小：保留", `{"messages":[{"role":"user","content":"x"}],"max_tokens":100}`, 4096, float64(100)},
		{"写得比上限大：收紧", `{"messages":[{"role":"user","content":"x"}],"max_tokens":99999}`, 4096, float64(4096)},
		{"没有上限（0）：照请求", `{"messages":[{"role":"user","content":"x"}],"max_tokens":99999}`, 0, float64(99999)},
		{"没有上限也没写：不加", `{"messages":[{"role":"user","content":"x"}]}`, 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
				sse(w, chunk(`{"content":"x"}`, "stop"), "[DONE]")
			})
			if _, _, err := streamRaw(t, gateway(), target(up), tc.body, tc.cap); err != nil {
				t.Fatal(err)
			}
			if got := up.requests()[0].Body["max_tokens"]; got != tc.want {
				t.Errorf("max_tokens=%v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestStreamRaw_TeesUpstreamLinesAndStillParses(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, chunk(`{"content":"你好"}`, ""), chunk(`{}`, "stop"), usageChunk(120, 8, 100), "[DONE]")
	})
	res, raw, err := streamRaw(t, gateway(), target(up), piBody, 8192)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"content":"你好"`, `"finish_reason":"stop"`, `"prompt_tokens":120`, "data: [DONE]"} {
		if !strings.Contains(raw, want) {
			t.Errorf("转给调用方的原始流里应有 %q:\n%s", want, raw)
		}
	}
	if res.Text != "你好" || res.Usage.InputTokens != 120 || res.Usage.CachedTokens != 100 {
		t.Errorf("透传的同时仍要解析出结果和用量（计费靠它）: %+v", res)
	}
}

func TestStreamRaw_WholeJSONResponseIsRewrittenAsSSE(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"整段","tool_calls":[{"id":"c1","type":"function","function":{"name":"plan_update","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	})
	res, raw, err := streamRaw(t, gateway(), target(up), piBody, 8192)
	if err != nil {
		t.Fatal(err)
	}
	// 调用方（pi）按 SSE 解析，所以整段响应也要改写成 SSE：正文、工具调用、结束原因、用量、[DONE]。
	for _, want := range []string{"data: ", `"content":"整段"`, `"name":"plan_update"`, `"finish_reason":"tool_calls"`, `"prompt_tokens":10`, "data: [DONE]"} {
		if !strings.Contains(raw, want) {
			t.Errorf("改写后的 SSE 里应有 %q:\n%s", want, raw)
		}
	}
	if res.Text != "整段" || len(res.ToolCalls) != 1 {
		t.Errorf("res=%+v", res)
	}
}

func TestStreamRaw_InvalidBodies(t *testing.T) {
	g := gateway()
	tg := llmgateway.Target{ChannelKey: "c", BaseURL: "https://example.com", APIKey: "k", UpstreamModel: "m"}
	for name, body := range map[string]string{
		"不是 JSON":       `not json`,
		"不是对象":          `[1,2]`,
		"没有 messages":   `{"model":"x"}`,
		"messages 为空":   `{"messages":[]}`,
		"messages 不是数组": `{"messages":"hi"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := streamRaw(t, g, tg, body, 100)
			if !errors.Is(err, llmgateway.ErrInvalidRequest) {
				t.Errorf("应返回 ErrInvalidRequest: %v", err)
			}
		})
	}
}

func TestStreamRaw_UpstreamErrorHappensBeforeAnyRawOutput(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	})
	_, raw, err := streamRaw(t, gateway(), target(up), piBody, 8192)
	var ue *llmgateway.UpstreamError
	if !errors.As(err, &ue) || ue.Status != 429 || !ue.Retryable {
		t.Fatalf("err=%v", err)
	}
	if raw != "" {
		t.Errorf("上游报错时还没开始流，不能有任何原始输出（调用方要据此返回 HTTP 错误码）: %q", raw)
	}
}
