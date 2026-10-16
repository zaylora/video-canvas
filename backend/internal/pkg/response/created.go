package response

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/pkg/utils"
)

// Created 返回 201：资源已创建，响应结构与 OK 一致。
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Body{Code: 0, Msg: "success", Data: data, RequestID: utils.GetRequestID(c)})
}
