package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// archiveWriteTimeout 是打包下载允许的写超时。比导入导出的 5 分钟更长：
// 打包要串行从对象存储拉完所有素材再推给浏览器，几十个文件几百 MB 是常态（45 MB 实测约 45 秒）。
// 前置 nginx 的 proxy_read_timeout 是 600s，所以这里取 15 分钟只防后端自己提前放弃，
// 真正的上限仍由 nginx 决定。
const archiveWriteTimeout = 15 * time.Minute

// relaxArchiveDeadline 放宽本次请求的写超时；测试用的 ResponseRecorder 不支持时忽略。
func relaxArchiveDeadline(c *gin.Context) {
	rc := http.NewResponseController(c.Writer)
	if err := rc.SetWriteDeadline(time.Now().Add(archiveWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		logger.Warn("放宽打包下载写超时失败", zap.Error(err))
	}
}

// CanvasArchiveHandler 处理画布素材的按节点归类打包下载。
type CanvasArchiveHandler struct {
	svc *service.CanvasArchiveService
}

// NewCanvasArchiveHandler 创建打包下载 handler。
func NewCanvasArchiveHandler(svc *service.CanvasArchiveService) *CanvasArchiveHandler {
	return &CanvasArchiveHandler{svc: svc}
}

// Ticket 签发一次打包下载：先把要打包的内容算一遍（没有素材当场报错），再返回带票据的地址与统计。
// 返回的地址不带 Authorization，浏览器可以直接 <a download> 拿它下载，几百 MB 也不进 JS 内存。
func (h *CanvasArchiveHandler) Ticket(c *gin.Context) {
	id, ok := CanvasPathID(c)
	if !ok {
		return
	}
	userID := currentUserID(c)
	a, err := h.svc.Plan(c.Request.Context(), userID, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	ticket, exp := h.svc.Sign(userID, id)
	if ticket == "" {
		response.Fail(c, errcode.ErrCanvasArchiveTicket.WithMsg("服务端没有配置下载签名密钥"))
		return
	}
	response.OK(c, gin.H{
		"url":         fmt.Sprintf("/canvas/%s/assets/archive?token=%s", idcodec.Encode(id), url.QueryEscape(ticket)),
		"file_name":   a.FileName,
		"count":       len(a.Entries),
		"groups":      a.Count(),
		"total_bytes": a.TotalBytes,
		"skipped":     len(a.Skipped),
		"expires_at":  exp.Unix(),
	})
}

// Download 按票据把素材打成 zip 流式下发。票据里带着画布和用户，路径里的 id 必须与之一致，
// 免得拿一张画布的票去下另一张。
func (h *CanvasArchiveHandler) Download(c *gin.Context) {
	relaxArchiveDeadline(c)
	userID, canvasID, err := h.svc.ParseTicket(c.Query("token"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if id, ok := CanvasPathID(c); !ok || id != canvasID {
		response.Fail(c, errcode.ErrCanvasArchiveTicket)
		return
	}
	// 票据校验通过就说明身份成立，这里不查 JWT：浏览器原生下载带不上请求头
	plan, err := h.svc.Plan(c.Request.Context(), userID, canvasID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", archiveDisposition(plan.FileName))
	c.Header("Cache-Control", "no-store")
	// 让前置 nginx 不要攒够整包再发，否则用户会盯着空白页等几分钟
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	if err := h.svc.Write(c.Request.Context(), userID, plan, c.Writer); err != nil {
		// 头已经发出去了，改不了状态码；用户断开连接也会走到这里，记一笔就够
		logger.Warn("画布素材打包下载中断", zap.Uint64("canvas_id", canvasID), zap.Error(err))
	}
}

// archiveDisposition 生成下载头：ASCII 兜底名 + RFC 5987 的 UTF-8 名（中文标题不被浏览器吃掉）。
func archiveDisposition(name string) string {
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", "canvas-assets.zip", escapeRFC5987(name))
}

// escapeRFC5987 按 RFC 5987 转义文件名：除 attr-char 之外一律百分号编码，空格写成 %20。
func escapeRFC5987(s string) string {
	const attr = "!#$&+-.^_`|~"
	out := make([]byte, 0, len(s)*3)
	for _, b := range []byte(s) {
		switch {
		case b >= '0' && b <= '9', b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z':
			out = append(out, b)
		case b < 0x80 && b != ' ' && strings.IndexByte(attr, b) >= 0:
			out = append(out, b)
		default:
			out = append(out, '%', "0123456789ABCDEF"[b>>4], "0123456789ABCDEF"[b&0xf])
		}
	}
	return string(out)
}
