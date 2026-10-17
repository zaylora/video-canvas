package handler

import (
	"context"
	"io"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
)

// PaymentService 是用户侧支付接口依赖的业务能力，由 service.PaymentService 实现。
type PaymentService interface {
	Store(ctx context.Context) (*model.PaymentStoreView, error)
	CreateOrder(ctx context.Context, userID uint64, req *model.CreatePaymentOrderReq, clientIP string) (*model.PaymentOrderView, error)
	GetOrder(ctx context.Context, userID uint64, orderNo string) (*model.PaymentOrderView, error)
	PendingOrder(ctx context.Context, userID uint64) (*model.PaymentOrderView, error)
	CheckOrder(ctx context.Context, userID uint64, orderNo string) (*model.PaymentOrderView, error)
	CancelOrder(ctx context.Context, userID uint64, orderNo string) (*model.PaymentOrderView, error)
	HandleNotify(ctx context.Context, provider string, req payment.NotifyRequest) payment.Ack
}

// maxNotifyBody 是渠道通知请求体的上限：正常通知只有几 KB，更大的多半是恶意灌包。
const maxNotifyBody = 64 << 10

// 订单号和渠道标识在路径里，限制字符集和长度，挡掉明显的垃圾输入。
var (
	orderNoRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	providerRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
)

// PaymentHandler 处理积分商城的用户侧接口和渠道的异步回调。回调接口不需要登录。
type PaymentHandler struct {
	svc PaymentService
}

// NewPaymentHandler 创建支付 handler。
func NewPaymentHandler(svc PaymentService) *PaymentHandler { return &PaymentHandler{svc: svc} }

// Store 返回积分商城的数据（GET /payment/store）。
func (h *PaymentHandler) Store(c *gin.Context) {
	v, err := h.svc.Store(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// CreateOrder 下单（POST /payment/orders），返回 201 和收银信息。
func (h *PaymentHandler) CreateOrder(c *gin.Context) {
	var req model.CreatePaymentOrderReq
	if !BindJSON(c, &req) {
		return
	}
	v, err := h.svc.CreateOrder(c.Request.Context(), currentUserID(c), &req, c.ClientIP())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, v)
}

// orderNoParam 解析路径里的订单号，格式不对按参数错误返回。
func orderNoParam(c *gin.Context) (string, bool) {
	no := c.Param("no")
	if !orderNoRe.MatchString(no) {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("订单号格式不正确"))
		return "", false
	}
	return no, true
}

// orderAction 是「取订单号 → 调 service → 返回订单」的公共流程。
func (h *PaymentHandler) orderAction(c *gin.Context, fn func(ctx context.Context, userID uint64, orderNo string) (*model.PaymentOrderView, error)) {
	no, ok := orderNoParam(c)
	if !ok {
		return
	}
	v, err := fn(c.Request.Context(), currentUserID(c), no)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// GetOrder 查询订单状态（GET /payment/orders/:no），只读库，前端每 3 秒轮询。
func (h *PaymentHandler) GetOrder(c *gin.Context) { h.orderAction(c, h.svc.GetOrder) }

// CheckOrder 主动向渠道查单（POST /payment/orders/:no/check）。
func (h *PaymentHandler) CheckOrder(c *gin.Context) { h.orderAction(c, h.svc.CheckOrder) }

// CancelOrder 取消订单（POST /payment/orders/:no/close）。
func (h *PaymentHandler) CancelOrder(c *gin.Context) { h.orderAction(c, h.svc.CancelOrder) }

// PendingOrder 返回当前用户最近一笔进行中的订单，没有时 data 为 null（GET /payment/orders/pending）。
func (h *PaymentHandler) PendingOrder(c *gin.Context) {
	v, err := h.svc.PendingOrder(c.Request.Context(), currentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Notify 接收渠道的异步通知（GET|POST /payment/notify/:provider），不需要登录：身份由渠道的签名保证。
// 请求体原样交给 service（验签要用原文），应答格式、状态码由渠道适配器决定，渠道才能识别「收到了」或「请重试」。
// 这是唯一不走 response 包的接口：渠道要的是纯文本 success 或特定 JSON，不是项目统一的响应结构。
func (h *PaymentHandler) Notify(c *gin.Context) {
	provider := c.Param("provider")
	if !providerRe.MatchString(provider) {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("渠道标识格式不正确"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxNotifyBody))
	if err != nil {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("通知内容过大或读取失败"))
		return
	}
	ack := h.svc.HandleNotify(c.Request.Context(), provider, payment.NotifyRequest{
		Method: c.Request.Method, Header: c.Request.Header.Clone(), Query: c.Request.URL.Query(), Body: body,
	})
	c.Data(ack.Status, ack.ContentType, []byte(ack.Body))
}
