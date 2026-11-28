package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/response"
)

// SubmitCompose accepts a versioned list of video nodes in playback order.
func (h *CanvasCollaborationHandler) SubmitCompose(c *gin.Context) {
	canvas, ok := CanvasPathID(c)
	if !ok {
		return
	}
	limitCollaborationRequest(c, 8*1024)
	var request model.CanvasComposeReq
	if !BindJSON(c, &request) {
		return
	}
	result, err := h.service.SubmitCompose(c.Request.Context(), currentUserID(c), canvas, request)
	collaborationReply(c, result, err)
}

// GetComposeJob returns the current state of the actor's composition.
func (h *CanvasCollaborationHandler) GetComposeJob(c *gin.Context) { h.composeJob(c, false) }

// CancelComposeJob cancels pending publication of the actor's composition.
func (h *CanvasCollaborationHandler) CancelComposeJob(c *gin.Context) { h.composeJob(c, true) }

func (h *CanvasCollaborationHandler) composeJob(c *gin.Context, cancel bool) {
	canvas, ok := CanvasPathID(c)
	if !ok {
		return
	}
	id, ok := idcodec.Decode(c.Param("jobId"))
	if !ok {
		response.Fail(c, errcode.ErrCanvasNotFound)
		return
	}
	var request struct {
		Epoch uint64 `form:"epoch" binding:"required"`
	}
	if !BindQuery(c, &request) {
		return
	}
	result, err := h.service.ComposeJob(c.Request.Context(), currentUserID(c), canvas, request.Epoch, id, cancel)
	collaborationReply(c, result, err)
}
