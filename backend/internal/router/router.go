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
	Files          gin.HandlerFunc                // 素材稳定地址 /files/*：本地存储直出，对象存储签名后 302
	AIModel        *handler.AIModelHandler        // 面向画布的模型清单
	AdminAI        *handler.AdminAIHandler        // AI 模型配置管理
	AdminPlugin    *handler.AdminPluginHandler    // 协议插件管理
	AdminChannel   *handler.AdminChannelHandler   // 渠道管理
	AdminStorage   *handler.AdminStorageHandler   // 存储配置管理
	AdminMe        *handler.AdminMeHandler        // 当前管理员身份
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
		auth.POST("/assets/upload-intents", h.Asset.CreateUploadIntent)          // 申请上传：直传凭证，或告知走后端中转
		auth.POST("/assets/upload-intents/:id/complete", h.Asset.CompleteUpload) // 直传完成后登记素材
		auth.GET("/assets/:id", h.Asset.Get)

		// 管理员后台接口：读与模型相关的写 = admin 或 super_admin；插件与渠道的写 = 仅 super_admin。
		// 凭证不再有独立接口，渠道 Key 统一走 PUT /channels/:key/secret。
		adminAI := auth.Group("/admin/ai", middleware.RequireAdmin(h.AdminRole))
		superOnly := middleware.RequireSuperAdmin(h.AdminRole)
		adminAI.GET("/me", h.AdminMe.Me)

		plugins := adminAI.Group("/plugins")
		plugins.GET("", h.AdminPlugin.List)
		plugins.POST("", superOnly, h.AdminPlugin.Upload)
		plugins.PUT("/:key/enabled", superOnly, h.AdminPlugin.SetEnabled)
		plugins.DELETE("/:key/versions/:version", superOnly, h.AdminPlugin.DeleteVersion)
		plugins.GET("/:key/delete-check", superOnly, h.AdminPlugin.DeleteCheck)
		plugins.DELETE("/:key", superOnly, h.AdminPlugin.Delete)

		channels := adminAI.Group("/channels")
		channels.GET("", h.AdminChannel.List)
		channels.GET("/loads", h.AdminChannel.Loads)
		channels.POST("", superOnly, h.AdminChannel.Create)
		channels.GET("/:key", h.AdminChannel.Get)
		channels.PUT("/:key", superOnly, h.AdminChannel.Update)
		channels.PUT("/:key/secret", superOnly, h.AdminChannel.SetSecret)
		channels.POST("/:key/check", superOnly, h.AdminChannel.Check)
		channels.POST("/:key/import", h.AdminChannel.Import)
		channels.GET("/:key/delete-check", superOnly, h.AdminChannel.DeleteCheck)
		channels.DELETE("/:key", superOnly, h.AdminChannel.Delete)

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
		models.GET("/:key/delete-check", h.AdminAI.DeleteCheck)
		models.DELETE("/:key", h.AdminAI.Delete)

		adminAI.GET("/test-runs/:id", h.AdminAI.GetTestRun)
		adminAI.GET("/test-runs/:id/trace", h.AdminAI.GetTestTrace)
		adminAI.GET("/schema/model", h.AdminAI.Schema)

		// 存储配置（素材存到哪）：读 = admin 或 super_admin；测试 / 创建 / 修改 / 换密钥 / 设默认 / 删除 = 仅 super_admin。
		// 密钥只写不读，和渠道 Key 同一套规则。
		storages := auth.Group("/admin/storages", middleware.RequireAdmin(h.AdminRole))
		storages.GET("", h.AdminStorage.List)
		storages.GET("/presets", h.AdminStorage.Presets)
		storages.POST("/test", superOnly, h.AdminStorage.Test)
		storages.POST("", superOnly, h.AdminStorage.Create)
		storages.PUT("/default", superOnly, h.AdminStorage.SetDefault)
		storages.GET("/:id", h.AdminStorage.Get)
		storages.PUT("/:id", superOnly, h.AdminStorage.Update)
		storages.PUT("/:id/secret", superOnly, h.AdminStorage.ReplaceSecret)
		storages.POST("/:id/check", superOnly, h.AdminStorage.Check)
		storages.GET("/:id/delete-check", superOnly, h.AdminStorage.DeleteCheck)
		storages.DELETE("/:id", superOnly, h.AdminStorage.Delete)

		// TODO: middleware/auth.go 里的 JWT 鉴权中间件还是空的，下面这组接口目前未做鉴权
		// users := v1.Group("/users")
		// users.POST("", h.User.Create)
		// users.GET("/:id", h.User.Get)
		// users.PUT("/:id", h.User.Update)
		// users.DELETE("/:id", h.User.Delete)
	}

	// 素材的稳定地址：画布 payload 里存的就是它，永不过期、换存储也不变。
	// key 不可猜测，不做鉴权，便于 <video>/<img> 直接引用
	if h.Files != nil {
		r.GET("/files/*filepath", h.Files)
		r.HEAD("/files/*filepath", h.Files)
	}

	r.NoRoute(func(c *gin.Context) {
		response.Fail(c, errcode.ErrNotFound)
	})
	return r
}
