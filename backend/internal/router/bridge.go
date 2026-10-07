package router

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
)

// NewBridge 创建 Agent 桥的路由：Node 里的 pi 调用 Go 的接口。
//
// 它必须挂在一个绑定 127.0.0.1 的独立监听上，不能并入对外的主路由：经过反向代理时 X-Forwarded-For 可以伪造，
// 靠请求 IP 判断「是不是本机」不可靠，只有监听地址本身是可靠的边界。鉴权用一次性桥令牌（Authorization: Bearer）。
// 这里没有 CORS：调用方是本机进程，不是浏览器。
func NewBridge(mode string, h *handler.AgentBridgeHandler) *gin.Engine {
	gin.SetMode(mode)
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Logger(), middleware.Recovery())

	g := r.Group("/internal/agent/bridge")
	g.POST("/v1/chat/completions", h.ChatCompletions) // OpenAI 兼容：pi 把它当作模型提供方
	g.POST("/tool", h.Tool)
	g.POST("/state", h.State)
	g.POST("/finish", h.Finish)
	return r
}
