package agent_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler/agent"
	"video-canvas/internal/pkg/errcode"
	agentsvc "video-canvas/internal/service/agent"
)

// fakePlatform 记录平台 handler 传给业务层的参数。
type fakePlatform struct {
	rc      agentsvc.RunContext
	callID  string
	name    string
	args    string
	idemKey string
	res     *agentsvc.ToolResult
	err     error
	ended   []uint64
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

func newPlatformRouter(api PlatformAPI) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewPlatformHandler(api)
	r := gin.New()
	g := r.Group("/internal/platform/v1")
	g.POST("/tools/:name", h.Tool)
	g.DELETE("/runs/:run_id", h.EndRun)
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
