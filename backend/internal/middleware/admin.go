package middleware

import (
	"context"
	"slices"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
)

// RoleLookup 按用户 ID 查询角色（user / admin / super_admin）。用户不存在时返回空字符串和 nil 错误，
// 只有数据库故障等意外情况才返回 error。接线时用 UserRepository.GetByID 适配即可。
type RoleLookup func(ctx context.Context, userID uint64) (string, error)

// RequireAdmin 要求当前用户是运营（admin）或运维（super_admin），必须挂在 JWTAuth 之后。
// super_admin 是 admin 的超集：能做的事只多不少，所以读接口与模型接口也要放行它。
// 每次都实时查角色，而不是把角色写进 JWT：这样降级 / 封禁管理员不必等 token 过期。
// 查询函数与 RequireActive 共用同一套（UserService.State：Redis 缓存 + 变更时主动失效），没有额外的内存缓存，改角色立即生效。
func RequireAdmin(lookup RoleLookup) gin.HandlerFunc {
	return requireRoles(lookup, model.RoleAdmin, model.RoleSuperAdmin)
}

// RequireSuperAdmin 要求当前用户是运维（super_admin），必须挂在 JWTAuth 之后。
// 用在装插件、管渠道与凭证这类写接口上：admin 调用一律 403（协议插件设计 6.8）。
func RequireSuperAdmin(lookup RoleLookup) gin.HandlerFunc {
	return requireRoles(lookup, model.RoleSuperAdmin)
}

// requireRoles 是 RequireAdmin / RequireSuperAdmin 共用的实现：查出当前用户角色，不在 allowed 里就 403。
func requireRoles(lookup RoleLookup, allowed ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 没有经过 JWTAuth（user_id 为 0）按未登录处理，避免配置错误时误放行
		userID := uint64(GetUserID(c))
		if userID == 0 {
			response.Fail(c, errcode.ErrUnauthorized)
			return
		}
		// 2. 查角色：查询失败是服务端问题，返回 500，而不是误报“没有权限”
		role, err := lookup(c.Request.Context(), userID)
		if err != nil {
			response.Fail(c, err)
			return
		}
		// 3. 角色不在允许列表里一律 403；用户不存在时角色为空，同样落在这里
		if !slices.Contains(allowed, role) {
			response.Fail(c, errcode.ErrForbidden)
			return
		}
		c.Next()
	}
}
