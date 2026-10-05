package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	. "video-canvas/internal/middleware"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// adminTestRouter 组装：测试中间件模拟登录（userID 为 0 表示没经过 JWT）→ RequireAdmin → 一个返回 200 的接口。
func adminTestRouter(userID uint, lookup RoleLookup) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if userID != 0 {
			c.Set(CtxUserIDKey, userID)
		}
		c.Next()
	})
	r.GET("/admin", RequireAdmin(lookup), func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/super", RequireSuperAdmin(lookup), func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

func TestRequireAdmin(t *testing.T) {
	tests := []struct {
		name       string
		userID     uint
		lookup     RoleLookup
		wantStatus int
		wantCode   int
	}{
		{"管理员放行", 1, func(context.Context, uint64) (string, error) { return model.RoleAdmin, nil }, http.StatusOK, 0},
		{"超级管理员也放行（admin 的超集）", 1, func(context.Context, uint64) (string, error) { return model.RoleSuperAdmin, nil }, http.StatusOK, 0},
		{"普通用户 403", 1, func(context.Context, uint64) (string, error) { return model.RoleUser, nil }, http.StatusForbidden, errcode.ErrForbidden.Code},
		{"用户不存在（角色为空）403", 1, func(context.Context, uint64) (string, error) { return "", nil }, http.StatusForbidden, errcode.ErrForbidden.Code},
		{"没经过 JWT 按未登录 401", 0, func(context.Context, uint64) (string, error) { return model.RoleAdmin, nil }, http.StatusUnauthorized, errcode.ErrUnauthorized.Code},
		{"查询角色失败返回 500 而不是 403", 1, func(context.Context, uint64) (string, error) { return "", errors.New("db down") }, http.StatusInternalServerError, errcode.ErrInternal.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			adminTestRouter(tt.userID, tt.lookup).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin", nil))
			if w.Code != tt.wantStatus {
				t.Fatalf("HTTP 状态期望 %d，实际 %d：%s", tt.wantStatus, w.Code, w.Body.String())
			}
			if tt.wantCode != 0 {
				var body struct {
					Code int `json:"code"`
				}
				_ = json.Unmarshal(w.Body.Bytes(), &body)
				if body.Code != tt.wantCode {
					t.Fatalf("业务码期望 %d，实际 %d", tt.wantCode, body.Code)
				}
			}
		})
	}
}

func TestRequireSuperAdmin(t *testing.T) {
	tests := []struct {
		name       string
		userID     uint
		lookup     RoleLookup
		wantStatus int
		wantCode   int
	}{
		{"超级管理员放行", 1, func(context.Context, uint64) (string, error) { return model.RoleSuperAdmin, nil }, http.StatusOK, 0},
		{"admin 403", 1, func(context.Context, uint64) (string, error) { return model.RoleAdmin, nil }, http.StatusForbidden, errcode.ErrForbidden.Code},
		{"普通用户 403", 1, func(context.Context, uint64) (string, error) { return model.RoleUser, nil }, http.StatusForbidden, errcode.ErrForbidden.Code},
		{"用户不存在（角色为空）403", 1, func(context.Context, uint64) (string, error) { return "", nil }, http.StatusForbidden, errcode.ErrForbidden.Code},
		{"没经过 JWT 按未登录 401", 0, func(context.Context, uint64) (string, error) { return model.RoleSuperAdmin, nil }, http.StatusUnauthorized, errcode.ErrUnauthorized.Code},
		{"查询角色失败返回 500", 1, func(context.Context, uint64) (string, error) { return "", errors.New("db down") }, http.StatusInternalServerError, errcode.ErrInternal.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			adminTestRouter(tt.userID, tt.lookup).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/super", nil))
			if w.Code != tt.wantStatus {
				t.Fatalf("HTTP 状态期望 %d，实际 %d：%s", tt.wantStatus, w.Code, w.Body.String())
			}
			if tt.wantCode != 0 {
				var body struct {
					Code int `json:"code"`
				}
				_ = json.Unmarshal(w.Body.Bytes(), &body)
				if body.Code != tt.wantCode {
					t.Fatalf("业务码期望 %d，实际 %d", tt.wantCode, body.Code)
				}
			}
		})
	}
}

func TestRequireAdmin_LookupReceivesUserID(t *testing.T) {
	var got uint64
	lookup := func(_ context.Context, id uint64) (string, error) { got = id; return model.RoleAdmin, nil }
	w := httptest.NewRecorder()
	adminTestRouter(42, lookup).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if got != 42 {
		t.Fatalf("应按当前用户 ID 查角色，实际 %d", got)
	}
}
