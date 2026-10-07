package agent_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler/agent"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	agentsvc "video-canvas/internal/service/agent"
)

// fakeAgentAPI 记录 handler 传给业务层的参数，并按 err 返回业务错误，用来验证绑定、编码 id、状态码和错误透传。
type fakeAgentAPI struct {
	err       error
	calls     []string
	gotUser   uint64
	gotID     uint64
	gotCanvas uint64
	gotStart  *model.StartAgentRunReq
	gotDecide *model.DecideAgentApprovalReq
	gotAfter  int64
	gotLimit  int
	gotBudget int
	gotText   string
}

func (f *fakeAgentAPI) rec(name string, user, id uint64) error {
	f.calls = append(f.calls, name)
	f.gotUser, f.gotID = user, id
	return f.err
}

func (f *fakeAgentAPI) Models(context.Context) ([]model.AgentModelView, error) {
	return []model.AgentModelView{{Key: "claude", Name: "Claude", Vision: true}}, f.err
}
func (f *fakeAgentAPI) ListSessions(_ context.Context, u, canvas uint64) ([]*agentsvc.AgentSessionView, error) {
	f.gotCanvas = canvas
	return []*agentsvc.AgentSessionView{{ID: 1, Title: "新对话"}}, f.rec("list", u, canvas)
}
func (f *fakeAgentAPI) CreateSession(_ context.Context, u, canvas uint64, _ *model.CreateAgentSessionReq) (*agentsvc.AgentSessionView, error) {
	f.gotCanvas = canvas
	return &agentsvc.AgentSessionView{ID: 5, Title: "新对话"}, f.rec("create", u, canvas)
}
func (f *fakeAgentAPI) RenameSession(_ context.Context, u, id uint64, title string) (*agentsvc.AgentSessionView, error) {
	f.gotText = title
	return &agentsvc.AgentSessionView{ID: idcodec.ID(id), Title: title}, f.rec("rename", u, id)
}
func (f *fakeAgentAPI) DeleteSession(_ context.Context, u, id uint64) error {
	return f.rec("delete", u, id)
}
func (f *fakeAgentAPI) Events(_ context.Context, u, id uint64, after int64, limit int) ([]*agentsvc.AgentEventView, error) {
	f.gotAfter, f.gotLimit = after, limit
	return []*agentsvc.AgentEventView{{Seq: after + 1, Type: "message.delta"}}, f.rec("events", u, id)
}
func (f *fakeAgentAPI) StartRun(_ context.Context, u, id uint64, req *model.StartAgentRunReq) (*agentsvc.AgentRunView, error) {
	f.gotStart = req
	return &agentsvc.AgentRunView{ID: 9, Status: "queued"}, f.rec("start", u, id)
}
func (f *fakeAgentAPI) Interject(_ context.Context, u, id uint64, msg string) error {
	f.gotText = msg
	return f.rec("interject", u, id)
}
func (f *fakeAgentAPI) Cancel(_ context.Context, u, id uint64) (*agentsvc.AgentRunView, error) {
	return &agentsvc.AgentRunView{ID: idcodec.ID(id), Status: "canceled"}, f.rec("cancel", u, id)
}
func (f *fakeAgentAPI) Resume(_ context.Context, u, id uint64, add int) (*agentsvc.AgentRunView, error) {
	f.gotBudget = add
	return &agentsvc.AgentRunView{ID: idcodec.ID(id), Status: "queued"}, f.rec("resume", u, id)
}
func (f *fakeAgentAPI) Undo(_ context.Context, u, id uint64) (*agentsvc.UndoResult, error) {
	return &agentsvc.UndoResult{Reverted: 3}, f.rec("undo", u, id)
}
func (f *fakeAgentAPI) Decide(_ context.Context, u, id uint64, req *model.DecideAgentApprovalReq) (*agentsvc.AgentApprovalView, error) {
	f.gotDecide = req
	return &agentsvc.AgentApprovalView{ID: idcodec.ID(id), Status: "approved"}, f.rec("decide", u, id)
}

func newAgentRouter(api AgentAPI) *gin.Engine {
	h := NewAgentHandler(api)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	v1 := r.Group("/api/v1")
	v1.GET("/canvas/:id/agent/sessions", h.ListSessions)
	v1.POST("/canvas/:id/agent/sessions", h.CreateSession)
	a := v1.Group("/agent")
	a.GET("/models", h.Models)
	a.PATCH("/sessions/:sid", h.RenameSession)
	a.DELETE("/sessions/:sid", h.DeleteSession)
	a.GET("/sessions/:sid/events", h.Events)
	a.POST("/sessions/:sid/runs", h.StartRun)
	a.POST("/runs/:rid/interject", h.Interject)
	a.POST("/runs/:rid/cancel", h.Cancel)
	a.POST("/runs/:rid/resume", h.Resume)
	a.POST("/runs/:rid/undo", h.Undo)
	a.POST("/approvals/:aid/decision", h.Decide)
	return r
}

func agentCall(t *testing.T, r *gin.Engine, method, path, body string) (int, map[string]any) {
	t.Helper()
	return canvasCall(t, r, method, path, body) // 同一个请求辅助：JSON 进，JSON 出
}

func codeOf(resp map[string]any) int { c, _ := resp["code"].(float64); return int(c) }

// 每个接口：成功（带上编码 id、当前用户）、编码 id 格式错误 → 400 + 10001、业务错误原样透传。
func TestAgentHandler_AllEndpoints(t *testing.T) {
	sid, rid, aid, cid := idcodec.Encode(11), idcodec.Encode(22), idcodec.Encode(33), idcodec.Encode(44)
	cases := []struct {
		name, method, path, body string
		wantStatus               int
		wantCall                 string
		wantID                   uint64
	}{
		{"模型清单", "GET", "/api/v1/agent/models", "", 200, "", 0},
		{"会话列表", "GET", "/api/v1/canvas/" + cid + "/agent/sessions", "", 200, "list", 44},
		{"新建会话", "POST", "/api/v1/canvas/" + cid + "/agent/sessions", `{"title":"分镜","mode":"storyboard"}`, 200, "create", 44},
		{"重命名", "PATCH", "/api/v1/agent/sessions/" + sid, `{"title":"新名字"}`, 200, "rename", 11},
		{"删除会话", "DELETE", "/api/v1/agent/sessions/" + sid, "", 200, "delete", 11},
		{"事件回放", "GET", "/api/v1/agent/sessions/" + sid + "/events?after=7&limit=50", "", 200, "events", 11},
		{"发起运行返回 202", "POST", "/api/v1/agent/sessions/" + sid + "/runs", `{"message":"拆分镜","selection":["n1"]}`, 202, "start", 11},
		{"插话", "POST", "/api/v1/agent/runs/" + rid + "/interject", `{"message":"再暖一点"}`, 200, "interject", 22},
		{"停止", "POST", "/api/v1/agent/runs/" + rid + "/cancel", "", 200, "cancel", 22},
		{"继续", "POST", "/api/v1/agent/runs/" + rid + "/resume", `{"add_budget":20}`, 200, "resume", 22},
		{"撤销本轮", "POST", "/api/v1/agent/runs/" + rid + "/undo", "", 200, "undo", 22},
		{"审批决定", "POST", "/api/v1/agent/approvals/" + aid + "/decision", `{"decision":"approve","items":[{"index":0,"approve":true,"count":1}]}`, 200, "decide", 33},
	}
	for _, tc := range cases {
		t.Run(tc.name+"：成功", func(t *testing.T) {
			api := &fakeAgentAPI{}
			status, resp := agentCall(t, newAgentRouter(api), tc.method, tc.path, tc.body)
			if status != tc.wantStatus || codeOf(resp) != 0 {
				t.Fatalf("status=%d resp=%v", status, resp)
			}
			if tc.wantCall != "" && (len(api.calls) != 1 || api.calls[0] != tc.wantCall || api.gotID != tc.wantID || api.gotUser != 1) {
				t.Errorf("业务层收到的参数不对: calls=%v id=%d user=%d", api.calls, api.gotID, api.gotUser)
			}
		})
		t.Run(tc.name+"：业务错误原样透传", func(t *testing.T) {
			if tc.wantCall == "" {
				t.Skip("无业务错误分支")
			}
			api := &fakeAgentAPI{err: errcode.ErrAgentRunActive}
			status, resp := agentCall(t, newAgentRouter(api), tc.method, tc.path, tc.body)
			if status != http.StatusConflict || codeOf(resp) != errcode.ErrAgentRunActive.Code {
				t.Errorf("应透传 409 + 60001: %d %v", status, resp)
			}
		})
	}
}

func TestAgentHandler_BadIDs(t *testing.T) {
	r := newAgentRouter(&fakeAgentAPI{})
	for _, bad := range []string{"1", "abc", strings.Repeat("0", 32)} {
		for _, p := range []struct{ method, path, body string }{
			{"PATCH", "/api/v1/agent/sessions/" + bad, `{"title":"x"}`},
			{"DELETE", "/api/v1/agent/sessions/" + bad, ""},
			{"GET", "/api/v1/agent/sessions/" + bad + "/events", ""},
			{"POST", "/api/v1/agent/sessions/" + bad + "/runs", `{"message":"x"}`},
			{"POST", "/api/v1/agent/runs/" + bad + "/interject", `{"message":"x"}`},
			{"POST", "/api/v1/agent/runs/" + bad + "/cancel", ""},
			{"POST", "/api/v1/agent/runs/" + bad + "/resume", `{}`},
			{"POST", "/api/v1/agent/runs/" + bad + "/undo", ""},
			{"POST", "/api/v1/agent/approvals/" + bad + "/decision", `{"decision":"reject"}`},
			{"GET", "/api/v1/canvas/" + bad + "/agent/sessions", ""},
			{"POST", "/api/v1/canvas/" + bad + "/agent/sessions", `{}`},
		} {
			status, resp := agentCall(t, r, p.method, p.path, p.body)
			if status != http.StatusBadRequest || codeOf(resp) != errcode.ErrInvalidParams.Code {
				t.Errorf("%s %s 应报 400 + 10001: %d %v", p.method, p.path, status, resp)
			}
		}
	}
}

func TestAgentHandler_ParamValidation(t *testing.T) {
	sid, rid, aid := idcodec.Encode(11), idcodec.Encode(22), idcodec.Encode(33)
	bad := []struct{ name, method, path, body string }{
		{"重命名缺标题", "PATCH", "/api/v1/agent/sessions/" + sid, `{}`},
		{"重命名标题超长", "PATCH", "/api/v1/agent/sessions/" + sid, `{"title":"` + strings.Repeat("长", 41) + `"}`},
		{"新建会话模式不认识", "POST", "/api/v1/canvas/" + idcodec.Encode(1) + "/agent/sessions", `{"mode":"hacker"}`},
		{"发起运行缺消息", "POST", "/api/v1/agent/sessions/" + sid + "/runs", `{}`},
		{"发起运行预算为负", "POST", "/api/v1/agent/sessions/" + sid + "/runs", `{"message":"x","budget_credits":-1}`},
		{"发起运行模式不认识", "POST", "/api/v1/agent/sessions/" + sid + "/runs", `{"message":"x","mode":"zzz"}`},
		{"插话缺消息", "POST", "/api/v1/agent/runs/" + rid + "/interject", `{}`},
		{"继续追加预算为负", "POST", "/api/v1/agent/runs/" + rid + "/resume", `{"add_budget":-5}`},
		{"决定缺 decision", "POST", "/api/v1/agent/approvals/" + aid + "/decision", `{}`},
		{"决定值不认识", "POST", "/api/v1/agent/approvals/" + aid + "/decision", `{"decision":"maybe"}`},
		{"条目张数超过 4", "POST", "/api/v1/agent/approvals/" + aid + "/decision", `{"decision":"approve","items":[{"index":0,"approve":true,"count":9}]}`},
		{"回放 after 为负", "GET", "/api/v1/agent/sessions/" + sid + "/events?after=-1", ""},
		{"回放 limit 超上限", "GET", "/api/v1/agent/sessions/" + sid + "/events?limit=201", ""},
		{"请求体不是 JSON", "POST", "/api/v1/agent/runs/" + rid + "/interject", `not json`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAgentAPI{}
			status, resp := agentCall(t, newAgentRouter(api), tc.method, tc.path, tc.body)
			if status != http.StatusBadRequest || codeOf(resp) != errcode.ErrInvalidParams.Code {
				t.Errorf("应 400 + 10001: %d %v", status, resp)
			}
			if len(api.calls) != 0 {
				t.Errorf("参数不合法时不应调用业务层: %v", api.calls)
			}
		})
	}
}

func TestAgentHandler_PassesParsedValues(t *testing.T) {
	sid, rid := idcodec.Encode(11), idcodec.Encode(22)

	api := &fakeAgentAPI{}
	agentCall(t, newAgentRouter(api), "POST", "/api/v1/agent/sessions/"+sid+"/runs", `{"message":"拆分镜","mode":"storyboard","selection":["a","b"],"budget_credits":0,"agent_model_key":"claude"}`)
	if got := api.gotStart; got == nil || got.Message != "拆分镜" || got.Mode != "storyboard" || len(got.Selection) != 2 || got.BudgetCredits == nil || *got.BudgetCredits != 0 || got.AgentModelKey != "claude" {
		t.Errorf("start=%+v（预算 0 必须能和「没传」区分开）", got)
	}

	api = &fakeAgentAPI{}
	agentCall(t, newAgentRouter(api), "GET", "/api/v1/agent/sessions/"+sid+"/events?after=7&limit=50", "")
	if api.gotAfter != 7 || api.gotLimit != 50 {
		t.Errorf("after=%d limit=%d", api.gotAfter, api.gotLimit)
	}

	api = &fakeAgentAPI{}
	agentCall(t, newAgentRouter(api), "POST", "/api/v1/agent/runs/"+rid+"/resume", `{"add_budget":20}`)
	if api.gotBudget != 20 {
		t.Errorf("add_budget=%d", api.gotBudget)
	}

	api = &fakeAgentAPI{}
	req := httptest.NewRequest("POST", "/api/v1/agent/approvals/"+idcodec.Encode(33)+"/decision", strings.NewReader(`{"decision":"approve","answer":"16:9","add_budget":5}`))
	req.Header.Set("Content-Type", "application/json")
	newAgentRouter(api).ServeHTTP(httptest.NewRecorder(), req)
	if d := api.gotDecide; d == nil || d.Decision != "approve" || d.Answer != "16:9" || d.AddBudget != 5 {
		t.Errorf("decide=%+v", d)
	}
}

// 响应里的 id 对外必须是编码串，和画布一致，前端才能直接拿它拼路径。
func TestAgentHandler_ResponseIDsAreEncoded(t *testing.T) {
	_, resp := agentCall(t, newAgentRouter(&fakeAgentAPI{}), "POST", "/api/v1/agent/sessions/"+idcodec.Encode(11)+"/runs", `{"message":"x"}`)
	data, _ := resp["data"].(map[string]any)
	if data["id"] != idcodec.Encode(9) {
		t.Errorf("运行 id 应是编码串: %v", data["id"])
	}
}
