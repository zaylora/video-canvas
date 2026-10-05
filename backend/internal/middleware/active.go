package middleware

import (
	"context"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
)

// UserState 是鉴权需要的用户当前状态。
type UserState struct {
	Role         string // user / admin / super_admin
	Status       string // active / disabled
	TokenVersion int    // 用户当前的 token_version
}

// UserStateLookup 按用户 ID 查询当前状态。用户不存在返回 (nil, nil)，只有数据库故障等意外情况才返回 error。
// 接线时用 UserService.State 适配（Redis 缓存 + 变更时主动失效）。
type UserStateLookup func(ctx context.Context, userID uint64) (*UserState, error)

// RequireActive 要求当前用户存在、未被停用、且 token 的 token_version 与用户当前值一致，必须挂在 JWTAuth 之后。
// 这样封禁、重置密码立即让已签发的 token 失效，不必等 token 自然过期。
// 停用返回 403（53004，前端据此清 token 并跳登录页）；版本不符 / 用户已不存在返回 401。
func RequireActive(lookup UserStateLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 没有经过 JWTAuth（user_id 为 0）按未登录处理，避免配置错误时误放行
		userID := uint64(GetUserID(c))
		if userID == 0 {
			response.Fail(c, errcode.ErrUnauthorized)
			return
		}
		// 2. 查状态：失败是服务端问题，返回 500，而不是误放行或误报“未登录”
		st, err := lookup(c.Request.Context(), userID)
		if err != nil {
			response.Fail(c, err)
			return
		}
		// 3. 用户已被删除：token 仍然有效但对应的人不在了，按未登录处理
		if st == nil {
			response.Fail(c, errcode.ErrUnauthorized)
			return
		}
		// 4. 先判停用再判版本：被停用的账号即使 token_version 也对不上，也应让前端看到“账号已停用”
		if st.Status == model.UserStatusDisabled {
			response.Fail(c, errcode.ErrAccountDisabled)
			return
		}
		if GetTokenVersion(c) != st.TokenVersion {
			response.Fail(c, errcode.ErrUnauthorized)
			return
		}
		c.Next()
	}
}
