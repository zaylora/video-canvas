package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// AdminCatalogHandler 是后台“渠道 / 插件 / 模型”三个列表的接口：分页、搜索、筛选，每行带跨表算好的状态；
// 以及给弹窗下拉用的不分页 options。admin 和 super_admin 都可读。
type AdminCatalogHandler struct {
	svc *service.AICatalogService
}

// NewAdminCatalogHandler 创建目录接口的 handler。
func NewAdminCatalogHandler(svc *service.AICatalogService) *AdminCatalogHandler {
	return &AdminCatalogHandler{svc: svc}
}

// ListChannels 分页返回渠道（GET /admin/ai/channels?q=&kind=&plugin=&status=&page=&page_size=）。
func (h *AdminCatalogHandler) ListChannels(c *gin.Context) {
	var req model.ListChannelReq
	if !BindQuery(c, &req) {
		return
	}
	page, err := h.svc.ListChannels(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, page)
}

// ChannelOptions 返回全部渠道的精简视图，不分页（GET /admin/ai/channels/options）。
func (h *AdminCatalogHandler) ChannelOptions(c *gin.Context) {
	list, err := h.svc.ChannelOptions(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// ListPlugins 分页返回插件（GET /admin/ai/plugins?q=&kind=&source=&status=&page=&page_size=）。
func (h *AdminCatalogHandler) ListPlugins(c *gin.Context) {
	var req model.ListPluginReq
	if !BindQuery(c, &req) {
		return
	}
	page, err := h.svc.ListPlugins(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, page)
}

// PluginOptions 返回全部插件（带全部版本和 meta），不分页（GET /admin/ai/plugins/options）。
func (h *AdminCatalogHandler) PluginOptions(c *gin.Context) {
	list, err := h.svc.PluginOptions(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// GetPlugin 返回一个插件（GET /admin/ai/plugins/:key），深链打开插件弹窗时用。
func (h *AdminCatalogHandler) GetPlugin(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	plugin, err := h.svc.GetPlugin(c.Request.Context(), key)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, plugin)
}

// ListModels 分页返回模型（GET /admin/ai/models?q=&kind=&channel=&status=&page=&page_size=）。
func (h *AdminCatalogHandler) ListModels(c *gin.Context) {
	var req model.ListModelReq
	if !BindQuery(c, &req) {
		return
	}
	page, err := h.svc.ListModels(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, page)
}

// ModelOptions 返回全部模型的列表项，不分页（GET /admin/ai/models/options）。
func (h *AdminCatalogHandler) ModelOptions(c *gin.Context) {
	list, err := h.svc.ModelOptions(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}
