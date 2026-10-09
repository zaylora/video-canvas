package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// AdminStatsHandler 是后台总览页的任务统计接口（admin 与 super_admin 都能读）。
type AdminStatsHandler struct {
	svc *service.AIStatsService
}

// NewAdminStatsHandler 创建总览统计接口的 handler。
func NewAdminStatsHandler(svc *service.AIStatsService) *AdminStatsHandler {
	return &AdminStatsHandler{svc: svc}
}

// Stats 返回最近 days 天（7 或 30，默认 7）的每日任务量与按模型、按类型的调用占比（GET /admin/ai/stats?days=）。
func (h *AdminStatsHandler) Stats(c *gin.Context) {
	var req model.AIStatsReq
	if !BindQuery(c, &req) {
		return
	}
	if req.Days == 0 {
		req.Days = 7
	}
	v, err := h.svc.Stats(c.Request.Context(), req.Days)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}
