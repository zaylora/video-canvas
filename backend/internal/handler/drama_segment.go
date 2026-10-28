package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// DramaSegmentHandler 提供片段编辑与生成任务接口。
type DramaSegmentHandler struct{ svc *service.DramaSegmentService }

// NewDramaSegmentHandler 创建片段HTTP入口。
func NewDramaSegmentHandler(svc *service.DramaSegmentService) *DramaSegmentHandler {
	return &DramaSegmentHandler{svc: svc}
}

// List 按seq返回用户分集的完整片段视图。
func (h *DramaSegmentHandler) List(c *gin.Context) {
	id, no, ok := dramaEpisodePath(c)
	if !ok {
		return
	}
	items, err := h.svc.List(c.Request.Context(), currentUserID(c), id, no)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

// Create 在分集指定位置新增片段，返回201。
func (h *DramaSegmentHandler) Create(c *gin.Context) {
	id, no, ok := dramaEpisodePath(c)
	if !ok {
		return
	}
	var req model.CreateDramaSegmentReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Create(c.Request.Context(), currentUserID(c), id, no, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, v)
}

// Update 按revision部分更新片段，返回完整视图。
func (h *DramaSegmentHandler) Update(c *gin.Context) {
	sid, ok := dramaSegmentPath(c)
	if !ok {
		return
	}
	var req model.UpdateDramaSegmentReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Update(c.Request.Context(), currentUserID(c), sid, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Delete 软删片段，生成任务和素材保留，成功返回204。
func (h *DramaSegmentHandler) Delete(c *gin.Context) {
	sid, ok := dramaSegmentPath(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), currentUserID(c), sid); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// Order 按完整ID集合重排某集片段。
func (h *DramaSegmentHandler) Order(c *gin.Context) {
	id, no, ok := dramaEpisodePath(c)
	if !ok {
		return
	}
	var req model.OrderDramaSegmentsReq
	if !BindJSON(c, &req) {
		return
	}
	items, err := h.svc.Order(c.Request.Context(), currentUserID(c), id, no, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

// Generate 固定video种类提交任务并转发Idempotency-Key。
func (h *DramaSegmentHandler) Generate(c *gin.Context) {
	h.generate(c, false)
}

// Keyframe 固定image种类提交首帧生成任务。
func (h *DramaSegmentHandler) Keyframe(c *gin.Context) {
	h.generate(c, true)
}

// generate 绑定共用生成请求，任务逐项错误在service转换为标准业务响应。
func (h *DramaSegmentHandler) generate(c *gin.Context, keyframe bool) {
	sid, ok := dramaSegmentPath(c)
	if !ok {
		return
	}
	var req model.GenerateDramaSegmentReq
	if !BindJSON(c, &req) {
		return
	}
	call := h.svc.Generate
	if keyframe {
		call = h.svc.Keyframe
	}
	task, err := call(c.Request.Context(), currentUserID(c), sid, c.GetHeader("Idempotency-Key"), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Accepted(c, gin.H{"task": task})
}

// dramaSegmentPath 校验并解码片段路径ID，非法编码统一为参数错误。
func dramaSegmentPath(c *gin.Context) (uint64, bool) {
	sid, ok := idcodec.Decode(c.Param("sid"))
	if !ok {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("片段id格式错误"))
	}
	return sid, ok
}
