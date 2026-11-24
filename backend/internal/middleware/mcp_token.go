package middleware

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/response"
)

// gin.Context 里存放 MCP 令牌身份的键。一个请求只可能是其中一种身份，不会和登录 JWT 混用。
const (
	CtxMCPProjectIDKey = "mcp_project_id"
	// CtxMCPDefaultCanvasKey 是项目上配的默认画布，0 表示没配。
	// 它只是「调用没指定 canvas 时用哪块」的兜底，不是权限边界。
	CtxMCPDefaultCanvasKey = "mcp_default_canvas_id"
	CtxMCPScopesKey        = "mcp_scopes"
)

// MCPIdentity 是 MCP 令牌认证通过后的身份：项目、归属用户、默认画布、权限档位集合。
// 能访问的画布范围由归属用户决定，不由某个字段限定。
type MCPIdentity struct {
	ProjectID       uint64
	UserID          uint64
	DefaultCanvasID uint64
	Scopes          map[string]bool
}

// MCPAuthenticator 校验 MCP 令牌明文；失败时返回的 error 直接交给 response.Fail。
type MCPAuthenticator func(ctx context.Context, plain string) (MCPIdentity, error)

// RequireMCPToken 只接受 Authorization: Bearer vcm_… 的 MCP 令牌。
// 不带令牌、前缀不对、令牌无效都一律 401：这条路不认登录 JWT，也不认分析令牌，避免三种身份串味。
func RequireMCPToken(auth MCPAuthenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		plain, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || !strings.HasPrefix(strings.TrimSpace(plain), model.MCPTokenPrefix) {
			response.Fail(c, errcode.ErrMCPTokenInvalid)
			c.Abort()
			return
		}
		id, err := auth(c.Request.Context(), strings.TrimSpace(plain))
		if err != nil {
			response.Fail(c, err)
			c.Abort()
			return
		}
		c.Set(CtxMCPProjectIDKey, id.ProjectID)
		c.Set(CtxMCPDefaultCanvasKey, id.DefaultCanvasID)
		c.Set(CtxMCPScopesKey, id.Scopes)
		// 业务层的画布查询与素材查询都按用户过滤，这里必须把归属用户写进上下文。
		c.Set(CtxUserIDKey, uint(id.UserID))
		c.Request = c.Request.WithContext(logger.WithUser(c.Request.Context(), id.UserID))
		c.Next()
	}
}

// GetMCPIdentity 取当前请求的 MCP 身份；没走 MCP 中间件时返回 nil。
func GetMCPIdentity(c *gin.Context) *MCPIdentity {
	scopes, ok := c.Get(CtxMCPScopesKey)
	if !ok {
		return nil
	}
	set, _ := scopes.(map[string]bool)
	return &MCPIdentity{
		ProjectID:       c.GetUint64(CtxMCPProjectIDKey),
		UserID:          uint64(c.GetUint(CtxUserIDKey)),
		DefaultCanvasID: c.GetUint64(CtxMCPDefaultCanvasKey),
		Scopes:          set,
	}
}
