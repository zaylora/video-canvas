package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"video-canvas/internal/pkg/logger"
)

// Logger 在每个请求结束时打一行访问汇总（event=http.request）：谁、哪个接口、状态、耗时，以及请求里创建或开始的运行。
// 5xx 用 Error 级别，4xx 用 Warn 级别。请求编号和用户来自请求 context（RequestID、JWTAuth 挂上的）。
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		ctx := c.Request.Context()
		fields := []zap.Field{
			logger.Event(logger.EventHTTPRequest),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("query", c.Request.URL.RawQuery),
			zap.Int("status", status),
			zap.Int64("latency_ms", time.Since(start).Milliseconds()),
			zap.String("ip", c.ClientIP()),
		}
		if uid := GetUserID(c); uid != 0 {
			fields = append(fields, zap.Uint64("user_id", uint64(uid)))
		}
		if kind, id := logger.RequestRun(ctx); kind != "" {
			fields = append(fields, logger.RunField(kind, id))
		}
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("errors", c.Errors.String()))
		}

		// 关联字段只取请求编号：用户和运行在上面按请求结束时的状态取（鉴权在本中间件之后才执行）
		log := logger.L()
		if rid := logger.RequestIDOf(ctx); rid != "" {
			log = log.With(zap.String("request_id", rid))
		}
		switch {
		case status >= 500:
			log.Error("http request", fields...)
		case status >= 400:
			log.Warn("http request", fields...)
		default:
			log.Info("http request", fields...)
		}
	}
}
