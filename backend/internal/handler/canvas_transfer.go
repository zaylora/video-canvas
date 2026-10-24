package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/canvasdoc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// importBodyMargin 是导入请求体在文件上限之外多留的余量，给 {"document": ...} 外壳和空白用。
const importBodyMargin = 1 << 20

// CanvasTransferHandler 处理画布的导出与导入。
type CanvasTransferHandler struct {
	svc *service.CanvasTransferService
}

// NewCanvasTransferHandler 创建画布导入导出 handler。
func NewCanvasTransferHandler(svc *service.CanvasTransferService) *CanvasTransferHandler {
	return &CanvasTransferHandler{svc: svc}
}

// Export 导出当前用户的一张画布为文档，素材以内容哈希引用、角色卡内联。
func (h *CanvasTransferHandler) Export(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	res, err := h.svc.Export(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Import 把导出文档还原成当前用户的一张新画布；请求体超过文件上限加 1 MB 时直接截断并返回 30007。
func (h *CanvasTransferHandler) Import(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, canvasdoc.DefaultLimits().MaxBytes+importBodyMargin)
	var req model.ImportCanvasReq
	if err := c.ShouldBindJSON(&req); err != nil {
		// 读取被截断不是参数格式问题，要和普通的格式错误区分开
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.Fail(c, errcode.ErrCanvasImportTooLarge)
			return
		}
		bindWith(c, err)
		return
	}
	res, err := h.svc.Import(c.Request.Context(), currentUserID(c), req.Document)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}
