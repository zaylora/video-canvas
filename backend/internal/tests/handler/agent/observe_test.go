package agent_test

import (
	"context"
	"encoding/json"
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
)

// ---- 反馈 ----

type fakeFeedbackAPI struct {
	err      error
	gotUser  uint64
	gotRun   uint64
	gotReq   *model.SetAgentFeedbackReq
	cleared  bool
	setCalls int
}

func (f *fakeFeedbackAPI) SetFeedback(_ context.Context, u, id uint64, req *model.SetAgentFeedbackReq) (*agentsvc.FeedbackView, error) {
	f.setCalls++
	f.gotUser, f.gotRun, f.gotReq = u, id, req
	if f.err != nil {
		return nil, f.err
	}
	return &agentsvc.FeedbackView{Rating: req.Rating, Reasons: req.Reasons, Comment: req.Comment}, nil
}

func (f *fakeFeedbackAPI) ClearFeedback(_ context.Context, u, id uint64) error {
	f.gotUser, f.gotRun, f.cleared = u, id, true
	return f.err
}

func newFeedbackRouter(api FeedbackAPI) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	h := NewAgentFeedbackHandler(api)
	r.PUT("/api/v1/agent/runs/:rid/feedback", h.Set)
	r.DELETE("/api/v1/agent/runs/:rid/feedback", h.Clear)
	return r
}

func TestFeedbackHandler(t *testing.T) {
	rid := idcodec.Encode(22)

	t.Run("点踩：带上当前用户和解码后的运行 id，返回服务端记下的评价", func(t *testing.T) {
		api := &fakeFeedbackAPI{}
		status, resp := agentCall(t, newFeedbackRouter(api), "PUT", "/api/v1/agent/runs/"+rid+"/feedback", `{"rating":-1,"reasons":["misread"],"comment":"不对"}`)
		if status != 200 || codeOf(resp) != 0 {
			t.Fatalf("status=%d resp=%v", status, resp)
		}
		if api.gotUser != 1 || api.gotRun != 22 || api.gotReq.Rating != -1 || api.gotReq.Reasons[0] != "misread" {
			t.Errorf("api=%+v req=%+v", api, api.gotReq)
		}
		data := resp["data"].(map[string]any)
		if data["rating"] != float64(-1) || data["comment"] != "不对" {
			t.Errorf("data=%v", data)
		}
	})

	t.Run("参数错误：评分缺失、不是 1/-1、运行 id 格式不对 → 400 + 10001，不调业务层", func(t *testing.T) {
		for name, c := range map[string]struct{ path, body string }{
			"没有评分":     {"/api/v1/agent/runs/" + rid + "/feedback", `{}`},
			"评分是 2":    {"/api/v1/agent/runs/" + rid + "/feedback", `{"rating":2}`},
			"评分是 0":    {"/api/v1/agent/runs/" + rid + "/feedback", `{"rating":0}`},
			"原因太多":     {"/api/v1/agent/runs/" + rid + "/feedback", `{"rating":-1,"reasons":["a","b","c","d","e","f"]}`},
			"运行 id 不对": {"/api/v1/agent/runs/nope/feedback", `{"rating":1}`},
		} {
			api := &fakeFeedbackAPI{}
			status, resp := agentCall(t, newFeedbackRouter(api), "PUT", c.path, c.body)
			if status != http.StatusBadRequest || codeOf(resp) != errcode.ErrInvalidParams.Code || api.setCalls != 0 {
				t.Errorf("%s：status=%d code=%d calls=%d", name, status, codeOf(resp), api.setCalls)
			}
		}
	})

	t.Run("业务错误原样透传：运行没结束 60014 → 409", func(t *testing.T) {
		api := &fakeFeedbackAPI{err: errcode.ErrAgentRunNotEnded}
		status, resp := agentCall(t, newFeedbackRouter(api), "PUT", "/api/v1/agent/runs/"+rid+"/feedback", `{"rating":1}`)
		if status != http.StatusConflict || codeOf(resp) != errcode.ErrAgentRunNotEnded.Code {
			t.Errorf("status=%d resp=%v", status, resp)
		}
	})

	t.Run("取消评价", func(t *testing.T) {
		api := &fakeFeedbackAPI{}
		status, resp := agentCall(t, newFeedbackRouter(api), "DELETE", "/api/v1/agent/runs/"+rid+"/feedback", "")
		if status != 200 || codeOf(resp) != 0 || !api.cleared || api.gotRun != 22 || api.gotUser != 1 {
			t.Errorf("status=%d resp=%v api=%+v", status, resp, api)
		}
		status, _ = agentCall(t, newFeedbackRouter(&fakeFeedbackAPI{}), "DELETE", "/api/v1/agent/runs/zzz/feedback", "")
		if status != http.StatusBadRequest {
			t.Errorf("id 格式错误应 400：%d", status)
		}
		status, resp = agentCall(t, newFeedbackRouter(&fakeFeedbackAPI{err: errcode.ErrAgentRunMissing}), "DELETE", "/api/v1/agent/runs/"+rid+"/feedback", "")
		if status != http.StatusNotFound || codeOf(resp) != errcode.ErrAgentRunMissing.Code {
			t.Errorf("status=%d resp=%v", status, resp)
		}
	})
}

// ---- 后台观测 ----

type fakeObserveAPI struct {
	err        error
	who        agentsvc.ObserveActor
	filter     *agentsvc.RunListFilter
	exportFmt  string
	exportID   uint64
	requestsID uint64
	groupBy    string
	since      time.Time
	issued     *model.IssueAnalysisTokenReq
	revoked    uint64
}

func (f *fakeObserveAPI) ListRuns(_ context.Context, who agentsvc.ObserveActor, flt *agentsvc.RunListFilter) (*agentsvc.RunListPage, error) {
	f.who, f.filter = who, flt
	return &agentsvc.RunListPage{Items: []*agentsvc.AgentRunDigestView{{RunID: 5, Status: "succeeded"}}, NextCursor: "c2"}, f.err
}

func (f *fakeObserveAPI) ExportRun(_ context.Context, who agentsvc.ObserveActor, id uint64, format string) (*agentsvc.RunExport, error) {
	f.who, f.exportID, f.exportFmt = who, id, format
	if f.err != nil {
		return nil, f.err
	}
	out := &agentsvc.RunExport{Format: format}
	if format != "json" {
		out.Content = "# md"
	}
	if format != "md" {
		out.Data = &agentsvc.RunExportView{RuntimeTail: "tail"}
	}
	return out, nil
}

func (f *fakeObserveAPI) RunRequests(_ context.Context, who agentsvc.ObserveActor, id uint64) (*agentsvc.RunRequestsExport, error) {
	f.who, f.requestsID = who, id
	if f.err != nil {
		return nil, f.err
	}
	return &agentsvc.RunRequestsExport{Items: []*agentsvc.RunRequestItem{{ID: 11, ModelRequestView: agentsvc.ModelRequestView{Messages: 3, Kept: true, Body: []byte(`{"model":"m"}`)}}}}, nil
}

func (f *fakeObserveAPI) Stats(_ context.Context, who agentsvc.ObserveActor, groupBy string, since, _ time.Time) ([]model.AgentStatsRow, error) {
	f.who, f.groupBy, f.since = who, groupBy, since
	return nil, f.err
}

func (f *fakeObserveAPI) IssueToken(_ context.Context, adminID uint64, req *model.IssueAnalysisTokenReq) (*agentsvc.IssuedToken, error) {
	f.issued = req
	f.who.UserID = adminID
	if f.err != nil {
		return nil, f.err
	}
	return &agentsvc.IssuedToken{Item: &agentsvc.AnalysisTokenView{ID: 1, Name: req.Name, Prefix: "vca_abcd"}, Token: "vca_" + strings.Repeat("a", 64)}, nil
}

func (f *fakeObserveAPI) ListTokens(context.Context) ([]*agentsvc.AnalysisTokenView, error) {
	return []*agentsvc.AnalysisTokenView{{ID: 1, Name: "本机"}}, f.err
}

func (f *fakeObserveAPI) RevokeToken(_ context.Context, adminID, id uint64) error {
	f.who.UserID, f.revoked = adminID, id
	return f.err
}

func newObserveRouter(api ObserveAPI, tokenID uint64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxUserIDKey, uint(9))
		if tokenID != 0 {
			c.Set(middleware.CtxAnalysisTokenIDKey, tokenID)
		}
		c.Next()
	})
	h := NewAgentObserveHandler(api)
	g := r.Group("/api/v1/admin/agent")
	g.GET("/runs", h.ListRuns)
	g.GET("/runs/:rid", h.Get)
	g.GET("/runs/:rid/export", h.Export)
	g.GET("/runs/:rid/requests", h.Requests)
	g.GET("/stats", h.Stats)
	g.GET("/tokens", h.ListTokens)
	g.POST("/tokens", h.IssueToken)
	g.DELETE("/tokens/:id", h.RevokeToken)
	return r
}

func TestObserveHandler_ListRuns(t *testing.T) {
	t.Run("筛选参数全部解析并交给业务层；返回编码 id 的摘要和游标", func(t *testing.T) {
		api := &fakeObserveAPI{}
		q := "?since=2026-10-01&until=2026-10-20T08:00:00Z&status=failed,%20step_limit&feedback=down&undone=true&harness_key=hk9&prompt_version=9&model_key=claude&user_id=7&cursor=c1&limit=50"
		status, resp := agentCall(t, newObserveRouter(api, 3), "GET", "/api/v1/admin/agent/runs"+q, "")
		if status != 200 || codeOf(resp) != 0 {
			t.Fatalf("status=%d resp=%v", status, resp)
		}
		f := api.filter
		if !f.Since.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) || !f.Until.Equal(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)) {
			t.Errorf("时间：%v %v", f.Since, f.Until)
		}
		if len(f.Statuses) != 2 || f.Statuses[1] != "step_limit" || f.Feedback != "down" || !f.Undone || f.HarnessKey != "hk9" || *f.PromptVersion != 9 || f.ModelKey != "claude" || f.UserID != 7 || f.Cursor != "c1" || f.Limit != 50 {
			t.Errorf("filter=%+v", f)
		}
		if api.who.UserID != 9 || api.who.TokenID != 3 {
			t.Errorf("访问者应带上令牌 id：%+v", api.who)
		}
		data := resp["data"].(map[string]any)
		if data["next_cursor"] != "c2" || len(data["items"].([]any)) != 1 {
			t.Errorf("data=%v", data)
		}
	})

	t.Run("管理员 JWT 访问：没有令牌 id", func(t *testing.T) {
		api := &fakeObserveAPI{}
		agentCall(t, newObserveRouter(api, 0), "GET", "/api/v1/admin/agent/runs", "")
		if api.who.TokenID != 0 {
			t.Errorf("who=%+v", api.who)
		}
	})

	t.Run("参数错误 → 400 + 10001", func(t *testing.T) {
		for name, q := range map[string]string{
			"时间格式不对":       "?since=yesterday",
			"limit 超过 500": "?limit=501",
			"版本不是数字":       "?prompt_version=abc",
			"user_id 不是数字": "?user_id=x",
		} {
			status, resp := agentCall(t, newObserveRouter(&fakeObserveAPI{}, 0), "GET", "/api/v1/admin/agent/runs"+q, "")
			if status != http.StatusBadRequest || codeOf(resp) != errcode.ErrInvalidParams.Code {
				t.Errorf("%s：status=%d resp=%v", name, status, resp)
			}
		}
	})

	t.Run("业务错误透传", func(t *testing.T) {
		status, resp := agentCall(t, newObserveRouter(&fakeObserveAPI{err: errcode.ErrInvalidParams.WithMsg("游标无效")}, 0), "GET", "/api/v1/admin/agent/runs?cursor=x", "")
		if status != http.StatusBadRequest || resp["msg"] != "游标无效" {
			t.Errorf("status=%d resp=%v", status, resp)
		}
	})
}

func TestObserveHandler_Export(t *testing.T) {
	rid := idcodec.Encode(22)
	base := "/api/v1/admin/agent/runs/" + rid

	t.Run("默认 md：返回 {format, content}", func(t *testing.T) {
		api := &fakeObserveAPI{}
		status, resp := agentCall(t, newObserveRouter(api, 0), "GET", base+"/export", "")
		data, _ := resp["data"].(map[string]any)
		if status != 200 || api.exportFmt != "md" || api.exportID != 22 || data["format"] != "md" || data["content"] != "# md" {
			t.Errorf("status=%d fmt=%s data=%v", status, api.exportFmt, data)
		}
	})

	t.Run("json：直接返回结构化对象", func(t *testing.T) {
		api := &fakeObserveAPI{}
		_, resp := agentCall(t, newObserveRouter(api, 0), "GET", base+"/export?format=json", "")
		data, _ := resp["data"].(map[string]any)
		if data["runtime_tail"] != "tail" || data["format"] != nil {
			t.Errorf("data=%v", data)
		}
	})

	t.Run("both：{format, content, data}，一次请求拿全", func(t *testing.T) {
		_, resp := agentCall(t, newObserveRouter(&fakeObserveAPI{}, 0), "GET", base+"/export?format=both", "")
		data, _ := resp["data"].(map[string]any)
		inner, _ := data["data"].(map[string]any)
		if data["format"] != "both" || data["content"] != "# md" || inner["runtime_tail"] != "tail" {
			t.Errorf("data=%v", data)
		}
	})

	t.Run("详情接口等同 json 导出", func(t *testing.T) {
		api := &fakeObserveAPI{}
		_, resp := agentCall(t, newObserveRouter(api, 0), "GET", base, "")
		data, _ := resp["data"].(map[string]any)
		if api.exportFmt != "json" || data["runtime_tail"] != "tail" {
			t.Errorf("fmt=%s data=%v", api.exportFmt, data)
		}
	})

	t.Run("运行 id 格式错误 → 400；运行不存在 → 404", func(t *testing.T) {
		status, _ := agentCall(t, newObserveRouter(&fakeObserveAPI{}, 0), "GET", "/api/v1/admin/agent/runs/zzz/export", "")
		if status != http.StatusBadRequest {
			t.Errorf("status=%d", status)
		}
		status, resp := agentCall(t, newObserveRouter(&fakeObserveAPI{err: errcode.ErrAgentRunMissing}, 0), "GET", base+"/export", "")
		if status != http.StatusNotFound || codeOf(resp) != errcode.ErrAgentRunMissing.Code {
			t.Errorf("status=%d resp=%v", status, resp)
		}
	})
}

func TestObserveHandler_Stats(t *testing.T) {
	api := &fakeObserveAPI{}
	status, resp := agentCall(t, newObserveRouter(api, 0), "GET", "/api/v1/admin/agent/stats?group_by=prompt_version&since=2026-10-01", "")
	data, _ := resp["data"].(map[string]any)
	groups, ok := data["groups"].([]any)
	if status != 200 || api.groupBy != "prompt_version" || api.since.IsZero() || !ok || len(groups) != 0 {
		t.Errorf("没有数据时 groups 是空数组而不是 null：status=%d data=%v", status, data)
	}
	status, _ = agentCall(t, newObserveRouter(&fakeObserveAPI{}, 0), "GET", "/api/v1/admin/agent/stats", "")
	if status != http.StatusBadRequest {
		t.Errorf("缺 group_by 应 400：%d", status)
	}
}

func TestObserveHandler_Tokens(t *testing.T) {
	t.Run("列表", func(t *testing.T) {
		_, resp := agentCall(t, newObserveRouter(&fakeObserveAPI{}, 0), "GET", "/api/v1/admin/agent/tokens", "")
		items := resp["data"].(map[string]any)["items"].([]any)
		if len(items) != 1 {
			t.Errorf("resp=%v", resp)
		}
	})

	t.Run("签发：返回 {item, token}，签发人是当前管理员", func(t *testing.T) {
		api := &fakeObserveAPI{}
		status, resp := agentCall(t, newObserveRouter(api, 0), "POST", "/api/v1/admin/agent/tokens", `{"name":"本机","days":30}`)
		data, _ := resp["data"].(map[string]any)
		if status != 200 || api.issued.Days != 30 || api.who.UserID != 9 || !strings.HasPrefix(data["token"].(string), "vca_") || data["item"].(map[string]any)["prefix"] != "vca_abcd" {
			t.Errorf("status=%d data=%v", status, data)
		}
		raw, _ := json.Marshal(resp)
		if strings.Contains(string(raw), "token_hash") {
			t.Error("响应里不能有哈希")
		}
	})

	t.Run("签发参数错误：缺用途、缺天数 → 10001，不调业务层", func(t *testing.T) {
		for _, body := range []string{`{}`, `{"name":"x"}`, `{"days":30}`, `{"name":"x","days":0}`, `not json`} {
			api := &fakeObserveAPI{}
			status, resp := agentCall(t, newObserveRouter(api, 0), "POST", "/api/v1/admin/agent/tokens", body)
			if status != http.StatusBadRequest || codeOf(resp) != errcode.ErrInvalidParams.Code || api.issued != nil {
				t.Errorf("body=%s status=%d resp=%v", body, status, resp)
			}
		}
	})

	t.Run("有效期超出范围：业务层给出明确提示", func(t *testing.T) {
		api := &fakeObserveAPI{err: errcode.ErrInvalidParams.WithMsg("有效期必须在 1–90 天之间")}
		status, resp := agentCall(t, newObserveRouter(api, 0), "POST", "/api/v1/admin/agent/tokens", `{"name":"x","days":91}`)
		if status != http.StatusBadRequest || resp["msg"] != "有效期必须在 1–90 天之间" {
			t.Errorf("status=%d resp=%v", status, resp)
		}
	})

	t.Run("吊销：id 是数字；格式错误 400；不存在 404", func(t *testing.T) {
		api := &fakeObserveAPI{}
		status, _ := agentCall(t, newObserveRouter(api, 0), "DELETE", "/api/v1/admin/agent/tokens/12", "")
		if status != 200 || api.revoked != 12 || api.who.UserID != 9 {
			t.Errorf("status=%d api=%+v", status, api)
		}
		for _, id := range []string{"abc", "0", "-1"} {
			if status, _ := agentCall(t, newObserveRouter(&fakeObserveAPI{}, 0), "DELETE", "/api/v1/admin/agent/tokens/"+id, ""); status != http.StatusBadRequest {
				t.Errorf("id=%s status=%d", id, status)
			}
		}
		status, _ = agentCall(t, newObserveRouter(&fakeObserveAPI{err: errcode.ErrNotFound}, 0), "DELETE", "/api/v1/admin/agent/tokens/9", "")
		if status != http.StatusNotFound {
			t.Errorf("status=%d", status)
		}
	})
}

// ---- 分析令牌中间件 ----

func TestAdminOrAnalysisToken(t *testing.T) {
	auth := func(_ context.Context, plain string) (uint64, uint64, error) {
		switch plain {
		case "vca_good":
			return 3, 9, nil
		case "vca_limited":
			return 0, 0, errcode.ErrTooManyReqs
		}
		return 0, 0, errcode.ErrUnauthorized.WithMsg("分析令牌无效、已过期或已吊销")
	}
	var chainRan bool
	jwtOK := func(c *gin.Context) { chainRan = true; c.Set(middleware.CtxUserIDKey, uint(5)) }
	jwtDeny := func(c *gin.Context) {
		chainRan = true
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": errcode.ErrUnauthorized.Code})
	}

	serve := func(mw gin.HandlerFunc, authz string) (*httptest.ResponseRecorder, map[string]any) {
		chainRan = false
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.GET("/x", mw, func(c *gin.Context) {
			c.JSON(200, gin.H{"user": middleware.GetUserID(c), "token": middleware.GetAnalysisTokenID(c)})
		})
		req := httptest.NewRequest("GET", "/x", nil)
		if authz != "" {
			req.Header.Set("Authorization", authz)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return w, m
	}

	t.Run("有效令牌：把签发人和令牌 id 写进上下文，不走 JWT 链", func(t *testing.T) {
		w, m := serve(middleware.AdminOrAnalysisToken(auth, jwtDeny), "Bearer vca_good")
		if w.Code != 200 || m["user"] != float64(9) || m["token"] != float64(3) || chainRan {
			t.Errorf("code=%d m=%v chainRan=%v", w.Code, m, chainRan)
		}
	})

	t.Run("无效令牌：401，不退回 JWT（vca_ 前缀就只当令牌看）", func(t *testing.T) {
		w, _ := serve(middleware.AdminOrAnalysisToken(auth, jwtOK), "Bearer vca_bad")
		if w.Code != http.StatusUnauthorized || chainRan {
			t.Errorf("code=%d chainRan=%v", w.Code, chainRan)
		}
	})

	t.Run("限流：429", func(t *testing.T) {
		w, _ := serve(middleware.AdminOrAnalysisToken(auth, jwtOK), "Bearer vca_limited")
		if w.Code != http.StatusTooManyRequests {
			t.Errorf("code=%d", w.Code)
		}
	})

	t.Run("JWT：按顺序执行校验链，通过后放行；没有令牌 id", func(t *testing.T) {
		w, m := serve(middleware.AdminOrAnalysisToken(auth, jwtOK), "Bearer eyJhbGciOi.jwt.sig")
		if w.Code != 200 || m["user"] != float64(5) || m["token"] != float64(0) || !chainRan {
			t.Errorf("code=%d m=%v chainRan=%v", w.Code, m, chainRan)
		}
	})

	t.Run("JWT 链中途拒绝：直接返回，不进业务 handler", func(t *testing.T) {
		w, _ := serve(middleware.AdminOrAnalysisToken(auth, jwtDeny), "Bearer eyJ.jwt.sig")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("code=%d", w.Code)
		}
	})

	t.Run("没有 Authorization：走 JWT 链（由它返回未登录）", func(t *testing.T) {
		w, _ := serve(middleware.AdminOrAnalysisToken(auth, jwtDeny), "")
		if w.Code != http.StatusUnauthorized || !chainRan {
			t.Errorf("code=%d chainRan=%v", w.Code, chainRan)
		}
	})
}

func TestObserveHandler_Requests(t *testing.T) {
	rid := idcodec.Encode(22)
	base := "/api/v1/admin/agent/runs/" + rid + "/requests"

	t.Run("返回每次模型调用的完整请求体", func(t *testing.T) {
		api := &fakeObserveAPI{}
		status, resp := agentCall(t, newObserveRouter(api, 0), "GET", base, "")
		data, _ := resp["data"].(map[string]any)
		items, _ := data["items"].([]any)
		if status != 200 || api.requestsID != 22 || len(items) != 1 {
			t.Fatalf("status=%d id=%d data=%v", status, api.requestsID, data)
		}
		item, _ := items[0].(map[string]any)
		body, _ := item["body"].(map[string]any)
		if item["id"] != idcodec.Encode(11) || item["messages"] != float64(3) || body["model"] != "m" {
			t.Errorf("item=%v", item)
		}
	})

	t.Run("运行 id 不合法返回参数错误，业务错误原样返回", func(t *testing.T) {
		api := &fakeObserveAPI{}
		status, _ := agentCall(t, newObserveRouter(api, 0), "GET", "/api/v1/admin/agent/runs/zzz/requests", "")
		if status == 200 || api.requestsID != 0 {
			t.Errorf("坏 id 不该走到业务：status=%d id=%d", status, api.requestsID)
		}
		api = &fakeObserveAPI{err: errcode.ErrAgentRunMissing}
		if status, _ := agentCall(t, newObserveRouter(api, 0), "GET", base, ""); status == 200 {
			t.Errorf("运行不存在不该返回 200")
		}
	})
}
