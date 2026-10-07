package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

type CanvasProjectHandler struct {
	svc *service.CanvasProjectService
}

func NewCanvasProjectHandler(svc *service.CanvasProjectService) *CanvasProjectHandler {
	return &CanvasProjectHandler{svc: svc}
}

// 创建画布
func (h *CanvasProjectHandler) Create(c *gin.Context) {
	var req model.CreateCanvasProjectReq
	if !BindJSON(c, &req) {
		return
	}
	p, err := h.svc.Create(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, p.View())
}

// 画布列表（不含 payload_json）
func (h *CanvasProjectHandler) List(c *gin.Context) {
	var req model.ListCanvasProjectReq
	if !BindQuery(c, &req) {
		return
	}
	items, total, q, err := h.svc.List(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OKPage(c, items, total, q.Page, q.PageSize)
}

// 画布详情（含完整 payload_json）
func (h *CanvasProjectHandler) Get(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	p, err := h.svc.Get(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, p.View())
}

// 更新画布，需带上当前 revision，版本不一致返回 409
func (h *CanvasProjectHandler) Update(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.UpdateCanvasProjectReq
	if !BindJSON(c, &req) {
		return
	}
	p, err := h.svc.Update(c.Request.Context(), currentUserID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, p.View())
}

// 删除画布（软删除）
func (h *CanvasProjectHandler) Delete(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), currentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// 获取当前的用户 ID
func currentUserID(c *gin.Context) uint64 {
	return uint64(middleware.GetUserID(c))
}
