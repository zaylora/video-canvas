package handler

import (
	"strings"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/response"
)

// CanvasAutomationAuth authenticates scoped routes without accepting a login token.
func (h *CanvasCollaborationHandler) CanvasAutomationAuth(c *gin.Context) {
	canvas, ok := CanvasPathID(c)
	if !ok {
		c.Abort()
		return
	}
	method, token, found := strings.Cut(c.GetHeader("Authorization"), " ")
	if !found || !strings.EqualFold(method, "Bearer") {
		response.Fail(c, errcode.ErrUnauthorized)
		c.Abort()
		return
	}
	ctx := c.Request.Context()
	key, err := h.service.AuthenticateAutomationKey(ctx, canvas, strings.TrimSpace(token))
	if err != nil {
		response.Fail(c, err)
		c.Abort()
		return
	}
	c.Set(middleware.CtxUserIDKey, uint(key.UserID))
	c.Set(middleware.CtxTokenVersionKey, key.TokenVersion)
	c.Request = c.Request.WithContext(model.WithCanvasAutomation(logger.WithUser(ctx, key.UserID), key))
	c.Next()
}

// IssueAutomationKey authenticates credential issuance using the normal account session.
func (h *CanvasCollaborationHandler) IssueAutomationKey(c *gin.Context) {
	canvas, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var request struct {
		Epoch         uint64 `json:"epoch" binding:"required"`
		Scope         string `json:"scope" binding:"required,oneof=read edit generate"`
		Hours         int    `json:"hours" binding:"required,min=1,max=168"`
		BudgetCredits *int   `json:"budgetCredits" binding:"omitempty,min=1,max=100000"`
	}
	limitCollaborationRequest(c, 1024)
	if !BindJSON(c, &request) {
		return
	}
	key := model.CanvasAutomationKey{CanvasID: canvas, UserID: currentUserID(c), Epoch: request.Epoch, Scope: request.Scope,
		TokenVersion: middleware.GetTokenVersion(c), BudgetCredits: request.BudgetCredits}
	view, err := h.service.IssueAutomationKey(c.Request.Context(), key, request.Hours)
	c.Header("Cache-Control", "no-store")
	collaborationReply(c, view, err)
}

// RequireAutomationGeneration checks the current canvas and paid credential before task access.
func (h *CanvasCollaborationHandler) RequireAutomationGeneration(c *gin.Context) {
	ctx := c.Request.Context()
	key, ok := model.CanvasAutomationFromContext(ctx)
	if !ok || key.Scope != model.CanvasAutomationGenerate {
		response.Fail(c, errcode.ErrForbidden)
		c.Abort()
		return
	}
	if err := h.service.AuthorizeAutomationGeneration(ctx, key); err != nil {
		response.Fail(c, err)
		c.Abort()
		return
	}
	c.Next()
}

// AutomationKeys lists the caller's keys without their secrets.
func (h *CanvasCollaborationHandler) AutomationKeys(c *gin.Context) {
	canvas, ok := CanvasPathID(c)
	if !ok {
		return
	}
	keys, err := h.service.AutomationKeys(c.Request.Context(), currentUserID(c), canvas)
	collaborationReply(c, keys, err)
}

// RevokeAutomationKey deletes a key's authorization without revealing its secret.
func (h *CanvasCollaborationHandler) RevokeAutomationKey(c *gin.Context) {
	canvas, ok := CanvasPathID(c)
	if !ok {
		return
	}
	id, ok := idcodec.Decode(c.Param("keyId"))
	if !ok {
		response.Fail(c, errcode.ErrInvalidParams)
		return
	}
	if err := h.service.RevokeAutomationKey(c.Request.Context(), currentUserID(c), canvas, id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}
