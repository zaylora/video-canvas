package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/marketing/format"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
)

// AdminFormatterHandler 是后台「视频提示词格式化器」只读列表（GET /admin/formatters）：代码维护，这里只展示。
type AdminFormatterHandler struct{}

// NewAdminFormatterHandler 创建格式化器列表 handler。
func NewAdminFormatterHandler() *AdminFormatterHandler { return &AdminFormatterHandler{} }

// List 返回全部格式化器：key、名称、说明和用内置示例渲染出的样例稿。
func (h *AdminFormatterHandler) List(c *gin.Context) {
	all := format.All()
	out := make([]model.FormatterView, 0, len(all))
	for _, f := range all {
		out = append(out, model.FormatterView{Key: f.Key(), Name: f.Name(), Description: f.Description(), Sample: format.SampleOf(f)})
	}
	response.OK(c, out)
}
