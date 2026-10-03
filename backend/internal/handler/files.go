package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

// FileResolver 决定一个对象 key 怎么提供给浏览器，由 service.AssetService 实现。
type FileResolver interface {
	ResolveFile(ctx context.Context, key string) (*service.FileTarget, error)
}

// NewFilesHandler 返回 /files/*filepath 的处理器（GET 与 HEAD 都挂它）。
//
// 画布 payload 里存的素材地址永远是这个稳定路径：本地存储直接由后端提供文件（支持 Range，视频可拖动），
// 对象存储在访问的这一刻现签名并 302 跳转，所以地址永不过期、换存储也不变。
// 不鉴权：<video> / <img> 要直接引用，靠 key 不可猜测保护，和原来的本地存储一致。
func NewFilesHandler(res FileResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := strings.TrimPrefix(c.Param("filepath"), "/")
		target, err := res.ResolveFile(c.Request.Context(), key)
		if err != nil {
			var e *errcode.Error
			if errors.As(err, &e) {
				c.AbortWithStatus(e.HTTPStatus())
				return
			}
			logger.Error("解析素材文件失败", zap.String("key", key), zap.Error(err))
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		if target.Local != nil {
			storage.FileServer(target.Local)(c)
			return
		}

		// 跳转本身可以缓存一段时间（不超过签名有效期的 1/4，保证浏览器拿到的地址一定还没过期）；
		// no-referrer 避免把站点页面地址带给存储服务商
		h := c.Writer.Header()
		h.Set("Cache-Control", "private, max-age="+strconv.Itoa(int(target.MaxAge.Seconds())))
		h.Set("Referrer-Policy", "no-referrer")
		c.Redirect(http.StatusFound, target.RedirectURL)
	}
}
