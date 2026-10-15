package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/pkg/response"
	"video-canvas/internal/provider"
	"video-canvas/internal/service"
)

// AdminChannelHandler 是渠道的管理接口：列表 / 详情 / 拉取与导入模型（admin 可用），创建 / 更新 / 设 Key / 设 Secret Key / 排序 /
// 删除及其预检（仅 super_admin，由路由挂中间件）。渠道只管连接信息，协议在模型里选。
type AdminChannelHandler struct {
	svc *service.AIChannelService
	ai  *service.AIConfigService
}

// NewAdminChannelHandler 创建渠道管理接口的 handler。ai 用来调整渠道内模型的顺序。
func NewAdminChannelHandler(svc *service.AIChannelService, ai *service.AIConfigService) *AdminChannelHandler {
	return &AdminChannelHandler{svc: svc, ai: ai}
}

// createChannelReq 新建渠道。key 格式、base_url、headers、rate_limit 的业务规则由 service 校验（返回 50013 并说明原因），
// 这里只拦“没传”和明显超长。
type createChannelReq struct {
	Key       string             `json:"key" binding:"omitempty,max=64" label:"key"` // 不传由后端生成
	Name      string             `json:"name" binding:"required,max=128" label:"name"`
	BaseURL   string             `json:"base_url" binding:"required,max=512" label:"base_url"`
	Headers   map[string]string  `json:"headers"`
	RateLimit provider.RateLimit `json:"rate_limit"`
	Enabled   *bool              `json:"enabled"` // 不传按 true
}

// updateChannelReq 更新渠道：字段都可选，不传表示不改；headers 传了（含空对象）就整体替换。
type updateChannelReq struct {
	Name      *string             `json:"name" binding:"omitempty,max=128" label:"name"`
	BaseURL   *string             `json:"base_url" binding:"omitempty,max=512" label:"base_url"`
	Headers   map[string]string   `json:"headers"`
	RateLimit *provider.RateLimit `json:"rate_limit"`
	Enabled   *bool               `json:"enabled"`
}

// setSecretKeyReq 设置或清除渠道 Secret Key：value 为空串表示清除，所以不加 required。
type setSecretKeyReq struct {
	Value string `json:"value" binding:"max=4096" label:"Secret Key"`
}

// orderKeysReq 是排序请求：keys 是完整的新顺序（是不是全集由 service 校验）。
type orderKeysReq struct {
	Keys []string `json:"keys" binding:"required,max=2000,dive,max=128" label:"keys"`
}

// importModelsReq 导入所选的上游模型。数量上限由 service 校验（给出“分批导入”的提示），这里只拦没传。
type importModelsReq struct {
	IDs []string `json:"ids" binding:"required,dive,max=256" label:"ids"`
}

// Loads 返回各渠道当前的任务负载（生成中 / 排队数）。
func (h *AdminChannelHandler) Loads(c *gin.Context) {
	list, err := h.svc.Loads(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Get 返回渠道详情。
func (h *AdminChannelHandler) Get(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	view, err := h.svc.Get(c.Request.Context(), key)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// Create 新建渠道。
func (h *AdminChannelHandler) Create(c *gin.Context) {
	var req createChannelReq
	if !BindJSON(c, &req) {
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	view, err := h.svc.Create(c.Request.Context(), service.ChannelCreateInput{
		Key: req.Key, Name: req.Name, BaseURL: req.BaseURL, Headers: req.Headers, RateLimit: req.RateLimit,
		Enabled: enabled, ActorID: currentUserID(c),
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// Update 更新渠道（不传的字段不改）。
func (h *AdminChannelHandler) Update(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req updateChannelReq
	if !BindJSON(c, &req) {
		return
	}
	view, err := h.svc.Update(c.Request.Context(), service.ChannelUpdateInput{
		Key: key, Name: req.Name, BaseURL: req.BaseURL, Headers: req.Headers, RateLimit: req.RateLimit,
		Enabled: req.Enabled, ActorID: currentUserID(c),
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// SetSecret 设置渠道 API Key。只写：响应里不回显任何内容。
func (h *AdminChannelHandler) SetSecret(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req setSecretReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.svc.SetSecret(c.Request.Context(), currentUserID(c), key, req.Value); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// SetSecretKey 设置或清除渠道 Secret Key（value 为空串表示清除）。只写：响应里不回显任何内容。
func (h *AdminChannelHandler) SetSecretKey(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req setSecretKeyReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.svc.SetSecretKey(c.Request.Context(), currentUserID(c), key, req.Value); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// SetOrder 调整渠道顺序：keys 是全部渠道的完整新顺序。
func (h *AdminChannelHandler) SetOrder(c *gin.Context) {
	var req orderKeysReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.svc.SetOrder(c.Request.Context(), currentUserID(c), req.Keys); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// SetModelOrder 调整渠道内模型的顺序：keys 是该渠道下全部模型的完整新顺序。
func (h *AdminChannelHandler) SetModelOrder(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req orderKeysReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.ai.SetChannelModelOrder(c.Request.Context(), key, req.Keys); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// PullModels 向上游拉取模型清单（不落库）：宿主内置的通用请求，不经过协议插件。
func (h *AdminChannelHandler) PullModels(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	res, err := h.svc.PullModels(c.Request.Context(), key)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// ImportModels 把所选的上游模型导入成“待配置”模型（未启用、没选协议）；渠道下已有的跳过。
func (h *AdminChannelHandler) ImportModels(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req importModelsReq
	if !BindJSON(c, &req) {
		return
	}
	res, err := h.svc.ImportModels(c.Request.Context(), currentUserID(c), key, req.IDs)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// DeleteCheck 删除渠道前的预检：返回阻断删除的原因，blockers 为空数组表示可以删。
func (h *AdminChannelHandler) DeleteCheck(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	res, err := h.svc.CheckDelete(c.Request.Context(), key)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Delete 删除一个未被引用的渠道（连同它的 Key 与 Secret Key）。
func (h *AdminChannelHandler) Delete(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), currentUserID(c), key); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
