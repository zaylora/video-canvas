package middleware

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/pkg/utils"
)

// gin.Context 里存放登录身份的键，由 JWTAuth 写入。
const (
	CtxUserIDKey       = "user_id"
	CtxUsernameKey     = "username"
	CtxTokenVersionKey = "token_version"
)

// JWTAuth 校验 Authorization: Bearer <token>，
// 通过后把 user_id / username 注入 gin.Context，后续 Handler 通过 GetUserID / GetUsername 获取。
func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 提取 Token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.Fail(c, errcode.ErrUnauthorized)
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
			response.Fail(c, errcode.ErrUnauthorized.WithMsg("Token 格式错误，应为 Bearer <token>"))
			return
		}

		// 2. 解析校验（response.Fail 内部已 Abort）
		claims, err := utils.ParseToken(strings.TrimSpace(parts[1]), secret)
		if err != nil {
			if errors.Is(err, jwt.ErrTokenExpired) {
				response.Fail(c, errcode.ErrTokenExpired)
			} else {
				response.Fail(c, errcode.ErrUnauthorized)
			}
			return
		}

		// 3. 注入用户身份，供后续 Handler / Service 使用
		c.Set(CtxUserIDKey, claims.UserID)
		c.Set(CtxUsernameKey, claims.Username)
		c.Set(CtxTokenVersionKey, claims.TokenVersion)
		c.Next()
	}
}

// GetUserID 返回当前登录用户 ID，未经过 JWTAuth 时为 0。
func GetUserID(c *gin.Context) uint {
	return c.GetUint(CtxUserIDKey)
}

// GetTokenVersion 返回 token 里的 token_version，未经过 JWTAuth 时为 0。
func GetTokenVersion(c *gin.Context) int {
	return c.GetInt(CtxTokenVersionKey)
}

func GetUsername(c *gin.Context) string {
	return c.GetString(CtxUsernameKey)
}
