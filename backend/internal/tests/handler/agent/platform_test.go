package agent_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/agent/canvasgraph"
	. "video-canvas/internal/handler/agent"
	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	agentsvc "video-canvas/internal/service/agent"
)

// fakePlatform 记录平台 handler 传给业务层的参数。
type fakePlatform struct {
	// 附件校验
	attUser     uint64
	attIDs      []string
	attachments []agentsvc.AttachmentCheck

	rc      agentsvc.RunContext
	callID  string
	name    string
	args    string
	idemKey string
	res     *agentsvc.ToolResult
	err     error
	ended   []uint64

	// 模型代理
	body        string
	purpose     string
	modelCallID uint64
	lines       []string
	// 用量
	usage       *agentsvc.PlatformUsage
	usageCallID uint64

	// 审批
	kind       string
	payload    string
	decision   model.DecideAgentApprovalReq
	approvalID uint64
	outcome    agentsvc.ApprovalOutcome
	proposal   *agentsvc.ApprovalProposal
	approved   *agentsvc.ApprovalOutcome
	execution  *agentsvc.ApprovalExecution

	// 运行准备
	prepReq agentsvc.PrepareRequest
	setup   *agentsvc.RunSetup

	// 会话侧
	models        []model.AgentModelView
	checkedUser   uint64
	checkedCanvas uint64
	checkedMsg    string
	checkedSel    []string
	selection     []string
	undo          *agentsvc.UndoResult
}

func (f *fakePlatform) AgentModels(context.Context) ([]model.AgentModelView, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.models, nil
}

func (f *fakePlatform) CheckCanvas(_ context.Context, userID, canvasID uint64) error {
	f.checkedUser, f.checkedCanvas = userID, canvasID
	return f.err
}

func (f *fakePlatform) CheckAttachments(_ context.Context, userID uint64, ids []string) ([]agentsvc.AttachmentCheck, error) {
	f.attUser, f.attIDs = userID, ids
	if f.err != nil {
		return nil, f.err
	}
	return f.attachments, nil
}

func (f *fakePlatform) CheckMessage(_ context.Context, userID, canvasID uint64, msg string, sel []string) ([]string, error) {
	f.checkedUser, f.checkedCanvas, f.checkedMsg, f.checkedSel = userID, canvasID, msg, sel
	if f.err != nil {
		return nil, f.err
	}
	return f.selection, nil
}

func (f *fakePlatform) UndoPlatformRun(_ context.Context, rc agentsvc.RunContext) (*agentsvc.UndoResult, error) {
	f.rc = rc
	if f.err != nil {
		return nil, f.err
	}
	return f.undo, nil
}

func (f *fakePlatform) PrepareRun(_ context.Context, rc agentsvc.RunContext, req agentsvc.PrepareRequest) (*agentsvc.RunSetup, error) {
	f.rc, f.prepReq = rc, req
	if f.err != nil {
		return nil, f.err
	}
	return f.setup, nil
}

func (f *fakePlatform) ProposeApproval(_ context.Context, rc agentsvc.RunContext, kind string, args json.RawMessage) (*agentsvc.ApprovalProposal, error) {
	f.rc, f.kind, f.args = rc, kind, string(args)
	if f.err != nil {
		return nil, f.err
	}
	return f.proposal, nil
}

func (f *fakePlatform) ResolveApproval(_ context.Context, rc agentsvc.RunContext, kind string, payload json.RawMessage, req model.DecideAgentApprovalReq) (*agentsvc.ApprovalOutcome, error) {
	f.rc, f.kind, f.payload, f.decision = rc, kind, string(payload), req
	if f.err != nil {
		return nil, f.err
	}
	return f.approved, nil
}

func (f *fakePlatform) ExecuteApproval(_ context.Context, rc agentsvc.RunContext, kind string, approvalID uint64, toolCallID string, o agentsvc.ApprovalOutcome) (*agentsvc.ApprovalExecution, error) {
	f.rc, f.kind, f.approvalID, f.callID, f.outcome = rc, kind, approvalID, toolCallID, o
	if f.err != nil {
		return nil, f.err
	}
	return f.execution, nil
}

func (f *fakePlatform) ExecutePlatformTool(ctx context.Context, rc agentsvc.RunContext, callID, name string, args json.RawMessage) (*agentsvc.ToolResult, error) {
	f.rc, f.callID, f.name, f.args, f.idemKey = rc, callID, name, string(args), agentsvc.IdempotencyKeyFrom(ctx)
	if f.err != nil {
		return nil, f.err
	}
	if f.res != nil {
		return f.res, nil
	}
	return &agentsvc.ToolResult{Content: "ok"}, nil
}

func (f *fakePlatform) EndPlatformRun(runID uint64) { f.ended = append(f.ended, runID) }

func (f *fakePlatform) ProxyModelPlatform(ctx context.Context, rc agentsvc.RunContext, body []byte, onCall func(uint64), write func([]byte)) error {
	f.rc, f.body, f.purpose = rc, string(body), agentsvc.ModelPurposeFrom(ctx)
	if f.err != nil {
		return f.err
	}
	if f.modelCallID != 0 {
		onCall(f.modelCallID)
	}
	for _, l := range f.lines {
		write([]byte(l))
	}
	return nil
}

func (f *fakePlatform) PlatformRunUsage(_ context.Context, rc agentsvc.RunContext, callID uint64) (*agentsvc.PlatformUsage, error) {
	f.rc, f.usageCallID = rc, callID
	if f.err != nil {
		return nil, f.err
	}
	return f.usage, nil
}

func newPlatformRouter(api PlatformAPI) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewPlatformHandler(api)
	r := gin.New()
	g := r.Group("/internal/platform/v1")
	g.POST("/tools/:name", h.Tool)
	g.DELETE("/runs/:run_id", h.EndRun)
	g.POST("/model/chat/completions", h.ChatCompletions)
	g.GET("/runs/:run_id/usage", h.Usage)
	g.POST("/runs/prepare", h.PrepareRun)
	g.GET("/agent/models", h.AgentModels)
	g.POST("/canvases/check", h.CheckCanvas)
	g.POST("/messages/check", h.CheckMessage)
	g.POST("/attachments/check", h.CheckAttachments)
	g.POST("/runs/:run_id/undo", h.UndoRun)
	g.POST("/approvals/propose", h.ProposeApproval)
	g.POST("/approvals/resolve", h.ResolveApproval)
	g.POST("/approvals/execute", h.ExecuteApproval)
	return r
}

// ctxHeader 把运行上下文编码成请求头的值：base64url（无填充）的 JSON，ID 是十进制字符串。
func ctxHeader(rc string) string { return base64.RawURLEncoding.EncodeToString([]byte(rc)) }

const goodCtx = `{"run_id":"1152921504606846977","user_id":"1","session_id":"1152921504606846978","canvas_id":"7","mode":"all","model_key":"claude","selection":["n1","n2"],"budget_credits":50,"spent_extra":8}`

func platformCall(r *gin.Engine, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func respCode(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	var b struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &b)
	return b.Code
}

func TestPlatformHandler_Tool_PassesContextAndKey(t *testing.T) {
	api := &fakePlatform{}
	w := platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/tools/canvas_apply_ops",
		`{"tool_call_id":"call_0","args":{"ops":[]}}`, map[string]string{"X-Agent-Context": ctxHeader(goodCtx), "X-Idempotency-Key": "task-0192f3a1-aaaa"})
	if w.Code != 200 || respCode(t, w) != 0 {
		t.Fatalf("status=%d %s", w.Code, w.Body)
	}
	if api.name != "canvas_apply_ops" || api.callID != "call_0" || api.args != `{"ops":[]}` || api.idemKey != "task-0192f3a1-aaaa" {
		t.Errorf("参数没传对: %+v", api)
	}
	// 雪花算法生成的 ID 超过 2^53，必须以十进制字符串传，不能经过浮点数丢精度
	if api.rc.RunID != 1152921504606846977 || api.rc.SessionID != 1152921504606846978 || api.rc.UserID != 1 || api.rc.CanvasID != 7 {
		t.Errorf("ID 要无损解析: %+v", api.rc)
	}
	if api.rc.Mode != "all" || api.rc.ModelKey != "claude" || len(api.rc.Selection) != 2 || api.rc.BudgetCredits != 50 || api.rc.SpentExtra != 8 {
		t.Errorf("上下文字段: %+v", api.rc)
	}
	var out struct {
		Data struct {
			Content string `json:"content"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Data.Content != "ok" {
		t.Errorf("响应应是标准信封，data 是工具结果: %s", w.Body)
	}
}

func TestPlatformHandler_Tool_EmptyArgsBecomeEmptyObject(t *testing.T) {
	api := &fakePlatform{}
	platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/tools/canvas_get_state", `{}`, map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
	if api.args != `{}` {
		t.Errorf("没带 args 按空对象: %q", api.args)
	}
	if api.idemKey != "" {
		t.Errorf("没带幂等键就没有: %q", api.idemKey)
	}
}

func TestPlatformHandler_Tool_RejectsBadContext(t *testing.T) {
	cases := map[string]map[string]string{
		"缺上下文头":           {},
		"不是 base64":       {"X-Agent-Context": "!!!不是 base64!!!"},
		"base64 里不是 JSON": {"X-Agent-Context": ctxHeader("not json")},
		"ID 不是字符串（浮点数会丢精度）": {"X-Agent-Context": ctxHeader(`{"run_id":1,"user_id":"1","session_id":"2","canvas_id":"7"}`)},
		"ID 不是数字":  {"X-Agent-Context": ctxHeader(`{"run_id":"abc","user_id":"1","session_id":"2","canvas_id":"7"}`)},
		"上下文太大":    {"X-Agent-Context": ctxHeader(`{"run_id":"1","user_id":"1","session_id":"2","canvas_id":"7","model_key":"` + strings.Repeat("x", 20000) + `"}`)},
		"幂等键太长":    {"X-Agent-Context": ctxHeader(goodCtx), "X-Idempotency-Key": strings.Repeat("k", 65)},
		"幂等键有非法字符": {"X-Agent-Context": ctxHeader(goodCtx), "X-Idempotency-Key": "key with space"},
	}
	for name, hdr := range cases {
		t.Run(name, func(t *testing.T) {
			api := &fakePlatform{}
			w := platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/tools/canvas_get_state", `{}`, hdr)
			if w.Code != 400 || respCode(t, w) != errcode.ErrInvalidParams.Code {
				t.Fatalf("应返回 400 参数错误: status=%d %s", w.Code, w.Body)
			}
			if api.name != "" {
				t.Error("非法请求不能进入业务")
			}
		})
	}
}

func TestPlatformHandler_Tool_BusinessErrorsMapToStandardResponse(t *testing.T) {
	w := platformCall(newPlatformRouter(&fakePlatform{err: errcode.ErrInvalidParams.WithMsg("工具 plan_update 不能经这个接口执行")}),
		http.MethodPost, "/internal/platform/v1/tools/plan_update", `{}`, map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
	if w.Code != 400 || !strings.Contains(w.Body.String(), "不能经这个接口执行") {
		t.Errorf("status=%d %s", w.Code, w.Body)
	}
}

func TestPlatformHandler_Tool_ToolErrorIsStillHTTP200(t *testing.T) {
	// 工具层的失败是给模型看的结果（IsError），不是 HTTP 错误
	api := &fakePlatform{res: &agentsvc.ToolResult{Content: "节点不存在", IsError: true}}
	w := platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/tools/canvas_apply_ops", `{}`, map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"is_error":true`) {
		t.Errorf("status=%d %s", w.Code, w.Body)
	}
}

func TestPlatformHandler_EndRun(t *testing.T) {
	api := &fakePlatform{}
	r := newPlatformRouter(api)
	if w := platformCall(r, http.MethodDelete, "/internal/platform/v1/runs/1152921504606846977", "", nil); w.Code != 200 || len(api.ended) != 1 || api.ended[0] != 1152921504606846977 {
		t.Errorf("应释放这个运行的状态: status=%d ended=%v", w.Code, api.ended)
	}
	for _, bad := range []string{"abc", "0", "-1", "1.5"} {
		if w := platformCall(r, http.MethodDelete, "/internal/platform/v1/runs/"+bad, "", nil); w.Code != 400 {
			t.Errorf("run_id=%q 应 400: %d", bad, w.Code)
		}
	}
}

// ---- 模型代理（OpenAI 兼容）和用量 ----

const chatPath = "/internal/platform/v1/model/chat/completions"
const chatBody = `{"model":"x","messages":[{"role":"user","content":"拆分镜"}]}`

func TestPlatformHandler_ChatCompletions_StreamsAndReportsCallID(t *testing.T) {
	api := &fakePlatform{modelCallID: 1152921504606846999, lines: []string{"data: {\"a\":1}\n", "\n", "data: [DONE]\n\n"}}
	w := platformCall(newPlatformRouter(api), http.MethodPost, chatPath, chatBody, map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
	if w.Code != 200 {
		t.Fatalf("status=%d %s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type=%q", ct)
	}
	if w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("X-Accel-Buffering") != "no" {
		t.Errorf("流式响应要禁缓存、禁代理缓冲: %v", w.Header())
	}
	// 调用 id 在响应头里，十进制字符串（雪花 ID 超过 2^53）
	if got := w.Header().Get("X-Agent-Call-Id"); got != "1152921504606846999" {
		t.Errorf("X-Agent-Call-Id=%q", got)
	}
	if w.Body.String() != "data: {\"a\":1}\n\ndata: [DONE]\n\n" {
		t.Errorf("上游的行应原样转出: %q", w.Body.String())
	}
	if api.rc.RunID != 1152921504606846977 || !strings.Contains(api.body, "拆分镜") {
		t.Errorf("上下文和请求体要传给业务: %+v %q", api.rc, api.body)
	}
}

func TestPlatformHandler_ChatCompletions_SummaryPurpose(t *testing.T) {
	api := &fakePlatform{lines: []string{"data: [DONE]\n\n"}}
	r := newPlatformRouter(api)
	platformCall(r, http.MethodPost, chatPath, chatBody, map[string]string{"X-Agent-Context": ctxHeader(goodCtx), "X-Agent-Purpose": "summary"})
	if api.purpose != agentsvc.ModelPurposeSummary {
		t.Errorf("摘要用途要传下去: %q", api.purpose)
	}
	platformCall(r, http.MethodPost, chatPath, chatBody, map[string]string{"X-Agent-Context": ctxHeader(goodCtx), "X-Agent-Purpose": "anything-else"})
	if api.purpose != "" {
		t.Errorf("只认已知的用途，别的值一律当普通对话: %q", api.purpose)
	}
}

func TestPlatformHandler_ChatCompletions_TitlePurpose(t *testing.T) {
	api := &fakePlatform{lines: []string{"data: [DONE]\n\n"}}
	r := newPlatformRouter(api)
	platformCall(r, http.MethodPost, chatPath, chatBody, map[string]string{"X-Agent-Context": ctxHeader(goodCtx), "X-Agent-Purpose": "title"})
	if api.purpose != agentsvc.ModelPurposeTitle {
		t.Errorf("会话命名用途要传下去: %q", api.purpose)
	}
}

// 流开始之前出错：返回 OpenAI 格式的 JSON 错误和对应的 HTTP 状态，调用方（pi 的 OpenAI 客户端）靠它显示报错、判断是否可重试。
func TestPlatformHandler_ChatCompletions_ErrorsAreOpenAIShaped(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantMsg    string
	}{
		{"预算用尽", errcode.ErrAgentOverBudget, 402, "预算"},
		{"余额不足", errcode.ErrInsufficientCredits, 402, "积分"},
		{"模型不可用", errcode.ErrAgentModelNA, 400, "模型"},
		{"上下文不合法", errcode.ErrInvalidParams.WithMsg("运行上下文缺少 model_key"), 400, "model_key"},
		{"上游限流（可重试）", &llmgateway.UpstreamError{Status: 429, Message: "上游请求太频繁，请稍后重试", Retryable: true}, 429, "频繁"},
		{"其他错误不外泄细节", errors.New("dial tcp 10.0.0.5:5432: secret-host"), 502, "上游"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := platformCall(newPlatformRouter(&fakePlatform{err: tc.err}), http.MethodPost, chatPath, chatBody, map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
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
				t.Errorf("内部错误细节不能返回: %s", w.Body)
			}
		})
	}
}

func TestPlatformHandler_ChatCompletions_BadContextIsAnOpenAIError(t *testing.T) {
	for name, hdr := range map[string]map[string]string{"缺上下文头": {}, "上下文不是 base64": {"X-Agent-Context": "!!!"}, "ID 是数字": {"X-Agent-Context": ctxHeader(`{"run_id":1,"user_id":"1","session_id":"2","canvas_id":"7"}`)}} {
		t.Run(name, func(t *testing.T) {
			api := &fakePlatform{}
			w := platformCall(newPlatformRouter(api), http.MethodPost, chatPath, chatBody, hdr)
			if w.Code != 400 || !strings.Contains(w.Body.String(), `"error"`) {
				t.Fatalf("OpenAI 客户端只认 {error:{...}}: status=%d %s", w.Code, w.Body)
			}
			if api.body != "" {
				t.Error("不能进入业务")
			}
		})
	}
}

func TestPlatformHandler_ChatCompletions_BodyTooLarge(t *testing.T) {
	big := `{"messages":[{"role":"user","content":"` + strings.Repeat("x", 33<<20) + `"}]}`
	w := platformCall(newPlatformRouter(&fakePlatform{}), http.MethodPost, chatPath, big, map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
	if w.Code != 413 {
		t.Errorf("请求体过大应 413: %d", w.Code)
	}
}

func TestPlatformHandler_Usage(t *testing.T) {
	api := &fakePlatform{usage: &agentsvc.PlatformUsage{SpentCredits: 14, ContextTokens: 3000, Call: &agentsvc.PlatformCallUsage{ID: 1152921504606846999, Status: "settled", Credits: 5, Charged: 5}}}
	r := newPlatformRouter(api)
	w := platformCall(r, http.MethodGet, "/internal/platform/v1/runs/1152921504606846977/usage?call_id=1152921504606846999", "", map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
	if w.Code != 200 || api.usageCallID != 1152921504606846999 {
		t.Fatalf("status=%d callID=%d %s", w.Code, api.usageCallID, w.Body)
	}
	var out struct {
		Data struct {
			Spent int `json:"spent_credits"`
			Ctx   int `json:"context_tokens"`
			Call  struct {
				ID      string `json:"id"`
				Charged int    `json:"charged"`
			} `json:"call"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Data.Spent != 14 || out.Data.Ctx != 3000 || out.Data.Call.ID != "1152921504606846999" || out.Data.Call.Charged != 5 {
		t.Errorf("响应: %s", w.Body)
	}
	// 不带 call_id
	platformCall(r, http.MethodGet, "/internal/platform/v1/runs/1152921504606846977/usage", "", map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
	if api.usageCallID != 0 {
		t.Errorf("没带 call_id 就是 0: %d", api.usageCallID)
	}
}

func TestPlatformHandler_Usage_Validation(t *testing.T) {
	hdr := map[string]string{"X-Agent-Context": ctxHeader(goodCtx)}
	cases := map[string]struct {
		path string
		hdr  map[string]string
	}{
		"路径里的运行和上下文里的不一致": {"/internal/platform/v1/runs/999/usage", hdr},
		"运行 id 不是数字":      {"/internal/platform/v1/runs/abc/usage", hdr},
		"call_id 不是数字":    {"/internal/platform/v1/runs/1152921504606846977/usage?call_id=x", hdr},
		"缺上下文头":           {"/internal/platform/v1/runs/1152921504606846977/usage", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api := &fakePlatform{usage: &agentsvc.PlatformUsage{}}
			w := platformCall(newPlatformRouter(api), http.MethodGet, tc.path, "", tc.hdr)
			if w.Code != 400 || respCode(t, w) != errcode.ErrInvalidParams.Code {
				t.Fatalf("应 400 参数错误: status=%d %s", w.Code, w.Body)
			}
		})
	}
	// 业务错误（比如调用不属于这个运行）原样返回标准错误
	w := platformCall(newPlatformRouter(&fakePlatform{err: errcode.ErrNotFound}), http.MethodGet, "/internal/platform/v1/runs/1152921504606846977/usage?call_id=5", "", hdr)
	if w.Code != 404 {
		t.Errorf("调用不存在应 404: %d", w.Code)
	}
}

// ---- 审批：预估、解析、执行 ----

const approvalsPath = "/internal/platform/v1/approvals/"

func TestPlatformHandler_ProposeApprovalPassesContextKindAndArgs(t *testing.T) {
	api := &fakePlatform{proposal: &agentsvc.ApprovalProposal{Payload: json.RawMessage(`{"items":[]}`), QuoteCredits: 5, Summary: "生成 1 个，预估 5 积分", Content: "已提交，等待决定", Terminate: true}}
	w := platformCall(newPlatformRouter(api), http.MethodPost, approvalsPath+"propose",
		`{"kind":"generate","args":{"items":[{"nodeId":"n1"}],"reason":"出图"}}`, map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
	if w.Code != 200 || respCode(t, w) != 0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if api.kind != "generate" || api.rc.RunID != 1152921504606846977 || api.rc.UserID != 1 || !strings.Contains(api.args, `"nodeId":"n1"`) {
		t.Errorf("业务层收到的: kind=%s rc=%+v args=%s", api.kind, api.rc, api.args)
	}
	var body struct {
		Data struct {
			QuoteCredits int    `json:"quote_credits"`
			Terminate    bool   `json:"terminate"`
			Content      string `json:"content"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Data.QuoteCredits != 5 || !body.Data.Terminate || body.Data.Content == "" {
		t.Errorf("响应: %s", w.Body)
	}
	// 不传参数按空对象
	platformCall(newPlatformRouter(api), http.MethodPost, approvalsPath+"propose", `{"kind":"ask"}`, map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
	if api.args != "{}" {
		t.Errorf("没有 args 要按空对象: %q", api.args)
	}
}

func TestPlatformHandler_ResolveApprovalPassesPayloadAndDecision(t *testing.T) {
	api := &fakePlatform{approved: &agentsvc.ApprovalOutcome{Status: "approved", QuoteCredits: 24}}
	w := platformCall(newPlatformRouter(api), http.MethodPost, approvalsPath+"resolve",
		`{"kind":"generate","payload":{"items":[{"node_id":"a"}]},"decision":{"decision":"approve","items":[{"index":0,"approve":true,"count":1}],"add_budget":10}}`,
		map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
	if w.Code != 200 || respCode(t, w) != 0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if api.kind != "generate" || !strings.Contains(api.payload, `"node_id":"a"`) || api.decision.Decision != "approve" || len(api.decision.Items) != 1 || api.decision.AddBudget != 10 {
		t.Errorf("业务层收到的: %s %s %+v", api.kind, api.payload, api.decision)
	}
	if !strings.Contains(w.Body.String(), `"status":"approved"`) || !strings.Contains(w.Body.String(), `"quote_credits":24`) {
		t.Errorf("响应: %s", w.Body)
	}
}

func TestPlatformHandler_ExecuteApprovalParsesTheSnowflakeApprovalID(t *testing.T) {
	api := &fakePlatform{execution: &agentsvc.ApprovalExecution{Status: "executed", Result: json.RawMessage(`{"changes":1}`), OutcomeText: "用户批准了删除，已删除 1 个节点。"}}
	w := platformCall(newPlatformRouter(api), http.MethodPost, approvalsPath+"execute",
		`{"kind":"delete","approval_id":"1152921504606846983","tool_call_id":"call_9","outcome":{"status":"approved","node_ids":["n1"],"edge_ids":[],"decision":{"decision":"approve"}}}`,
		map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
	if w.Code != 200 || respCode(t, w) != 0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// 审批 id 超过 2^53，必须是十进制字符串，不能经过浮点数
	if api.approvalID != 1152921504606846983 || api.callID != "call_9" || api.kind != "delete" {
		t.Errorf("业务层收到的: id=%d call=%s kind=%s", api.approvalID, api.callID, api.kind)
	}
	if api.outcome.Status != "approved" || len(api.outcome.NodeIDs) != 1 || api.outcome.NodeIDs[0] != "n1" || api.outcome.Decision["decision"] != "approve" {
		t.Errorf("outcome 要原样传下去: %+v", api.outcome)
	}
	if !strings.Contains(w.Body.String(), `"outcome_text":"用户批准了删除，已删除 1 个节点。"`) || !strings.Contains(w.Body.String(), `"status":"executed"`) {
		t.Errorf("响应: %s", w.Body)
	}
}

func TestPlatformHandler_ApprovalEndpointsRejectBadRequests(t *testing.T) {
	hdr := map[string]string{"X-Agent-Context": ctxHeader(goodCtx)}
	cases := map[string]struct {
		path, body string
		hdr        map[string]string
	}{
		"预估缺上下文头":           {"propose", `{"kind":"ask"}`, nil},
		"预估缺 kind":          {"propose", `{"args":{}}`, hdr},
		"预估不是 JSON":         {"propose", `不是 JSON`, hdr},
		"解析缺上下文头":           {"resolve", `{"kind":"delete","payload":{},"decision":{"decision":"approve"}}`, nil},
		"解析缺 payload":       {"resolve", `{"kind":"delete","decision":{"decision":"approve"}}`, hdr},
		"解析 payload 是 null": {"resolve", `{"kind":"delete","payload":null,"decision":{"decision":"approve"}}`, hdr},
		"解析决定不认识":           {"resolve", `{"kind":"delete","payload":{},"decision":{"decision":"maybe"}}`, hdr},
		"解析缺决定":             {"resolve", `{"kind":"delete","payload":{}}`, hdr},
		"执行缺上下文头":           {"execute", `{"kind":"delete","approval_id":"1","outcome":{"status":"approved"}}`, nil},
		"执行审批 id 是数字":       {"execute", `{"kind":"delete","approval_id":1,"outcome":{"status":"approved"}}`, hdr},
		"执行审批 id 不是数字":      {"execute", `{"kind":"delete","approval_id":"abc","outcome":{"status":"approved"}}`, hdr},
		"执行审批 id 为 0":       {"execute", `{"kind":"delete","approval_id":"0","outcome":{"status":"approved"}}`, hdr},
		"执行缺 outcome":       {"execute", `{"kind":"delete","approval_id":"1"}`, hdr},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api := &fakePlatform{}
			w := platformCall(newPlatformRouter(api), http.MethodPost, approvalsPath+tc.path, tc.body, tc.hdr)
			if w.Code != 400 || respCode(t, w) != errcode.ErrInvalidParams.Code {
				t.Fatalf("应 400 参数错误: status=%d %s", w.Code, w.Body)
			}
			if api.kind != "" {
				t.Error("请求不合法不能走到业务层")
			}
		})
	}
}

func TestPlatformHandler_ApprovalBusinessErrorsKeepTheirCode(t *testing.T) {
	hdr := map[string]string{"X-Agent-Context": ctxHeader(goodCtx)}
	api := &fakePlatform{err: errcode.ErrAgentOverBudget}
	w := platformCall(newPlatformRouter(api), http.MethodPost, approvalsPath+"resolve", `{"kind":"generate","payload":{},"decision":{"decision":"approve"}}`, hdr)
	if respCode(t, w) != errcode.ErrAgentOverBudget.Code {
		t.Errorf("超预算要原样返回业务错误码（Agent 服务据此提示用户加预算）: %d %s", w.Code, w.Body)
	}
}

// ---- 运行准备 ----

func TestPlatformHandler_PrepareRunPassesContextAndBody(t *testing.T) {
	api := &fakePlatform{setup: &agentsvc.RunSetup{SystemPrompt: "你是画布助手", FirstMessage: "拆分镜", Model: agentsvc.RunModel{ContextWindow: 200000}}}
	w := platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/runs/prepare", `{"message":"拆分镜","max_steps":25}`, map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
	if w.Code != 200 || respCode(t, w) != 0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if api.rc.RunID != 1152921504606846977 || api.rc.ModelKey != "claude" || api.prepReq.Message != "拆分镜" || api.prepReq.MaxSteps != 25 {
		t.Errorf("业务层收到的: %+v %+v", api.rc, api.prepReq)
	}
	for _, want := range []string{`"system_prompt":"你是画布助手"`, `"first_message":"拆分镜"`, `"context_window":200000`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("响应缺少 %s: %s", want, w.Body)
		}
	}
	// 续跑：不带请求体也行
	platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/runs/prepare", ``, map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
	if api.prepReq.Message != "" {
		t.Errorf("续跑没有消息: %+v", api.prepReq)
	}
}

func TestPlatformHandler_PrepareRunRejectsBadRequests(t *testing.T) {
	hdr := map[string]string{"X-Agent-Context": ctxHeader(goodCtx)}
	for name, c := range map[string]struct {
		body string
		hdr  map[string]string
	}{
		"缺上下文头":   {`{}`, nil},
		"不是 JSON": {`不是 JSON`, hdr},
		"步数上限是负数": {`{"message":"x","max_steps":-3}`, hdr},
		"步数上限太大":  {`{"message":"x","max_steps":100000}`, hdr},
		"消息太长":    {`{"message":"` + strings.Repeat("字", 20001) + `"}`, hdr},
	} {
		t.Run(name, func(t *testing.T) {
			api := &fakePlatform{setup: &agentsvc.RunSetup{}}
			w := platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/runs/prepare", c.body, c.hdr)
			if w.Code != 400 || respCode(t, w) != errcode.ErrInvalidParams.Code {
				t.Fatalf("应 400 参数错误: status=%d %s", w.Code, w.Body)
			}
			if api.prepReq.Message != "" || api.prepReq.MaxSteps != 0 {
				t.Error("不合法的请求不能进入业务层")
			}
		})
	}
	// 模型不可用等业务错误原样返回
	w := platformCall(newPlatformRouter(&fakePlatform{err: errcode.ErrAgentModelNA}), http.MethodPost, "/internal/platform/v1/runs/prepare", `{}`, hdr)
	if respCode(t, w) != errcode.ErrAgentModelNA.Code {
		t.Errorf("业务错误要保留错误码: %d %s", w.Code, w.Body)
	}
}

// 模型代理的业务错误带一个响应头 X-Agent-Error-Code：Agent 服务据此区分「本轮额度用完」（运行进入 budget_exhausted、
// 追加额度后可以继续）和别的模型错误。OpenAI 格式的错误体只有文字，不能靠比对文案。
func TestPlatformHandler_ChatCompletions_BusinessErrorsCarryTheirCodeInAHeader(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"预算用尽":   {errcode.ErrAgentOverBudget, strconv.Itoa(errcode.ErrAgentOverBudget.Code)},
		"余额不足":   {errcode.ErrInsufficientCredits, strconv.Itoa(errcode.ErrInsufficientCredits.Code)},
		"模型不可用":  {errcode.ErrAgentModelNA, strconv.Itoa(errcode.ErrAgentModelNA.Code)},
		"内部错误不带": {errors.New("boom"), ""},
	} {
		t.Run(name, func(t *testing.T) {
			w := platformCall(newPlatformRouter(&fakePlatform{err: tc.err}), http.MethodPost, chatPath, chatBody, map[string]string{"X-Agent-Context": ctxHeader(goodCtx)})
			if got := w.Header().Get("X-Agent-Error-Code"); got != tc.want {
				t.Errorf("X-Agent-Error-Code = %q，期望 %q", got, tc.want)
			}
		})
	}
	// 令牌模式的桥同样（共用 serveModelStream）
}

// ---- 会话侧：Agent 服务实现前端的 REST 接口时向 Go 问的几件事 ----

func TestPlatformHandler_AgentModels(t *testing.T) {
	api := &fakePlatform{models: []model.AgentModelView{{Key: "claude", Name: "Claude", Vision: true, ContextWindow: 200000}}}
	w := platformCall(newPlatformRouter(api), http.MethodGet, "/internal/platform/v1/agent/models", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"key":"claude"`) || !strings.Contains(w.Body.String(), `"context_window":200000`) {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	w = platformCall(newPlatformRouter(&fakePlatform{err: errcode.ErrAgentModelNA}), http.MethodGet, "/internal/platform/v1/agent/models", "", nil)
	if respCode(t, w) != errcode.ErrAgentModelNA.Code {
		t.Errorf("业务错误保留错误码: %s", w.Body)
	}
}

func TestPlatformHandler_CheckCanvas(t *testing.T) {
	api := &fakePlatform{}
	// 雪花大小的用户和画布 id 必须是十进制字符串，不经过浮点数
	w := platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/canvases/check", `{"user_id":"1152921504606846977","canvas_id":"7"}`, nil)
	if w.Code != 200 || respCode(t, w) != 0 || api.checkedUser != 1152921504606846977 || api.checkedCanvas != 7 {
		t.Errorf("%d %s user=%d canvas=%d", w.Code, w.Body, api.checkedUser, api.checkedCanvas)
	}
	for name, body := range map[string]string{
		"id 是数字": `{"user_id":1,"canvas_id":"7"}`, "缺画布": `{"user_id":"1"}`, "id 为 0": `{"user_id":"0","canvas_id":"7"}`, "不是 JSON": `x`,
	} {
		t.Run(name, func(t *testing.T) {
			w := platformCall(newPlatformRouter(&fakePlatform{}), http.MethodPost, "/internal/platform/v1/canvases/check", body, nil)
			if w.Code != 400 || respCode(t, w) != errcode.ErrInvalidParams.Code {
				t.Errorf("应 400 参数错误: %d %s", w.Code, w.Body)
			}
		})
	}
	w = platformCall(newPlatformRouter(&fakePlatform{err: errcode.ErrCanvasNotFound}), http.MethodPost, "/internal/platform/v1/canvases/check", `{"user_id":"2","canvas_id":"7"}`, nil)
	if w.Code != 404 || respCode(t, w) != errcode.ErrCanvasNotFound.Code {
		t.Errorf("画布不存在原样返回: %d %s", w.Code, w.Body)
	}
}

func TestPlatformHandler_CheckMessage(t *testing.T) {
	api := &fakePlatform{selection: []string{"n1"}}
	w := platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/messages/check", `{"user_id":"1","canvas_id":"7","message":"拆 @[剧本](node:n1)","selection":["n1","ghost"]}`, nil)
	if w.Code != 200 || api.checkedMsg != "拆 @[剧本](node:n1)" || len(api.checkedSel) != 2 || !strings.Contains(w.Body.String(), `"selection":["n1"]`) {
		t.Errorf("%d %s %+v", w.Code, w.Body, api)
	}
	// 选中的节点为空时响应里是空数组，不是 null
	api2 := &fakePlatform{selection: nil}
	w = platformCall(newPlatformRouter(api2), http.MethodPost, "/internal/platform/v1/messages/check", `{"user_id":"1","canvas_id":"7","message":"你好"}`, nil)
	if !strings.Contains(w.Body.String(), `"selection":[]`) {
		t.Errorf("空选择是 []: %s", w.Body)
	}
	for name, body := range map[string]string{
		"消息太长": `{"user_id":"1","canvas_id":"7","message":"` + strings.Repeat("字", 20001) + `"}`,
		"选中太多": `{"user_id":"1","canvas_id":"7","message":"x","selection":[` + strings.TrimSuffix(strings.Repeat(`"n",`, 201), ",") + `]}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := platformCall(newPlatformRouter(&fakePlatform{}), http.MethodPost, "/internal/platform/v1/messages/check", body, nil)
			if w.Code != 400 {
				t.Errorf("应 400: %d %s", w.Code, w.Body)
			}
		})
	}
	w = platformCall(newPlatformRouter(&fakePlatform{err: errcode.ErrInvalidParams.WithMsg("引用的节点「幽灵」不可用")}), http.MethodPost, "/internal/platform/v1/messages/check", `{"user_id":"1","canvas_id":"7","message":"x"}`, nil)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "幽灵") {
		t.Errorf("引用不合法的说明原样返回: %d %s", w.Code, w.Body)
	}
}

func TestPlatformHandler_CheckAttachments(t *testing.T) {
	api := &fakePlatform{attachments: []agentsvc.AttachmentCheck{{AssetID: "11", Name: "参考.png", MimeType: "image/png", URL: "/files/u1/a.png", Image: agentsvc.ToolImage{MimeType: "image/png", Data: "QUJD"}}}}
	w := platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/attachments/check", `{"user_id":"1","asset_ids":["11"]}`, nil)
	if w.Code != 200 || api.attUser != 1 || len(api.attIDs) != 1 || api.attIDs[0] != "11" {
		t.Fatalf("%d %s %+v", w.Code, w.Body, api)
	}
	for _, want := range []string{`"asset_id":"11"`, `"name":"参考.png"`, `"url":"/files/u1/a.png"`, `"data":"QUJD"`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("响应里要有 %s: %s", want, w.Body)
		}
	}
	// 没有附件：响应里是空数组，不是 null
	w = platformCall(newPlatformRouter(&fakePlatform{}), http.MethodPost, "/internal/platform/v1/attachments/check", `{"user_id":"1","asset_ids":[]}`, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"attachments":[]`) {
		t.Errorf("空附件是 []: %d %s", w.Code, w.Body)
	}
	for name, body := range map[string]string{
		"缺 user_id": `{"asset_ids":["1"]}`,
		"附件太多":      `{"user_id":"1","asset_ids":[` + strings.TrimSuffix(strings.Repeat(`"1",`, 10), ",") + `]}`,
		"素材 id 太长":  `{"user_id":"1","asset_ids":["` + strings.Repeat("9", 33) + `"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := platformCall(newPlatformRouter(&fakePlatform{}), http.MethodPost, "/internal/platform/v1/attachments/check", body, nil)
			if w.Code != 400 {
				t.Errorf("应 400: %d %s", w.Code, w.Body)
			}
		})
	}
	w = platformCall(newPlatformRouter(&fakePlatform{err: errcode.ErrInvalidParams.WithMsg("图 2：图片素材不存在或已删除")}), http.MethodPost, "/internal/platform/v1/attachments/check", `{"user_id":"1","asset_ids":["1","2"]}`, nil)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "图 2：图片素材不存在或已删除") {
		t.Errorf("校验不通过的说明原样返回: %d %s", w.Code, w.Body)
	}
}

func TestPlatformHandler_UndoRun(t *testing.T) {
	hdr := map[string]string{"X-Agent-Context": ctxHeader(goodCtx)}
	api := &fakePlatform{undo: &agentsvc.UndoResult{Reverted: 3, Skipped: []canvasgraph.Skip{}, Revision: 9}}
	w := platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/runs/1152921504606846977/undo", ``, hdr)
	if w.Code != 200 || api.rc.RunID != 1152921504606846977 || !strings.Contains(w.Body.String(), `"reverted":3`) || !strings.Contains(w.Body.String(), `"skipped":[]`) {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	for name, c := range map[string]struct {
		path string
		hdr  map[string]string
	}{
		"路径里的运行和上下文里的不一致": {"/internal/platform/v1/runs/999/undo", hdr},
		"运行 id 不是数字":      {"/internal/platform/v1/runs/abc/undo", hdr},
		"缺上下文头":           {"/internal/platform/v1/runs/1152921504606846977/undo", nil},
	} {
		t.Run(name, func(t *testing.T) {
			w := platformCall(newPlatformRouter(&fakePlatform{undo: &agentsvc.UndoResult{}}), http.MethodPost, c.path, ``, c.hdr)
			if w.Code != 400 || respCode(t, w) != errcode.ErrInvalidParams.Code {
				t.Errorf("应 400: %d %s", w.Code, w.Body)
			}
		})
	}
	w = platformCall(newPlatformRouter(&fakePlatform{err: errcode.ErrAgentAlreadyUndone}), http.MethodPost, "/internal/platform/v1/runs/1152921504606846977/undo", ``, hdr)
	if w.Code != 409 || respCode(t, w) != errcode.ErrAgentAlreadyUndone.Code {
		t.Errorf("已撤销过原样返回: %d %s", w.Code, w.Body)
	}
}

// fakeRelay 记录事件推送收到的批次。
type fakeRelay struct {
	got []agentsvc.RelayEvent
	err error
}

func (f *fakeRelay) Publish(_ context.Context, events []agentsvc.RelayEvent) error {
	f.got = append(f.got, events...)
	return f.err
}

func relayRouter(relay EventRelayAPI) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewPlatformHandler(&fakePlatform{}).WithEventRelay(relay)
	r := gin.New()
	r.POST("/internal/platform/v1/events", h.PushEvents)
	return r
}

func TestPlatform_PushEventsPassesBatchToRelay(t *testing.T) {
	relay := &fakeRelay{}
	body := `{"events":[{"user_id":"42","session_id":"7","canvas_id":"9","run_id":"8","seq":3,"type":"run.status","data":{"status":"running"},"created_at":"2026-10-25T08:00:00Z"}]}`
	status, resp := canvasCall(t, relayRouter(relay), "POST", "/internal/platform/v1/events", body)
	if status != http.StatusOK || resp["code"] != float64(0) {
		t.Fatalf("应成功：%d %v", status, resp)
	}
	if len(relay.got) != 1 || relay.got[0].UserID != "42" || relay.got[0].Seq != 3 || string(relay.got[0].Data) != `{"status":"running"}` {
		t.Errorf("事件应原样交给中继：%+v", relay.got)
	}
}

func TestPlatform_PushEventsErrors(t *testing.T) {
	status, resp := canvasCall(t, relayRouter(&fakeRelay{err: errcode.ErrInvalidParams.WithMsg("第 1 条事件的 type 必须是 1 到 64 个字符")}), "POST", "/internal/platform/v1/events", `{"events":[{}]}`)
	if status != http.StatusBadRequest || resp["code"] != float64(10001) {
		t.Errorf("中继的参数错误要原样返回：%d %v", status, resp)
	}
	status, _ = canvasCall(t, relayRouter(&fakeRelay{}), "POST", "/internal/platform/v1/events", `not json`)
	if status != http.StatusBadRequest {
		t.Errorf("请求体不是 JSON 应是 400：%d", status)
	}
}

// ID 密钥一致性自检：Agent 服务用同一个密钥给前端的 ID 编码，两边不一致时前端拿到的 ID 全都对不上。
// Go 把「编码后是什么」告诉 Agent，让它在启动时对一下。
func TestPlatform_IDProbeEncodesWithGoKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewPlatformHandler(&fakePlatform{})
	r := gin.New()
	r.GET("/internal/platform/v1/ids/:n", h.EncodeID)
	// 超过 2^53 的雪花 id 也要对
	for _, n := range []string{"1", "281474976710657123"} {
		status, resp := canvasCall(t, r, "GET", "/internal/platform/v1/ids/"+n, "")
		id, _ := strconv.ParseUint(n, 10, 64)
		data, _ := resp["data"].(map[string]any)
		if status != http.StatusOK || data["encoded"] != idcodec.Encode(id) {
			t.Errorf("%s：%d %v", n, status, resp)
		}
	}
	for _, bad := range []string{"0", "abc", "-1", "18446744073709551616"} {
		if status, _ := canvasCall(t, r, "GET", "/internal/platform/v1/ids/"+bad, ""); status != http.StatusBadRequest {
			t.Errorf("%q 应是 400：%d", bad, status)
		}
	}
}
