package router

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
)

// Handlers 汇总所有 handler，新增模块时在这里加字段并注册路由。
type Handlers struct {
	Health *handler.HealthHandler
	User   *handler.UserHandler
}

func New(mode string, h Handlers) *gin.Engine {
	gin.SetMode(mode)

	r := gin.New()
	r.Use(
		middleware.RequestID(),
		middleware.Logger(),
		middleware.Recovery(),
		middleware.CORS(),
	)

	r.GET("/health", h.Health.Check)

	v1 := r.Group("/api/v1")
	{
		// 无需鉴权
		v1.POST("/auth/register", h.User.Register)
		v1.POST("/auth/login", h.User.Login)

		// TODO: middleware/auth.go 里的 JWT 鉴权中间件还是空的，下面这组接口目前未做鉴权
		// users := v1.Group("/users")
		// users.POST("", h.User.Create)
		// users.GET("/:id", h.User.Get)
		// users.PUT("/:id", h.User.Update)
		// users.DELETE("/:id", h.User.Delete)
	}

	r.NoRoute(func(c *gin.Context) {
		response.Fail(c, errcode.ErrNotFound)
	})
	return r
}
