package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
)

// StageMediaDraft 接收加工产物，返回可用于确认挂载的暂存编号。
func (h *CanvasCollaborationHandler) StageMediaDraft(c *gin.Context) {
	canvas, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.CanvasMediaDraftReq
	if !BindQuery(c, &req) {
		return
	}
	req.ID = c.Param("draftId")
	limit := h.service.MediaDraftUploadLimit()
	if limit == 0 {
		response.Fail(c, errcode.ErrNotFound)
		return
	}
	if !limitAssetUpload(c, limit) {
		return
	}
	file, err := readAssetUpload(c)
	if err == nil {
		err = h.service.StageMediaDraft(c.Request.Context(), currentUserID(c), canvas, req, file)
	}
	collaborationReply(c, gin.H{"draftId": req.ID}, err)
}

// AttachMediaDraft 确认暂存产物，并返回持久化的协作收据。
func (h *CanvasCollaborationHandler) AttachMediaDraft(c *gin.Context) {
	canvas, ok := CanvasPathID(c)
	if !ok {
		return
	}
	limitCollaborationRequest(c, 1024)
	var req struct {
		Epoch uint64 `json:"epoch" binding:"required"`
	}
	if !BindJSON(c, &req) {
		return
	}
	result, err := h.service.AttachMediaDraft(c.Request.Context(), currentUserID(c), canvas, req.Epoch, c.Param("draftId"))
	collaborationReply(c, result, err)
}
