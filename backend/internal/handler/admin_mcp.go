package handler

import (
	"context"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
)

// MCPProjectAPI 是后台「MCP 接入」页依赖的业务，由 *service.MCPProjectService 实现。
type MCPProjectAPI interface {
	// List 列出全部项目，带令牌数量与最近使用时间。
	List(ctx context.Context) ([]*model.MCPProjectView, error)
	// Create 新建项目并签发首枚令牌（明文只在这一次响应里）。
	Create(ctx context.Context, adminID uint64, req *model.MCPProjectReq) (*model.MCPProjectCreated, error)
	// Update 修改项目（含启停）。
	Update(ctx context.Context, adminID, projectID uint64, req *model.MCPProjectReq) (*model.MCPProjectView, error)
	// Delete 删除项目与它的令牌。
	Delete(ctx context.Context, adminID, projectID uint64) error
	// IssueToken 生成或轮换项目那枚令牌，明文只返回这一次。
	IssueToken(ctx context.Context, adminID, projectID uint64) (*model.MCPIssuedToken, error)
	// TokenSecret 取回项目那枚令牌的明文。
	TokenSecret(ctx context.Context, callerID, projectID uint64) (string, error)
}

// MCPAdminHandler 是后台「MCP 接入」接口。
type MCPAdminHandler struct {
	svc MCPProjectAPI
}

// NewMCPAdminHandler 创建后台 MCP 接入 handler。
func NewMCPAdminHandler(svc MCPProjectAPI) *MCPAdminHandler { return &MCPAdminHandler{svc: svc} }

// List 返回全部项目。
func (h *MCPAdminHandler) List(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

// Create 新建项目。
func (h *MCPAdminHandler) Create(c *gin.Context) {
	var req model.MCPProjectReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Create(c.Request.Context(), adminID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Update 修改项目。
func (h *MCPAdminHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.MCPProjectReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.Update(c.Request.Context(), adminID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Delete 删除项目。
func (h *MCPAdminHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), adminID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// IssueToken 生成或轮换项目那枚令牌：一个项目一枚，明文只在这个响应里出现一次。
func (h *MCPAdminHandler) IssueToken(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, err := h.svc.IssueToken(c.Request.Context(), adminID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// TokenSecret 取回项目那枚令牌的明文：只给未注销、未过期、当初存了密文的令牌。
func (h *MCPAdminHandler) TokenSecret(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	token, err := h.svc.TokenSecret(c.Request.Context(), adminID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"token": token})
}

// adminID 取当前管理员 id：这些接口都挂在管理员中间件后面。
func adminID(c *gin.Context) uint64 { return uint64(middleware.GetUserID(c)) }
