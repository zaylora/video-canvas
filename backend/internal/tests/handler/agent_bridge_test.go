package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler"
	"video-canvas/internal/llmgateway"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/service"
)

// fakeBridge 记录桥 handler 传给业务层的参数，按 err / lines 回放。
type fakeBridge struct {
	err       error
	lines     []string
	failAfter bool // 写完 lines 之后再返回 err（流中途出错）
	token     string
	body      string
	toolName  string
	toolID    string
	toolArgs  string
	state     string
	finStatus string
	finMsg    string
	toolRes   *service.ToolResult
}

func (f *fakeBridge) ProxyModel(_ context.Context, token string, body []byte, write func([]byte)) error {
	f.token, f.body = token, string(body)
	if f.err != nil && !f.failAfter {
		return f.err
	}
	for _, l := range f.lines {
		write([]byte(l))
	}
	return f.err
}

func (f *fakeBridge) ExecuteTool(_ context.Context, token, id, name string, args json.RawMessage) (*service.ToolResult, error) {
	f.token, f.toolID, f.toolName, f.toolArgs = token, id, name, string(args)
	if f.err != nil {
		return nil, f.err
	}
	if f.toolRes != nil {
		return f.toolRes, nil
	}
	return &service.ToolResult{Content: "ok"}, nil
}

func (f *fakeBridge) SaveState(_ context.Context, token string, messages json.RawMessage) error {
	f.token, f.state = token, string(messages)
	return f.err
}

func (f *fakeBridge) Finish(_ context.Context, token, status, message string) error {
	f.token, f.finStatus, f.finMsg = token, status, message
	return f.err
}

func newBridgeRouter(api AgentBridgeAPI) *gin.Engine {
	h := NewAgentBridgeHandler(api)
	r := gin.New()
	g := r.Group("/internal/agent/bridge")
	g.POST("/v1/chat/completions", h.ChatCompletions)
	g.POST("/tool", h.Tool)
	g.POST("/state", h.State)
	g.POST("/finish", h.Finish)
	return r
}

func bridgeCall(r *gin.Engine, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/internal/agent/bridge"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestBridgeHandler_ChatCompletions_Streams(t *testing.T) {
	api := &fakeBridge{lines: []string{"data: {\"a\":1}\n", "\n", "data: [DONE]\n\n"}}
	w := bridgeCall(newBridgeRouter(api), "/v1/chat/completions", "tok-1", `{"messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != 200 {
		t.Fatalf("status=%d %s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type=%q", ct)
	}
	if w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("X-Accel-Buffering") != "no" {
		t.Errorf("流式响应要禁缓存、禁代理缓冲: %v", w.Header())
	}
	if w.Body.String() != "data: {\"a\":1}\n\ndata: [DONE]\n\n" {
		t.Errorf("上游的行应原样转出: %q", w.Body.String())
	}
	if api.token != "tok-1" || !strings.Contains(api.body, `"hi"`) {
		t.Errorf("token=%q body=%q", api.token, api.body)
	}
}

// 流开始之前出错：返回 OpenAI 格式的 JSON 错误和对应的 HTTP 状态，pi 靠它显示报错、判断是否可重试。
func TestBridgeHandler_ChatCompletions_ErrorsBeforeStream(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantMsg    string
	}{
		{"令牌无效", service.ErrBridgeToken, 401, "令牌"},
		{"预算用尽", errcode.ErrAgentOverBudget, 402, "预算"},
		{"余额不足", errcode.ErrInsufficientCredits, 402, "积分"},
		{"模型不可用", errcode.ErrAgentModelNA, 400, "模型"},
		{"运行状态不对", errcode.ErrAgentState, 409, "状态"},
		{"上游限流（可重试）", &llmgateway.UpstreamError{Status: 429, Message: "上游请求太频繁，请稍后重试", Retryable: true}, 429, "频繁"},
		{"上游故障", &llmgateway.UpstreamError{Status: 503, Message: "上游服务暂时不可用（HTTP 503）"}, 503, "上游"},
		{"请求体不合法", llmgateway.ErrInvalidRequest, 400, "请求"},
		{"其他错误不外泄细节", errors.New("dial tcp 10.0.0.5:5432: secret-host"), 502, "上游"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := bridgeCall(newBridgeRouter(&fakeBridge{err: tc.err}), "/v1/chat/completions", "tok", `{"messages":[{"role":"user","content":"x"}]}`)
			if w.Code != tc.wantStatus {
				t.Fatalf("status=%d，期望 %d: %s", w.Code, tc.wantStatus, w.Body)
			}
			var body struct {
				Error struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || !strings.Contains(body.Error.Message, tc.wantMsg) || body.Error.Type == "" {
				t.Errorf("应是 OpenAI 格式的错误 {error:{message,type}}: %s", w.Body)
			}
			if strings.Contains(w.Body.String(), "secret-host") {
				t.Errorf("内部错误细节不能返回给调用方: %s", w.Body)
			}
		})
	}
}

func TestBridgeHandler_ChatCompletions_MissingOrBadAuth(t *testing.T) {
	r := newBridgeRouter(&fakeBridge{})
	for name, tok := range map[string]string{"没带令牌": ""} {
		w := bridgeCall(r, "/v1/chat/completions", tok, `{}`)
		if w.Code != 401 {
			t.Errorf("%s: status=%d", name, w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/agent/bridge/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Basic abc")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("非 Bearer 鉴权: status=%d", w.Code)
	}
}

// 流已经开始之后出错：状态码改不了，只能结束流；不能再追加 JSON 破坏 SSE。
func TestBridgeHandler_ChatCompletions_ErrorAfterStreamStarted(t *testing.T) {
	api := &fakeBridge{lines: []string{"data: {\"a\":1}\n\n"}, err: llmgateway.ErrStreamTruncated, failAfter: true}
	w := bridgeCall(newBridgeRouter(api), "/v1/chat/completions", "tok", `{"messages":[{"role":"user","content":"x"}]}`)
	if w.Code != 200 {
		t.Errorf("流已开始，状态码保持 200: %d", w.Code)
	}
	if strings.Contains(w.Body.String(), `"error"`) && !strings.HasPrefix(w.Body.String(), "data:") {
		t.Errorf("不能把 JSON 错误拼到 SSE 后面: %q", w.Body.String())
	}
}

func TestBridgeHandler_Tool(t *testing.T) {
	t.Run("成功：参数原样交给业务层，结果原样返回", func(t *testing.T) {
		api := &fakeBridge{toolRes: &service.ToolResult{Content: "画布有 3 个节点", Terminate: true}}
		w := bridgeCall(newBridgeRouter(api), "/tool", "tok", `{"tool_call_id":"tc1","name":"canvas_get_state","args":{"nodeIds":["a"]}}`)
		if w.Code != 200 {
			t.Fatalf("status=%d %s", w.Code, w.Body)
		}
		if api.token != "tok" || api.toolID != "tc1" || api.toolName != "canvas_get_state" || api.toolArgs != `{"nodeIds":["a"]}` {
			t.Errorf("api=%+v", api)
		}
		var resp struct {
			Code int
			Data service.ToolResult
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Code != 0 || resp.Data.Content != "画布有 3 个节点" || !resp.Data.Terminate {
			t.Errorf("resp=%s", w.Body)
		}
	})
	t.Run("args 不传按空对象", func(t *testing.T) {
		api := &fakeBridge{}
		bridgeCall(newBridgeRouter(api), "/tool", "tok", `{"tool_call_id":"tc1","name":"model_list"}`)
		if api.toolArgs != `{}` {
			t.Errorf("args=%q", api.toolArgs)
		}
	})
	t.Run("缺 name：400 + 10001", func(t *testing.T) {
		w := bridgeCall(newBridgeRouter(&fakeBridge{}), "/tool", "tok", `{"tool_call_id":"tc1"}`)
		if w.Code != 400 {
			t.Errorf("status=%d", w.Code)
		}
	})
	t.Run("令牌无效：401", func(t *testing.T) {
		w := bridgeCall(newBridgeRouter(&fakeBridge{err: service.ErrBridgeToken}), "/tool", "tok", `{"tool_call_id":"a","name":"b"}`)
		if w.Code != 401 {
			t.Errorf("status=%d", w.Code)
		}
	})
	t.Run("运行不在 running：业务错误透传 409 + 60003", func(t *testing.T) {
		w := bridgeCall(newBridgeRouter(&fakeBridge{err: errcode.ErrAgentState}), "/tool", "tok", `{"tool_call_id":"a","name":"b"}`)
		if w.Code != 409 || !strings.Contains(w.Body.String(), "60003") {
			t.Errorf("status=%d %s", w.Code, w.Body)
		}
	})
	t.Run("没带令牌：401", func(t *testing.T) {
		if w := bridgeCall(newBridgeRouter(&fakeBridge{}), "/tool", "", `{"tool_call_id":"a","name":"b"}`); w.Code != 401 {
			t.Errorf("status=%d", w.Code)
		}
	})
}

func TestBridgeHandler_StateAndFinish(t *testing.T) {
	t.Run("保存状态", func(t *testing.T) {
		api := &fakeBridge{}
		w := bridgeCall(newBridgeRouter(api), "/state", "tok", `{"messages":[{"role":"user","content":"你好"}]}`)
		if w.Code != 200 || api.state != `[{"role":"user","content":"你好"}]` {
			t.Errorf("status=%d state=%q", w.Code, api.state)
		}
	})
	t.Run("保存状态：缺 messages → 400", func(t *testing.T) {
		if w := bridgeCall(newBridgeRouter(&fakeBridge{}), "/state", "tok", `{}`); w.Code != 400 {
			t.Errorf("status=%d", w.Code)
		}
	})
	t.Run("结束", func(t *testing.T) {
		api := &fakeBridge{}
		w := bridgeCall(newBridgeRouter(api), "/finish", "tok", `{"status":"error","message":"上游 502"}`)
		if w.Code != 200 || api.finStatus != "error" || api.finMsg != "上游 502" {
			t.Errorf("status=%d api=%+v", w.Code, api)
		}
	})
	t.Run("结束：status 缺失 → 400", func(t *testing.T) {
		if w := bridgeCall(newBridgeRouter(&fakeBridge{}), "/finish", "tok", `{}`); w.Code != 400 {
			t.Errorf("status=%d", w.Code)
		}
	})
	t.Run("令牌无效：401", func(t *testing.T) {
		for _, p := range []string{"/state", "/finish"} {
			body := `{"messages":[],"status":"done"}`
			if w := bridgeCall(newBridgeRouter(&fakeBridge{err: service.ErrBridgeToken}), p, "tok", body); w.Code != 401 {
				t.Errorf("%s status=%d", p, w.Code)
			}
		}
	})
}
