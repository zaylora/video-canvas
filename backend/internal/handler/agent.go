package handler

import (
	"context"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// AgentAPI 是 handler 依赖的 Agent 业务，由 *service.AgentService 实现；声明成接口是为了 handler 测试不必搭整套存储。
type AgentAPI interface {
	// Models 返回当前可用的 Agent 模型。
	Models(ctx context.Context) ([]model.AgentModelView, error)
	// ListSessions 列出画布上的会话。
	ListSessions(ctx context.Context, userID, canvasID uint64) ([]*service.AgentSessionView, error)
	// CreateSession 新建会话。
	CreateSession(ctx context.Context, userID, canvasID uint64, req *model.CreateAgentSessionReq) (*service.AgentSessionView, error)
	// RenameSession 重命名会话。
	RenameSession(ctx context.Context, userID, sessionID uint64, title string) (*service.AgentSessionView, error)
	// DeleteSession 删除会话。
	DeleteSession(ctx context.Context, userID, sessionID uint64) error
	// Events 回放会话事件。
	Events(ctx context.Context, userID, sessionID uint64, after int64, limit int) ([]*service.AgentEventView, error)
	// StartRun 发起一轮运行。
	StartRun(ctx context.Context, userID, sessionID uint64, req *model.StartAgentRunReq) (*service.AgentRunView, error)
	// Interject 运行中插话。
	Interject(ctx context.Context, userID, runID uint64, message string) error
	// Cancel 停止运行。
	Cancel(ctx context.Context, userID, runID uint64) (*service.AgentRunView, error)
	// Resume 继续运行。
	Resume(ctx context.Context, userID, runID uint64, addBudget int) (*service.AgentRunView, error)
	// Undo 撤销本轮对画布的改动。
	Undo(ctx context.Context, userID, runID uint64) (*service.UndoResult, error)
	// Decide 处理用户对审批的决定。
	Decide(ctx context.Context, userID, approvalID uint64, req *model.DecideAgentApprovalReq) (*service.AgentApprovalView, error)
}

// AgentHandler 是画布 Agent 的 HTTP 接口。
type AgentHandler struct {
	svc AgentAPI
}

// NewAgentHandler 创建 handler。
func NewAgentHandler(svc AgentAPI) *AgentHandler { return &AgentHandler{svc: svc} }

// encodedParam 解析路径里的编码 id（会话、运行、审批都和画布一样用十六进制串）；格式不对按参数错误返回。
func encodedParam(c *gin.Context, name string) (uint64, bool) {
	id, ok := idcodec.Decode(c.Param(name))
	if !ok {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg(name+" 格式错误"))
		return 0, false
	}
	return id, true
}

// Models 返回当前可用的 Agent 模型。
func (h *AgentHandler) Models(c *gin.Context) {
	list, err := h.svc.Models(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// ListSessions 列出画布上的会话。
func (h *AgentHandler) ListSessions(c *gin.Context) {
	canvasID, ok := canvasPathID(c)
	if !ok {
		return
	}
	list, err := h.svc.ListSessions(c.Request.Context(), currentUserID(c), canvasID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// CreateSession 新建会话。
func (h *AgentHandler) CreateSession(c *gin.Context) {
	canvasID, ok := canvasPathID(c)
	if !ok {
		return
	}
	var req model.CreateAgentSessionReq
	if !bindJSON(c, &req) {
		return
	}
	v, err := h.svc.CreateSession(c.Request.Context(), currentUserID(c), canvasID, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// RenameSession 重命名会话。
func (h *AgentHandler) RenameSession(c *gin.Context) {
	id, ok := encodedParam(c, "sid")
	if !ok {
		return
	}
	var req model.RenameAgentSessionReq
	if !bindJSON(c, &req) {
		return
	}
	v, err := h.svc.RenameSession(c.Request.Context(), currentUserID(c), id, req.Title)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// DeleteSession 删除会话。
func (h *AgentHandler) DeleteSession(c *gin.Context) {
	id, ok := encodedParam(c, "sid")
	if !ok {
		return
	}
	if err := h.svc.DeleteSession(c.Request.Context(), currentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// agentEventsQuery 是回放事件的查询参数。
type agentEventsQuery struct {
	After int64 `form:"after" binding:"min=0" label:"after"`         // 已收到的最大序号
	Limit int   `form:"limit" binding:"min=0,max=200" label:"limit"` // 一页最多多少条，0 取最大值
}

// Events 回放会话里序号大于 after 的事件，断线重连后前端用它对账。
func (h *AgentHandler) Events(c *gin.Context) {
	id, ok := encodedParam(c, "sid")
	if !ok {
		return
	}
	var q agentEventsQuery
	if !bindQuery(c, &q) {
		return
	}
	list, err := h.svc.Events(c.Request.Context(), currentUserID(c), id, q.After, q.Limit)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// StartRun 发起一轮运行，返回 202：运行异步执行，进度走 WebSocket 事件。
func (h *AgentHandler) StartRun(c *gin.Context) {
	id, ok := encodedParam(c, "sid")
	if !ok {
		return
	}
	var req model.StartAgentRunReq
	if !bindJSON(c, &req) {
		return
	}
	v, err := h.svc.StartRun(c.Request.Context(), currentUserID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Accepted(c, v)
}

// Interject 运行中插话。
func (h *AgentHandler) Interject(c *gin.Context) {
	id, ok := encodedParam(c, "rid")
	if !ok {
		return
	}
	var req model.InterjectAgentReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.Interject(c.Request.Context(), currentUserID(c), id, req.Message); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// Cancel 停止运行。
func (h *AgentHandler) Cancel(c *gin.Context) {
	id, ok := encodedParam(c, "rid")
	if !ok {
		return
	}
	v, err := h.svc.Cancel(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Resume 继续被中断、预算用尽或步数用尽的运行。
func (h *AgentHandler) Resume(c *gin.Context) {
	id, ok := encodedParam(c, "rid")
	if !ok {
		return
	}
	var req model.ResumeAgentReq
	if !bindJSON(c, &req) {
		return
	}
	v, err := h.svc.Resume(c.Request.Context(), currentUserID(c), id, req.AddBudget)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Undo 撤销本轮对画布的改动。
func (h *AgentHandler) Undo(c *gin.Context) {
	id, ok := encodedParam(c, "rid")
	if !ok {
		return
	}
	res, err := h.svc.Undo(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Decide 处理用户对审批的决定：批准、拒绝或回答提问。
func (h *AgentHandler) Decide(c *gin.Context) {
	id, ok := encodedParam(c, "aid")
	if !ok {
		return
	}
	var req model.DecideAgentApprovalReq
	if !bindJSON(c, &req) {
		return
	}
	v, err := h.svc.Decide(c.Request.Context(), currentUserID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}
