package agent_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	agenthandler "video-canvas/internal/handler/agent"
	"video-canvas/internal/middleware"
)

const proxyToken = "0123456789abcdef0123456789abcdef"

// seenRequest 是上游（Agent 服务）收到的请求。
type seenRequest struct {
	method, path, query, body string
	header                    http.Header
}

// upstreamStub 起一个假的 Agent 服务，记下收到的请求，按给定状态码和响应体回复。
func upstreamStub(t *testing.T, status int, reply string) (*httptest.Server, func() seenRequest) {
	t.Helper()
	var mu sync.Mutex
	var got seenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = seenRequest{r.Method, r.URL.Path, r.URL.RawQuery, string(b), r.Header.Clone()}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, func() seenRequest { mu.Lock(); defer mu.Unlock(); return got }
}

// proxyRouter 把代理挂在 /api/v1/agent 和画布会话路径上；userID 模拟 JWT 中间件写入的当前用户。
func proxyRouter(t *testing.T, target string, userID uint) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	p, err := agenthandler.NewAgentProxy(target, proxyToken)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	g := r.Group("/api/v1", func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, userID); c.Next() })
	g.Any("/agent/*rest", p.Forward)
	g.Any("/canvas/:id/agent/sessions", p.Forward)
	return r
}

func TestAgentProxy_ForwardsRequestWithServiceIdentity(t *testing.T) {
	srv, seen := upstreamStub(t, http.StatusAccepted, `{"code":0,"msg":"success","data":{"run_id":"abc"}}`)
	r := proxyRouter(t, srv.URL, 42)

	req := httptest.NewRequest("POST", "/api/v1/agent/sessions/s1/runs?x=1", strings.NewReader(`{"message":"你好"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer user-jwt")
	req.Header.Set("Cookie", "session=secret")
	req.Header.Set("X-Agent-User", "999")                   // 前端伪造的用户头必须被覆盖
	req.Header.Set("X-Agent-Service-Token", "forged-token") // 前端伪造的服务令牌必须被覆盖
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"run_id":"abc"`) {
		t.Fatalf("上游的状态码和响应体应原样返回：%d %s", w.Code, w.Body.String())
	}
	got := seen()
	if got.method != "POST" || got.path != "/agent/sessions/s1/runs" || got.query != "x=1" || got.body != `{"message":"你好"}` {
		t.Errorf("方法、路径、查询串、请求体要原样转发：%+v", got)
	}
	if got.header.Get("X-Agent-User") != "42" {
		t.Errorf("用户头必须是登录态里的用户：%q", got.header.Get("X-Agent-User"))
	}
	if got.header.Get("X-Agent-Service-Token") != proxyToken {
		t.Errorf("服务令牌必须是配置里的：%q", got.header.Get("X-Agent-Service-Token"))
	}
	if got.header.Get("Authorization") != "" || got.header.Get("Cookie") != "" {
		t.Errorf("用户的登录凭据不能转给 Agent 服务：%v", got.header)
	}
}

func TestAgentProxy_RejectsRequestWithoutUser(t *testing.T) {
	srv, seen := upstreamStub(t, 200, `{}`)
	r := proxyRouter(t, srv.URL, 0)
	status, resp := canvasCall(t, r, "GET", "/api/v1/agent/models", "")
	if status != http.StatusUnauthorized || resp["code"] != float64(10003) {
		t.Errorf("没有登录用户不能转发：%d %v", status, resp)
	}
	if seen().method != "" {
		t.Error("没有登录用户时上游不应收到请求")
	}
}

func TestAgentProxy_UpstreamDownIsAgentUnavailable(t *testing.T) {
	srv, _ := upstreamStub(t, 200, `{}`)
	url := srv.URL
	srv.Close() // 上游挂了
	r := proxyRouter(t, url, 7)
	status, resp := canvasCall(t, r, "GET", "/api/v1/agent/models", "")
	if status != http.StatusServiceUnavailable || resp["code"] != float64(60005) {
		t.Errorf("Agent 服务连不上要报 60005/503，不暴露内部地址：%d %v", status, resp)
	}
}

func TestAgentProxy_PassesBusinessErrorsThrough(t *testing.T) {
	srv, _ := upstreamStub(t, http.StatusConflict, `{"code":60001,"msg":"这个画布上已有运行中的 Agent"}`)
	r := proxyRouter(t, srv.URL, 7)
	status, resp := canvasCall(t, r, "POST", "/api/v1/canvas/c1/agent/sessions", `{}`)
	if status != http.StatusConflict || resp["code"] != float64(60001) || resp["msg"] != "这个画布上已有运行中的 Agent" {
		t.Errorf("业务错误要原样给前端：%d %v", status, resp)
	}
}

func TestNewAgentProxy_ValidatesTarget(t *testing.T) {
	for _, bad := range []string{"", "agent:8090", "ftp://agent", "http://"} {
		if _, err := agenthandler.NewAgentProxy(bad, proxyToken); err == nil {
			t.Errorf("%q 不是合法的 Agent 地址，应报错", bad)
		}
	}
	if _, err := agenthandler.NewAgentProxy("http://agent:8090", ""); err == nil {
		t.Error("没有服务令牌应报错")
	}
}
