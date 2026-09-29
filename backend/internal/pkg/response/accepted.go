package response

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/pkg/utils"
)

// Accepted 返回 202：请求已受理但处理尚未完成（如提交长任务），响应结构与 OK 一致。
func Accepted(c *gin.Context, data any) {
	c.JSON(http.StatusAccepted, Body{Code: 0, Msg: "success", Data: data, RequestID: utils.GetRequestID(c)})
}
