package middleware

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/pkg/utils"
)

// Recovery 捕获 panic，记录堆栈并返回统一的 500 响应，避免进程崩溃。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.L().Error("panic recovered",
					zap.Any("panic", r),
					zap.String("request_id", utils.GetRequestID(c)),
					zap.String("path", c.Request.URL.Path),
					zap.Stack("stack"),
				)
				response.Fail(c, errcode.ErrInternal)
			}
		}()
		c.Next()
	}
}
