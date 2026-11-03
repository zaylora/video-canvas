package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// PricingSettingsHandler 提供管理员共用的定价设置接口。
type PricingSettingsHandler struct {
	svc *service.PricingSettingsService
}

// NewPricingSettingsHandler 创建定价设置接口。
func NewPricingSettingsHandler(svc *service.PricingSettingsService) *PricingSettingsHandler {
	return &PricingSettingsHandler{svc: svc}
}

// Get 返回人工规则和生效汇率。
func (h *PricingSettingsHandler) Get(c *gin.Context) {
	v, err := h.svc.Get(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Update 校验并保存规则，审计与配置原子写入。
func (h *PricingSettingsHandler) Update(c *gin.Context) {
	var req model.PricingSettingsReq
	if !BindJSON(c, &req) {
		return
	}
	v, _, err := h.svc.Update(c.Request.Context(), currentUserID(c), req, "", nil)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}
