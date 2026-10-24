package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler/agent"
	"video-canvas/internal/llmgateway"
	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/errcode"
	agentsvc "video-canvas/internal/service/agent"
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
	toolRes   *agentsvc.ToolResult
	purpose   string // ProxyModel 收到的调用用途
	parent    string // ExecuteNestedTool 收到的父调用 id
	nestedHit bool
	event     agentsvc.AgentToolEvent
	eventRes  *agentsvc.AgentToolEventResult
}

func (f *fakeBridge) ProxyModel(ctx context.Context, token string, body []byte, write func([]byte)) error {
	f.token, f.body = token, string(body)
	f.purpose = agentsvc.ModelPurposeFrom(ctx)
	if f.err != nil && !f.failAfter {
		return f.err
	}
	for _, l := range f.lines {
		write([]byte(l))
	}
	return f.err
}

func (f *fakeBridge) ExecuteTool(_ context.Context, token, id, name string, args json.RawMessage) (*agentsvc.ToolResult, error) {
	f.token, f.toolID, f.toolName, f.toolArgs = token, id, name, string(args)
	if f.err != nil {
		return nil, f.err
	}
	if f.toolRes != nil {
		return f.toolRes, nil
	}
	return &agentsvc.ToolResult{Content: "ok"}, nil
}

func (f *fakeBridge) ExecuteNestedTool(_ context.Context, token, parent, id, name string, args json.RawMessage) (*agentsvc.ToolResult, error) {
	f.nestedHit, f.parent = true, parent
	return f.ExecuteTool(context.Background(), token, id, name, args)
}

func (f *fakeBridge) ReportAgentTool(_ context.Context, token string, ev agentsvc.AgentToolEvent) (*agentsvc.AgentToolEventResult, error) {
	f.token, f.event = token, ev
	if f.err != nil {
		return nil, f.err
	}
	if f.eventRes != nil {
		return f.eventRes, nil
	}
	return &agentsvc.AgentToolEventResult{Allowed: true}, nil
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
	g.POST("/tool-event", h.ToolEvent)
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
		{"令牌无效", agentsvc.ErrBridgeToken, 401, "令牌"},
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

// 上游一直不响应（网关的空闲超时）要和「上游故障」分开：504 + upstream_timeout，文案直接说是模型响应超时，
// 用户和排障的人看到就知道是等不到模型、而不是请求被拒绝。
func TestBridgeHandler_ChatCompletions_IdleTimeoutIsGatewayTimeout(t *testing.T) {
	for name, err := range map[string]error{
		"直接返回": llmgateway.ErrIdleTimeout,
		"被包装过": fmt.Errorf("调用模型：%w", llmgateway.ErrIdleTimeout),
	} {
		t.Run(name, func(t *testing.T) {
			w := bridgeCall(newBridgeRouter(&fakeBridge{err: err}), "/v1/chat/completions", "tok", `{"messages":[{"role":"user","content":"x"}]}`)
			if w.Code != http.StatusGatewayTimeout {
				t.Fatalf("status=%d，期望 504: %s", w.Code, w.Body)
			}
			var body struct {
				Error struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
			}
			if jerr := json.Unmarshal(w.Body.Bytes(), &body); jerr != nil || body.Error.Type != "upstream_timeout" || !strings.Contains(body.Error.Message, "超时") {
				t.Errorf("应是 upstream_timeout 且文案说明是超时: %s", w.Body)
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
		api := &fakeBridge{toolRes: &agentsvc.ToolResult{Content: "画布有 3 个节点", Terminate: true}}
		w := bridgeCall(newBridgeRouter(api), "/tool", "tok", `{"tool_call_id":"tc1","name":"canvas_get_state","args":{"nodeIds":["a"]}}`)
		if w.Code != 200 {
			t.Fatalf("status=%d %s", w.Code, w.Body)
		}
		if api.token != "tok" || api.toolID != "tc1" || api.toolName != "canvas_get_state" || api.toolArgs != `{"nodeIds":["a"]}` {
			t.Errorf("api=%+v", api)
		}
		var resp struct {
			Code int
			Data agentsvc.ToolResult
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
		w := bridgeCall(newBridgeRouter(&fakeBridge{err: agentsvc.ErrBridgeToken}), "/tool", "tok", `{"tool_call_id":"a","name":"b"}`)
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
	t.Run("结束：paused 也接受（因工具要求停下而结束）", func(t *testing.T) {
		api := &fakeBridge{}
		if w := bridgeCall(newBridgeRouter(api), "/finish", "tok", `{"status":"paused"}`); w.Code != 200 || api.finStatus != "paused" {
			t.Errorf("status=%d api=%+v", w.Code, api)
		}
	})
	t.Run("结束：不认识的 status → 400", func(t *testing.T) {
		if w := bridgeCall(newBridgeRouter(&fakeBridge{}), "/finish", "tok", `{"status":"weird"}`); w.Code != 400 {
			t.Errorf("status=%d", w.Code)
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
			if w := bridgeCall(newBridgeRouter(&fakeBridge{err: agentsvc.ErrBridgeToken}), p, "tok", body); w.Code != 401 {
				t.Errorf("%s status=%d", p, w.Code)
			}
		}
	})
}

func TestBridgeHandler_ChatCompletions_PurposeHeader(t *testing.T) {
	call := func(api *fakeBridge, purpose string) {
		req := httptest.NewRequest(http.MethodPost, "/internal/agent/bridge/v1/chat/completions", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer tok-1")
		if purpose != "" {
			req.Header.Set("X-Agent-Purpose", purpose)
		}
		newBridgeRouter(api).ServeHTTP(httptest.NewRecorder(), req)
	}
	api := &fakeBridge{}
	call(api, "summary")
	if api.purpose != agentsvc.ModelPurposeSummary {
		t.Errorf("X-Agent-Purpose: summary 应传给业务层，实际 %q", api.purpose)
	}
	call(api, "")
	if api.purpose != "" {
		t.Errorf("没带请求头就是普通对话调用，实际 %q", api.purpose)
	}
	call(api, "something-else")
	if api.purpose != "" {
		t.Errorf("不认识的用途一律按普通调用处理，免得被用来藏起助手的回复，实际 %q", api.purpose)
	}
}

func TestBridgeHandler_Tool_NestedCallUsesParentID(t *testing.T) {
	api := &fakeBridge{toolRes: &agentsvc.ToolResult{Content: "ok"}}
	w := bridgeCall(newBridgeRouter(api), "/tool", "tok-1", `{"tool_call_id":"cm1.1","name":"canvas_get_state","args":{},"parent_tool_call_id":"cm1"}`)
	if w.Code != 200 || !api.nestedHit || api.parent != "cm1" || api.toolID != "cm1.1" {
		t.Errorf("带 parent_tool_call_id 的调用走嵌套: code=%d nested=%v parent=%q id=%q", w.Code, api.nestedHit, api.parent, api.toolID)
	}
	api2 := &fakeBridge{toolRes: &agentsvc.ToolResult{Content: "ok"}}
	bridgeCall(newBridgeRouter(api2), "/tool", "tok-1", `{"tool_call_id":"c1","name":"canvas_get_state","args":{}}`)
	if api2.nestedHit {
		t.Error("没带 parent_tool_call_id 是普通调用")
	}
}

func TestBridgeHandler_ToolEvent(t *testing.T) {
	api := &fakeBridge{}
	w := bridgeCall(newBridgeRouter(api), "/tool-event", "tok-1", `{"phase":"end","tool_call_id":"cm1","name":"codemode","label":"运行脚本","verb":"脚本","is_error":true,"summary":"脚本失败"}`)
	if w.Code != 200 {
		t.Fatalf("status=%d %s", w.Code, w.Body)
	}
	ev := api.event
	if api.token != "tok-1" || ev.Phase != "end" || ev.ToolCallID != "cm1" || ev.Name != "codemode" || ev.Label != "运行脚本" || ev.Verb != "脚本" || !ev.IsError || ev.Summary != "脚本失败" {
		t.Errorf("token=%q ev=%+v", api.token, ev)
	}
	var out struct {
		Data struct {
			Allowed   bool `json:"allowed"`
			Terminate bool `json:"terminate"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if !out.Data.Allowed {
		t.Errorf("响应里应有 allowed: %s", w.Body)
	}
	api.eventRes = &agentsvc.AgentToolEventResult{Allowed: false, Terminate: true, Content: "步数用尽"}
	w = bridgeCall(newBridgeRouter(api), "/tool-event", "tok-1", `{"phase":"start","tool_call_id":"cm1","name":"codemode"}`)
	if !strings.Contains(w.Body.String(), `"terminate":true`) || !strings.Contains(w.Body.String(), "步数用尽") {
		t.Errorf("不允许时要带上原因和 terminate: %s", w.Body)
	}
}

func TestBridgeHandler_ToolEvent_RejectsBadInput(t *testing.T) {
	r := newBridgeRouter(&fakeBridge{})
	if w := bridgeCall(r, "/tool-event", "", `{"phase":"start","tool_call_id":"c","name":"codemode"}`); w.Code != 401 && !strings.Contains(w.Body.String(), "令牌") {
		t.Errorf("没有令牌: %d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{"phase":"pause","tool_call_id":"c","name":"x"}`, `{"phase":"start","name":"x"}`, `{"phase":"start","tool_call_id":"c"}`, `{}`} {
		w := bridgeCall(r, "/tool-event", "tok-1", body)
		if strings.Contains(w.Body.String(), `"code":0`) {
			t.Errorf("%s 应被拒绝: %s", body, w.Body)
		}
	}
}

// fakeRunTagger 让桥 handler 的 TagRun 中间件能把令牌解析成运行 id。
type fakeRunTagger struct {
	*fakeBridge
}

func (f fakeRunTagger) RunIDForToken(tok string) uint64 {
	if tok == "good" {
		return 77
	}
	return 0
}

func TestBridgeHandler_TagRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	serve := func(h *AgentBridgeHandler, authz string) uint64 {
		r := gin.New()
		r.Use(h.TagRun())
		var got uint64
		r.POST("/x", func(c *gin.Context) { got = c.GetUint64(middleware.CtxAgentRunIDKey); c.Status(200) })
		req := httptest.NewRequest("POST", "/x", nil)
		if authz != "" {
			req.Header.Set("Authorization", authz)
		}
		r.ServeHTTP(httptest.NewRecorder(), req)
		return got
	}
	h := NewAgentBridgeHandler(fakeRunTagger{&fakeBridge{}})
	if got := serve(h, "Bearer good"); got != 77 {
		t.Errorf("有效令牌应把运行 id 写进上下文，访问日志据此关联：%d", got)
	}
	if got := serve(h, "Bearer bad"); got != 0 {
		t.Errorf("无效令牌不写：%d", got)
	}
	if got := serve(h, ""); got != 0 {
		t.Errorf("没有令牌不写：%d", got)
	}
	// 业务层不支持解析运行 id 时：什么都不做，不报错
	if got := serve(NewAgentBridgeHandler(&fakeBridge{}), "Bearer good"); got != 0 {
		t.Errorf("got=%d", got)
	}
}
