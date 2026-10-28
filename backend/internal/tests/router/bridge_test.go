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
	"video-canvas/internal/model"
	"video-canvas/internal/router"
	agentsvc "video-canvas/internal/service/agent"
)

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

func (p *platformStub) PrepareRun(context.Context, agentsvc.RunContext, agentsvc.PrepareRequest) (*agentsvc.RunSetup, error) {
	p.calls++
	return &agentsvc.RunSetup{}, nil
}
func (p *platformStub) AgentModels(context.Context) ([]model.AgentModelView, error) {
	p.calls++
	return []model.AgentModelView{}, nil
}
func (p *platformStub) CheckCanvas(context.Context, uint64, uint64) error {
	p.calls++
	return nil
}
func (p *platformStub) CheckAttachments(context.Context, uint64, []string) ([]agentsvc.AttachmentCheck, error) {
	return nil, nil
}

func (p *platformStub) CheckMessage(context.Context, uint64, uint64, string, []string) ([]string, error) {
	p.calls++
	return nil, nil
}
func (p *platformStub) UndoPlatformRun(context.Context, agentsvc.RunContext) (*agentsvc.UndoResult, error) {
	p.calls++
	return &agentsvc.UndoResult{}, nil
}
func (p *platformStub) ProposeApproval(context.Context, agentsvc.RunContext, string, json.RawMessage) (*agentsvc.ApprovalProposal, error) {
	p.calls++
	return &agentsvc.ApprovalProposal{}, nil
}
func (p *platformStub) ResolveApproval(context.Context, agentsvc.RunContext, string, json.RawMessage, model.DecideAgentApprovalReq) (*agentsvc.ApprovalOutcome, error) {
	p.calls++
	return &agentsvc.ApprovalOutcome{}, nil
}
func (p *platformStub) ExecuteApproval(context.Context, agentsvc.RunContext, string, uint64, string, agentsvc.ApprovalOutcome) (*agentsvc.ApprovalExecution, error) {
	p.calls++
	return &agentsvc.ApprovalExecution{}, nil
}

const platformCtxJSON = `{"run_id":"9","user_id":"1","session_id":"5","canvas_id":"7"}`

func platformReq(t *testing.T, srv *httptest.Server, method, path, serviceToken string) int {
	t.Helper()
	return platformReqBody(t, srv, method, path, serviceToken, `{}`)
}

func platformReqBody(t *testing.T, srv *httptest.Server, method, path, serviceToken, body string) int {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
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
	srv := httptest.NewServer(router.NewBridge("test", agenthandler.NewPlatformHandler(plat), token))
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

// 运行准备、审批的预估解析执行、会话侧的画布校验 / 消息校验 / 撤销同样只认服务令牌：执行会删画布、创建生成任务（花积分），不能裸露
func TestNewBridge_ApprovalRoutesNeedTheServiceToken(t *testing.T) {
	const token = "service-token-0123456789abcdef0123456789"
	plat := &platformStub{}
	srv := httptest.NewServer(router.NewBridge("test", agenthandler.NewPlatformHandler(plat), token))
	defer srv.Close()
	for path, body := range map[string]string{
		"/internal/platform/v1/canvases/check":    `{"user_id":"1","canvas_id":"7"}`,
		"/internal/platform/v1/messages/check":    `{"user_id":"1","canvas_id":"7","message":"x"}`,
		"/internal/platform/v1/runs/9/undo":       `{}`,
		"/internal/platform/v1/runs/prepare":      `{}`,
		"/internal/platform/v1/approvals/propose": `{"kind":"ask","args":{}}`,
		"/internal/platform/v1/approvals/resolve": `{"kind":"ask","payload":{},"decision":{"decision":"approve"}}`,
		"/internal/platform/v1/approvals/execute": `{"kind":"delete","approval_id":"1","outcome":{"status":"approved"}}`,
	} {
		before := plat.calls
		if code := platformReqBody(t, srv, http.MethodPost, path, "", body); code != http.StatusUnauthorized || plat.calls != before {
			t.Errorf("%s 没带服务令牌应 401 且不进业务: code=%d", path, code)
		}
		if code := platformReqBody(t, srv, http.MethodPost, path, token, body); code != http.StatusOK || plat.calls != before+1 {
			t.Errorf("%s 令牌正确应放行: code=%d calls=%d", path, code, plat.calls)
		}
	}
}

// 没配服务令牌时（桥只监听回环、只靠运行令牌）平台接口根本不存在：它是整个内网信任的入口，不能在没有这道门的情况下暴露
func TestNewBridge_PlatformRoutesAreAbsentWithoutServiceToken(t *testing.T) {
	plat := &platformStub{}
	srv := httptest.NewServer(router.NewBridge("test", agenthandler.NewPlatformHandler(plat), ""))
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
	srv := httptest.NewServer(router.NewBridge("test", nil, token))
	defer srv.Close()
	if code := platformReq(t, srv, http.MethodPost, "/internal/platform/v1/tools/canvas_get_state", token); code != http.StatusNotFound {
		t.Errorf("没有平台 handler 时应 404: %d", code)
	}
}

type relayCounter struct{ batches int }

func (r *relayCounter) Publish(context.Context, []agentsvc.RelayEvent) error { r.batches++; return nil }

// 事件推送只在 durable 模式（配了中继）挂载，并且和其他平台接口一样只认服务令牌：它能往任意用户的 WebSocket 频道推消息。
func TestNewBridge_EventPushRouteNeedsRelayAndServiceToken(t *testing.T) {
	const token = "service-token-0123456789abcdef0123456789"
	const body = `{"events":[{"user_id":"1","session_id":"5","canvas_id":"7","run_id":"9","seq":1,"type":"run.status","data":{},"created_at":"2026-10-25T08:00:00Z"}]}`

	relay := &relayCounter{}
	srv := httptest.NewServer(router.NewBridge("test", agenthandler.NewPlatformHandler(&platformStub{}).WithEventRelay(relay), token))
	defer srv.Close()
	if code := platformReqBody(t, srv, http.MethodPost, "/internal/platform/v1/events", "", body); code != http.StatusUnauthorized || relay.batches != 0 {
		t.Errorf("没带服务令牌应 401 且不能进入业务: code=%d batches=%d", code, relay.batches)
	}
	if code := platformReqBody(t, srv, http.MethodPost, "/internal/platform/v1/events", token, body); code != http.StatusOK || relay.batches != 1 {
		t.Errorf("令牌正确应放行: code=%d batches=%d", code, relay.batches)
	}

	// 没配中继（http / process 模式）：路由不存在
	plain := httptest.NewServer(router.NewBridge("test", agenthandler.NewPlatformHandler(&platformStub{}), token))
	defer plain.Close()
	if code := platformReqBody(t, plain, http.MethodPost, "/internal/platform/v1/events", token, body); code != http.StatusNotFound {
		t.Errorf("没配中继不应挂载事件推送: %d", code)
	}
}

func TestNewBridge_IDProbeNeedsTheServiceToken(t *testing.T) {
	const token = "service-token-0123456789abcdef0123456789"
	srv := httptest.NewServer(router.NewBridge("test", agenthandler.NewPlatformHandler(&platformStub{}), token))
	defer srv.Close()
	if code := platformReq(t, srv, http.MethodGet, "/internal/platform/v1/ids/7", ""); code != http.StatusUnauthorized {
		t.Errorf("没带服务令牌应 401: %d", code)
	}
	if code := platformReq(t, srv, http.MethodGet, "/internal/platform/v1/ids/7", token); code != http.StatusOK {
		t.Errorf("令牌正确应放行: %d", code)
	}
}
