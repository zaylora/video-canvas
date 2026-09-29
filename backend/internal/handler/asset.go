package handler

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

const (
	// assetFormField 是前端上传时 multipart 里文件字段的名字（web/src/api/asset 里的 formData.append('file', ...)）。
	assetFormField = "file"
	// assetMultipartOverhead 是请求体上限在文件大小之外预留的 multipart 边界与其他字段开销。
	assetMultipartOverhead = 1 << 20
	// assetUploadTimeout 是单次上传允许的读写总时长。
	// 服务端全局 ReadTimeout 只有 10 秒，大文件一定会被掐断，所以上传路由单独放宽。
	assetUploadTimeout = 10 * time.Minute
)

type AssetHandler struct {
	svc *service.AssetService
}

func NewAssetHandler(svc *service.AssetService) *AssetHandler {
	return &AssetHandler{svc: svc}
}

// 上传素材：multipart/form-data，文件字段名为 file，响应为素材视图。
func (h *AssetHandler) Upload(c *gin.Context) {
	// 1. 放宽本次请求的读写超时（全局 ReadTimeout/WriteTimeout 对大文件上传太短）。
	//    测试用的 ResponseRecorder 等不支持时会返回 ErrNotSupported，忽略即可
	rc := http.NewResponseController(c.Writer)
	deadline := time.Now().Add(assetUploadTimeout)
	if err := rc.SetReadDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		logger.Warn("放宽上传读超时失败", zap.Error(err))
	}
	if err := rc.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		logger.Warn("放宽上传写超时失败", zap.Error(err))
	}

	// 2. 限制请求体大小：超过上限时读取会返回 *http.MaxBytesError，并让服务端关闭连接；
	//    声明的 Content-Length 已经超限就不必读了
	maxBody := h.svc.MaxUpload() + assetMultipartOverhead
	if c.Request.ContentLength > maxBody {
		response.Fail(c, errcode.ErrAssetTooLarge)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBody)

	// 3. 流式读取 multipart，找到 file 字段；不用 ParseMultipartForm，避免先把整个文件缓存一遍
	mr, err := c.Request.MultipartReader()
	if err != nil {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("请使用 multipart/form-data 上传文件"))
		return
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			response.Fail(c, errcode.ErrInvalidParams.WithMsg("请选择要上传的文件（字段名 file）"))
			return
		}
		if err != nil {
			response.Fail(c, uploadReadErr(err))
			return
		}
		if part.FormName() != assetFormField || part.FileName() == "" {
			continue // 跳过其他字段
		}

		// 4. 交给 service：类型嗅探、大小校验、写存储、入库
		view, err := h.svc.Upload(c.Request.Context(), currentUserID(c), service.UploadInput{
			FileName: part.FileName(),
			Size:     -1,
			Body:     part,
		})
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, view)
		return
	}
}

// 获取素材详情（URL 每次现生成，用于刷新过期的签名地址）
func (h *AssetHandler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	view, err := h.svc.View(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// uploadReadErr 把读取 multipart 的错误翻译成业务错误：超过请求体上限是 413，其余是 400。
func uploadReadErr(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return errcode.ErrAssetTooLarge
	}
	return errcode.ErrInvalidParams.WithMsg("上传内容格式错误，请重试")
}
