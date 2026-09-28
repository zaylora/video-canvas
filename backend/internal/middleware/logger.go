package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/utils"
)

// Logger 记录每个请求的访问日志，5xx 用 Error 级别，4xx 用 Warn 级别。
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		fields := []zap.Field{
			zap.String("request_id", utils.GetRequestID(c)),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("query", c.Request.URL.RawQuery),
			zap.Int("status", status),
			zap.Duration("latency", time.Since(start)),
			zap.String("ip", c.ClientIP()),
		}
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("errors", c.Errors.String()))
		}

		switch {
		case status >= 500:
			logger.L().Error("http request", fields...)
		case status >= 400:
			logger.L().Warn("http request", fields...)
		default:
			logger.L().Info("http request", fields...)
		}
	}
}
