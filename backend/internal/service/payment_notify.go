package service

import (
	"context"
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/pkg/logger"
)

// genericFailAck 是渠道标识未知、渠道没配置这类连适配器都拿不到时的应答：HTTP 400，渠道会稍后重试。
var genericFailAck = payment.Ack{Status: http.StatusBadRequest, ContentType: "text/plain; charset=utf-8", Body: "fail"}

// HandleNotify 处理渠道的异步通知并返回要回给渠道的应答。流程：
// 验签（失败不改任何数据）→ 订单不存在或通知未付款只记录 → MD5 渠道再查单确认 → settleTrade 入账。
// 应答成功表示「这条通知已妥善处理，别再发了」：重复通知、订单不存在、已转人工的异常订单都应答成功，避免渠道无限重试；
// 验签失败和入账失败（数据库故障）应答失败，让渠道稍后重试。
func (s *PaymentService) HandleNotify(ctx context.Context, providerKey string, req payment.NotifyRequest) payment.Ack {
	// 1. 装载渠道：渠道停用后已有订单的通知仍要处理，所以这里不检查 enabled
	lp, err := s.loadProvider(ctx, providerKey)
	if err != nil {
		logger.Warn("支付通知：渠道不可用", zap.String("provider", providerKey), zap.Error(err))
		return genericFailAck
	}
	// 2. 验签（微信还要解密）。失败只记一条 warn：不落库、不改订单，避免被伪造请求污染数据
	res, err := lp.provider.VerifyNotify(ctx, lp.cfg, req)
	if err != nil {
		logger.Warn("支付通知验签失败", zap.String("provider", providerKey), zap.Error(err))
		return lp.provider.NotifyAck(false)
	}
	return lp.provider.NotifyAck(s.processNotify(ctx, lp, res))
}

// processNotify 处理一条验签通过的通知，返回是否已妥善处理（决定应答成功还是失败）。
func (s *PaymentService) processNotify(ctx context.Context, lp *loadedProvider, res payment.TradeResult) bool {
	o, err := s.orders.FindOrder(ctx, res.OrderNo)
	if isNotFound(err) {
		s.recordNotify(ctx, lp.provider.Key(), res, model.PayNotifyNotFound)
		return true
	}
	if err != nil {
		warnSwallowed("支付通知：读订单失败", res.OrderNo, err)
		return false
	}
	// 订单必须属于收到通知的这个渠道，否则是别的渠道的订单号被塞进来了
	if o.Provider != lp.provider.Key() {
		s.recordNotify(ctx, lp.provider.Key(), res, model.PayNotifyStateRejected)
		return true
	}
	if res.State != payment.StatePaid {
		s.recordNotify(ctx, lp.provider.Key(), res, model.PayNotifyUnpaid)
		return true
	}
	// MD5 签名的渠道，签名能被抓包重放，所以验签通过后再向渠道查一次单，以查单结果的金额和交易号为准
	if lp.provider.Capabilities().ConfirmNotify {
		confirmed, ok := s.confirmByQuery(ctx, lp, o, res)
		if !ok {
			return false
		}
		res = confirmed
	}
	outcome, err := s.settleTrade(ctx, o, res, lp.provider.Identity(lp.cfg, o.Method))
	if err != nil {
		warnSwallowed("支付通知：入账失败", o.OrderNo, err)
		return false
	}
	s.recordNotify(ctx, lp.provider.Key(), res, outcome)
	return true
}

// confirmByQuery 向渠道查单确认已付，成功时返回以查单结果的金额和交易号覆盖后的通知结果。
// 查单失败或渠道说没付都返回 ok=false：此时不入账，应答失败让渠道稍后重发。
func (s *PaymentService) confirmByQuery(ctx context.Context, lp *loadedProvider, o *model.PaymentOrder, res payment.TradeResult) (payment.TradeResult, bool) {
	q, err := queryUpstream(ctx, lp, o)
	if err != nil {
		warnSwallowed("支付通知：查单确认失败", o.OrderNo, err)
		return res, false
	}
	if q.State != payment.StatePaid {
		warnSwallowed("支付通知：验签通过但查单显示未付", o.OrderNo, nil)
		return res, false
	}
	res.PaidFen = q.PaidFen
	if q.TradeNo != "" {
		res.TradeNo = q.TradeNo
	}
	return res, true
}

// recordNotify 记一条验签通过的通知，用来排查「付了没到账」。记录失败只打日志：入账已经完成，不能因此让渠道重试。
func (s *PaymentService) recordNotify(ctx context.Context, provider string, res payment.TradeResult, result string) {
	key := res.EventKey
	if key == "" {
		key = "evt:" + res.OrderNo + ":" + res.State + ":" + res.TradeNo
	}
	payload, err := json.Marshal(map[string]any{
		"order_no": res.OrderNo, "trade_no": res.TradeNo, "state": res.State, "paid_fen": res.PaidFen, "identity": res.Identity,
	})
	if err != nil { // 都是字符串和数字，编码不会失败；万一失败就不带 payload
		payload = []byte("{}")
	}
	err = s.orders.InsertNotification(ctx, &model.PaymentNotification{
		Provider: provider, EventKey: key, OrderNo: res.OrderNo, Result: result, Payload: payload, ReceivedAt: s.now(),
	})
	if err != nil {
		warnSwallowed("记录支付通知失败", res.OrderNo, err)
	}
}
