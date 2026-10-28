package router

import (
	"github.com/gin-gonic/gin"

	agenthandler "video-canvas/internal/handler/agent"
	"video-canvas/internal/middleware"
)

// NewBridge 创建平台 API 的路由：独立的 Agent 服务调用 Go 平台能力（画布读写、工具、模型代理和计费、审批的预估和执行、
// 运行准备、事件推送）的入口。
//
// 它必须挂在只在 compose 内网可达的独立监听上，不能并入对外的主路由；每个请求都要带请求头 X-Agent-Service-Token，
// 在业务之前校验——它是内网信任的入口，没有服务令牌这道门就不能暴露（serviceToken 为空时一个路由都不挂）。
// 运行信息放在请求头 X-Agent-Context 里，没有按运行签发的令牌。这里没有 CORS：调用方是内网服务，不是浏览器。
func NewBridge(mode string, platform *agenthandler.PlatformHandler, serviceToken string) *gin.Engine {
	gin.SetMode(mode)
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Logger(), middleware.Recovery())

	if platform != nil && serviceToken != "" {
		p := r.Group("/internal/platform/v1", middleware.RequireServiceToken(serviceToken))
		p.POST("/tools/:name", platform.Tool)
		p.DELETE("/runs/:run_id", platform.EndRun)
		p.GET("/runs/:run_id/usage", platform.Usage)
		p.POST("/model/chat/completions", platform.ChatCompletions)
		p.GET("/agent/models", platform.AgentModels)
		p.POST("/canvases/check", platform.CheckCanvas)
		p.POST("/tasks/status", platform.TaskStatus) // Agent 补收任务结束的通知、取结果
		p.POST("/messages/check", platform.CheckMessage)
		p.POST("/attachments/check", platform.CheckAttachments)
		p.POST("/runs/:run_id/undo", platform.UndoRun)
		p.GET("/ids/:n", platform.EncodeID) // Agent 服务启动时核对 ID 编码密钥
		p.POST("/runs/prepare", platform.PrepareRun)
		p.POST("/approvals/propose", platform.ProposeApproval)
		p.POST("/approvals/resolve", platform.ResolveApproval)
		p.POST("/approvals/execute", platform.ExecuteApproval)
		if platform.HasEventRelay() {
			p.POST("/events", platform.PushEvents) // Agent 服务把事件成批推来，Go 推给浏览器
		}
	}
	return r
}
