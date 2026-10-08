package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// avatarMultipartOverhead 是头像请求体在文件大小之外预留的 multipart 边界与其他字段开销。
const avatarMultipartOverhead = 64 << 10

// MeHandler 处理个人中心 /me/* 接口：资料、头像、改密码、统计、热力图、积分流水。
type MeHandler struct {
	svc *service.MeService
}

// NewMeHandler 创建个人中心 handler。
func NewMeHandler(svc *service.MeService) *MeHandler {
	return &MeHandler{svc: svc}
}

// Get 返回当前用户资料（GET /me）。
func (h *MeHandler) Get(c *gin.Context) {
	v, err := h.svc.Me(c.Request.Context(), currentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Patch 修改昵称（PATCH /me）。
func (h *MeHandler) Patch(c *gin.Context) {
	var req model.UpdateMeReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.UpdateProfile(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// UploadAvatar 上传头像（POST /me/avatar，multipart，文件字段名 file，≤2MB）。
func (h *MeHandler) UploadAvatar(c *gin.Context) {
	// 1. 限制请求体：声明的长度已超限就不必读；读取时超限由 MaxBytesReader 报错，service 按 55006 处理
	maxBody := int64(service.AvatarMaxBytes + avatarMultipartOverhead)
	if c.Request.ContentLength > maxBody {
		response.Fail(c, errcode.ErrAvatarTooLarge.WithMsg("头像文件不能超过 2MB"))
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBody)

	// 2. 流式读取 multipart，找到 file 字段交给 service
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
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				response.Fail(c, errcode.ErrAvatarTooLarge.WithMsg("头像文件不能超过 2MB"))
				return
			}
			response.Fail(c, errcode.ErrInvalidParams.WithMsg("上传中断或读取文件失败，请重试"))
			return
		}
		if part.FormName() != assetFormField || part.FileName() == "" {
			continue // 跳过其他字段
		}
		v, err := h.svc.UploadAvatar(c.Request.Context(), currentUserID(c), part)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, v)
		return
	}
}

// DeleteAvatar 移除头像（DELETE /me/avatar）。
func (h *MeHandler) DeleteAvatar(c *gin.Context) {
	v, err := h.svc.DeleteAvatar(c.Request.Context(), currentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// ChangePassword 修改密码（PUT /me/password），成功返回给当前设备续签的 token。
func (h *MeHandler) ChangePassword(c *gin.Context) {
	var req model.ChangePasswordReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.ChangePassword(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Stats 返回概览统计（GET /me/stats）。
func (h *MeHandler) Stats(c *gin.Context) {
	v, err := h.svc.Stats(c.Request.Context(), currentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Activity 返回热力图数据（GET /me/activity?tz=&year=）。
func (h *MeHandler) Activity(c *gin.Context) {
	var req model.MeActivityReq
	if !BindQuery(c, &req) {
		return
	}
	v, err := h.svc.Activity(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Ledger 返回积分流水（GET /me/credits/ledger?type=&page=&page_size=）。
func (h *MeHandler) Ledger(c *gin.Context) {
	var req model.MeLedgerReq
	if !BindQuery(c, &req) {
		return
	}
	v, err := h.svc.Ledger(c.Request.Context(), currentUserID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}
