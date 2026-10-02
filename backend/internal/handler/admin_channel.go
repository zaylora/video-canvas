package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/pkg/response"
	"video-canvas/internal/provider"
	"video-canvas/internal/service"
)

// AdminChannelHandler 是渠道的管理接口：列表 / 详情 / 导入模型（admin 可用），创建 / 更新 / 设 Key / 连通性检查（仅 super_admin，由路由挂中间件）。
type AdminChannelHandler struct {
	svc *service.AIChannelService
}

// NewAdminChannelHandler 创建渠道管理接口的 handler。
func NewAdminChannelHandler(svc *service.AIChannelService) *AdminChannelHandler {
	return &AdminChannelHandler{svc: svc}
}

// createChannelReq 新建渠道。key 格式、base_url、settings、rate_limit 的业务规则由 service 校验（返回 50013 并说明原因），
// 这里只拦“没传”和明显超长。
type createChannelReq struct {
	Key              string             `json:"key" binding:"required,max=64" label:"key"`
	Name             string             `json:"name" binding:"required,max=128" label:"name"`
	PluginKey        string             `json:"plugin_key" binding:"required,max=30" label:"plugin_key"`
	PluginVersion    string             `json:"plugin_version" binding:"required,max=32" label:"plugin_version"`
	BaseURL          string             `json:"base_url" binding:"required,max=512" label:"base_url"`
	TrustedInternal  bool               `json:"trusted_internal"`
	AllowCredentials bool               `json:"allow_credentials"`
	Settings         map[string]any     `json:"settings"`
	RateLimit        provider.RateLimit `json:"rate_limit"`
	Enabled          *bool              `json:"enabled"` // 不传按 true
}

// updateChannelReq 更新渠道：字段都可选，不传表示不改。
type updateChannelReq struct {
	Name             *string             `json:"name" binding:"omitempty,max=128" label:"name"`
	PluginVersion    *string             `json:"plugin_version" binding:"omitempty,max=32" label:"plugin_version"`
	BaseURL          *string             `json:"base_url" binding:"omitempty,max=512" label:"base_url"`
	TrustedInternal  *bool               `json:"trusted_internal"`
	AllowCredentials *bool               `json:"allow_credentials"`
	Settings         map[string]any      `json:"settings"`
	RateLimit        *provider.RateLimit `json:"rate_limit"`
	Enabled          *bool               `json:"enabled"`
}

// importChannelReq 导入模型：args 的取值按插件 meta.import.args，可以不传。
type importChannelReq struct {
	Args map[string]any `json:"args"`
}

// List 返回所有渠道。
func (h *AdminChannelHandler) List(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
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
	if !bindJSON(c, &req) {
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	view, err := h.svc.Create(c.Request.Context(), service.ChannelCreateInput{
		Key: req.Key, Name: req.Name, PluginKey: req.PluginKey, PluginVersion: req.PluginVersion, BaseURL: req.BaseURL,
		TrustedInternal: req.TrustedInternal, AllowCredentials: req.AllowCredentials,
		Settings: req.Settings, RateLimit: req.RateLimit, Enabled: enabled, ActorID: currentUserID(c),
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
	if !bindJSON(c, &req) {
		return
	}
	view, err := h.svc.Update(c.Request.Context(), service.ChannelUpdateInput{
		Key: key, Name: req.Name, PluginVersion: req.PluginVersion, BaseURL: req.BaseURL,
		TrustedInternal: req.TrustedInternal, AllowCredentials: req.AllowCredentials,
		Settings: req.Settings, RateLimit: req.RateLimit, Enabled: req.Enabled, ActorID: currentUserID(c),
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// SetSecret 设置渠道 Key。只写：响应里不回显任何内容。
func (h *AdminChannelHandler) SetSecret(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req setSecretReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.SetSecret(c.Request.Context(), currentUserID(c), key, req.Value); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// Check 渠道连通性检查。
func (h *AdminChannelHandler) Check(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	res, err := h.svc.Check(c.Request.Context(), key)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Import 从渠道导入模型草稿（不落库）。
func (h *AdminChannelHandler) Import(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req importChannelReq
	if !bindOptionalJSON(c, &req) {
		return
	}
	res, err := h.svc.Import(c.Request.Context(), key, req.Args)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}
