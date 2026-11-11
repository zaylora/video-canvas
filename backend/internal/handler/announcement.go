package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// announcementPublicCacheControl 是公开接口的缓存头：只让浏览器缓存 60 秒（private：接口需要登录，不给 CDN 缓存），
// 首页每次切页不必都打到后端；后台改动最迟 60 秒后用户刷新可见。
const announcementPublicCacheControl = "private, max-age=60"

// AnnouncementHandler 处理首页公告接口：用户端读取（GET /announcements，需登录）与后台管理（/admin/settings/announcements/*）。
// 后台读写都是 admin 或 super_admin（由路由挂载保证）。
type AnnouncementHandler struct {
	svc *service.AnnouncementService
}

// NewAnnouncementHandler 创建公告 handler。
func NewAnnouncementHandler(svc *service.AnnouncementService) *AnnouncementHandler {
	return &AnnouncementHandler{svc: svc}
}

// Public 返回生效中的公告（GET /announcements），带 60 秒的私有缓存头。
func (h *AnnouncementHandler) Public(c *gin.Context) {
	v, err := h.svc.Public(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	// 缓存头只在成功时设置：错误响应不能被缓存
	c.Header("Cache-Control", announcementPublicCacheControl)
	response.OK(c, v)
}

// AdminList 返回后台公告列表（GET /admin/settings/announcements）。
func (h *AnnouncementHandler) AdminList(c *gin.Context) {
	v, err := h.svc.AdminList(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Create 新建公告（POST /admin/settings/announcements）。
func (h *AnnouncementHandler) Create(c *gin.Context) {
	var req model.SaveAnnouncementReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Create(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Update 整体替换一条公告（PUT /admin/settings/announcements/:id）。
func (h *AnnouncementHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.SaveAnnouncementReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Update(c.Request.Context(), currentUserID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// SetEnabled 启用或停用一条公告（PUT /admin/settings/announcements/:id/enabled）。
func (h *AnnouncementHandler) SetEnabled(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.SetAnnouncementEnabledReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.SetEnabled(c.Request.Context(), currentUserID(c), id, *req.Enabled)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Delete 删除一条公告（DELETE /admin/settings/announcements/:id）。
func (h *AnnouncementHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), currentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// Reorder 按给定的 id 顺序重排全部公告（PUT /admin/settings/announcements/order）。
func (h *AnnouncementHandler) Reorder(c *gin.Context) {
	var req model.ReorderAnnouncementsReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.svc.Reorder(c.Request.Context(), currentUserID(c), req.IDs); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
