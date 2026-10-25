package router_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agenthandler "video-canvas/internal/handler/agent"
	"video-canvas/internal/middleware"
	"video-canvas/internal/router"
	agentsvc "video-canvas/internal/service/agent"
)

// stubBridge 只数一数 Finish 被调用了几次：服务令牌的检查必须发生在业务之前。
type stubBridge struct{ finishes int }

func (s *stubBridge) ProxyModel(context.Context, string, []byte, func([]byte)) error { return nil }
func (s *stubBridge) ExecuteTool(context.Context, string, string, string, json.RawMessage) (*agentsvc.ToolResult, error) {
	return &agentsvc.ToolResult{Content: "ok"}, nil
}
func (s *stubBridge) ExecuteNestedTool(context.Context, string, string, string, string, json.RawMessage) (*agentsvc.ToolResult, error) {
	return &agentsvc.ToolResult{Content: "ok"}, nil
}
func (s *stubBridge) ReportAgentTool(context.Context, string, agentsvc.AgentToolEvent) (*agentsvc.AgentToolEventResult, error) {
	return &agentsvc.AgentToolEventResult{Allowed: true}, nil
}
func (s *stubBridge) SaveState(context.Context, string, json.RawMessage) error { return nil }
func (s *stubBridge) Finish(context.Context, string, string, string) error     { s.finishes++; return nil }

func finish(t *testing.T, srv *httptest.Server, serviceToken string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/internal/agent/bridge/finish", strings.NewReader(`{"status":"done"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer run-token")
	if serviceToken != "" {
		req.Header.Set(middleware.ServiceTokenHeader, serviceToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestNewBridge_ServiceToken(t *testing.T) {
	const token = "service-token-0123456789abcdef0123456789"
	stub := &stubBridge{}
	srv := httptest.NewServer(router.NewBridge("test", agenthandler.NewAgentBridgeHandler(stub), nil, token))
	defer srv.Close()

	if code := finish(t, srv, ""); code != http.StatusUnauthorized {
		t.Errorf("配了服务令牌，没带就该 401，实际 %d", code)
	}
	if code := finish(t, srv, "wrong-token-0123456789abcdef012345678"); code != http.StatusUnauthorized {
		t.Errorf("令牌不对该 401，实际 %d", code)
	}
	if stub.finishes != 0 {
		t.Fatalf("被拒绝的请求不能进入业务: finishes=%d", stub.finishes)
	}
	if code := finish(t, srv, token); code != http.StatusOK || stub.finishes != 1 {
		t.Errorf("令牌正确应放行: code=%d finishes=%d", code, stub.finishes)
	}
}

func TestNewBridge_NoServiceTokenKeepsOldBehaviour(t *testing.T) {
	stub := &stubBridge{}
	srv := httptest.NewServer(router.NewBridge("test", agenthandler.NewAgentBridgeHandler(stub), nil, ""))
	defer srv.Close()
	if code := finish(t, srv, ""); code != http.StatusOK || stub.finishes != 1 {
		t.Errorf("没配服务令牌时只靠运行令牌: code=%d finishes=%d", code, stub.finishes)
	}
}

// ---- 平台 API 的路由 ----

type platformStub struct{ calls, ended int }

func (p *platformStub) ExecutePlatformTool(context.Context, agentsvc.RunContext, string, string, json.RawMessage) (*agentsvc.ToolResult, error) {
	p.calls++
	return &agentsvc.ToolResult{Content: "ok"}, nil
}
func (p *platformStub) EndPlatformRun(uint64) { p.ended++ }
func (p *platformStub) ProxyModelPlatform(_ context.Context, _ agentsvc.RunContext, _ []byte, _ func(uint64), write func([]byte)) error {
	p.calls++
	write([]byte("data: [DONE]\n\n"))
	return nil
}
func (p *platformStub) PlatformRunUsage(context.Context, agentsvc.RunContext, uint64) (*agentsvc.PlatformUsage, error) {
	p.calls++
	return &agentsvc.PlatformUsage{}, nil
}

const platformCtxJSON = `{"run_id":"9","user_id":"1","session_id":"5","canvas_id":"7"}`

func platformReq(t *testing.T, srv *httptest.Server, method, path, serviceToken string) int {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Context", base64.RawURLEncoding.EncodeToString([]byte(platformCtxJSON)))
	if serviceToken != "" {
		req.Header.Set(middleware.ServiceTokenHeader, serviceToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestNewBridge_PlatformRoutesNeedTheServiceToken(t *testing.T) {
	const token = "service-token-0123456789abcdef0123456789"
	plat := &platformStub{}
	srv := httptest.NewServer(router.NewBridge("test", agenthandler.NewAgentBridgeHandler(&stubBridge{}), agenthandler.NewPlatformHandler(plat), token))
	defer srv.Close()

	// 平台接口不用桥的运行令牌（Authorization），只认服务令牌
	if code := platformReq(t, srv, http.MethodPost, "/internal/platform/v1/tools/canvas_get_state", ""); code != http.StatusUnauthorized {
		t.Errorf("没带服务令牌应 401，实际 %d", code)
	}
	if code := platformReq(t, srv, http.MethodPost, "/internal/platform/v1/tools/canvas_get_state", "wrong-token-0123456789abcdef012345678"); code != http.StatusUnauthorized {
		t.Errorf("令牌不对应 401，实际 %d", code)
	}
	if plat.calls != 0 {
		t.Fatalf("被拒绝的请求不能进入业务: %d", plat.calls)
	}
	if code := platformReq(t, srv, http.MethodPost, "/internal/platform/v1/tools/canvas_get_state", token); code != http.StatusOK || plat.calls != 1 {
		t.Errorf("令牌正确应放行: code=%d calls=%d", code, plat.calls)
	}
	if code := platformReq(t, srv, http.MethodDelete, "/internal/platform/v1/runs/9", token); code != http.StatusOK || plat.ended != 1 {
		t.Errorf("释放运行状态: code=%d ended=%d", code, plat.ended)
	}
	if code := platformReq(t, srv, http.MethodDelete, "/internal/platform/v1/runs/9", ""); code != http.StatusUnauthorized {
		t.Errorf("释放运行状态也要服务令牌: %d", code)
	}
	// 模型代理和用量查询同样只认服务令牌
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/internal/platform/v1/model/chat/completions"},
		{http.MethodGet, "/internal/platform/v1/runs/9/usage"},
	} {
		before := plat.calls
		if code := platformReq(t, srv, c.method, c.path, ""); code != http.StatusUnauthorized {
			t.Errorf("%s %s 没带服务令牌应 401: %d", c.method, c.path, code)
		}
		if plat.calls != before {
			t.Errorf("%s 被拒绝的请求不能进入业务", c.path)
		}
		if code := platformReq(t, srv, c.method, c.path, token); code != http.StatusOK || plat.calls != before+1 {
			t.Errorf("%s %s 令牌正确应放行: code=%d", c.method, c.path, code)
		}
	}
}

// 没配服务令牌时（桥只监听回环、只靠运行令牌）平台接口根本不存在：它是整个内网信任的入口，不能在没有这道门的情况下暴露
func TestNewBridge_PlatformRoutesAreAbsentWithoutServiceToken(t *testing.T) {
	plat := &platformStub{}
	srv := httptest.NewServer(router.NewBridge("test", agenthandler.NewAgentBridgeHandler(&stubBridge{}), agenthandler.NewPlatformHandler(plat), ""))
	defer srv.Close()
	for _, tok := range []string{"", "anything-0123456789abcdef0123456789abcd"} {
		if code := platformReq(t, srv, http.MethodPost, "/internal/platform/v1/tools/canvas_get_state", tok); code != http.StatusNotFound {
			t.Errorf("没配服务令牌时平台路由不存在，应 404，实际 %d（请求带令牌=%v）", code, tok != "")
		}
	}
	if plat.calls != 0 {
		t.Error("不能进入业务")
	}
}

func TestNewBridge_NoPlatformHandlerMeansNoPlatformRoutes(t *testing.T) {
	const token = "service-token-0123456789abcdef0123456789"
	srv := httptest.NewServer(router.NewBridge("test", agenthandler.NewAgentBridgeHandler(&stubBridge{}), nil, token))
	defer srv.Close()
	if code := platformReq(t, srv, http.MethodPost, "/internal/platform/v1/tools/canvas_get_state", token); code != http.StatusNotFound {
		t.Errorf("没有平台 handler 时应 404: %d", code)
	}
}
