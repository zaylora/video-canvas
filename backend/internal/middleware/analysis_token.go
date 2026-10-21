package middleware

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
)

// CtxAnalysisTokenIDKey 是 gin.Context 里存放分析令牌 ID 的键：用管理员 JWT 访问时没有这个键。
const CtxAnalysisTokenIDKey = "analysis_token_id"

// TokenAuthenticator 校验分析令牌明文，返回令牌 ID 和签发人；失败时返回的 error 直接交给 response.Fail。
type TokenAuthenticator func(ctx context.Context, plain string) (tokenID, userID uint64, err error)

// CtxAgentRunIDKey 是 gin.Context 里存放「这个桥请求属于哪次 Agent 运行」的键：桥 handler 从令牌解析出来，访问日志据此带上 run_id。
const CtxAgentRunIDKey = "agent_run_id"

// GetAnalysisTokenID 返回当前请求用的分析令牌 ID，不是用令牌访问的为 0。
func GetAnalysisTokenID(c *gin.Context) uint64 {
	return c.GetUint64(CtxAnalysisTokenIDKey)
}

// AdminOrAnalysisToken 让同一组只读接口既能被登录的管理员访问，也能被本地 CLI 用分析令牌访问：
// Authorization: Bearer 带 vca_ 前缀的是分析令牌，由 auth 校验（有效期、注销、签发人仍是管理员、限流），通过后把签发人写进上下文；
// 其余一律按管理员 JWT 处理，依次执行 jwtChain（JWTAuth、RequireActive、RequireAdmin）。
// 令牌只能走这一组只读接口：它没有挂到任何写接口上，令牌管理接口也只接受 JWT。
func AdminOrAnalysisToken(auth TokenAuthenticator, jwtChain ...gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if plain, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer "); ok && strings.HasPrefix(strings.TrimSpace(plain), model.TokenPrefix) {
			tokenID, userID, err := auth(c.Request.Context(), strings.TrimSpace(plain))
			if err != nil {
				response.Fail(c, err)
				return
			}
			c.Set(CtxUserIDKey, uint(userID))
			c.Set(CtxAnalysisTokenIDKey, tokenID)
			c.Next()
			return
		}
		for _, h := range jwtChain {
			h(c)
			if c.IsAborted() {
				return
			}
		}
		c.Next()
	}
}
