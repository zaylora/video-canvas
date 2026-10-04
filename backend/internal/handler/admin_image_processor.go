package handler

import (
	"context"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/imageproc"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// ImageProcessorAdminService 是图片处理服务管理接口依赖的业务能力，由 service.ImageProcessorService 实现。
type ImageProcessorAdminService interface {
	Presets() []imageproc.Preset
	List(ctx context.Context) ([]service.ProcessorView, error)
	Get(ctx context.Context, id uint64) (*service.ProcessorView, error)
	Create(ctx context.Context, actorID uint64, in service.ProcessorCreateInput) (*service.ProcessorView, error)
	Update(ctx context.Context, actorID, id uint64, in service.ProcessorUpdateInput) (*service.ProcessorView, error)
	Check(ctx context.Context, actorID, id uint64) (*service.ProcessorView, error)
	Publish(ctx context.Context, actorID, id uint64, version int) (*service.ProcessorView, error)
	Rollback(ctx context.Context, actorID, id uint64) (*service.ProcessorView, error)
	Disable(ctx context.Context, actorID, id uint64) (*service.ProcessorView, error)
	Delete(ctx context.Context, actorID, id uint64) error
}

// AdminImageProcessorHandler 是图片处理服务的管理接口：列表 / 详情 / 预设（admin 可用），
// 新建 / 保存草稿 / 校验 / 发布 / 回滚 / 停用 / 删除（仅 super_admin，由路由挂中间件）。
type AdminImageProcessorHandler struct {
	svc ImageProcessorAdminService
}

// NewAdminImageProcessorHandler 创建图片处理服务管理接口的 handler。
func NewAdminImageProcessorHandler(svc ImageProcessorAdminService) *AdminImageProcessorHandler {
	return &AdminImageProcessorHandler{svc: svc}
}

// processorConfigReq 是处理服务的配置。各厂商的取值规则（长边范围、格式白名单、域名写法）由 service 校验，
// 这里只拦明显超长、越界的值。
type processorConfigReq struct {
	Domain          string  `json:"domain" binding:"max=255" label:"访问域名"`
	Width           int     `json:"width" binding:"min=0,max=10000" label:"缩略图长边"`
	Format          string  `json:"format" binding:"max=16" label:"输出格式"`
	TimeSec         float64 `json:"time_sec" binding:"min=0,max=36000" label:"取帧时间"`
	Quality         int     `json:"quality" binding:"min=0,max=100" label:"图片质量"`
	OnErrorRedirect *bool   `json:"on_error_redirect"`
	MediaEnabled    bool    `json:"media_enabled"`
}

func (r processorConfigReq) config() imageproc.Config {
	return imageproc.Config{
		Domain: r.Domain, Width: r.Width, Format: r.Format, TimeSec: r.TimeSec,
		Quality: r.Quality, OnErrorRedirect: r.OnErrorRedirect, MediaEnabled: r.MediaEnabled,
	}
}

// processorCreateReq 是新建处理服务的参数。
type processorCreateReq struct {
	Name      string              `json:"name" binding:"required,max=64" label:"名称"`
	Vendor    string              `json:"vendor" binding:"required,max=24" label:"厂商"`
	StorageID uint64              `json:"storage_id" binding:"required" label:"绑定存储"`
	Config    *processorConfigReq `json:"config" binding:"required" label:"配置"`
}

// processorUpdateReq 是保存草稿的参数（整份提交）。
type processorUpdateReq struct {
	Version int                 `json:"version" binding:"required,min=1" label:"版本号"`
	Name    string              `json:"name" binding:"required,max=64" label:"名称"`
	Config  *processorConfigReq `json:"config" binding:"required" label:"配置"`
}

// processorPublishReq 是发布的参数：带上读到的草稿版本，发布的必须正是前端校验过的那一版。
type processorPublishReq struct {
	Version int `json:"version" binding:"required,min=1" label:"版本号"`
}

// Presets 返回全部厂商预设（允许绑定的存储、可选格式、是否支持视频封面），表单据此渲染。
func (h *AdminImageProcessorHandler) Presets(c *gin.Context) {
	response.OK(c, h.svc.Presets())
}

// List 返回所有处理服务。
func (h *AdminImageProcessorHandler) List(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Get 返回处理服务详情。
func (h *AdminImageProcessorHandler) Get(c *gin.Context) {
	h.byID(c, func(ctx context.Context, id uint64) (*service.ProcessorView, error) { return h.svc.Get(ctx, id) })
}

// Create 新建一个草稿处理服务。
func (h *AdminImageProcessorHandler) Create(c *gin.Context) {
	var req processorCreateReq
	if !bindJSON(c, &req) {
		return
	}
	view, err := h.svc.Create(c.Request.Context(), currentUserID(c), service.ProcessorCreateInput{
		Name: req.Name, Vendor: req.Vendor, StorageID: req.StorageID, Config: req.Config.config(),
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// Update 保存草稿（带 version 乐观锁；已发布的线上配置不受影响）。
func (h *AdminImageProcessorHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req processorUpdateReq
	if !bindJSON(c, &req) {
		return
	}
	view, err := h.svc.Update(c.Request.Context(), currentUserID(c), id, service.ProcessorUpdateInput{
		Version: req.Version, Name: req.Name, Config: req.Config.config(),
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// Check 对当前草稿做校验与试跑，结果写入 check，校验没通过也返回 200。
func (h *AdminImageProcessorHandler) Check(c *gin.Context) {
	h.byID(c, func(ctx context.Context, id uint64) (*service.ProcessorView, error) {
		return h.svc.Check(ctx, currentUserID(c), id)
	})
}

// Publish 发布当前草稿：要求草稿已通过校验与试跑，同存储已有已发布的处理服务时替换它。
func (h *AdminImageProcessorHandler) Publish(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req processorPublishReq
	if !bindJSON(c, &req) {
		return
	}
	view, err := h.svc.Publish(c.Request.Context(), currentUserID(c), id, req.Version)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// Rollback 回滚到上一个已发布版本。
func (h *AdminImageProcessorHandler) Rollback(c *gin.Context) {
	h.byID(c, func(ctx context.Context, id uint64) (*service.ProcessorView, error) {
		return h.svc.Rollback(ctx, currentUserID(c), id)
	})
}

// Disable 停用已发布的处理服务，该存储回退原图 / 占位。
func (h *AdminImageProcessorHandler) Disable(c *gin.Context) {
	h.byID(c, func(ctx context.Context, id uint64) (*service.ProcessorView, error) {
		return h.svc.Disable(ctx, currentUserID(c), id)
	})
}

// Delete 删除草稿或已停用的处理服务。
func (h *AdminImageProcessorHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), currentUserID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// byID 是“取路径 id → 调 service → 返回视图”的公共流程。
func (h *AdminImageProcessorHandler) byID(c *gin.Context, fn func(ctx context.Context, id uint64) (*service.ProcessorView, error)) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	view, err := fn(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}
