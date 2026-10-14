package handler

import (
	"context"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
)

// SystemService 是版本面板接口依赖的业务能力，由 service.SystemUpdateService 实现。
type SystemService interface {
	Version(ctx context.Context) (*model.SystemVersionView, error)
	CheckUpdate(ctx context.Context, force bool) *model.UpdateCheckView
	Migrations(ctx context.Context) ([]model.MigrationFileView, error)
}

// AdminSystemHandler 处理后台版本面板接口（/admin/system），admin 与 super_admin 可读。
type AdminSystemHandler struct {
	svc SystemService
}

// NewAdminSystemHandler 创建版本面板 handler。
func NewAdminSystemHandler(svc SystemService) *AdminSystemHandler {
	return &AdminSystemHandler{svc: svc}
}

// Version 返回当前程序版本与数据库迁移进度（GET /admin/system/version）。
func (h *AdminSystemHandler) Version(c *gin.Context) {
	v, err := h.svc.Version(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Updates 检查有没有新版本（GET /admin/system/updates?force=1）。检查失败也返回 200，原因在 warning。
func (h *AdminSystemHandler) Updates(c *gin.Context) {
	response.OK(c, h.svc.CheckUpdate(c.Request.Context(), c.Query("force") == "1"))
}

// Migrations 返回每个数据库迁移文件的执行状态（GET /admin/system/migrations）。
func (h *AdminSystemHandler) Migrations(c *gin.Context) {
	v, err := h.svc.Migrations(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}
