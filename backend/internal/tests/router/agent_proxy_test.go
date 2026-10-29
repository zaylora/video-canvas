package router_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	agenthandler "video-canvas/internal/handler/agent"
	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/router"
)

const (
	proxyJWTSecret = "test-secret-test-secret-test-secret"
	proxyToken     = "0123456789abcdef0123456789abcdef"
)

// durableRouter 搭一个 agent.runtime=durable 的路由：前端的 Agent 接口走代理，上游是记录请求的假 Agent 服务。
func durableRouter(t *testing.T) (r *gin.Engine, jwt string, seen func() []string) {
	t.Helper()
	var mu sync.Mutex
	var hits []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		mu.Lock()
		hits = append(hits, req.Method+" "+req.URL.Path+" user="+req.Header.Get("X-Agent-User"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	t.Cleanup(up.Close)
	proxy, err := agenthandler.NewAgentProxy(up.URL, proxyToken)
	if err != nil {
		t.Fatal(err)
	}
	jwt, _, err = utils.GenerateToken(42, "alice", 1, proxyJWTSecret, "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	r = router.New("test", proxyJWTSecret, router.Handlers{
		Health:     &handler.HealthHandler{},
		AgentProxy: proxy,
		UserState: func(context.Context, uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Role: "user", Status: "active", TokenVersion: 1}, nil
		},
	})
	return r, jwt, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), hits...) }
}

func TestRouter_DurableForwardsAgentRoutesWithLoginUser(t *testing.T) {
	r, jwt, seen := durableRouter(t)
	routes := []struct{ method, path string }{
		{"GET", "/api/v1/agent/models"},
		{"GET", "/api/v1/canvas/c1/agent/sessions"},
		{"POST", "/api/v1/canvas/c1/agent/sessions"},
		{"PATCH", "/api/v1/agent/sessions/s1"},
		{"DELETE", "/api/v1/agent/sessions/s1"},
		{"GET", "/api/v1/agent/sessions/s1/events"},
		{"GET", "/api/v1/agent/sessions/s1/history"},
		{"POST", "/api/v1/agent/sessions/s1/runs"},
		{"GET", "/api/v1/agent/sessions/s1/watches"},
		{"POST", "/api/v1/agent/sessions/s1/watches/cancel"},
		{"POST", "/api/v1/agent/runs/r1/interject"},
		{"POST", "/api/v1/agent/runs/r1/cancel"},
		{"POST", "/api/v1/agent/runs/r1/resume"},
		{"POST", "/api/v1/agent/runs/r1/undo"},
		{"PUT", "/api/v1/agent/runs/r1/feedback"},
		{"DELETE", "/api/v1/agent/runs/r1/feedback"},
		{"POST", "/api/v1/agent/approvals/a1/decision"},
	}
	for _, rt := range routes {
		req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+jwt)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s %s 应转发给 Agent 服务：%d %s", rt.method, rt.path, w.Code, w.Body.String())
		}
	}
	got := seen()
	if len(got) != len(routes) {
		t.Fatalf("上游应收到 %d 个请求，实际 %d：%v", len(routes), len(got), got)
	}
	for i, rt := range routes {
		if want := rt.method + " " + strings.TrimPrefix(rt.path, "/api/v1") + " user=42"; got[i] != want {
			t.Errorf("第 %d 个请求：期望 %q，实际 %q", i, want, got[i])
		}
	}
}

func TestRouter_DurableAgentRoutesStillNeedLogin(t *testing.T) {
	r, _, seen := durableRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/agent/sessions/s1/runs", strings.NewReader(`{}`)))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("没带登录态应是 401：%d", w.Code)
	}
	if len(seen()) != 0 {
		t.Error("没登录时 Agent 服务不应收到请求")
	}
}

// 没启用 Agent（agent.enabled=false）：这些接口统一返回「Agent 暂不可用」，而不是 404，前端据此提示。
func TestRouter_AgentRoutesWithoutAgentAreUnavailable(t *testing.T) {
	jwt, _, err := utils.GenerateToken(42, "alice", 1, proxyJWTSecret, "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	r := router.New("test", proxyJWTSecret, router.Handlers{
		Health: &handler.HealthHandler{},
		UserState: func(context.Context, uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Role: "user", Status: "active", TokenVersion: 1}, nil
		},
	})
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/v1/agent/models"}, {"POST", "/api/v1/canvas/c1/agent/sessions"}, {"POST", "/api/v1/agent/sessions/s1/runs"}, {"POST", "/api/v1/agent/approvals/a1/decision"},
	} {
		req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+jwt)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"code":60005`) {
			t.Errorf("%s %s 应返回 503/60005：%d %s", rt.method, rt.path, w.Code, w.Body.String())
		}
	}
}
