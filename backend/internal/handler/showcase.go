package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// showcasePublicCacheControl 是公开接口的缓存头：让浏览器 / CDN 缓存 60 秒，登录页每次打开不必都打到后端；
// 后端本身没有缓存层，后台改动在缓存过期后立即可见。
const showcasePublicCacheControl = "public, max-age=60"

// ShowcaseHandler 处理登录页展示接口：公开读取（GET /showcase）与后台配置（/admin/settings/showcase/*）。
// 公开读取无需登录；后台读 = admin 或 super_admin，写 = 仅 super_admin（由路由挂载保证）。
type ShowcaseHandler struct {
	svc *service.ShowcaseService
}

// NewShowcaseHandler 创建登录页展示 handler。
func NewShowcaseHandler(svc *service.ShowcaseService) *ShowcaseHandler {
	return &ShowcaseHandler{svc: svc}
}

// Public 返回登录页公开数据：设置 + 已启用条目（GET /showcase，无需登录），带 60 秒的公共缓存头。
func (h *ShowcaseHandler) Public(c *gin.Context) {
	v, err := h.svc.Public(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	// 缓存头只在成功时设置：错误响应不能被缓存
	c.Header("Cache-Control", showcasePublicCacheControl)
	response.OK(c, v)
}

// AdminGet 返回后台数据：设置 + 全部条目（GET /admin/settings/showcase）。
func (h *ShowcaseHandler) AdminGet(c *gin.Context) {
	v, err := h.svc.AdminView(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Library 分页返回素材库（全平台生成的视频），供后台“从素材库添加”挑选（GET /admin/settings/showcase/library）。
func (h *ShowcaseHandler) Library(c *gin.Context) {
	var req model.ListShowcaseLibraryReq
	if !BindQuery(c, &req) {
		return
	}
	v, err := h.svc.Library(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// CreateItem 新增展示条目（POST /admin/settings/showcase/items）。
func (h *ShowcaseHandler) CreateItem(c *gin.Context) {
	var req model.CreateShowcaseItemReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.CreateItem(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// UpdateItem 部分更新展示条目（PUT /admin/settings/showcase/items/:id）。
func (h *ShowcaseHandler) UpdateItem(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.UpdateShowcaseItemReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.UpdateItem(c.Request.Context(), currentUserID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// DeleteItem 删除展示条目，只删条目不删素材（DELETE /admin/settings/showcase/items/:id）。
func (h *ShowcaseHandler) DeleteItem(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteItem(c.Request.Context(), currentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// Reorder 按给定的 id 顺序重排全部条目（PUT /admin/settings/showcase/order）。
func (h *ShowcaseHandler) Reorder(c *gin.Context) {
	var req model.ReorderShowcaseReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.svc.Reorder(c.Request.Context(), currentUserID(c), req.IDs); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// UpdateSettings 保存展示设置并返回最新值（PUT /admin/settings/showcase/settings）。
func (h *ShowcaseHandler) UpdateSettings(c *gin.Context) {
	var req model.UpdateShowcaseSettingsReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.UpdateSettings(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}
