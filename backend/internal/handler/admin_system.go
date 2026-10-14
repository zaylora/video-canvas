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

// AdminSystemHandler 处理后台版本面板接口（/admin/system）：读 = admin 或 super_admin，升级与回退 = 仅 super_admin（由路由挂载保证）。
type AdminSystemHandler struct {
	svc     SystemService
	upgrade UpgradeService
}

// NewAdminSystemHandler 创建版本面板 handler。
func NewAdminSystemHandler(svc SystemService, upgrade UpgradeService) *AdminSystemHandler {
	return &AdminSystemHandler{svc: svc, upgrade: upgrade}
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

// UpgradeService 是一键升级接口依赖的业务能力，由 service.SystemUpgradeService 实现。
type UpgradeService interface {
	Info(ctx context.Context) (*model.UpgradeInfoView, error)
	Upgrade(ctx context.Context, actorID uint64, target string) error
	Rollback(ctx context.Context, actorID uint64, target string) error
}

// UpgradeInfo 返回升级面板数据（GET /admin/system/upgrade）；升级中前端轮询它看进度。
func (h *AdminSystemHandler) UpgradeInfo(c *gin.Context) {
	v, err := h.upgrade.Info(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Upgrade 开始升级到检查到的最新版本（POST /admin/system/upgrade），立即返回 202，进度看 UpgradeInfo。
func (h *AdminSystemHandler) Upgrade(c *gin.Context) {
	var req model.UpgradeReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.upgrade.Upgrade(c.Request.Context(), currentUserID(c), req.Target); err != nil {
		response.Fail(c, err)
		return
	}
	response.Accepted(c, gin.H{"target": req.Target})
}

// Rollback 开始回退到最近一次一键升级之前的版本（POST /admin/system/rollback），立即返回 202。
func (h *AdminSystemHandler) Rollback(c *gin.Context) {
	var req model.UpgradeReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.upgrade.Rollback(c.Request.Context(), currentUserID(c), req.Target); err != nil {
		response.Fail(c, err)
		return
	}
	response.Accepted(c, gin.H{"target": req.Target})
}
