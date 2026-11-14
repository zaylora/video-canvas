package middleware_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/config"
	. "video-canvas/internal/middleware"
	"video-canvas/internal/pkg/logger"
)

// accessLog 把日志写进临时文件，返回读取全部行的函数。
func accessLog(t *testing.T) func() []map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.log")
	if err := logger.Init(config.Log{Level: "debug", Format: "json", Filename: path, MaxSize: 10}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(logger.Close)
	return func() []map[string]any {
		logger.Close()
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		var out []map[string]any
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var m map[string]any
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Fatalf("不是合法 JSON：%s", sc.Text())
			}
			out = append(out, m)
		}
		return out
	}
}

func accessLine(t *testing.T, lines []map[string]any) map[string]any {
	t.Helper()
	for _, m := range lines {
		if m["event"] == logger.EventHTTPRequest {
			return m
		}
	}
	t.Fatalf("没有访问日志：%v", lines)
	return nil
}

func TestAccessLogCarriesUserAndRunAndRequestContext(t *testing.T) {
	read := accessLog(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID(), Logger())
	var seen []string
	g := r.Group("", JWTAuth(activeTestSecret))
	g.POST("/tasks", func(c *gin.Context) {
		// 业务从 c.Request.Context() 往下传：请求编号和用户都要能带到 service
		ctx := c.Request.Context()
		for _, f := range logger.Fields(ctx) {
			seen = append(seen, f.Key)
		}
		logger.NoteRun(ctx, "gen", "a81b")
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodPost, "/tasks?x=1", nil)
	req.Header.Set("Authorization", bearer(t, 42, 0))
	req.Header.Set("X-Request-ID", "req-abc")
	r.ServeHTTP(httptest.NewRecorder(), req)

	if len(seen) < 2 || seen[0] != "request_id" || seen[1] != "user_id" {
		t.Errorf("handler 里的 ctx 应带 request_id 和 user_id，实际 %v", seen)
	}
	m := accessLine(t, read())
	want := map[string]any{"request_id": "req-abc", "user_id": float64(42), "run_kind": "gen", "run_id": "a81b",
		"method": "POST", "path": "/tasks", "status": float64(200)}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("访问日志 %s = %v，想要 %v", k, m[k], v)
		}
	}
	if _, ok := m["latency_ms"]; !ok {
		t.Error("访问日志应带 latency_ms")
	}
}

func TestAccessLogWithoutLoginHasNoUser(t *testing.T) {
	read := accessLog(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID(), Logger())
	r.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	m := accessLine(t, read())
	if _, ok := m["user_id"]; ok {
		t.Errorf("未登录请求不应带 user_id：%v", m)
	}
	if id, _ := m["request_id"].(string); id == "" {
		t.Errorf("没传 X-Request-ID 时也应生成一个：%v", m)
	}
}
