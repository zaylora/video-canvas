package response

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/utils"
)

// Body 是统一的响应结构，code 为 0 表示成功。
type Body struct {
	Code      int    `json:"code"`
	Msg       string `json:"msg"`
	Data      any    `json:"data,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

type PageData struct {
	List     any   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Code: 0, Msg: "success", Data: data, RequestID: utils.GetRequestID(c)})
}

func OKPage(c *gin.Context, list any, total int64, page, pageSize int) {
	OK(c, PageData{List: list, Total: total, Page: page, PageSize: pageSize})
}

// Fail 把 error 转成统一响应；非 *errcode.Error 的错误视为内部错误，记录日志后只返回通用提示。
func Fail(c *gin.Context, err error) {
	var e *errcode.Error
	if !errors.As(err, &e) {
		logger.Error("internal error",
			zap.Error(err),
			zap.String("request_id", utils.GetRequestID(c)),
			zap.String("path", c.Request.URL.Path),
		)
		e = errcode.ErrInternal
	}
	c.AbortWithStatusJSON(e.HTTPStatus(), Body{Code: e.Code, Msg: e.Msg, RequestID: utils.GetRequestID(c)})
}
