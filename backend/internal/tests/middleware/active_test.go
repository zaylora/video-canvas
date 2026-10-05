package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/utils"
)

const activeTestSecret = "active-test-secret"

// activeRouter 组装真实的 JWTAuth → RequireActive → 一个返回 user_id 的接口。
func activeRouter(lookup UserStateLookup) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("", JWTAuth(activeTestSecret), RequireActive(lookup))
	g.GET("/me", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

func bearer(t *testing.T, id uint, tokenVersion int) string {
	t.Helper()
	tok, _, err := utils.GenerateToken(id, "u", tokenVersion, activeTestSecret, "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + tok
}

func codeFrom(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	var body struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Code
}

func TestRequireActive(t *testing.T) {
	active := func(version int) UserStateLookup {
		return func(context.Context, uint64) (*UserState, error) {
			return &UserState{Role: model.RoleUser, Status: model.UserStatusActive, TokenVersion: version}, nil
		}
	}
	tests := []struct {
		name       string
		lookup     UserStateLookup
		tokenVer   int
		wantStatus int
		wantCode   int
	}{
		{"正常用户放行", active(0), 0, http.StatusOK, 0},
		{"token_version 一致且非 0 放行", active(4), 4, http.StatusOK, 0},
		{"账号已停用：403 + 53004", func(context.Context, uint64) (*UserState, error) {
			return &UserState{Role: model.RoleUser, Status: model.UserStatusDisabled}, nil
		}, 0, http.StatusForbidden, errcode.ErrAccountDisabled.Code},
		{"token_version 比库里旧（密码被重置）：401", active(5), 4, http.StatusUnauthorized, errcode.ErrUnauthorized.Code},
		{"token_version 比库里新：401", active(1), 2, http.StatusUnauthorized, errcode.ErrUnauthorized.Code},
		{"用户已不存在：401", func(context.Context, uint64) (*UserState, error) { return nil, nil }, 0, http.StatusUnauthorized, errcode.ErrUnauthorized.Code},
		{"查询状态失败：500 而不是误放行", func(context.Context, uint64) (*UserState, error) { return nil, errors.New("db down") }, 0, http.StatusInternalServerError, errcode.ErrInternal.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/me", nil)
			req.Header.Set("Authorization", bearer(t, 7, tt.tokenVer))
			activeRouter(tt.lookup).ServeHTTP(w, req)
			if w.Code != tt.wantStatus || (tt.wantCode != 0 && codeFrom(t, w) != tt.wantCode) {
				t.Fatalf("期望 HTTP %d / code %d，实际 %d：%s", tt.wantStatus, tt.wantCode, w.Code, w.Body.String())
			}
		})
	}

	t.Run("lookup 收到 token 里的用户 ID", func(t *testing.T) {
		var got uint64
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/me", nil)
		req.Header.Set("Authorization", bearer(t, 42, 0))
		activeRouter(func(_ context.Context, id uint64) (*UserState, error) {
			got = id
			return &UserState{Status: model.UserStatusActive}, nil
		}).ServeHTTP(w, req)
		if got != 42 {
			t.Fatalf("用户 ID 期望 42，实际 %d", got)
		}
	})

	t.Run("没经过 JWTAuth（没有用户 ID）按未登录 401", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.GET("/me", RequireActive(active(0)), func(c *gin.Context) { c.String(http.StatusOK, "ok") })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("应 401：%d", w.Code)
		}
	})
}
