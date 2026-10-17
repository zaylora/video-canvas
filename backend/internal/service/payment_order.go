package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// newOrderNo 生成商户订单号：VC + 年月日时分秒 + 8 位随机数字，共 22 位，发给渠道也是它。
func newOrderNo(now time.Time) (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(100_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("VC%s%08d", now.Format("060102150405"), n.Int64()), nil
}

// methodRoute 找出承接某种支付方式的渠道。reason 非空表示该方式当前不可用（原因直接展示给用户）；
// err 只表示读库失败。只读渠道行和注册表，不读密钥，所以商城列表可以放心调用。
func (s *PaymentService) methodRoute(ctx context.Context, st *paymentSettings, method string) (key, reason string, err error) {
	key = st.Methods[method]
	if key == "" {
		return "", "未配置承接渠道", nil
	}
	p, ok := s.registry.Get(key)
	if !ok {
		return "", "承接渠道不存在", nil
	}
	ch, err := s.channels.GetChannel(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return "", "承接渠道尚未配置", nil
	}
	if err != nil {
		return "", "", err
	}
	if !ch.Enabled {
		return "", "承接渠道已停用", nil
	}
	if !slices.Contains(p.Capabilities().Methods, method) {
		return "", "承接渠道不支持该支付方式", nil
	}
	return key, "", nil
}

// Store 返回积分商城的数据：支付总开关、上架商品和两种支付方式的可用状态。
// 总开关关闭时只返回 enabled=false，前端据此保持积分胶囊原来的余额小卡片。
func (s *PaymentService) Store(ctx context.Context) (*model.PaymentStoreView, error) {
	// 1. 总开关
	st, err := s.loadSettings(ctx)
	if err != nil {
		return nil, err
	}
	v := &model.PaymentStoreView{Enabled: st.Enabled, Products: []model.PaymentProductView{}, Methods: []model.PaymentMethodView{}}
	if !st.Enabled {
		return v, nil
	}
	// 2. 上架商品（仓储已按 sort、id 排好序）
	products, err := s.products.ListProducts(ctx, true)
	if err != nil {
		return nil, err
	}
	for _, p := range products {
		v.Products = append(v.Products, model.PaymentProductView{
			ID: p.ID, Name: p.Name, PriceFen: p.PriceFen, BaseCredits: p.BaseCredits, BonusCredits: p.BonusCredits, Badge: p.Badge,
		})
	}
	// 3. 两种支付方式：不可用也要返回并写明原因，前台置灰而不是隐藏
	for _, m := range model.PayMethods {
		_, reason, err := s.methodRoute(ctx, st, m)
		if err != nil {
			return nil, err
		}
		v.Methods = append(v.Methods, model.PaymentMethodView{Method: m, Available: reason == "", Reason: reason})
	}
	return v, nil
}

// CreateOrder 下单：落库 creating 订单 → 向渠道下单 → 转 pending 并返回收银信息；渠道失败转 create_failed。
// 同一个 (userID, 幂等键) 只会产生一笔订单，重复提交直接返回已有订单。
func (s *PaymentService) CreateOrder(ctx context.Context, userID uint64, req *model.CreatePaymentOrderReq, clientIP string) (*model.PaymentOrderView, error) {
	// 1. 幂等：前端重复点击或网络重试时返回同一笔订单；放在所有校验之前，重试不应因为额度或开关变化而失败
	existing, err := s.orders.FindByIdempotencyKey(ctx, userID, req.IdempotencyKey)
	if err == nil {
		return orderView(existing), nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	// 2. 总开关、商品、承接渠道、公网地址、未支付订单数
	pre, err := s.prepareOrder(ctx, userID, req)
	if err != nil {
		return nil, err
	}
	// 3. 落库 creating 订单（锁定商品快照和渠道配置版本）；幂等键撞上并发的同一请求时取回那一笔
	o, existing, err := s.insertOrder(ctx, userID, req, clientIP, pre)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return orderView(existing), nil
	}
	// 4. 向渠道下单（事务外的网络请求）
	return s.submitUpstream(ctx, o, pre)
}

// orderPrep 是下单前校验得到的东西。
type orderPrep struct {
	product *model.RechargeProduct
	lp      *loadedProvider
	baseURL string
}

// prepareOrder 做下单前的全部校验，任何一项不满足都返回对应的业务错误。
func (s *PaymentService) prepareOrder(ctx context.Context, userID uint64, req *model.CreatePaymentOrderReq) (*orderPrep, error) {
	st, err := s.loadSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !st.Enabled {
		return nil, errcode.ErrPaymentDisabled
	}
	product, err := s.products.GetProduct(ctx, req.ProductID)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && !product.OnSale) {
		return nil, errcode.ErrProductUnavailable
	}
	if err != nil {
		return nil, err
	}
	key, reason, err := s.methodRoute(ctx, st, req.Method)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return nil, errcode.ErrPayMethodUnavail.WithMsg("该支付方式暂不可用：" + reason)
	}
	// 回调地址和跳回地址都要用公网地址拼；没配就下不了单，比下一笔回调永远到不了的订单好
	if st.BaseURL == "" {
		return nil, errcode.ErrPayBaseURLMissing
	}
	// create_failed / closed 的订单不占名额，只数进行中的
	n, err := s.orders.CountOpenOrders(ctx, userID)
	if err != nil {
		return nil, err
	}
	if n >= int64(st.MaxPending) {
		return nil, errcode.ErrTooManyPendingOrder
	}
	lp, err := s.loadProvider(ctx, key)
	if err != nil {
		return nil, err
	}
	return &orderPrep{product: product, lp: lp, baseURL: st.BaseURL}, nil
}

// insertOrder 落库 creating 订单。订单号撞库（极小概率）时换号重试；幂等键撞上并发的同一请求时返回那一笔（existing 非空）。
func (s *PaymentService) insertOrder(ctx context.Context, userID uint64, req *model.CreatePaymentOrderReq, clientIP string, pre *orderPrep) (o, existing *model.PaymentOrder, err error) {
	snap, err := json.Marshal(map[string]any{
		"name": pre.product.Name, "price_fen": pre.product.PriceFen, "base_credits": pre.product.BaseCredits, "bonus_credits": pre.product.BonusCredits,
	})
	if err != nil {
		return nil, nil, err
	}
	now := s.now()
	timeout := time.Duration(pre.lp.channel.OrderTimeoutMinutes) * time.Minute
	for range payOrderNoRetries {
		no, err := newOrderNo(now)
		if err != nil {
			return nil, nil, err
		}
		o = &model.PaymentOrder{
			OrderNo: no, UserID: userID, IdempotencyKey: req.IdempotencyKey, ProductID: pre.product.ID, ProductSnapshot: snap,
			AmountFen: pre.product.PriceFen, Credits: pre.product.BaseCredits + pre.product.BonusCredits, Method: req.Method,
			Provider: pre.lp.provider.Key(), ChannelVersion: pre.lp.channel.Version, Status: model.PayOrderCreating,
			ExpiresAt: now.Add(timeout), ClientIP: clientIP, CreatedAt: now, UpdatedAt: now,
		}
		inserted, err := s.orders.InsertOrder(ctx, o)
		if err != nil {
			return nil, nil, err
		}
		if inserted {
			return o, nil, nil
		}
		// 没写入：要么幂等键被并发的同一请求抢先了，要么订单号撞了。前者取回那一笔，后者换号再来
		existing, err := s.orders.FindByIdempotencyKey(ctx, userID, req.IdempotencyKey)
		if err == nil {
			return nil, existing, nil
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, nil, err
		}
	}
	return nil, nil, errcode.ErrInternal.WithMsg("生成订单号失败，请重试")
}

// submitUpstream 向渠道下单并把订单推进到 pending（成功）或 create_failed（失败）。
func (s *PaymentService) submitUpstream(ctx context.Context, o *model.PaymentOrder, pre *orderPrep) (*model.PaymentOrderView, error) {
	uctx, cancel := context.WithTimeout(ctx, payUpstreamTimeout)
	defer cancel()
	co, err := pre.lp.provider.CreateOrder(uctx, pre.lp.cfg, payment.CreateReq{
		OrderNo: o.OrderNo, Method: o.Method, Subject: "积分充值 " + pre.product.Name, AmountFen: o.AmountFen,
		NotifyURL: pre.baseURL + "/api/v1/payment/notify/" + o.Provider,
		ReturnURL: pre.baseURL + "/pay/return?order_no=" + o.OrderNo,
		ExpiresAt: o.ExpiresAt, ClientIP: o.ClientIP,
	})
	if err != nil {
		// 失败原因入库前去掉疑似密钥的片段；返回给用户的永远是固定文案，不透传渠道的错误
		logger.Warn("向渠道下单失败", zap.String("order_no", o.OrderNo), zap.String("provider", o.Provider), zap.Error(err))
		if _, uerr := s.orders.UpdateOrderIf(ctx, o.ID, []string{model.PayOrderCreating},
			map[string]any{"status": model.PayOrderCreateFailed, "fail_reason": failReason(err), "updated_at": s.now()}); uerr != nil {
			return nil, uerr
		}
		return nil, errcode.ErrPayCreateFailed
	}
	updated, err := s.orders.UpdateOrderIf(ctx, o.ID, []string{model.PayOrderCreating}, map[string]any{
		"status": model.PayOrderPending, "checkout_mode": co.Mode, "checkout_value": co.Value, "updated_at": s.now(),
	})
	if err != nil {
		return nil, err
	}
	if !updated { // 补单任务抢先把它转走了（下单耗时超过 2 分钟的极端情况），以库里的为准
		cur, err := s.reload(ctx, o.OrderNo)
		if err != nil {
			return nil, err
		}
		return orderView(cur), nil
	}
	o.Status, o.CheckoutMode, o.CheckoutValue = model.PayOrderPending, co.Mode, co.Value
	return orderView(o), nil
}

// GetOrder 查询当前用户的订单（只读库，前端每 3 秒轮询它）。别人的订单和不存在一样返回 65006。
func (s *PaymentService) GetOrder(ctx context.Context, userID uint64, orderNo string) (*model.PaymentOrderView, error) {
	o, err := s.orders.FindUserOrder(ctx, userID, orderNo)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrPayOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	return orderView(o), nil
}

// PendingOrder 返回当前用户最近一笔进行中的订单，用于刷新页面或重新打开商城后恢复收银面板；没有返回 nil。
func (s *PaymentService) PendingOrder(ctx context.Context, userID uint64) (*model.PaymentOrderView, error) {
	o, err := s.orders.LatestOpenOrder(ctx, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil // 没有进行中的订单是正常情况，调用方判 nil
	}
	if err != nil {
		return nil, err
	}
	return orderView(o), nil
}
