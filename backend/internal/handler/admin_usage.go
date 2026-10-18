package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// AdminUsageHandler 处理后台使用记录接口（/admin/usage）。必须挂在 JWTAuth + RequireActive + RequireAdmin 之后，全部只读。
type AdminUsageHandler struct {
	svc *service.AdminUsageService
}

// NewAdminUsageHandler 创建后台使用记录 handler。
func NewAdminUsageHandler(svc *service.AdminUsageService) *AdminUsageHandler {
	return &AdminUsageHandler{svc: svc}
}

// List 返回全站任务流水（GET /admin/usage），游标分页。
func (h *AdminUsageHandler) List(c *gin.Context) {
	var req model.ListUsageReq
	if !BindQuery(c, &req) {
		return
	}
	page, err := h.svc.List(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, page)
}

// Summary 返回与列表同一组筛选条件下的任务数与实扣积分（GET /admin/usage/summary）。
func (h *AdminUsageHandler) Summary(c *gin.Context) {
	var req model.UsageFilterReq
	if !BindQuery(c, &req) {
		return
	}
	sum, err := h.svc.Summary(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, sum)
}

// Get 返回一个任务的完整详情（GET /admin/usage/:ref），ref 是任务编号。
func (h *AdminUsageHandler) Get(c *gin.Context) {
	detail, err := h.svc.Get(c.Request.Context(), c.Param("ref"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, detail)
}
