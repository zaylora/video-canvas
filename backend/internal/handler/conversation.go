package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// ConversationHandler 首页生成的对话与记录接口。对话和记录的 id 都是十六进制串。
type ConversationHandler struct {
	svc *service.ConversationService
}

// NewConversationHandler 创建对话与记录接口。
func NewConversationHandler(svc *service.ConversationService) *ConversationHandler {
	return &ConversationHandler{svc: svc}
}

// convRecordURI 是 /:id/records/:rid 的路径参数。
type convRecordURI struct {
	ID  string `uri:"id" binding:"required" label:"id"`
	RID string `uri:"rid" binding:"required" label:"rid"`
}

// deleteRecordQuery 是删除记录的查询参数。
type deleteRecordQuery struct {
	CancelActive bool `form:"cancel_active" label:"取消进行中的任务"` // true 时先取消记录里进行中的任务并退回积分
}

// List 对话列表：默认创作永远在第一条
func (h *ConversationHandler) List(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context(), currentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, items)
}

// Create 新建对话
func (h *ConversationHandler) Create(c *gin.Context) {
	var req model.CreateConversationReq
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

// Rename 重命名对话
func (h *ConversationHandler) Rename(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.RenameConversationReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.svc.Rename(c.Request.Context(), currentUserID(c), id, req.Title); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// Delete 删除对话（软删除）；默认创作不能删
func (h *ConversationHandler) Delete(c *gin.Context) {
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

// ListRecords 记录分页：?before=上一页返回的 next&limit=20，每页按新到旧
func (h *ConversationHandler) ListRecords(c *gin.Context) {
	convID, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.ListConversationRecordsReq
	if !BindQuery(c, &req) {
		return
	}
	var before uint64
	if req.Before != "" {
		if before, ok = idcodec.Decode(req.Before); !ok {
			response.Fail(c, errcode.ErrInvalidParams.WithMsg("before 格式错误"))
			return
		}
	}
	page, err := h.svc.ListRecords(c.Request.Context(), currentUserID(c), convID, &req, before)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, page)
}

// Submit 提交一条生成记录：:id 可以是 default（默认创作）、new（新建一段对话）或对话 id；
// 请求头 Idempotency-Key 保证重复提交只创建一条记录；返回 202 + 记录和任务快照
func (h *ConversationHandler) Submit(c *gin.Context) {
	target, ok := submitTarget(c)
	if !ok {
		return
	}
	var req model.SubmitConversationRecordReq
	if !BindJSON(c, &req) {
		return
	}
	resp, err := h.svc.Submit(c.Request.Context(), currentUserID(c), target, c.GetHeader("Idempotency-Key"), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Accepted(c, resp)
}

// DeleteRecord 删除一条记录：?cancel_active=true 时先取消其中进行中的任务
func (h *ConversationHandler) DeleteRecord(c *gin.Context) {
	var uri convRecordURI
	if !bindURI(c, &uri) {
		return
	}
	convID, ok1 := idcodec.Decode(uri.ID)
	recID, ok2 := idcodec.Decode(uri.RID)
	if !ok1 || !ok2 {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("id 格式错误"))
		return
	}
	var q deleteRecordQuery
	if !BindQuery(c, &q) {
		return
	}
	if err := h.svc.DeleteRecord(c.Request.Context(), currentUserID(c), convID, recID, q.CancelActive); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// submitTarget 解析提交目标：default / new / 十六进制对话 id，其余按参数错误返回。
func submitTarget(c *gin.Context) (service.ConversationTarget, bool) {
	var uri canvasURI
	if !bindURI(c, &uri) {
		return service.ConversationTarget{}, false
	}
	switch uri.ID {
	case "default":
		return service.ConversationTarget{Default: true}, true
	case "new":
		return service.ConversationTarget{New: true}, true
	}
	id, ok := idcodec.Decode(uri.ID)
	if !ok {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("id 格式错误"))
		return service.ConversationTarget{}, false
	}
	return service.ConversationTarget{ID: id}, true
}
