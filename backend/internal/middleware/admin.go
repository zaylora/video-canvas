package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
)

// RoleLookup 按用户 ID 查询角色（user / admin）。用户不存在时返回空字符串和 nil 错误，
// 只有数据库故障等意外情况才返回 error。接线时用 UserRepository.GetByID 适配即可。
type RoleLookup func(ctx context.Context, userID uint64) (string, error)

// RequireAdmin 要求当前用户是管理员，必须挂在 JWTAuth 之后。
// 每次都实时（或带短缓存）查角色，而不是把角色写进 JWT：这样降级 / 封禁管理员不必等 token 过期。
func RequireAdmin(lookup RoleLookup) gin.HandlerFunc {
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
		// 3. 非管理员一律 403；用户不存在时角色为空，同样落在这里
		if role != model.RoleAdmin {
			response.Fail(c, errcode.ErrForbidden)
			return
		}
		c.Next()
	}
}

// NewCachedRoleLookup 给 RoleLookup 加一层内存缓存（ttl 内不重复查库），管理接口访问频率低但每次都查库也没必要。
// 出错的结果不缓存；缓存意味着角色变更最多延迟 ttl 生效。
func NewCachedRoleLookup(get RoleLookup, ttl time.Duration) RoleLookup {
	type item struct {
		role    string
		expires time.Time
	}
	var (
		mu    sync.Mutex
		cache = map[uint64]item{}
	)
	return func(ctx context.Context, userID uint64) (string, error) {
		now := time.Now()
		mu.Lock()
		if it, ok := cache[userID]; ok && now.Before(it.expires) {
			mu.Unlock()
			return it.role, nil
		}
		mu.Unlock()

		role, err := get(ctx, userID)
		if err != nil {
			return "", err
		}
		mu.Lock()
		cache[userID] = item{role: role, expires: now.Add(ttl)}
		mu.Unlock()
		return role, nil
	}
}
