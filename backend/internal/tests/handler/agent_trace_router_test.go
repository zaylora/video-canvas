package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	agenthandler "video-canvas/internal/handler/agent"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/router"
	agentsvc "video-canvas/internal/service/agent"
	"video-canvas/pkg/pagination"
)

const traceRouterSecret = "trace-router-secret"

// traceRouterAPI 只记录有没有被调用到：真正要验证的是路由挂在 RequireAdmin 之下。
type traceRouterAPI struct{ calls int }

func (a *traceRouterAPI) ListRuns(context.Context, *model.ListAgentRunsReq) (*pagination.Result[agentsvc.AdminRunItem], error) {
	a.calls++
	return &pagination.Result[agentsvc.AdminRunItem]{List: []agentsvc.AdminRunItem{}, Page: 1, PageSize: 20}, nil
}

func (a *traceRouterAPI) ExportTrace(context.Context, uint64, uint64, string) (*agentsvc.TraceBundle, error) {
	a.calls++
	return &agentsvc.TraceBundle{Files: []agentsvc.TraceFile{{Name: "report.md", Content: []byte("x")}}}, nil
}

// 后台轨迹接口走真实 router：不登录 401，普通用户 403（连自己的轨迹也不能看），admin 与 super_admin 可用；
// 路由注册冲突（和技能管理同在 /admin/agent 下）会在构造时 panic。
func TestAgentTraceRouter_AdminOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	roles := map[uint64]string{1: model.RoleUser, 2: model.RoleAdmin, 3: model.RoleSuperAdmin}
	api := &traceRouterAPI{}
	tokens := map[uint64]string{}
	for id := range roles {
		tok, _, err := utils.GenerateToken(uint(id), "u"+strconv.Itoa(int(id)), 0, traceRouterSecret, "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		tokens[id] = tok
	}
	engine := router.New(gin.TestMode, traceRouterSecret, router.Handlers{
		AgentTrace: agenthandler.NewTraceAdminHandler(api),
		AgentSkill: agenthandler.NewSkillHandler(nil),
		AdminRole:  func(_ context.Context, id uint64) (string, error) { return roles[id], nil },
		UserState: func(_ context.Context, id uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Role: roles[id], Status: model.UserStatusActive}, nil
		},
	})
	do := func(path string, user uint64) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if user > 0 {
			req.Header.Set("Authorization", "Bearer "+tokens[user])
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w.Code
	}
	list := "/api/v1/admin/agent/runs"
	export := "/api/v1/admin/agent/runs/" + idcodec.Encode(1) + "/trace"
	for _, path := range []string{list, export} {
		if c := do(path, 0); c != http.StatusUnauthorized {
			t.Errorf("%s 不登录应 401: %d", path, c)
		}
		if c := do(path, 1); c != http.StatusForbidden {
			t.Errorf("%s 普通用户应 403: %d", path, c)
		}
	}
	if api.calls != 0 {
		t.Fatalf("没有权限的请求不能进到业务层: %d", api.calls)
	}
	for _, user := range []uint64{2, 3} {
		for _, path := range []string{list, export} {
			if c := do(path, user); c != http.StatusOK {
				t.Errorf("%s 用户 %d（%s）应 200: %d", path, user, roles[user], c)
			}
		}
	}
	if api.calls != 4 {
		t.Errorf("admin 与 super_admin 的请求都应到达业务层: %d", api.calls)
	}
}
