package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/response"
)

// AdminMeHandler 返回当前管理员的身份，前端据此决定是否显示写操作（插件 / 渠道的写接口只有 super_admin 能调）。
// 必须挂在 JWTAuth + RequireAdmin 之后。
type AdminMeHandler struct {
	lookup middleware.RoleLookup
}

// NewAdminMeHandler 创建当前管理员身份接口的 handler。
func NewAdminMeHandler(lookup middleware.RoleLookup) *AdminMeHandler {
	return &AdminMeHandler{lookup: lookup}
}

// MeView 是 GET /admin/ai/me 的响应。
type MeView struct {
	UserID uint64 `json:"user_id"`
	Role   string `json:"role"` // admin / super_admin
}

// Me 返回当前用户的 ID 与角色（实时查询，与 RequireAdmin 用的是同一个查询函数）。
func (h *AdminMeHandler) Me(c *gin.Context) {
	id := currentUserID(c)
	role, err := h.lookup(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, MeView{UserID: id, Role: role})
}
