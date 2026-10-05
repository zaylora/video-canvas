package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// AdminSettingsHandler 处理系统设置接口（/admin/settings）：注册设置与邮件（SMTP）设置。
// 读 = admin 或 super_admin，写 = 仅 super_admin（由路由挂载保证）。
type AdminSettingsHandler struct {
	settings *service.SettingsService
	smtp     *service.SMTPService
}

// NewAdminSettingsHandler 创建系统设置 handler。
func NewAdminSettingsHandler(settings *service.SettingsService, smtp *service.SMTPService) *AdminSettingsHandler {
	return &AdminSettingsHandler{settings: settings, smtp: smtp}
}

// GetRegister 返回注册设置（GET /admin/settings/register）。
func (h *AdminSettingsHandler) GetRegister(c *gin.Context) {
	v, err := h.settings.RegisterSettings(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// UpdateRegister 保存注册设置（PUT /admin/settings/register）。
func (h *AdminSettingsHandler) UpdateRegister(c *gin.Context) {
	var req model.RegisterSettingsView
	if !bindJSON(c, &req) {
		return
	}
	v, err := h.settings.UpdateRegisterSettings(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// GetSMTP 返回邮件服务配置，密码永不返回（GET /admin/settings/smtp）。
func (h *AdminSettingsHandler) GetSMTP(c *gin.Context) {
	v, err := h.smtp.Get(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// UpdateSMTP 保存邮件服务配置（不含密码）（PUT /admin/settings/smtp）。
func (h *AdminSettingsHandler) UpdateSMTP(c *gin.Context) {
	var req model.UpdateSMTPSettingsReq
	if !bindJSON(c, &req) {
		return
	}
	v, err := h.smtp.Update(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// SetSMTPPassword 设置邮件服务密码，只写不读（PUT /admin/settings/smtp/password）。
func (h *AdminSettingsHandler) SetSMTPPassword(c *gin.Context) {
	var req model.SetSMTPPasswordReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.smtp.SetPassword(c.Request.Context(), currentUserID(c), req.Password); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// TestSMTP 向指定地址发一封测试邮件（POST /admin/settings/smtp/test）。
func (h *AdminSettingsHandler) TestSMTP(c *gin.Context) {
	var req model.TestSMTPReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.smtp.Test(c.Request.Context(), req.To); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
