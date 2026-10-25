package router_test

import (
	"context"
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
	srv := httptest.NewServer(router.NewBridge("test", agenthandler.NewAgentBridgeHandler(stub), token))
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
	srv := httptest.NewServer(router.NewBridge("test", agenthandler.NewAgentBridgeHandler(stub), ""))
	defer srv.Close()
	if code := finish(t, srv, ""); code != http.StatusOK || stub.finishes != 1 {
		t.Errorf("没配服务令牌时只靠运行令牌: code=%d finishes=%d", code, stub.finishes)
	}
}
