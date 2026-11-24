package handler

import (
	"context"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
)

// MCPSelfAPI 是「我的 MCP 接入」页依赖的业务，由 *service.MCPProjectService 实现。
//
// 与后台那套的区别：这里的归属用户永远是当前登录用户，项目不是自己的就按「不存在」回。
// 任何用户都能给自己开接入，不必先找管理员建项目。
type MCPSelfAPI interface {
	// OwnedList 列出自己的项目。
	OwnedList(ctx context.Context, ownerID uint64) ([]*model.MCPProjectView, error)
	// OwnedCreate 给自己建项目，并签发首枚令牌（明文只在这一次响应里）。
	OwnedCreate(ctx context.Context, ownerID uint64, req *model.MCPProjectReq) (*model.MCPProjectCreated, error)
	// OwnedUpdate 改自己的项目（含启停）。
	OwnedUpdate(ctx context.Context, ownerID, projectID uint64, req *model.MCPProjectReq) (*model.MCPProjectView, error)
	// OwnedDelete 删自己的项目与它的令牌。
	OwnedDelete(ctx context.Context, ownerID, projectID uint64) error
	// OwnedIssueToken 给自己项目生成（或轮换）那枚令牌，明文只返回这一次。
	OwnedIssueToken(ctx context.Context, ownerID, projectID uint64) (*model.MCPIssuedToken, error)
	// OwnedTokenSecret 取回自己项目那枚令牌的明文。
	OwnedTokenSecret(ctx context.Context, ownerID, projectID uint64) (string, error)
}

// MCPSelfHandler 是「我的 MCP 接入」接口：挂在登录态下，只操作当前用户自己的数据。
type MCPSelfHandler struct {
	svc MCPSelfAPI
}

// NewMCPSelfHandler 创建用户自助 MCP handler。
func NewMCPSelfHandler(svc MCPSelfAPI) *MCPSelfHandler { return &MCPSelfHandler{svc: svc} }

// List 列出自己的接入项目。
func (h *MCPSelfHandler) List(c *gin.Context) {
	items, err := h.svc.OwnedList(c.Request.Context(), selfUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

// Create 新建接入项目，归属固定为自己。
func (h *MCPSelfHandler) Create(c *gin.Context) {
	var req model.MCPProjectReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.OwnedCreate(c.Request.Context(), selfUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Update 修改自己的项目。
func (h *MCPSelfHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.MCPProjectReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.OwnedUpdate(c.Request.Context(), selfUserID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Delete 删除自己的项目。
func (h *MCPSelfHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.OwnedDelete(c.Request.Context(), selfUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// IssueToken 给自己项目生成（或轮换）那枚令牌：一个项目一枚，明文只在这个响应里出现一次。
func (h *MCPSelfHandler) IssueToken(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, err := h.svc.OwnedIssueToken(c.Request.Context(), selfUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// TokenSecret 取回自己项目那枚令牌的明文。
func (h *MCPSelfHandler) TokenSecret(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	token, err := h.svc.OwnedTokenSecret(c.Request.Context(), selfUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"token": token})
}

// selfUserID 取当前登录用户 id：这些接口都挂在登录中间件后面。
func selfUserID(c *gin.Context) uint64 { return uint64(middleware.GetUserID(c)) }
