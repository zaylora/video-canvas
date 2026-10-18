package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// AdminLogHandler 处理后台日志管理接口（/admin/logs）。必须挂在 JWTAuth + RequireActive + RequireAdmin 之后，全部只读。
type AdminLogHandler struct {
	svc *service.AuditLogService
}

// NewAdminLogHandler 创建后台日志管理 handler。
func NewAdminLogHandler(svc *service.AuditLogService) *AdminLogHandler {
	return &AdminLogHandler{svc: svc}
}

// ListAudits 返回合并后的操作审计（GET /admin/logs/audits），游标分页。
func (h *AdminLogHandler) ListAudits(c *gin.Context) {
	var req model.ListAuditReq
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
