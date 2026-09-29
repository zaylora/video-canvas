package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/provider"
	"video-canvas/internal/pkg/response"
)

// AIModelHandler 面向画布的模型清单接口。
type AIModelHandler struct {
	registry provider.Registry
}

func NewAIModelHandler(registry provider.Registry) *AIModelHandler {
	return &AIModelHandler{registry: registry}
}

// listModelsQuery 是 GET /models 的查询参数。
type listModelsQuery struct {
	Kind string `form:"kind" binding:"omitempty,oneof=video image audio" label:"种类"`
}

// List 返回已发布且上架的模型清单（可按 kind 过滤）。
// 只返回 provider.ModelInfo 的公开字段（key / kind / label / hint / credits / input_schema），
// 不含 params / mapping / provider / 凭证等内部配置——这是它的类型保证，不依赖手工过滤。
func (h *AIModelHandler) List(c *gin.Context) {
	var q listModelsQuery
	if !bindQuery(c, &q) {
		return
	}
	models, err := h.registry.ListModels(c.Request.Context(), q.Kind)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, models)
}
