package middleware

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/pkg/utils"
)

// RequestID 沿用上游传入的 X-Request-ID，没有则生成一个，并回写到响应头。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(utils.RequestIDHeader)
		if id == "" {
			id = utils.NewRequestID()
		}
		c.Set(utils.RequestIDKey, id)
		c.Header(utils.RequestIDHeader, id)
		c.Next()
	}
}
