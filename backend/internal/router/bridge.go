package router

import (
	"github.com/gin-gonic/gin"

	agenthandler "video-canvas/internal/handler/agent"
	"video-canvas/internal/middleware"
)

// NewBridge 创建 Agent 桥的路由：Node 里的 pi 调用 Go 的接口。
//
// 它必须挂在一个绑定 127.0.0.1 的独立监听上，不能并入对外的主路由：经过反向代理时 X-Forwarded-For 可以伪造，
// 靠请求 IP 判断「是不是本机」不可靠，只有监听地址本身是可靠的边界。鉴权用一次性桥令牌（Authorization: Bearer）。
// Agent 作为独立服务从内网访问时，桥监听内网地址，serviceToken 不为空：每个请求还要带请求头 X-Agent-Service-Token，
// 在业务之前校验；为空表示只靠运行令牌（回环监听）。
// 这里没有 CORS：调用方是本机进程或内网服务，不是浏览器。
func NewBridge(mode string, h *agenthandler.AgentBridgeHandler, serviceToken string) *gin.Engine {
	gin.SetMode(mode)
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Logger(), middleware.Recovery())

	var guards []gin.HandlerFunc
	if serviceToken != "" {
		guards = append(guards, middleware.RequireServiceToken(serviceToken))
	}
	g := r.Group("/internal/agent/bridge", append(guards, h.TagRun())...)
	g.POST("/v1/chat/completions", h.ChatCompletions) // OpenAI 兼容：pi 把它当作模型提供方
	g.POST("/tool", h.Tool)
	g.POST("/tool-event", h.ToolEvent)
	g.POST("/state", h.State)
	g.POST("/finish", h.Finish)
	return r
}
