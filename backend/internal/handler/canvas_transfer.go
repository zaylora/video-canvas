package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"video-canvas/internal/canvasdoc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

const (
	// importBodyMargin 是导入请求体在文件上限之外多留的余量，给 {"document": ...} 外壳和空白用。
	importBodyMargin = 1 << 20
	// canvasTransferTimeout 是导出 / 导入单次请求允许的读写总时长。
	// 全局 WriteTimeout 只有 60 秒，而导入要逐个复制最多 1 GB 素材、导出要为老素材补算哈希；
	// 写超时不会取消请求 ctx，超时后导入照样提交、只是响应写不出去，用户重试就会多出一张「（导入）」画布和一批重复素材。
	// 5 分钟足够复制 1 GB，并与前端为这两个接口放宽的 5 分钟请求超时一致（默认 2 分钟）
	canvasTransferTimeout = 5 * time.Minute
)

// relaxTransferDeadlines 放宽本次请求的读写超时；测试用的 ResponseRecorder 等不支持时返回 ErrNotSupported，忽略即可。
func relaxTransferDeadlines(c *gin.Context) time.Time {
	rc := http.NewResponseController(c.Writer)
	deadline := time.Now().Add(canvasTransferTimeout)
	if err := rc.SetReadDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		logger.Warn("放宽素材交付读超时失败", zap.Error(err))
	}
	if err := rc.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		logger.Warn("放宽素材交付写超时失败", zap.Error(err))
	}
	return deadline
}

// CanvasTransferHandler 处理画布的导出与导入。
type CanvasTransferHandler struct {
	svc *service.CanvasTransferService
}

// NewCanvasTransferHandler 创建画布导入导出 handler。
func NewCanvasTransferHandler(svc *service.CanvasTransferService) *CanvasTransferHandler {
	return &CanvasTransferHandler{svc: svc}
}

// Export 导出当前用户的一张画布为文档，素材以内容哈希引用、角色卡内联；读写超时放宽到 5 分钟。
func (h *CanvasTransferHandler) Export(c *gin.Context) {
	relaxTransferDeadlines(c)
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	userID := currentUserID(c)
	epochs, epochSet := c.GetQueryArray("epoch")
	revisions, revisionSet := c.GetQueryArray("revision")
	if epochSet || revisionSet {
		if len(epochs) != 1 || len(revisions) != 1 {
			response.Fail(c, errcode.ErrInvalidParams)
			return
		}
		epoch, epochErr := strconv.ParseUint(epochs[0], 10, 64)
		revision, revisionErr := strconv.ParseUint(revisions[0], 10, 64)
		if epochErr != nil || revisionErr != nil || epoch == 0 || revision == 0 {
			response.Fail(c, errcode.ErrInvalidParams)
			return
		}
		res, err := h.svc.ExportVersion(c.Request.Context(), userID, id, epoch, revision)
		collaborationReply(c, res, err)
		return
	}
	res, err := h.svc.Export(c.Request.Context(), userID, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Import 把导出文档还原成当前用户的一张新画布；读写超时放宽到 5 分钟，请求体超过文件上限加 1 MB 时直接截断并返回 30007。
func (h *CanvasTransferHandler) Import(c *gin.Context) {
	relaxTransferDeadlines(c)
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
