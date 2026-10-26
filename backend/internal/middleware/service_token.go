package middleware

import (
	"crypto/subtle"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
)

// ServiceTokenHeader 是服务间调用携带服务令牌的请求头。
const ServiceTokenHeader = "X-Agent-Service-Token"

// RequireServiceToken 要求请求带上正确的服务令牌：Agent 作为独立服务从内网访问桥时，
// 在一次性运行令牌之外再多一道校验，证明调用方是 Agent 服务本身。
// 按常量时间比较，不论错在哪里都只回同一个 401，也不回显配置的令牌。
// token 为空说明装配出了错，此时拒绝所有请求，不能变成不设防。
func RequireServiceToken(token string) gin.HandlerFunc {
	want := []byte(token)
	return func(c *gin.Context) {
		if len(want) == 0 || subtle.ConstantTimeCompare([]byte(c.GetHeader(ServiceTokenHeader)), want) != 1 {
			response.Fail(c, errcode.ErrUnauthorized.WithMsg("服务令牌无效"))
			return
		}
		c.Next()
	}
}
