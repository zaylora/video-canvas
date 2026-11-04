package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
)

// PlanningPause 在当前任务完成后暂停规划。
func (h *DramaHandler) PlanningPause(c *gin.Context) { h.planning(c, "pause", false) }

// PlanningResume 恢复未完成的批次。
func (h *DramaHandler) PlanningResume(c *gin.Context) { h.planning(c, "resume", false) }

// PlanningRange 扩大已完成项目的规划范围。
func (h *DramaHandler) PlanningRange(c *gin.Context) { h.planning(c, "range", true) }

// PlanningRetryBuiltin 将失败的改编正文恢复内置并继续。
func (h *DramaHandler) PlanningRetryBuiltin(c *gin.Context) { h.planning(c, "retry-builtin", true) }

// planning 统一控制请求的绑定与项目视图响应。
func (h *DramaHandler) planning(c *gin.Context, action string, body bool) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.DramaPlanningReq
	if body && !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Planning(c.Request.Context(), currentUserID(c), id, action, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// PlanningEstimate 返回继续规划部分的积分和时间预估。
func (h *DramaHandler) PlanningEstimate(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.DramaPlanningReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.EstimatePlanning(c.Request.Context(), currentUserID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}
