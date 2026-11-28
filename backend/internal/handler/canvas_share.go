package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/provider"
	"video-canvas/internal/service"
	"video-canvas/pkg/pagination"
)

// CanvasShareHandler exposes canvas sharing endpoints.
type CanvasShareHandler struct{ service *service.CanvasShareService }

// NewCanvasShareHandler wires the sharing service into HTTP handlers.
func NewCanvasShareHandler(s *service.CanvasShareService) *CanvasShareHandler {
	return &CanvasShareHandler{service: s}
}

// Share grants view access, creates a copy, or transfers a canvas.
func (h *CanvasShareHandler) Share(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	var req model.CanvasShareReq
	if !BindJSON(c, &req) {
		return
	}
	ctx := c.Request.Context()
	if req.Mode != "view" {
		deadline := relaxTransferDeadlines(c)
		// 给失败后的素材回收和 HTTP 响应留出时间。
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline.Add(-30*time.Second))
		defer cancel()
	}
	if err := h.service.Share(ctx, currentUserID(c), id, req); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = errcode.ErrInternal.WithMsg("画布交付超时，请重试")
		}
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// Receipt returns only the sending user's status for this canvas and request.
func (h *CanvasShareHandler) Receipt(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	status, err := h.service.Receipt(c.Request.Context(), currentUserID(c), id, c.Param("key"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.OK(c, gin.H{"status": status})
}

// Members lists viewers for a canvas owned by the current user.
func (h *CanvasShareHandler) Members(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	items, err := h.service.Members(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, items)
}

// Remove revokes a viewer's access or lets the viewer leave.
func (h *CanvasShareHandler) Remove(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	member := currentUserID(c)
	if raw := c.Param("member"); raw != "me" {
		member, ok = idcodec.Decode(raw)
		if !ok {
			response.Fail(c, errcode.ErrInvalidParams)
			return
		}
	}
	if err := h.service.Remove(c.Request.Context(), currentUserID(c), id, member); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// Shared lists canvases shared with the current user.
func (h *CanvasShareHandler) Shared(c *gin.Context) {
	var req struct {
		pagination.Query
		Keyword string `form:"keyword" binding:"max=255"`
	}
	if !BindQuery(c, &req) {
		return
	}
	req.Normalize()
	items, total, err := h.service.Shared(c.Request.Context(), currentUserID(c), req.Keyword, req.Page, req.PageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OKPage(c, items, total, req.Page, req.PageSize)
}

// Workspace opens the shared canvas and issues its scoped media cookie.
func (h *CanvasShareHandler) Workspace(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := CanvasPathID(c)
		if !ok {
			return
		}
		var query struct {
			Revision   uint64 `form:"revision"`
			AccessOnly bool   `form:"access_only"`
		}
		if !BindQuery(c, &query) {
			return
		}
		var work *model.CanvasProjectView
		var err error
		if query.AccessOnly {
			err = h.service.CheckAccess(c.Request.Context(), currentUserID(c), id)
		} else {
			work, err = h.service.ReadIfChanged(c.Request.Context(), currentUserID(c), id, query.Revision)
		}
		if err != nil {
			response.Fail(c, err)
			return
		}
		if !issueCanvasMediaCookie(c, secret) {
			return
		}
		c.Header("Cache-Control", "private, no-store")
		if work == nil {
			response.OK(c, gin.H{"unchanged": true})
			return
		}
		response.OK(c, work)
	}
}

// Media streams one asset after checking its current project grant.
func (h *CanvasShareHandler) Media(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	asset, ok := idcodec.Decode(c.Param("asset"))
	if !ok {
		response.Fail(c, errcode.ErrAssetNotFound)
		return
	}
	open := h.service.Media
	if c.Query("v") == "poster" {
		open = h.service.Poster
	}
	file, err := open(c.Request.Context(), currentUserID(c), id, asset)
	serveCanvasMedia(c, file, err)
}

// DownloadMedia 使用登录态读取画布内的素材。
func (h *CanvasShareHandler) DownloadMedia(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	asset, err := strconv.ParseUint(c.Param("asset"), 10, 64)
	if err != nil || asset == 0 {
		response.Fail(c, errcode.ErrAssetNotFound)
		return
	}
	file, err := h.service.DownloadMedia(c.Request.Context(), currentUserID(c), id, asset)
	serveCanvasMedia(c, file, err)
}

func serveCanvasMedia(c *gin.Context, file *provider.AssetFile, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	defer file.Body.Close()
	if !strings.HasPrefix(file.Asset.MimeType, "image/") && !strings.HasPrefix(file.Asset.MimeType, "video/") && !strings.HasPrefix(file.Asset.MimeType, "audio/") {
		response.Fail(c, errcode.ErrAssetNotFound)
		return
	}
	c.Header("Content-Security-Policy", "sandbox; default-src 'none'")
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Type", file.Asset.MimeType)
	if c.Request.Method == http.MethodHead && file.Asset.ByteSize > 0 {
		c.Header("Content-Length", strconv.FormatInt(file.Asset.ByteSize, 10))
		c.Status(http.StatusOK)
		return
	}
	if body, ok := file.Body.(io.ReadSeeker); ok {
		http.ServeContent(c.Writer, c.Request, "media", file.Asset.CreatedAt, body)
		return
	}
	serveCanvasMediaStream(c, file.Body, file.Asset.MimeType, file.Asset.ByteSize)
}

func serveCanvasMediaStream(c *gin.Context, body io.Reader, mime string, size int64) {
	rangeHeader := c.GetHeader("Range")
	if size <= 0 || rangeHeader == "" || c.GetHeader("If-Range") != "" || strings.Contains(rangeHeader, ",") {
		c.DataFromReader(http.StatusOK, size, mime, body, nil)
		return
	}
	start, end, ok := canvasMediaRange(rangeHeader, size)
	if !ok {
		c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if _, err := io.CopyN(io.Discard, body, start); err != nil {
		response.Fail(c, errcode.ErrAssetNotFound)
		return
	}
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	length := end - start + 1
	c.DataFromReader(http.StatusPartialContent, length, mime, io.LimitReader(body, length), nil)
}

func canvasMediaRange(header string, size int64) (int64, int64, bool) {
	value, found := strings.CutPrefix(header, "bytes=")
	if !found {
		return 0, 0, false
	}
	first, last, found := strings.Cut(value, "-")
	if !found {
		return 0, 0, false
	}
	if first == "" {
		length, err := strconv.ParseInt(last, 10, 64)
		return max(0, size-length), size - 1, err == nil && length > 0
	}
	start, err := strconv.ParseInt(first, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	if last == "" {
		return start, size - 1, true
	}
	end, err := strconv.ParseInt(last, 10, 64)
	return start, min(end, size-1), err == nil && end >= start
}
