package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/service"
)

// AdminPluginHandler 是协议插件的管理接口：列表（admin 可读），上传 / 启停 / 删除版本 / 删除插件及其预检（仅 super_admin，由路由挂中间件）。
type AdminPluginHandler struct {
	svc *service.AIPluginService
}

// NewAdminPluginHandler 创建插件管理接口的 handler。
func NewAdminPluginHandler(svc *service.AIPluginService) *AdminPluginHandler {
	return &AdminPluginHandler{svc: svc}
}

// uploadBodyLimit 是上传请求体的总上限：插件文件上限加上 multipart 边界与字段头的余量。
// 超过它的请求在读取阶段就被截断，不会把一个巨大的请求体读进内存。
const uploadBodyLimit = pluginmeta.MaxPluginBytes + 64<<10

// pluginVersionURI 是 /plugins/:key/versions/:version 的路径参数。
type pluginVersionURI struct {
	Key     string `uri:"key" binding:"required,max=30" label:"插件 key"`
	Version string `uri:"version" binding:"required,max=32" label:"版本号"`
}

// List 返回所有插件与它们的版本。
func (h *AdminPluginHandler) List(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Upload 上传插件文件（multipart，字段 file）。预检不通过也返回 200，问题在响应的 issues 里。
func (h *AdminPluginHandler) Upload(c *gin.Context) {
	// 1. 先限制请求体总大小，再取文件字段；超限（读取时被截断）统一返回 413
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, uploadBodyLimit)
	fh, err := c.FormFile("file")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.Fail(c, errcode.ErrPluginTooLarge)
			return
		}
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("请以 multipart/form-data 上传插件文件（字段名 file）"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("无法读取上传的文件"))
		return
	}
	defer f.Close()
	// 2. 多读一个字节：让 service 能分辨“刚好达到上限”和“超限”
	code, err := io.ReadAll(io.LimitReader(f, pluginmeta.MaxPluginBytes+1))
	if err != nil {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("无法读取上传的文件"))
		return
	}
	res, err := h.svc.Upload(c.Request.Context(), currentUserID(c), code)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// SetEnabled 启用 / 停用插件。
func (h *AdminPluginHandler) SetEnabled(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	var req setEnabledReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.svc.SetEnabled(c.Request.Context(), currentUserID(c), key, *req.Enabled); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// DeleteVersion 删除一个未被引用的插件版本。
func (h *AdminPluginHandler) DeleteVersion(c *gin.Context) {
	var uri pluginVersionURI
	if !bindURI(c, &uri) {
		return
	}
	if err := h.svc.DeleteVersion(c.Request.Context(), currentUserID(c), uri.Key, uri.Version); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// DeleteCheck 删除整个插件前的预检：返回阻断删除的原因，blockers 为空数组表示可以删。
func (h *AdminPluginHandler) DeleteCheck(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	res, err := h.svc.CheckDelete(c.Request.Context(), key)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Delete 删除整个插件（全部版本），只能删未被引用的上传插件。
func (h *AdminPluginHandler) Delete(c *gin.Context) {
	key, ok := pathKey(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), currentUserID(c), key); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
