package handler

import "github.com/gin-gonic/gin"

// TaskSummaries 提供编辑成员可读取的当前协作任务摘要。
func (h *CanvasCollaborationHandler) TaskSummaries(c *gin.Context) {
	canvas, ok := CanvasPathID(c)
	if !ok {
		return
	}
	req := struct {
		Epoch uint64 `form:"epoch" binding:"required"`
		Limit int    `form:"limit" binding:"min=1,max=100"`
	}{Limit: 50}
	if !BindQuery(c, &req) {
		return
	}
	items, err := h.service.TaskSummaries(c.Request.Context(), currentUserID(c), canvas, req.Epoch, req.Limit)
	collaborationReply(c, gin.H{"items": items}, err)
}
