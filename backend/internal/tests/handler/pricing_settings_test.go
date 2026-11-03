package handler_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/service"
	"video-canvas/internal/service/pricingfake"
)

func TestPricingSettingsHandler(t *testing.T) {
	for _, tc := range []struct {
		name, method, role, body string
		want                     int
		repoErr                  bool
	}{
		{"管理员读取", "GET", "admin", "", 200, false},
		{"超级管理员读取", "GET", "super_admin", "", 200, false},
		{"普通用户被拒绝", "GET", "user", "", 403, false},
		{"管理员修改成功", "PUT", "admin", `{"credits_per_yuan":7.5,"default_markup_pct":30,"usd_cny_override":null}`, 200, false},
		{"普通用户修改被拒绝", "PUT", "user", `{"credits_per_yuan":7.5,"default_markup_pct":30,"usd_cny_override":null}`, 403, false},
		{"参数非法", "PUT", "admin", `{"credits_per_yuan":0,"default_markup_pct":30,"usd_cny_override":null}`, 400, false},
		{"小数位越界", "PUT", "admin", `{"credits_per_yuan":1.001,"default_markup_pct":30,"usd_cny_override":null}`, 400, false},
		{"JSON 错误", "PUT", "admin", `{`, 400, false},
		{"缺少默认加价率", "PUT", "admin", `{"credits_per_yuan":7.5,"usd_cny_override":null}`, 400, false},
		{"缺少汇率字段", "PUT", "admin", `{"credits_per_yuan":7.5,"default_markup_pct":30}`, 400, false},
		{"仓储失败", "GET", "admin", "", 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			f := pricingfake.New()
			if tc.repoErr {
				f.ReadErr = errors.New("db down")
			}
			h := NewPricingSettingsHandler(service.NewPricingSettingsService(f, nil))
			r := gin.New()
			g := r.Group("/admin/settings", func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(7)); c.Next() }, middleware.RequireAdmin(func(context.Context, uint64) (string, error) { return tc.role, nil }))
			g.GET("/pricing", h.Get)
			g.PUT("/pricing", h.Update)
			req := httptest.NewRequest(tc.method, "/admin/settings/pricing", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if tc.want == 400 && !strings.Contains(w.Body.String(), `"code":10001`) {
				t.Fatal(w.Body.String())
			}
		})
	}
}
