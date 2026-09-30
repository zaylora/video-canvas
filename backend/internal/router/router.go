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
	Health        *handler.HealthHandler
	User          *handler.UserHandler
	CanvasProject *handler.CanvasProjectHandler

	// 长任务生成相关
	GenerationTask *handler.GenerationTaskHandler // 任务提交 / 对账 / 取消 / 积分 / webhook
	WS             *handler.WSHandler             // 用户级 WebSocket 推送
	Asset          *handler.AssetHandler          // 素材上传与查询
	LocalFiles     gin.HandlerFunc                // 本地存储的静态文件服务，仅 local 驱动时非 nil
	AIModel        *handler.AIModelHandler        // 面向画布的模型清单
	AdminAI        *handler.AdminAIHandler        // AI 配置管理
	AdminRole      middleware.RoleLookup          // 管理接口的角色查询
}

func New(mode, jwtSecret string, h Handlers) *gin.Engine {
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

		// 无需 JWT：身份由一次性 ticket 决定（浏览器 WebSocket 不能带请求头）
		v1.GET("/ws", h.WS.Connect)
		// 需要登录
		auth := v1.Group("", middleware.JWTAuth(jwtSecret))

		// 用户鉴权
		users := auth.Group("/users")
		users.GET("", h.User.List)
		users.GET("/:id", h.User.Get)

		// 画布操作
		canvas := auth.Group("/canvas")
		canvas.POST("", h.CanvasProject.Create)
		canvas.GET("", h.CanvasProject.List)
		canvas.GET("/:id", h.CanvasProject.Get)
		canvas.PUT("/:id", h.CanvasProject.Update)
		canvas.DELETE("/:id", h.CanvasProject.Delete)

		// 长任务生成
		auth.POST("/ws/ticket", h.WS.IssueTicket)
		tasks := auth.Group("/generation-tasks")
		tasks.POST("", h.GenerationTask.Create) // 202
		tasks.GET("", h.GenerationTask.List)    // ?ids=1,2,3 或 ?status=active
		tasks.GET("/:id", h.GenerationTask.Get)
		tasks.POST("/:id/cancel", h.GenerationTask.Cancel)
		auth.GET("/credits", h.GenerationTask.Credits)
		auth.GET("/models", h.AIModel.List)
		auth.POST("/assets", h.Asset.Upload)
		auth.GET("/assets/:id", h.Asset.Get)

		// 管理员后台接口
		adminAI := auth.Group("/admin/ai", middleware.RequireAdmin(h.AdminRole))
		models := adminAI.Group("/models")
		models.GET("", h.AdminAI.List)
		models.POST("", h.AdminAI.Create)
		models.GET("/:key", h.AdminAI.Get)
		models.PUT("/:key", h.AdminAI.Update)
		models.POST("/:key/validate", h.AdminAI.Validate)
		models.POST("/:key/publish", h.AdminAI.Publish)
		models.POST("/:key/rollback", h.AdminAI.Rollback)
		models.GET("/:key/revisions", h.AdminAI.Revisions)
		models.GET("/:key/revisions/:rid", h.AdminAI.Revision)
		models.POST("/:key/dry-run", h.AdminAI.DryRun)
		models.POST("/:key/test-run", h.AdminAI.TestRun)
		models.PUT("/:key/enabled", h.AdminAI.SetEnabled)
		models.PUT("/:key/sort", h.AdminAI.SetSort)

		adminAI.GET("/test-runs/:id", h.AdminAI.GetTestRun)
		adminAI.GET("/secrets", h.AdminAI.ListSecrets)
		adminAI.PUT("/secrets/:name", h.AdminAI.SetSecret)
		adminAI.GET("/schema/model", h.AdminAI.Schema)

		// TODO: middleware/auth.go 里的 JWT 鉴权中间件还是空的，下面这组接口目前未做鉴权
		// users := v1.Group("/users")
		// users.POST("", h.User.Create)
		// users.GET("/:id", h.User.Get)
		// users.PUT("/:id", h.User.Update)
		// users.DELETE("/:id", h.User.Delete)
	}

	// 本地存储的素材文件（仅开发环境）；key 不可猜测，不做鉴权，便于 <video>/<img> 直接引用
	if h.LocalFiles != nil {
		r.GET("/files/*filepath", h.LocalFiles)
		r.HEAD("/files/*filepath", h.LocalFiles)
	}

	r.NoRoute(func(c *gin.Context) {
		response.Fail(c, errcode.ErrNotFound)
	})
	return r
}
