package agent

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/pkg/response"
)

// SetDramaSteps 更新技能的短剧适用环节，管理员权限由路由中间件保证。
func (h *SkillHandler) SetDramaSteps(c *gin.Context) {
	var req struct {
		Steps []string `json:"steps" binding:"required,max=4,dive,oneof=adapt assets asset_prompts video"`
	}
	if !handler.BindJSON(c, &req) {
		return
	}
	v, err := h.svc.SetDramaSteps(c.Request.Context(), actorID(c), skillName(c), req.Steps)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}
