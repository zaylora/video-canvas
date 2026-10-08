package router

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	agenthandler "video-canvas/internal/handler/agent"
	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
)

// Handlers 汇总所有 handler，新增模块时在这里加字段并注册路由。
type Handlers struct {
	Health        *handler.HealthHandler
	User          *handler.UserHandler
	CanvasProject *handler.CanvasProjectHandler
	Agent         *agenthandler.AgentHandler // 画布 Agent：会话、运行、审批
	AgentSkill    *agenthandler.SkillHandler // Agent 技能：用户端目录 + 管理端导入 / 版本 / 启停

	// 长任务生成相关
	GenerationTask *handler.GenerationTaskHandler      // 任务提交 / 对账 / 取消 / 积分 / webhook
	WS             *handler.WSHandler                  // 用户级 WebSocket 推送
	Asset          *handler.AssetHandler               // 素材上传与查询
	Files          gin.HandlerFunc                     // 素材稳定地址 /files/*：本地存储直出，对象存储签名后 302
	AIModel        *handler.AIModelHandler             // 面向画布的模型清单
	AdminAI        *handler.AdminAIHandler             // AI 模型配置管理
	AdminPlugin    *handler.AdminPluginHandler         // 协议插件管理
	AdminChannel   *handler.AdminChannelHandler        // 渠道管理
	AdminStorage   *handler.AdminStorageHandler        // 存储配置管理
	AdminImageProc *handler.AdminImageProcessorHandler // 图片处理服务管理
	AdminMe        *handler.AdminMeHandler             // 当前管理员身份
	AdminUser      *handler.AdminUserHandler           // 后台用户管理
	AdminSettings  *handler.AdminSettingsHandler       // 注册与邮件设置
	Showcase       *handler.ShowcaseHandler            // 登录页展示：公开读取 + 后台配置
	AdminRole      middleware.RoleLookup               // 管理接口的角色查询（与 UserState 同一套：Redis 缓存 + 变更时主动失效）
	UserState      middleware.UserStateLookup          // 登录后分组的用户状态查询（停用 / token_version）
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
		v1.GET("/auth/config", h.User.Config)
		v1.POST("/auth/register/code", h.User.SendRegisterCode)
		v1.POST("/auth/register", h.User.Register)
		v1.POST("/auth/login", h.User.Login)
		v1.GET("/showcase", h.Showcase.Public) // 登录页背景轮播：未登录即可读取，只暴露播放所需字段

		// 无需 JWT：身份由一次性 ticket 决定（浏览器 WebSocket 不能带请求头）
		v1.GET("/ws", h.WS.Connect)
		// 需要登录：JWT 校验之后再查用户状态（停用 → 403，token_version 不符 → 401），封禁与重置密码立即生效
		auth := v1.Group("", middleware.JWTAuth(jwtSecret), middleware.RequireActive(h.UserState))

		// 画布操作
		canvas := auth.Group("/canvas")
		canvas.POST("", h.CanvasProject.Create)
		canvas.GET("", h.CanvasProject.List)
		canvas.GET("/:id", h.CanvasProject.Get)
		canvas.PUT("/:id", h.CanvasProject.Update)
		canvas.DELETE("/:id", h.CanvasProject.Delete)
		canvas.GET("/:id/agent/sessions", h.Agent.ListSessions)
		canvas.POST("/:id/agent/sessions", h.Agent.CreateSession)

		// 画布 Agent：id 与画布一样是十六进制串
		agent := auth.Group("/agent")
		agent.GET("/models", h.Agent.Models)
		agent.GET("/skills", h.AgentSkill.UserList) // 已启用技能的目录（@ 弹层）
		agent.PATCH("/sessions/:sid", h.Agent.RenameSession)
		agent.DELETE("/sessions/:sid", h.Agent.DeleteSession)
		agent.GET("/sessions/:sid/events", h.Agent.Events)  // ?after=序号&limit=
		agent.POST("/sessions/:sid/runs", h.Agent.StartRun) // 202
		agent.POST("/runs/:rid/interject", h.Agent.Interject)
		agent.POST("/runs/:rid/cancel", h.Agent.Cancel)
		agent.POST("/runs/:rid/resume", h.Agent.Resume)
		agent.POST("/runs/:rid/undo", h.Agent.Undo)
		agent.POST("/approvals/:aid/decision", h.Agent.Decide)

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

		// Agent 技能管理：admin 与 super_admin 都能导入、启停、删除（产品决定，不收紧）；全部写操作进审计。
		// imports 是静态段，与 /:name 同层；Gin 优先匹配静态段。
		skillsAdmin := auth.Group("/admin/agent/skills", middleware.RequireAdmin(h.AdminRole))
		skillsAdmin.GET("", h.AgentSkill.List)
		skillsAdmin.POST("/imports", h.AgentSkill.Import)
		skillsAdmin.GET("/imports/:id/files", h.AgentSkill.ImportFile)
		skillsAdmin.DELETE("/imports/:id", h.AgentSkill.DiscardImport)
		skillsAdmin.POST("/imports/:id/confirm", h.AgentSkill.ConfirmImport)
		skillsAdmin.GET("/:name", h.AgentSkill.Get)
		skillsAdmin.PUT("/:name", h.AgentSkill.Rename)
		skillsAdmin.PUT("/:name/enabled", h.AgentSkill.SetEnabled)
		skillsAdmin.PUT("/:name/active-version", h.AgentSkill.SetActiveVersion)
		skillsAdmin.GET("/:name/delete-check", h.AgentSkill.DeleteCheck)
		skillsAdmin.GET("/:name/versions/:v", h.AgentSkill.Version)
		skillsAdmin.GET("/:name/versions/:v/files", h.AgentSkill.File)
		skillsAdmin.GET("/:name/versions/:v/download", h.AgentSkill.Download)
		skillsAdmin.DELETE("/:name/versions/:v", h.AgentSkill.DeleteVersion)
		skillsAdmin.DELETE("/:name", h.AgentSkill.Delete)

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

		// 图片处理服务（给素材所在存储配置缩略图 / 视频封面的处理服务）：读 = admin 或 super_admin；写 = 仅 super_admin。
		processors := auth.Group("/admin/image-processors", middleware.RequireAdmin(h.AdminRole))
		processors.GET("", h.AdminImageProc.List)
		processors.GET("/presets", h.AdminImageProc.Presets)
		processors.POST("", superOnly, h.AdminImageProc.Create)
		processors.GET("/:id", h.AdminImageProc.Get)
		processors.PUT("/:id", superOnly, h.AdminImageProc.Update)
		processors.POST("/:id/check", superOnly, h.AdminImageProc.Check)
		processors.POST("/:id/publish", superOnly, h.AdminImageProc.Publish)
		processors.POST("/:id/rollback", superOnly, h.AdminImageProc.Rollback)
		processors.POST("/:id/disable", superOnly, h.AdminImageProc.Disable)
		processors.DELETE("/:id", superOnly, h.AdminImageProc.Delete)

		// 用户管理：读 = admin 或 super_admin。原先任何登录用户都能调的 GET /users、GET /users/:id 已删除。
		adminUsers := auth.Group("/admin/users", middleware.RequireAdmin(h.AdminRole))
		adminUsers.GET("", h.AdminUser.List)
		// 批量接口的 batch 是静态段，与 /:id/... 在同一层；Gin 优先匹配静态段，不冲突（router 测试覆盖）
		adminUsers.POST("/batch/credits", h.AdminUser.BatchCredits)
		adminUsers.PUT("/batch/status", h.AdminUser.BatchStatus)
		adminUsers.GET("/:id", h.AdminUser.Get)
		adminUsers.GET("/:id/tasks", h.AdminUser.Tasks)
		adminUsers.GET("/:id/ledger", h.AdminUser.Ledger)
		adminUsers.GET("/:id/logins", h.AdminUser.Logins)
		adminUsers.POST("/:id/credits", h.AdminUser.AdjustCredits)
		adminUsers.PUT("/:id/limits", h.AdminUser.SetLimit)
		adminUsers.PUT("/:id/status", h.AdminUser.SetStatus)
		adminUsers.PUT("/:id/role", superOnly, h.AdminUser.SetRole)                  // 仅 super_admin
		adminUsers.POST("/:id/reset-password", superOnly, h.AdminUser.ResetPassword) // 仅 super_admin

		// 系统设置（注册 / 邮件）：读 = admin 或 super_admin；写 = 仅 super_admin。SMTP 密码只写不读。
		settings := auth.Group("/admin/settings", middleware.RequireAdmin(h.AdminRole))
		settings.GET("/register", h.AdminSettings.GetRegister)
		settings.PUT("/register", superOnly, h.AdminSettings.UpdateRegister)
		settings.GET("/smtp", h.AdminSettings.GetSMTP)
		settings.PUT("/smtp", superOnly, h.AdminSettings.UpdateSMTP)
		settings.PUT("/smtp/password", superOnly, h.AdminSettings.SetSMTPPassword)
		settings.POST("/smtp/test", superOnly, h.AdminSettings.TestSMTP)

		// 登录页展示（背景轮播视频）：读 = admin 或 super_admin；写 = 仅 super_admin，全部写操作进审计。
		// order 是静态段，与 items/:id 不在同一层，不冲突。
		settings.GET("/showcase", h.Showcase.AdminGet)
		settings.GET("/showcase/library", h.Showcase.Library) // 素材库：全平台生成的视频，供“从素材库添加”挑选
		settings.POST("/showcase/items", superOnly, h.Showcase.CreateItem)
		settings.PUT("/showcase/items/:id", superOnly, h.Showcase.UpdateItem)
		settings.DELETE("/showcase/items/:id", superOnly, h.Showcase.DeleteItem)
		settings.PUT("/showcase/order", superOnly, h.Showcase.Reorder)
		settings.PUT("/showcase/settings", superOnly, h.Showcase.UpdateSettings)
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
