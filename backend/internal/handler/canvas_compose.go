package handler

import (
	"strings"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// CanvasComposeHandler 处理画布成片合成：提交、查活跃任务、按 id 对账、取消。
type CanvasComposeHandler struct {
	svc *service.CanvasComposeService
}

// NewCanvasComposeHandler 创建成片合成 handler。
func NewCanvasComposeHandler(svc *service.CanvasComposeService) *CanvasComposeHandler {
	return &CanvasComposeHandler{svc: svc}
}

// Compose 异步提交一次合成：建任务 + 入队，立刻返回任务摘要（status=queued）。
// asset_ids 的顺序就是成片顺序，服务端不猜。
func (h *CanvasComposeHandler) Compose(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.CanvasComposeReq
	if !BindJSON(c, &req) {
		return
	}
	res, err := h.svc.Submit(c.Request.Context(), currentUserID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Active 查某个节点上的成片任务（弹窗 / 节点挂载时补查），并返回成片是否过期。
// asset_ids 传当前准备合成的素材序列，服务端据此算指纹；不传时不判断过期。
func (h *CanvasComposeHandler) Active(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	nodeID := c.Query("node_id")
	if nodeID == "" {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("缺少 node_id"))
		return
	}
	var assetIDs []string
	if raw := c.Query("asset_ids"); raw != "" {
		assetIDs = strings.Split(raw, ",")
	}
	res, err := h.svc.Active(c.Request.Context(), currentUserID(c), id, nodeID, assetIDs)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Job 读任务当前状态（断线对账）：别人的任务按不存在处理。
func (h *CanvasComposeHandler) Job(c *gin.Context) {
	if _, ok := CanvasPathID(c); !ok {
		return
	}
	jobID, ok := idcodec.Decode(c.Param("jobId"))
	if !ok {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("job id 格式错误"))
		return
	}
	res, err := h.svc.Job(c.Request.Context(), currentUserID(c), jobID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Cancel 取消一条还没落终态的合成任务。
func (h *CanvasComposeHandler) Cancel(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	jobID, ok := idcodec.Decode(c.Param("jobId"))
	if !ok {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("job id 格式错误"))
		return
	}
	if err := h.svc.Cancel(c.Request.Context(), currentUserID(c), id, jobID); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"canceled": true})
}
