package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// StoryboardHandler 处理画布分镜表的「从剧本拆分镜」。
type StoryboardHandler struct {
	svc *service.StoryboardService
}

// NewStoryboardHandler 创建分镜表 handler。
func NewStoryboardHandler(svc *service.StoryboardService) *StoryboardHandler {
	return &StoryboardHandler{svc: svc}
}

// Split 异步提交一次拆分镜：建任务 + 入队，立刻返回任务摘要（status=pending）。
// worker 在后台跑，终态和中间状态通过 WebSocket 推。
func (h *StoryboardHandler) Split(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.StoryboardSplitReq
	if !BindJSON(c, &req) {
		return
	}
	res, err := h.svc.Split(c.Request.Context(), currentUserID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Job 读任务当前状态（断线对账）：别人的任务按不存在处理。
func (h *StoryboardHandler) Job(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	jobID, ok := idcodec.Decode(c.Param("jobId"))
	if !ok {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("job id 格式错误"))
		return
	}
	res, err := h.svc.Job(c.Request.Context(), currentUserID(c), id, jobID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// ActiveJob 查某个分镜表节点上进行中的任务（弹窗打开或刷新页面时用）；没有时返回 null。
func (h *StoryboardHandler) ActiveJob(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	nodeID := c.Query("node_id")
	if nodeID == "" {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("缺少 node_id"))
		return
	}
	res, err := h.svc.ActiveJob(c.Request.Context(), currentUserID(c), id, nodeID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}
