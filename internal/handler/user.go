package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

type UserHandler struct {
	svc *service.UserService
}

func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// 用户注册
func (h *UserHandler) Register(c *gin.Context) {
	var req model.RegisterUserReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.Register(c.Request.Context(), req.Username, req.Password); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// 用户登录，返回 JWT Token。
func (h *UserHandler) Login(c *gin.Context) {
	var req model.LoginUserReq
	if !bindJSON(c, &req) {
		return
	}
	token, expireAt, err := h.svc.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"token": token, "expire_at": expireAt})
}
