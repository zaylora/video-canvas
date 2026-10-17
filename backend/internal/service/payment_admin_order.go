package service

import (
	"context"
	"encoding/json"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/pkg/pagination"
)

// ListOrders 分页查询订单：关键字匹配订单号（精确）和用户名（模糊），可按状态、支付方式、渠道、创建时间筛选。
func (s *PaymentAdminService) ListOrders(ctx context.Context, req *model.ListPaymentOrdersReq) ([]model.AdminPaymentOrderView, int64, pagination.Query, error) {
	q := pagination.AdminQuery(req.Page, req.PageSize)
	f := repository.PaymentOrderFilter{Q: req.Q, Status: req.Status, Method: req.Method, Provider: req.Provider, Offset: q.Offset(), Limit: q.Limit()}
	var err error
	if f.From, err = parseRFC3339(req.From, "开始时间"); err != nil {
		return nil, 0, q, err
	}
	if f.To, err = parseRFC3339(req.To, "结束时间"); err != nil {
		return nil, 0, q, err
	}
	rows, total, err := s.orders.ListOrders(ctx, f)
	if err != nil {
		return nil, 0, q, err
	}
	out := make([]model.AdminPaymentOrderView, len(rows))
	for i := range rows {
		out[i] = adminOrderView(&rows[i])
	}
	return out, total, q, nil
}

// GetOrder 返回订单详情：订单本身和渠道发来的通知记录（排查「付了没到账」时看）。不存在返回 65006。
func (s *PaymentAdminService) GetOrder(ctx context.Context, orderNo string) (*model.AdminPaymentOrderDetail, error) {
	row, err := s.orders.FindOrderWithUser(ctx, orderNo)
	if isNotFound(err) {
		return nil, errcode.ErrPayOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	notes, err := s.orders.ListNotifications(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if notes == nil {
		notes = []model.PaymentNotification{}
	}
	return &model.AdminPaymentOrderDetail{AdminPaymentOrderView: adminOrderView(row), Notifications: notes}, nil
}

// parseRFC3339 解析可选的 RFC3339 时间，空串返回 nil。
func parseRFC3339(raw, label string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, errcode.ErrInvalidParams.WithMsg(label + "必须是 RFC3339 格式，例如 2026-10-16T00:00:00+08:00")
	}
	return &t, nil
}

// adminOrderView 把订单行转成后台视图；商品名取自下单时的快照，而不是商品当前的名字。
func adminOrderView(r *model.AdminPaymentOrderRow) model.AdminPaymentOrderView {
	var snap struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(r.ProductSnapshot, &snap) // 快照损坏时商品名留空，不影响其它信息展示
	return model.AdminPaymentOrderView{
		OrderNo: r.OrderNo, UserID: r.UserID, Username: r.Username, ProductName: snap.Name, AmountFen: r.AmountFen, Credits: r.Credits,
		Method: r.Method, Provider: r.Provider, ProviderName: model.PaymentProviderNames[r.Provider], Status: r.Status,
		ProviderTradeNo: r.ProviderTradeNo, PaidAmountFen: r.PaidAmountFen, LatePaid: r.LatePaid, CloseReason: r.CloseReason,
		AbnormalReason: r.AbnormalReason, FailReason: r.FailReason, ChannelVersion: r.ChannelVersion, ExpiresAt: r.ExpiresAt,
		PaidAt: r.PaidAt, CreditedAt: r.CreditedAt, ClosedAt: r.ClosedAt, CreatedAt: r.CreatedAt,
	}
}
