package handler

import (
	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// UserHandler 处理注册、登录与注册配置。
type UserHandler struct {
	svc *service.UserService
}

// NewUserHandler 创建用户 handler。
func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// clientMeta 取请求方的 IP 与 User-Agent，供登录记录与发码限频使用。
func clientMeta(c *gin.Context) service.ClientMeta {
	return service.ClientMeta{IP: c.ClientIP(), UserAgent: c.GetHeader("User-Agent")}
}

// Config 返回登录页需要的注册配置（GET /auth/config）。
func (h *UserHandler) Config(c *gin.Context) {
	cfg, err := h.svc.AuthConfig(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, cfg)
}

// SendRegisterCode 给邮箱发送注册验证码（POST /auth/register/code）。
func (h *UserHandler) SendRegisterCode(c *gin.Context) {
	var req model.SendRegisterCodeReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.svc.SendRegisterCode(c.Request.Context(), req.Email, c.ClientIP()); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// Register 用户注册，成功后直接登录并返回 token（POST /auth/register）。
func (h *UserHandler) Register(c *gin.Context) {
	var req model.RegisterUserReq
	if !BindJSON(c, &req) {
		return
	}
	view, err := h.svc.Register(c.Request.Context(), &req, clientMeta(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// Login 用户登录，返回 JWT token、过期时间与角色（POST /auth/login）。
func (h *UserHandler) Login(c *gin.Context) {
	var req model.LoginUserReq
	if !BindJSON(c, &req) {
		return
	}
	view, err := h.svc.Login(c.Request.Context(), req.Username, req.Password, clientMeta(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}
