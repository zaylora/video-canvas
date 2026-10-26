package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/middleware"
	"video-canvas/internal/pkg/errcode"
)

func serviceTokenRouter(token string) (*gin.Engine, *int) {
	gin.SetMode(gin.TestMode)
	hits := 0
	r := gin.New()
	r.Use(RequireServiceToken(token))
	r.POST("/x", func(c *gin.Context) { hits++; c.String(http.StatusOK, "ok") })
	return r, &hits
}

func callWithServiceToken(r *gin.Engine, header string, set bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	if set {
		req.Header.Set(ServiceTokenHeader, header)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRequireServiceToken(t *testing.T) {
	token := strings.Repeat("s", 40)
	cases := []struct {
		name   string
		header string
		set    bool
		pass   bool
	}{
		{"令牌正确", token, true, true},
		{"没带令牌", "", false, false},
		{"带了空令牌", "", true, false},
		{"令牌不对", strings.Repeat("x", 40), true, false},
		{"长度不同", token[:39], true, false},
		{"只是前缀相同", token + "x", true, false},
		{"首尾多了空格", " " + token, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, hits := serviceTokenRouter(token)
			w := callWithServiceToken(r, tc.header, tc.set)
			if tc.pass {
				if w.Code != http.StatusOK || *hits != 1 {
					t.Fatalf("应放行: code=%d hits=%d", w.Code, *hits)
				}
				return
			}
			if w.Code != http.StatusUnauthorized || codeFrom(t, w) != errcode.ErrUnauthorized.Code || *hits != 0 {
				t.Errorf("应拒绝且不进入业务: code=%d body=%s hits=%d", w.Code, w.Body.String(), *hits)
			}
			if strings.Contains(w.Body.String(), token) {
				t.Error("响应里不能带出配置的令牌")
			}
		})
	}
}

// 没配置令牌却挂了这个中间件，说明装配出了错：宁可全部拒绝，也不能变成不设防。
func TestRequireServiceToken_EmptyConfiguredTokenRejectsEverything(t *testing.T) {
	r, hits := serviceTokenRouter("")
	for _, set := range []bool{false, true} {
		if w := callWithServiceToken(r, "", set); w.Code != http.StatusUnauthorized {
			t.Errorf("set=%v: code=%d，应拒绝", set, w.Code)
		}
	}
	if *hits != 0 {
		t.Error("不能进入业务")
	}
}
