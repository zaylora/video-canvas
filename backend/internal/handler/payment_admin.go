package handler

import (
	"context"
	"regexp"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
	"video-canvas/pkg/pagination"
)

// PaymentAdminService 是后台支付管理接口依赖的业务能力，由 service.PaymentAdminService 实现。
type PaymentAdminService interface {
	Settings(ctx context.Context) (*model.PaymentSettingsView, error)
	UpdateSettings(ctx context.Context, actorID uint64, req *model.UpdatePaymentSettingsReq) (*model.PaymentSettingsView, error)
	ListChannels(ctx context.Context) ([]model.PaymentChannelView, error)
	UpdateChannel(ctx context.Context, actorID uint64, key string, req *model.UpdatePaymentChannelReq) (*model.PaymentChannelView, error)
	ValidateChannel(ctx context.Context, actorID uint64, key string) (*model.PaymentChannelView, error)
	EnableChannel(ctx context.Context, actorID uint64, key string) (*model.PaymentChannelView, error)
	DisableChannel(ctx context.Context, actorID uint64, key string) (*model.PaymentChannelView, error)
	ListProducts(ctx context.Context) ([]model.RechargeProduct, error)
	CreateProduct(ctx context.Context, actorID uint64, req *model.PaymentProductReq) (*model.RechargeProduct, error)
	UpdateProduct(ctx context.Context, actorID, id uint64, req *model.PaymentProductReq) (*model.RechargeProduct, error)
	DeleteProduct(ctx context.Context, actorID, id uint64) error
	SortProducts(ctx context.Context, actorID uint64, ids []uint64) error
	ListOrders(ctx context.Context, req *model.ListPaymentOrdersReq) ([]model.AdminPaymentOrderView, int64, pagination.Query, error)
	GetOrder(ctx context.Context, orderNo string) (*model.AdminPaymentOrderDetail, error)
}

// PaymentOrderOps 是管理员对订单的写操作（查单、关单）：它们要走入账事务，由用户侧的 service.PaymentService 实现。
type PaymentOrderOps interface {
	AdminQueryOrder(ctx context.Context, actorID uint64, orderNo string) (*model.PaymentOrderView, error)
	AdminCloseOrder(ctx context.Context, actorID uint64, orderNo string) (*model.PaymentOrderView, error)
}

// PaymentAdminHandler 处理后台支付管理接口（/admin/payment）。必须挂在 JWTAuth + RequireActive + RequireAdmin 之后；
// 全局设置和渠道配置的写操作另外要求 super_admin（路由保证）。
type PaymentAdminHandler struct {
	svc PaymentAdminService
	ops PaymentOrderOps
}

// NewPaymentAdminHandler 创建后台支付管理 handler。
func NewPaymentAdminHandler(svc PaymentAdminService, ops PaymentOrderOps) *PaymentAdminHandler {
	return &PaymentAdminHandler{svc: svc, ops: ops}
}

// providerKeyRe 限制路径里的渠道标识。
var providerKeyRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// providerKey 解析路径里的渠道标识。
func providerKey(c *gin.Context) (string, bool) {
	k := c.Param("provider")
	if !providerKeyRe.MatchString(k) {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("渠道标识格式不正确"))
		return "", false
	}
	return k, true
}

// GetSettings 读取全局设置（GET /admin/payment/settings）。
func (h *PaymentAdminHandler) GetSettings(c *gin.Context) {
	v, err := h.svc.Settings(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// UpdateSettings 保存全局设置（PUT /admin/payment/settings，仅 super_admin）。
func (h *PaymentAdminHandler) UpdateSettings(c *gin.Context) {
	var req model.UpdatePaymentSettingsReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.UpdateSettings(c.Request.Context(), actorID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// ListChannels 返回全部内置渠道及其配置状态（GET /admin/payment/channels）。敏感字段只返回是否已设置。
func (h *PaymentAdminHandler) ListChannels(c *gin.Context) {
	v, err := h.svc.ListChannels(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// UpdateChannel 保存渠道配置（PUT /admin/payment/channels/:provider，仅 super_admin）。
func (h *PaymentAdminHandler) UpdateChannel(c *gin.Context) {
	key, ok := providerKey(c)
	if !ok {
		return
	}
	var req model.UpdatePaymentChannelReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.UpdateChannel(c.Request.Context(), actorID(c), key, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// channelAction 是校验、启用、停用共用的「取渠道标识 → 调 service → 返回渠道」流程。
func (h *PaymentAdminHandler) channelAction(c *gin.Context, fn func(ctx context.Context, actorID uint64, key string) (*model.PaymentChannelView, error)) {
	key, ok := providerKey(c)
	if !ok {
		return
	}
	v, err := fn(c.Request.Context(), actorID(c), key)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// ValidateChannel 校验渠道配置（POST /admin/payment/channels/:provider/validate，仅 super_admin）。
func (h *PaymentAdminHandler) ValidateChannel(c *gin.Context) {
	h.channelAction(c, h.svc.ValidateChannel)
}

// EnableChannel 启用渠道（POST /admin/payment/channels/:provider/enable，仅 super_admin）。
func (h *PaymentAdminHandler) EnableChannel(c *gin.Context) { h.channelAction(c, h.svc.EnableChannel) }

// DisableChannel 停用渠道（POST /admin/payment/channels/:provider/disable，仅 super_admin）。
func (h *PaymentAdminHandler) DisableChannel(c *gin.Context) {
	h.channelAction(c, h.svc.DisableChannel)
}

// ListProducts 返回全部充值商品，含已下架（GET /admin/payment/products）。
func (h *PaymentAdminHandler) ListProducts(c *gin.Context) {
	v, err := h.svc.ListProducts(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// CreateProduct 新建充值商品（POST /admin/payment/products），201。
func (h *PaymentAdminHandler) CreateProduct(c *gin.Context) {
	var req model.PaymentProductReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.CreateProduct(c.Request.Context(), actorID(c), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, v)
}

// UpdateProduct 修改充值商品（PUT /admin/payment/products/:id）。
func (h *PaymentAdminHandler) UpdateProduct(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.PaymentProductReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.UpdateProduct(c.Request.Context(), actorID(c), id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// DeleteProduct 删除充值商品（DELETE /admin/payment/products/:id）；已有订单的商品只能下架。
func (h *PaymentAdminHandler) DeleteProduct(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteProduct(c.Request.Context(), actorID(c), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// SortProducts 按 ids 的顺序重排商品（PUT /admin/payment/products/sort）。
func (h *PaymentAdminHandler) SortProducts(c *gin.Context) {
	var req model.SortPaymentProductsReq
	if !BindJSON(c, &req) {
		return
	}
	if err := h.svc.SortProducts(c.Request.Context(), actorID(c), req.IDs); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// ListOrders 分页查询支付订单（GET /admin/payment/orders）。
func (h *PaymentAdminHandler) ListOrders(c *gin.Context) {
	var req model.ListPaymentOrdersReq
	if !BindQuery(c, &req) {
		return
	}
	items, total, q, err := h.svc.ListOrders(c.Request.Context(), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OKPage(c, items, total, q.Page, q.PageSize)
}

// GetOrder 返回订单详情和渠道通知记录（GET /admin/payment/orders/:no）。
func (h *PaymentAdminHandler) GetOrder(c *gin.Context) {
	no, ok := orderNoParam(c)
	if !ok {
		return
	}
	v, err := h.svc.GetOrder(c.Request.Context(), no)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// orderOp 是管理员查单、关单共用的「取订单号 → 调 service → 返回订单」流程。
func (h *PaymentAdminHandler) orderOp(c *gin.Context, fn func(ctx context.Context, actorID uint64, orderNo string) (*model.PaymentOrderView, error)) {
	no, ok := orderNoParam(c)
	if !ok {
		return
	}
	v, err := fn(c.Request.Context(), actorID(c), no)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// QueryOrder 管理员查单：向渠道确认状态，已付就入账（POST /admin/payment/orders/:no/query）。
func (h *PaymentAdminHandler) QueryOrder(c *gin.Context) { h.orderOp(c, h.ops.AdminQueryOrder) }

// CloseOrder 管理员关单：先查单再关单（POST /admin/payment/orders/:no/close）。
func (h *PaymentAdminHandler) CloseOrder(c *gin.Context) { h.orderOp(c, h.ops.AdminCloseOrder) }
