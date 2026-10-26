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
	Health         *handler.HealthHandler
	User           *handler.UserHandler
	Me             *handler.MeHandler // 个人中心：资料、头像、改密码、统计、热力图、积分流水
	CanvasProject  *handler.CanvasProjectHandler
	CanvasTransfer *handler.CanvasTransferHandler // 画布导出为文档、从文档导入为新画布
	Conversation   *handler.ConversationHandler   // 首页生成：对话与生成记录
	// AgentProxy：画布 Agent 的会话、运行、审批、评价由独立的 Agent 服务负责，这些接口只校验登录态后反向代理过去
	// （路径、方法、信封不变）；技能目录等仍由 Go 自己处理。为 nil 表示没有启用 Agent，这些接口统一返回「Agent 暂不可用」。
	AgentProxy *agenthandler.AgentProxyHandler
	AgentSkill *agenthandler.SkillHandler // Agent 技能：用户端目录 + 管理端导入 / 版本 / 启停

	// Agent 运行观测：后台只读查看与导出、分析令牌
	AgentObserve  *agenthandler.AgentObserveHandler // 后台运行记录（只读）和分析令牌管理
	AnalysisToken middleware.TokenAuthenticator     // 校验分析令牌，本地 agent-trace 用它只读拉取运行记录

	// 长任务生成相关
	GenerationTask *handler.GenerationTaskHandler      // 任务提交 / 对账 / 取消 / 积分 / webhook
	WS             *handler.WSHandler                  // 用户级 WebSocket 推送
	Asset          *handler.AssetHandler               // 素材上传与查询
	AssetLibrary   *handler.AssetLibraryHandler        // 资产库：文件夹、库归属、生成历史
	CharacterCard  *handler.CharacterCardHandler       // 用户私有角色卡 CRUD 与批量读取
	Files          gin.HandlerFunc                     // 素材稳定地址 /files/*：本地存储直出，对象存储签名后 302
	AIModel        *handler.AIModelHandler             // 面向画布的模型清单
	AdminAI        *handler.AdminAIHandler             // AI 模型配置管理
	AdminCatalog   *handler.AdminCatalogHandler        // 渠道 / 插件 / 模型的分页列表与下拉 options
	AdminPlugin    *handler.AdminPluginHandler         // 协议插件管理
	AdminChannel   *handler.AdminChannelHandler        // 渠道管理
	AdminStats     *handler.AdminStatsHandler          // 总览页任务统计
	AdminQueue     *handler.AdminQueueHandler          // 后台「任务队列」页：队列、渠道名额、吞吐、实例，调并发与暂停
	RiverUI        *handler.RiverUIHandler             // 嵌入的 River UI（任务级监控）：一次性 ticket 换会话 cookie，仅 super_admin
	AdminStorage   *handler.AdminStorageHandler        // 存储配置管理
	AdminImageProc *handler.AdminImageProcessorHandler // 图片处理服务管理
	AdminMe        *handler.AdminMeHandler             // 当前管理员身份
	AdminUser      *handler.AdminUserHandler           // 后台用户管理
	AdminUsage     *handler.AdminUsageHandler          // 后台使用记录（用户 › 使用记录）：全站生成任务的流水、摘要与详情，只读
	AdminLogs      *handler.AdminLogHandler            // 后台日志管理（日志管理 › 操作审计），只读
	AdminSettings  *handler.AdminSettingsHandler       // 注册与邮件设置
	AdminSystem    *handler.AdminSystemHandler         // 版本面板：当前版本、检查更新、迁移状态
	Showcase       *handler.ShowcaseHandler            // 登录页展示：公开读取 + 后台配置
	Payment        *handler.PaymentHandler             // 积分商城：商品、下单、订单状态；渠道回调（无需登录）
	AdminPayment   *handler.PaymentAdminHandler        // 后台支付管理：全局设置、渠道、商品、订单
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

		// 支付渠道的异步通知：渠道服务器调用，不带 JWT，身份由渠道的签名保证；GET（易支付）和 POST（其余）都要接
		v1.Any("/payment/notify/:provider", h.Payment.Notify)

		// 无需 JWT：身份由一次性 ticket 决定（浏览器 WebSocket 不能带请求头）
		v1.GET("/ws", h.WS.Connect)

		// River UI：浏览器在新标签页里直接打开，带不上 Authorization 头；身份由一次性 ticket 换来的会话 cookie 决定，仅 super_admin
		v1.GET("/admin/river", h.RiverUI.Redirect)
		v1.Any("/admin/river/*path", h.RiverUI.Serve)
		// 需要登录：JWT 校验之后再查用户状态（停用 → 403，token_version 不符 → 401），封禁与重置密码立即生效
		auth := v1.Group("", middleware.JWTAuth(jwtSecret), middleware.RequireActive(h.UserState))

		// 个人中心：只操作当前登录用户自己的数据
		me := auth.Group("/me")
		me.GET("", h.Me.Get)
		me.PATCH("", h.Me.Patch)
		me.POST("/avatar", h.Me.UploadAvatar)
		me.DELETE("/avatar", h.Me.DeleteAvatar)
		me.PUT("/password", h.Me.ChangePassword)
		me.GET("/stats", h.Me.Stats)
		me.GET("/activity", h.Me.Activity)     // ?tz=Asia/Shanghai&year=2026
		me.GET("/credits/ledger", h.Me.Ledger) // ?type=all|task|admin&page=1&page_size=20

		// 画布操作
		canvas := auth.Group("/canvas")
		canvas.POST("", h.CanvasProject.Create)
		canvas.GET("", h.CanvasProject.List)
		canvas.GET("/:id", h.CanvasProject.Get)
		canvas.PUT("/:id", h.CanvasProject.Update)
		canvas.DELETE("/:id", h.CanvasProject.Delete)
		canvas.GET("/:id/export", h.CanvasTransfer.Export)
		canvas.POST("/import", h.CanvasTransfer.Import)
		fwd := agentRoute(h.AgentProxy)
		canvas.GET("/:id/agent/sessions", fwd)
		canvas.POST("/:id/agent/sessions", fwd)

		// 首页生成的对话：对话和记录的 id 与画布一样是十六进制串；POST /:id/records 的 id 还可以是 default / new
		conv := auth.Group("/conversations")
		conv.GET("", h.Conversation.List)
		conv.POST("", h.Conversation.Create)
		conv.PATCH("/:id", h.Conversation.Rename)
		conv.DELETE("/:id", h.Conversation.Delete)
		conv.GET("/:id/records", h.Conversation.ListRecords)          // ?before=&limit=
		conv.POST("/:id/records", h.Conversation.Submit)              // 202
		conv.DELETE("/:id/records/:rid", h.Conversation.DeleteRecord) // ?cancel_active=true

		// 画布 Agent：id 与画布一样是十六进制串
		agent := auth.Group("/agent")
		agent.GET("/skills", h.AgentSkill.UserList) // 已启用技能的目录（@ 弹层），技能库在 Go
		agent.GET("/models", fwd)
		agent.PATCH("/sessions/:sid", fwd)
		agent.DELETE("/sessions/:sid", fwd)
		agent.GET("/sessions/:sid/events", fwd)  // ?after=序号&limit=
		agent.GET("/sessions/:sid/history", fwd) // ?before=序号&limit=，聊天记录从最新往回翻
		agent.POST("/sessions/:sid/runs", fwd)   // 202
		agent.POST("/runs/:rid/interject", fwd)
		agent.POST("/runs/:rid/cancel", fwd)
		agent.POST("/runs/:rid/resume", fwd)
		agent.POST("/runs/:rid/undo", fwd)
		agent.PUT("/runs/:rid/feedback", fwd)    // 👍/👎，再调用就是改选
		agent.DELETE("/runs/:rid/feedback", fwd) // 取消评价
		agent.POST("/approvals/:aid/decision", fwd)

		// 积分商城：只操作当前用户自己的订单；pending 是静态段，优先于 /orders/:no
		pay := auth.Group("/payment")
		pay.GET("/store", h.Payment.Store)
		pay.POST("/orders", h.Payment.CreateOrder) // 201
		pay.GET("/orders/pending", h.Payment.PendingOrder)
		pay.GET("/orders/:no", h.Payment.GetOrder)
		pay.POST("/orders/:no/check", h.Payment.CheckOrder)
		pay.POST("/orders/:no/close", h.Payment.CancelOrder)

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
		auth.GET("/assets/generated", h.AssetLibrary.ListGenerated)              // 生成历史；静态段优先于 /assets/:id
		auth.GET("/assets/:id", h.Asset.Get)

		// 资产库：文件夹与库条目的 id 是十六进制串；POST /asset-folders、新增归属返回 201，已在库里返回 200
		auth.GET("/asset-folders", h.AssetLibrary.ListFolders)
		auth.POST("/asset-folders", h.AssetLibrary.CreateFolder)
		auth.PATCH("/asset-folders/:id", h.AssetLibrary.RenameFolder)
		auth.DELETE("/asset-folders/:id", h.AssetLibrary.DeleteFolder)
		auth.GET("/asset-library/items", h.AssetLibrary.ListItems) // ?folder_id=root|<id>|all&kind=&q=&cursor=&limit=
		auth.POST("/asset-library/items", h.AssetLibrary.AddItem)
		auth.PATCH("/asset-library/items/:id", h.AssetLibrary.MoveItem)
		auth.DELETE("/asset-library/items/:id", h.AssetLibrary.RemoveItem)

		// 角色卡：ID 为十六进制串，素材 ID 为数字；所有操作按当前用户隔离。
		characters := auth.Group("/characters")
		characters.GET("", h.CharacterCard.List)
		characters.GET("/:id", h.CharacterCard.Get)
		characters.POST("", h.CharacterCard.Create)
		characters.PATCH("/:id", h.CharacterCard.Update)
		characters.DELETE("/:id", h.CharacterCard.Delete)

		// 管理员后台接口：读与模型相关的写 = admin 或 super_admin；插件与渠道的写 = 仅 super_admin。
		// 凭证不再有独立接口，渠道 Key 统一走 PUT /channels/:key/secret。
		adminAI := auth.Group("/admin/ai", middleware.RequireAdmin(h.AdminRole))
		superOnly := middleware.RequireSuperAdmin(h.AdminRole)
		adminAI.GET("/me", h.AdminMe.Me)
		adminAI.GET("/stats", h.AdminStats.Stats)
		adminAI.GET("/overview", h.AdminCatalog.Overview)

		// 任务队列：读 = admin 或 super_admin；调并发、暂停 / 恢复、打开 River UI = 仅 super_admin
		queueAdmin := auth.Group("/admin/queue", middleware.RequireAdmin(h.AdminRole))
		queueAdmin.GET("/overview", h.AdminQueue.Overview)
		queueAdmin.GET("/throughput", h.AdminQueue.Throughput)
		queueAdmin.PUT("/concurrency", superOnly, h.AdminQueue.SetConcurrency)
		queueAdmin.POST("/pause", superOnly, h.AdminQueue.Pause)
		queueAdmin.POST("/resume", superOnly, h.AdminQueue.Resume)
		queueAdmin.POST("/river-ticket", superOnly, h.RiverUI.IssueTicket)

		plugins := adminAI.Group("/plugins")
		plugins.GET("", h.AdminCatalog.ListPlugins)
		plugins.GET("/options", h.AdminCatalog.PluginOptions)
		plugins.GET("/:key", h.AdminCatalog.GetPlugin)
		plugins.POST("", superOnly, h.AdminPlugin.Upload)
		plugins.PUT("/:key/enabled", superOnly, h.AdminPlugin.SetEnabled)
		plugins.DELETE("/:key/versions/:version", superOnly, h.AdminPlugin.DeleteVersion)
		plugins.GET("/:key/delete-check", superOnly, h.AdminPlugin.DeleteCheck)
		plugins.POST("/:key/upgrade-models", superOnly, h.AdminPlugin.UpgradeModels)
		plugins.DELETE("/:key", superOnly, h.AdminPlugin.Delete)

		channels := adminAI.Group("/channels")
		channels.GET("", h.AdminCatalog.ListChannels)
		channels.GET("/options", h.AdminCatalog.ChannelOptions)
		channels.GET("/loads", h.AdminChannel.Loads)
		channels.POST("", superOnly, h.AdminChannel.Create)
		channels.PUT("/sort", superOnly, h.AdminChannel.SetOrder) // 必须在 /:key 之前：否则 "sort" 会被当成渠道 key
		channels.GET("/:key", h.AdminChannel.Get)
		channels.PUT("/:key", superOnly, h.AdminChannel.Update)
		channels.PUT("/:key/secret", superOnly, h.AdminChannel.SetSecret)
		channels.PUT("/:key/secret-key", superOnly, h.AdminChannel.SetSecretKey)
		channels.POST("/:key/pull-models", h.AdminChannel.PullModels)
		channels.POST("/:key/models/import", h.AdminChannel.ImportModels)
		channels.PUT("/:key/models/sort", h.AdminChannel.SetModelOrder)
		channels.GET("/:key/delete-check", superOnly, h.AdminChannel.DeleteCheck)
		channels.DELETE("/:key", superOnly, h.AdminChannel.Delete)

		models := adminAI.Group("/models")
		models.GET("", h.AdminCatalog.ListModels)
		models.GET("/options", h.AdminCatalog.ModelOptions)
		models.GET("/limits", h.AdminCatalog.ModelLimits) // 静态段，与 /:key 同层，Gin 优先匹配静态段
		models.POST("", h.AdminAI.Create)
		models.GET("/:key", h.AdminAI.Get)
		models.PUT("/:key", h.AdminAI.Update)
		models.POST("/:key/validate", h.AdminAI.Validate)
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

		// Agent 运行观测：运行记录是只读接口，管理员 JWT 和后台签发的分析令牌（本地 agent-trace 用）都能访问；
		// 令牌只挂在这一组只读接口上。令牌管理本身只接受管理员 JWT，不能用令牌签发或吊销令牌
		observe := v1.Group("/admin/agent", middleware.AdminOrAnalysisToken(h.AnalysisToken,
			middleware.JWTAuth(jwtSecret), middleware.RequireActive(h.UserState), middleware.RequireAdmin(h.AdminRole)))
		observe.GET("/runs", h.AgentObserve.ListRuns) // 摘要列表，游标分页
		observe.GET("/runs/:rid", h.AgentObserve.Get)
		observe.GET("/runs/:rid/export", h.AgentObserve.Export)     // ?format=md|json|both
		observe.GET("/runs/:rid/requests", h.AgentObserve.Requests) // 各次模型调用实际发给上游的完整请求体，体量大，单独取
		observe.GET("/stats", h.AgentObserve.Stats)                 // ?group_by=harness_key|prompt_version|model_key
		tokensAdmin := auth.Group("/admin/agent/tokens", middleware.RequireAdmin(h.AdminRole))
		tokensAdmin.GET("", h.AgentObserve.ListTokens)
		tokensAdmin.POST("", h.AgentObserve.IssueToken)
		tokensAdmin.DELETE("/:id", h.AgentObserve.RevokeToken)

		// 存储配置（素材存到哪）：读 = admin 或 super_admin；测试 / 创建 / 修改 / 换密钥 / 设默认 / 删除 = 仅 super_admin。
		// 密钥只写不读，和渠道 Key 同一套规则。
		storages := auth.Group("/admin/storages", middleware.RequireAdmin(h.AdminRole))
		storages.GET("", h.AdminStorage.List)
		storages.GET("/presets", h.AdminStorage.Presets)
		storages.GET("/options", h.AdminStorage.Options)
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
		processors.GET("/options", h.AdminImageProc.Options)
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

		// 使用记录：全站生成任务的流水、摘要与详情，全部只读，admin 与 super_admin 同权。
		// summary 是静态段，与 /:ref 同层，Gin 优先匹配静态段，不冲突（router 测试覆盖）
		adminUsage := auth.Group("/admin/usage", middleware.RequireAdmin(h.AdminRole))
		adminUsage.GET("", h.AdminUsage.List)
		adminUsage.GET("/summary", h.AdminUsage.Summary)
		adminUsage.GET("/:ref", h.AdminUsage.Get)

		// 日志管理 › 操作审计：两张审计表合并后的只读列表，admin 与 super_admin 同权
		adminLogs := auth.Group("/admin/logs", middleware.RequireAdmin(h.AdminRole))
		adminLogs.GET("/audits", h.AdminLogs.ListAudits)

		// 版本面板：admin 与 super_admin 可读；一键升级与回退仅 super_admin
		system := auth.Group("/admin/system", middleware.RequireAdmin(h.AdminRole))
		system.GET("/version", h.AdminSystem.Version)
		system.GET("/updates", h.AdminSystem.Updates)
		system.GET("/migrations", h.AdminSystem.Migrations)
		system.GET("/upgrade", h.AdminSystem.UpgradeInfo)
		system.POST("/upgrade", superOnly, h.AdminSystem.Upgrade)   // 仅 super_admin，写审计
		system.POST("/rollback", superOnly, h.AdminSystem.Rollback) // 仅 super_admin，写审计

		// 系统设置（注册 / 邮件）：读 = admin 或 super_admin；写 = 仅 super_admin。SMTP 密码只写不读。
		settings := auth.Group("/admin/settings", middleware.RequireAdmin(h.AdminRole))
		settings.GET("/register", h.AdminSettings.GetRegister)
		settings.PUT("/register", superOnly, h.AdminSettings.UpdateRegister)
		settings.GET("/smtp", h.AdminSettings.GetSMTP)
		settings.PUT("/smtp", superOnly, h.AdminSettings.UpdateSMTP)
		settings.PUT("/smtp/password", superOnly, h.AdminSettings.SetSMTPPassword)
		settings.POST("/smtp/test", superOnly, h.AdminSettings.TestSMTP)

		// 支付管理：读与商品、订单操作 = admin 或 super_admin；全局设置与渠道配置的写（含校验、启用、停用）= 仅 super_admin。
		// 渠道密钥只写不读，响应里只有「是否已设置」。sort 是静态段，与 /:id 同层，Gin 优先匹配静态段。
		adminPay := auth.Group("/admin/payment", middleware.RequireAdmin(h.AdminRole))
		adminPay.GET("/settings", h.AdminPayment.GetSettings)
		adminPay.PUT("/settings", superOnly, h.AdminPayment.UpdateSettings)
		adminPay.GET("/channels", h.AdminPayment.ListChannels)
		adminPay.PUT("/channels/:provider", superOnly, h.AdminPayment.UpdateChannel)
		adminPay.POST("/channels/:provider/validate", superOnly, h.AdminPayment.ValidateChannel)
		adminPay.POST("/channels/:provider/enable", superOnly, h.AdminPayment.EnableChannel)
		adminPay.POST("/channels/:provider/disable", superOnly, h.AdminPayment.DisableChannel)
		adminPay.GET("/products", h.AdminPayment.ListProducts)
		adminPay.POST("/products", h.AdminPayment.CreateProduct)
		adminPay.PUT("/products/sort", h.AdminPayment.SortProducts)
		adminPay.PUT("/products/:id", h.AdminPayment.UpdateProduct)
		adminPay.DELETE("/products/:id", h.AdminPayment.DeleteProduct)
		adminPay.GET("/orders", h.AdminPayment.ListOrders)
		adminPay.GET("/orders/:no", h.AdminPayment.GetOrder)
		adminPay.POST("/orders/:no/query", h.AdminPayment.QueryOrder)
		adminPay.POST("/orders/:no/close", h.AdminPayment.CloseOrder)

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

// agentRoute 是画布 Agent 接口的处理函数：启用了 Agent 就反向代理给 Agent 服务；没启用统一返回「Agent 暂不可用」。
func agentRoute(p *agenthandler.AgentProxyHandler) gin.HandlerFunc {
	if p != nil {
		return p.Forward
	}
	return func(c *gin.Context) { response.Fail(c, errcode.ErrAgentUnavailable) }
}
