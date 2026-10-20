package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// AdminQueueHandler 是后台「任务队列」页的接口（/admin/queue）：总览、吞吐、调并发、暂停 / 恢复。
// 读 = admin 或 super_admin，写 = 仅 super_admin（由路由挂载保证）。
type AdminQueueHandler struct {
	svc *service.QueueAdminService
}

// NewAdminQueueHandler 创建任务队列后台 handler。
func NewAdminQueueHandler(svc *service.QueueAdminService) *AdminQueueHandler {
	return &AdminQueueHandler{svc: svc}
}

// Overview 返回三张队列卡片、渠道名额占用与实例列表（GET /admin/queue/overview）。
func (h *AdminQueueHandler) Overview(c *gin.Context) {
	v, err := h.svc.Overview(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Throughput 返回最近 minutes 分钟每分钟的完成数、失败数与平均排队时长（GET /admin/queue/throughput?minutes=60）。
func (h *AdminQueueHandler) Throughput(c *gin.Context) {
	var req model.QueueThroughputReq
	if !BindQuery(c, &req) {
		return
	}
	v, err := h.svc.Throughput(c.Request.Context(), req.Minutes)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// SetConcurrency 调整一个阶段的并发（PUT /admin/queue/queues/:name），立即生效。
func (h *AdminQueueHandler) SetConcurrency(c *gin.Context) {
	var req model.QueueConcurrencyReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.SetConcurrency(c.Request.Context(), currentUserID(c), c.Param("name"), req.Concurrency)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Pause 暂停一个阶段（POST /admin/queue/queues/:name/pause），幂等。
func (h *AdminQueueHandler) Pause(c *gin.Context) { h.setPaused(c, true) }

// Resume 恢复一个阶段（POST /admin/queue/queues/:name/resume），幂等。
func (h *AdminQueueHandler) Resume(c *gin.Context) { h.setPaused(c, false) }

func (h *AdminQueueHandler) setPaused(c *gin.Context, paused bool) {
	v, err := h.svc.SetPaused(c.Request.Context(), currentUserID(c), c.Param("name"), paused)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}
