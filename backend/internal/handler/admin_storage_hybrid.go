package handler

import (
	"context"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// StorageHybridAdminService 是混合策略管理入口依赖的业务接口。
type StorageHybridAdminService interface {
	Status(ctx context.Context) (service.StorageHybridView, error)
	Enable(ctx context.Context, limit int64) (service.StorageHybridView, error)
	Disable(ctx context.Context) (service.StorageHybridView, error)
}

type storageHybridReq struct {
	Enabled    *bool `json:"enabled" binding:"required" label:"状态"`
	LimitBytes int64 `json:"limit_bytes" binding:"min=0" label:"限额"`
}

// WithHybrid 组装当期部署的混合策略服务。
func (h *AdminStorageHandler) WithHybrid(svc StorageHybridAdminService) *AdminStorageHandler {
	h.hybrid = svc
	return h
}

// Hybrid 返回混合策略的当前状态。
func (h *AdminStorageHandler) Hybrid(c *gin.Context) {
	view, err := h.hybrid.Status(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// SetHybrid 记录启用请求或关闭当前策略。
func (h *AdminStorageHandler) SetHybrid(c *gin.Context) {
	var req storageHybridReq
	if !BindJSON(c, &req) {
		return
	}
	ctx := c.Request.Context()
	if *req.Enabled {
		view, err := h.hybrid.Enable(ctx, req.LimitBytes)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, view)
		return
	}
	view, err := h.hybrid.Disable(ctx)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}
