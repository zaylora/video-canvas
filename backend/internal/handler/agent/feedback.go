package agent

import (
	"context"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	agentsvc "video-canvas/internal/service/agent"
)

// FeedbackAPI 是 handler 依赖的反馈业务，由 *agentsvc.ObserveService 实现。
type FeedbackAPI interface {
	// SetFeedback 记下用户对一次运行的评价（👍/👎），再次调用就是改选。
	SetFeedback(ctx context.Context, userID, runID uint64, req *model.SetAgentFeedbackReq) (*agentsvc.FeedbackView, error)
	// ClearFeedback 取消评价，本来就没有是幂等的。
	ClearFeedback(ctx context.Context, userID, runID uint64) error
}

// AgentFeedbackHandler 是用户对 Agent 运行打 👍/👎 的 HTTP 接口。
type AgentFeedbackHandler struct {
	svc FeedbackAPI
}

// NewAgentFeedbackHandler 创建 handler。
func NewAgentFeedbackHandler(svc FeedbackAPI) *AgentFeedbackHandler {
	return &AgentFeedbackHandler{svc: svc}
}

// Set 对一次运行打 👍/👎：PUT /agent/runs/:rid/feedback，返回服务端记下的评价。
func (h *AgentFeedbackHandler) Set(c *gin.Context) {
	id, ok := encodedParam(c, "rid")
	if !ok {
		return
	}
	var req model.SetAgentFeedbackReq
	if !handler.BindJSON(c, &req) {
		return
	}
	v, err := h.svc.SetFeedback(c.Request.Context(), uint64(middleware.GetUserID(c)), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Clear 取消评价：DELETE /agent/runs/:rid/feedback。
func (h *AgentFeedbackHandler) Clear(c *gin.Context) {
	id, ok := encodedParam(c, "rid")
	if !ok {
		return
	}
	if err := h.svc.ClearFeedback(c.Request.Context(), uint64(middleware.GetUserID(c)), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
