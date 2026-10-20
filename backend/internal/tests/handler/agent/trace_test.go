package agent_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler/agent"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	agentsvc "video-canvas/internal/service/agent"
	"video-canvas/pkg/pagination"
)

// ---- 发起运行：请求头 X-Agent-Harness 原样交给业务层 ----

func TestStartRun_PassesHarnessHeader(t *testing.T) {
	api := &fakeAgentAPI{}
	r := newAgentRouter(api)
	sid := idcodec.Encode(3)

	do := func(header string) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/sessions/"+sid+"/runs", strings.NewReader(`{"message":"拆分镜"}`))
		req.Header.Set("Content-Type", "application/json")
		if header != "" {
			req.Header.Set("X-Agent-Harness", header)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusAccepted {
			t.Fatalf("status=%d %s", w.Code, w.Body.String())
		}
	}
	do("storyboard-9x16")
	if api.gotStart == nil || api.gotStart.HarnessCase != "storyboard-9x16" {
		t.Errorf("请求头应交给业务层: %+v", api.gotStart)
	}
	do("")
	if api.gotStart.HarnessCase != "" {
		t.Errorf("没带请求头就是真实用户: %+v", api.gotStart)
	}
}

func TestStartRun_HarnessCaseCannotComeFromBody(t *testing.T) {
	api := &fakeAgentAPI{}
	r := newAgentRouter(api)
	code, _ := agentCall(t, r, http.MethodPost, "/api/v1/agent/sessions/"+idcodec.Encode(3)+"/runs", `{"message":"a","HarnessCase":"伪造","harness_case":"伪造"}`)
	if code != http.StatusAccepted || api.gotStart.HarnessCase != "" {
		t.Errorf("用例标签只认请求头，不认请求体: code=%d %+v", code, api.gotStart)
	}
}

// ---- 后台接口 ----

type fakeTraceAPI struct {
	err       error
	gotList   model.ListAgentRunsReq
	gotActor  uint64
	gotRun    uint64
	gotFormat string
	calls     []string
}

func (f *fakeTraceAPI) ListRuns(_ context.Context, req *model.ListAgentRunsReq) (*pagination.Result[agentsvc.AdminRunItem], error) {
	f.calls = append(f.calls, "list")
	f.gotList = *req
	return &pagination.Result[agentsvc.AdminRunItem]{List: []agentsvc.AdminRunItem{{ID: 5, Status: "succeeded", Trace: "available", Source: "user"}}, Total: 1, Page: 1, PageSize: 20}, f.err
}

func (f *fakeTraceAPI) ExportTrace(_ context.Context, actor, run uint64, format string) (*agentsvc.TraceBundle, error) {
	f.calls = append(f.calls, "export")
	f.gotActor, f.gotRun, f.gotFormat = actor, run, format
	if f.err != nil {
		return nil, f.err
	}
	return &agentsvc.TraceBundle{ExportedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), Files: []agentsvc.TraceFile{
		{Name: "report.md", Content: []byte("# 报告\n")}, {Name: "meta.json", Content: []byte(`{"run_id":"x"}`)}}}, nil
}

func newTraceRouter(api TraceAdminAPI) *gin.Engine {
	h := NewTraceAdminHandler(api)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(9)); c.Next() })
	a := r.Group("/api/v1/admin/agent/runs")
	a.GET("", h.List)
	a.GET("/:rid/trace", h.Export)
	return r
}

func TestTraceAdmin_ListBindsQuery(t *testing.T) {
	api := &fakeTraceAPI{}
	r := newTraceRouter(api)
	code, resp := canvasCall(t, r, http.MethodGet, "/api/v1/admin/agent/runs?user=alice&canvas_id=c1&run_id=r1&status=failed&source=harness&from=2026-10-01T00:00:00Z&to=2026-10-20T00:00:00Z&page=2&page_size=50", "")
	if code != http.StatusOK || resp["code"].(float64) != 0 {
		t.Fatalf("code=%d %v", code, resp)
	}
	g := api.gotList
	if g.User != "alice" || g.CanvasID != "c1" || g.RunID != "r1" || g.Status != "failed" || g.Source != "harness" || g.Page != 2 || g.PageSize != 50 ||
		g.From != "2026-10-01T00:00:00Z" || g.To != "2026-10-20T00:00:00Z" {
		t.Errorf("查询参数绑定不对: %+v", g)
	}
	data := resp["data"].(map[string]any)
	if data["total"].(float64) != 1 || data["page"].(float64) != 1 || data["page_size"].(float64) != 20 || len(data["list"].([]any)) != 1 {
		t.Errorf("分页信封: %v", data)
	}
	item := data["list"].([]any)[0].(map[string]any)
	if item["id"] != idcodec.Encode(5) {
		t.Errorf("id 应是编码后的串: %v", item["id"])
	}
}

// 状态只认真实的 12 个运行状态；done / error 不是状态。
func TestTraceAdmin_ListStatusValues(t *testing.T) {
	for _, st := range []string{"queued", "running", "waiting_approval", "waiting_input", "succeeded", "failed", "canceled", "budget_exhausted", "step_limit", "timeout", "interrupted", "expired"} {
		code, resp := canvasCall(t, newTraceRouter(&fakeTraceAPI{}), http.MethodGet, "/api/v1/admin/agent/runs?status="+st, "")
		if code != http.StatusOK {
			t.Errorf("status=%s 应被接受: %d %v", st, code, resp)
		}
	}
	for _, st := range []string{"done", "error", "pending", "bogus"} {
		code, _ := canvasCall(t, newTraceRouter(&fakeTraceAPI{}), http.MethodGet, "/api/v1/admin/agent/runs?status="+st, "")
		if code != http.StatusBadRequest {
			t.Errorf("status=%s 不是运行状态，应 400: %d", st, code)
		}
	}
	if code, _ := canvasCall(t, newTraceRouter(&fakeTraceAPI{}), http.MethodGet, "/api/v1/admin/agent/runs?source=robot", ""); code != http.StatusBadRequest {
		t.Errorf("source 只能是 user 或 harness: %d", code)
	}
}

func TestTraceAdmin_ExportZip(t *testing.T) {
	api := &fakeTraceAPI{}
	r := newTraceRouter(api)
	rid := idcodec.Encode(42)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/agent/runs/"+rid+"/trace", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type=%q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="agent-trace-`+rid+`.zip"` {
		t.Errorf("Content-Disposition=%q", cd)
	}
	if api.gotActor != 9 || api.gotRun != 42 || api.gotFormat != "zip" {
		t.Errorf("参数: actor=%d run=%d format=%s", api.gotActor, api.gotRun, api.gotFormat)
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil || len(zr.File) != 2 || zr.File[0].Name != "report.md" {
		t.Fatalf("zip 不对: %v %v", err, zr)
	}
	f, _ := zr.File[0].Open()
	body, _ := io.ReadAll(f)
	if string(body) != "# 报告\n" {
		t.Errorf("内容: %q", body)
	}
}

func TestTraceAdmin_ExportJSON(t *testing.T) {
	api := &fakeTraceAPI{}
	r := newTraceRouter(api)
	code, resp := canvasCall(t, r, http.MethodGet, "/api/v1/admin/agent/runs/"+idcodec.Encode(42)+"/trace?format=json", "")
	if code != http.StatusOK || resp["code"].(float64) != 0 || api.gotFormat != "json" {
		t.Fatalf("code=%d %v format=%s", code, resp, api.gotFormat)
	}
	files := resp["data"].(map[string]any)["files"].(map[string]any)
	if files["report.md"] != "# 报告\n" || files["meta.json"] != `{"run_id":"x"}` {
		t.Errorf("files: %v", files)
	}
	var _ = json.Marshal
}

func TestTraceAdmin_ExportErrors(t *testing.T) {
	cases := []struct {
		name string
		path string
		err  error
		code int
		biz  int
	}{
		{"格式不对", "/api/v1/admin/agent/runs/" + idcodec.Encode(1) + "/trace?format=tar", nil, http.StatusBadRequest, errcode.ErrInvalidParams.Code},
		{"id 不是编码串", "/api/v1/admin/agent/runs/12/trace", nil, http.StatusBadRequest, errcode.ErrInvalidParams.Code},
		{"运行不存在", "/api/v1/admin/agent/runs/" + idcodec.Encode(1) + "/trace", errcode.ErrAgentRunMissing, http.StatusNotFound, errcode.ErrAgentRunMissing.Code},
		{"轨迹已过期", "/api/v1/admin/agent/runs/" + idcodec.Encode(1) + "/trace", errcode.ErrAgentTraceExpired, http.StatusGone, errcode.ErrAgentTraceExpired.Code},
		{"轨迹已关闭", "/api/v1/admin/agent/runs/" + idcodec.Encode(1) + "/trace", errcode.ErrAgentTraceDisabled, http.StatusNotFound, errcode.ErrAgentTraceDisabled.Code},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeTraceAPI{err: tc.err}
			code, resp := canvasCall(t, newTraceRouter(api), http.MethodGet, tc.path, "")
			if code != tc.code || int(resp["code"].(float64)) != tc.biz {
				t.Errorf("code=%d biz=%v, want %d / %d", code, resp["code"], tc.code, tc.biz)
			}
			if tc.err == nil && len(api.calls) != 0 {
				t.Error("参数不对不应进入业务层")
			}
		})
	}
}
