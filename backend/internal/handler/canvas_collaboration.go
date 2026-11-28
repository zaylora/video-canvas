package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/service"
)

// CanvasCollaborationHandler exposes the authenticated collaboration protocol.
type CanvasCollaborationHandler struct {
	service *service.CanvasCollaborationService
}

// NewCanvasCollaborationHandler binds the collaboration service to HTTP.
func NewCanvasCollaborationHandler(svc *service.CanvasCollaborationService) *CanvasCollaborationHandler {
	return &CanvasCollaborationHandler{service: svc}
}

// Mode sets the canvas's authoritative write mode.
func (h *CanvasCollaborationHandler) Mode(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req struct {
		Active   *bool  `json:"active" binding:"required"`
		Revision uint64 `json:"revision" binding:"required"`
		Preview  string `json:"preview" binding:"omitempty,len=64,hexadecimal"`
	}
	limitCollaborationRequest(c, 2048)
	if !BindJSON(c, &req) {
		return
	}
	result, err := h.service.SetMode(c.Request.Context(), currentUserID(c), id, req.Revision, *req.Active, req.Preview)
	collaborationReply(c, result, err)
}

// ModePreview 返回协作模式切换前需要确认的成员范围。
func (h *CanvasCollaborationHandler) ModePreview(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var query struct {
		Active *bool `form:"active" binding:"required"`
	}
	if !BindQuery(c, &query) {
		return
	}
	result, err := h.service.ModePreview(c.Request.Context(), currentUserID(c), id, *query.Active)
	collaborationReply(c, result, err)
}

// Manage changes an owned canvas at the revision confirmed by the caller.
func (h *CanvasCollaborationHandler) Manage(c *gin.Context) {
	canvas, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.CanvasManagementReq
	limitCollaborationRequest(c, 2048)
	if !BindJSON(c, &req) {
		return
	}
	project, err := h.service.Manage(c.Request.Context(), currentUserID(c), canvas, req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"id": idcodec.Encode(canvas), "revision": project.Revision, "title": project.Title, "archived": req.Archive})
}

// ArchivePreview returns the owner-confirmed impact summary for archiving.
func (h *CanvasCollaborationHandler) ArchivePreview(c *gin.Context) {
	canvas, ok := CanvasPathID(c)
	if !ok {
		return
	}
	preview, err := h.service.ArchivePreview(c.Request.Context(), currentUserID(c), canvas)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, preview)
}

// Member invites a collaborator or changes an existing collaborator's role.
func (h *CanvasCollaborationHandler) Member(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req struct {
		Email string `json:"email" binding:"required,email,max=128"`
		Role  string `json:"role" binding:"required,oneof=editor viewer"`
	}
	limitCollaborationRequest(c, 2048)
	if !BindJSON(c, &req) {
		return
	}
	if err := h.service.SetMember(c.Request.Context(), currentUserID(c), id, req.Email, req.Role); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// MemberGeneration 设置指定编辑成员的生成授权。
func (h *CanvasCollaborationHandler) MemberGeneration(c *gin.Context) {
	h.memberPermission(c, h.service.SetGenerationPermission)
}

// MemberLibrary 设置指定编辑成员的个人收藏授权。
func (h *CanvasCollaborationHandler) MemberLibrary(c *gin.Context) {
	h.memberPermission(c, h.service.SetLibraryPermission)
}

func (h *CanvasCollaborationHandler) memberPermission(c *gin.Context, set func(context.Context, uint64, uint64, uint64, bool) error) {
	canvas, ok := CanvasPathID(c)
	if !ok {
		return
	}
	member, ok := idcodec.Decode(c.Param("member"))
	if !ok || member == 0 {
		response.Fail(c, errcode.ErrInvalidParams)
		return
	}
	var req struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	limitCollaborationRequest(c, 1024)
	if !BindJSON(c, &req) {
		return
	}
	if err := set(c.Request.Context(), currentUserID(c), canvas, member, *req.Enabled); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// CopyToLibrary 将协作画布的素材加入当前用户的资产库。
func (h *CanvasCollaborationHandler) CopyToLibrary(c *gin.Context) {
	canvas, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.CanvasLibraryCopyReq
	limitCollaborationRequest(c, 4096)
	if !BindJSON(c, &req) {
		return
	}
	if err := h.service.CopyToLibrary(c.Request.Context(), currentUserID(c), canvas, req); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// Snapshot opens a session and scopes the media cookie to the requested canvas.
func (h *CanvasCollaborationHandler) Snapshot(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := CanvasPathID(c)
		if !ok {
			return
		}
		result, err := h.service.Snapshot(c.Request.Context(), currentUserID(c), id)
		if err != nil {
			response.Fail(c, err)
			return
		}
		_, scoped := model.CanvasAutomationFromContext(c.Request.Context())
		if !scoped && result.Role != model.CanvasRoleOwner && !issueCanvasMediaCookie(c, secret) {
			return
		}
		collaborationReply(c, result, nil)
	}
}

// Commit submits an atomic batch of editing operations.
func (h *CanvasCollaborationHandler) Commit(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.CanvasCollaborationBatch
	limitCollaborationRequest(c, model.CanvasCollaborationMaxRequestBytes)
	if !BindJSON(c, &req) {
		return
	}
	ctx := c.Request.Context()
	if key, scoped := model.CanvasAutomationFromContext(ctx); scoped && !key.CanEdit() {
		response.Fail(c, errcode.ErrForbidden)
		return
	}
	start := time.Now()
	result, err := h.service.Commit(ctx, currentUserID(c), id, req)
	if err == nil {
		logger.Ctx(ctx).Debug("画布协作提交", zap.Uint64("canvas_id", id), zap.Uint64("revision", result.Revision),
			zap.Int("changes", len(result.Changes)), zap.Int("conflicts", len(result.Conflicts)), zap.Duration("elapsed", time.Since(start)))
	}
	collaborationReply(c, result, err)
}

// ReceiptStatus 查询当前编辑者自己的操作结果摘要。
func (h *CanvasCollaborationHandler) ReceiptStatus(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var query struct {
		Epoch uint64 `form:"epoch" binding:"required"`
	}
	if !BindQuery(c, &query) {
		return
	}
	result, err := h.service.ReceiptStatus(c.Request.Context(), currentUserID(c), id, query.Epoch, c.Param("opId"))
	collaborationReply(c, result, err)
}

// Undo 撤销调用者最近确认的协作编辑。
func (h *CanvasCollaborationHandler) Undo(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.CanvasCollaborationUndo
	limitCollaborationRequest(c, 1024)
	if !BindJSON(c, &req) {
		return
	}
	result, err := h.service.Undo(c.Request.Context(), currentUserID(c), id, req)
	collaborationReply(c, result, err)
}

// Replay catches a session up from its last confirmed revision.
func (h *CanvasCollaborationHandler) Replay(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var query struct {
		Epoch uint64 `form:"epoch" binding:"required"`
		After uint64 `form:"after"`
		Limit int    `form:"limit" binding:"omitempty,min=1,max=100"`
	}
	if !BindQuery(c, &query) {
		return
	}
	if query.Limit == 0 {
		query.Limit = 50
	}
	ctx := c.Request.Context()
	start := time.Now()
	result, err := h.service.Replay(ctx, currentUserID(c), id, query.Epoch, query.After, query.Limit)
	if err == nil {
		logger.Ctx(ctx).Debug("画布协作对账", zap.Uint64("canvas_id", id), zap.Uint64("revision", result.Revision),
			zap.Int("replay_events", len(result.Events)), zap.Bool("snapshot_required", result.SnapshotRequired),
			zap.Duration("elapsed", time.Since(start)))
	}
	collaborationReply(c, result, err)
}

// Comments 返回节点的当前协作讨论及成员候选。
func (h *CanvasCollaborationHandler) Comments(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var query struct {
		Epoch  uint64 `form:"epoch" binding:"required"`
		NodeID string `form:"nodeId" binding:"required,max=128"`
		Before string `form:"before" binding:"omitempty,max=64"`
	}
	if !BindQuery(c, &query) {
		return
	}
	var before uint64
	if query.Before != "" {
		before, ok = idcodec.Decode(query.Before)
		if !ok || before == 0 || idcodec.Encode(before) != query.Before {
			response.Fail(c, errcode.ErrInvalidParams)
			return
		}
	}
	result, err := h.service.Comments(c.Request.Context(), currentUserID(c), id, query.Epoch, query.NodeID, before)
	collaborationReply(c, result, err)
}

// CommentEvents 返回当前编辑成员可读取的评论增量。
func (h *CanvasCollaborationHandler) CommentEvents(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var query struct {
		Epoch uint64 `form:"epoch" binding:"required"`
		After string `form:"after" binding:"omitempty,max=64"`
	}
	if !BindQuery(c, &query) {
		return
	}
	var after *uint64
	if c.Request.URL.Query().Has("after") {
		var cursor uint64
		if query.After != "0" {
			cursor, ok = idcodec.Decode(query.After)
			if !ok || cursor == 0 || idcodec.Encode(cursor) != query.After {
				response.Fail(c, errcode.ErrInvalidParams)
				return
			}
		}
		after = &cursor
	}
	c.Header("Cache-Control", "private, no-store")
	result, err := h.service.ReplayComments(c.Request.Context(), currentUserID(c), id, query.Epoch, after)
	collaborationReply(c, result, err)
}

// Mentions 返回当前协作者可读取的提及提醒。
func (h *CanvasCollaborationHandler) Mentions(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var query struct {
		Epoch uint64 `form:"epoch" binding:"required"`
		After string `form:"after" binding:"omitempty,max=64"`
	}
	if !BindQuery(c, &query) {
		return
	}
	var after uint64
	if query.After != "" {
		after, ok = idcodec.Decode(query.After)
		if !ok || after == 0 || idcodec.Encode(after) != query.After {
			response.Fail(c, errcode.ErrInvalidParams)
			return
		}
	}
	c.Header("Cache-Control", "private, no-store")
	items, err := h.service.Mentions(c.Request.Context(), currentUserID(c), id, query.Epoch, after)
	collaborationReply(c, gin.H{"items": items}, err)
}

// AddComment 提交当前编辑成员的节点评论。
func (h *CanvasCollaborationHandler) AddComment(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.CanvasCommentRequest
	limitCollaborationRequest(c, 16*1024)
	if !BindJSON(c, &req) {
		return
	}
	result, err := h.service.AddComment(c.Request.Context(), currentUserID(c), id, req)
	collaborationReply(c, result, err)
}

// History 返回协作会话的已确认版本活动。
func (h *CanvasCollaborationHandler) History(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var query struct {
		Epoch  uint64 `form:"epoch" binding:"required"`
		Before uint64 `form:"before"`
	}
	if !BindQuery(c, &query) {
		return
	}
	result, err := h.service.History(c.Request.Context(), currentUserID(c), id, query.Epoch, query.Before)
	collaborationReply(c, result, err)
}

// HistoricalSnapshot 返回所有者选定版本的确认图谱。
func (h *CanvasCollaborationHandler) HistoricalSnapshot(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	revision, err := strconv.ParseUint(c.Param("revision"), 10, 64)
	if err != nil || revision == 0 {
		response.Fail(c, errcode.ErrInvalidParams)
		return
	}
	var query struct {
		Epoch uint64 `form:"epoch" binding:"required"`
	}
	if !BindQuery(c, &query) {
		return
	}
	result, err := h.service.HistoricalSnapshot(c.Request.Context(), currentUserID(c), id, query.Epoch, revision)
	collaborationReply(c, result, err)
}

// CopyHistoricalVersion 从历史版本另建个人画布。
func (h *CanvasCollaborationHandler) CopyHistoricalVersion(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	revision, err := strconv.ParseUint(c.Param("revision"), 10, 64)
	if err != nil || revision == 0 {
		response.Fail(c, errcode.ErrInvalidParams)
		return
	}
	var req struct {
		Epoch uint64 `json:"epoch" binding:"required"`
		OpID  string `json:"opId" binding:"required,max=55"`
	}
	limitCollaborationRequest(c, 1024)
	if !BindJSON(c, &req) {
		return
	}
	result, err := h.service.CopyHistoricalVersion(c.Request.Context(), currentUserID(c), id, req.Epoch, revision, req.OpID)
	collaborationReply(c, result, err)
}

func issueCanvasMediaCookie(c *gin.Context, secret string) bool {
	id := c.Param("id")
	ticket, _, err := utils.GenerateToken(middleware.GetUserID(c), "", middleware.GetTokenVersion(c), secret+":"+id, "canvas-media", 1)
	if err != nil {
		response.Fail(c, errcode.ErrInternal)
		return false
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("canvas_media", ticket, 3600, "/api/v1/canvas-media/"+id+"/", "", c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https", true)
	return true
}

func limitCollaborationRequest(c *gin.Context, limit int64) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
}

func collaborationReply(c *gin.Context, result any, err error) {
	c.Header("Cache-Control", "private, no-store")
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, result)
}
