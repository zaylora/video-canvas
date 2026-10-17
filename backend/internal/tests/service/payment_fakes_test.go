package service_test

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// ---------- 内存版支付仓储 ----------

// fakePay 同时实现 PaymentOrderRepo / PaymentTx / PaymentChannelRepo / PaymentProductRepo。
// WithTx 整个过程持有互斥锁（模拟行锁串行化），出错时还原快照（模拟回滚）；tx 方法不再加锁，
// 所以 service 在事务里误调非事务方法会死锁，测试会因此暴露出来。
type fakePay struct {
	mu         sync.Mutex
	nextID     uint64
	orders     map[uint64]*model.PaymentOrder
	credits    map[uint64]*model.UserCredit
	ledger     []model.CreditLedger
	products   map[uint64]*model.RechargeProduct
	channels   map[string]*model.PaymentChannel
	notifs     []model.PaymentNotification
	lastFilter repository.PaymentOrderFilter // 最近一次 ListOrders 收到的筛选条件
	txErr      error                         // 非空时 WithTx 里的 fn 之后返回它，用来模拟提交失败
}

func newFakePay() *fakePay {
	return &fakePay{
		nextID: 100, orders: map[uint64]*model.PaymentOrder{}, credits: map[uint64]*model.UserCredit{},
		products: map[uint64]*model.RechargeProduct{}, channels: map[string]*model.PaymentChannel{},
	}
}

func (f *fakePay) orderByNo(no string) *model.PaymentOrder {
	for _, o := range f.orders {
		if o.OrderNo == no {
			return o
		}
	}
	return nil
}

func (f *fakePay) ledgerOf(orderID uint64) []model.CreditLedger {
	var out []model.CreditLedger
	for _, l := range f.ledger {
		if l.PaymentOrderID != nil && *l.PaymentOrderID == orderID {
			out = append(out, l)
		}
	}
	return out
}

type paySnap struct {
	orders   map[uint64]model.PaymentOrder
	credits  map[uint64]model.UserCredit
	ledger   []model.CreditLedger
	products map[uint64]model.RechargeProduct
}

func (f *fakePay) snapshot() paySnap {
	s := paySnap{orders: map[uint64]model.PaymentOrder{}, credits: map[uint64]model.UserCredit{}, products: map[uint64]model.RechargeProduct{}}
	for k, v := range f.orders {
		s.orders[k] = *v
	}
	for k, v := range f.credits {
		s.credits[k] = *v
	}
	for k, v := range f.products {
		s.products[k] = *v
	}
	s.ledger = append(s.ledger, f.ledger...)
	return s
}

func (f *fakePay) restore(s paySnap) {
	f.orders, f.credits, f.products = map[uint64]*model.PaymentOrder{}, map[uint64]*model.UserCredit{}, map[uint64]*model.RechargeProduct{}
	for k, v := range s.orders {
		v := v
		f.orders[k] = &v
	}
	for k, v := range s.credits {
		v := v
		f.credits[k] = &v
	}
	for k, v := range s.products {
		v := v
		f.products[k] = &v
	}
	f.ledger = s.ledger
}

func (f *fakePay) WithTx(_ context.Context, fn func(tx PaymentTx) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	snap := f.snapshot()
	if err := fn(f); err != nil {
		f.restore(snap)
		return err
	}
	if f.txErr != nil {
		f.restore(snap)
		return f.txErr
	}
	return nil
}

// ---- PaymentTx ----

func (f *fakePay) LockOrder(_ context.Context, orderNo string) (*model.PaymentOrder, error) {
	if o := f.orderByNo(orderNo); o != nil {
		c := *o
		return &c, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakePay) UpdateOrder(_ context.Context, id uint64, fields map[string]any) error {
	o, ok := f.orders[id]
	if !ok {
		return repository.ErrNotFound
	}
	applyOrderFields(o, fields)
	return nil
}

func (f *fakePay) TradeNoTaken(_ context.Context, provider, tradeNo string, exceptID uint64) (bool, error) {
	for _, o := range f.orders {
		if o.ID != exceptID && o.Provider == provider && o.ProviderTradeNo == tradeNo {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakePay) EnsureCredit(_ context.Context, userID uint64, initial int) error {
	if _, ok := f.credits[userID]; !ok {
		f.credits[userID] = &model.UserCredit{UserID: userID, Balance: initial}
	}
	return nil
}

func (f *fakePay) AddCredit(_ context.Context, userID uint64, dBalance, dFrozen int) error {
	c, ok := f.credits[userID]
	if !ok {
		return repository.ErrNotFound
	}
	c.Balance += dBalance
	c.Frozen += dFrozen
	return nil
}

func (f *fakePay) InsertLedger(_ context.Context, e *model.CreditLedger) (bool, error) {
	if e.PaymentOrderID != nil {
		for _, l := range f.ledger {
			if l.PaymentOrderID != nil && *l.PaymentOrderID == *e.PaymentOrderID && l.Type == e.Type {
				return false, nil
			}
		}
	}
	f.ledger = append(f.ledger, *e)
	return true, nil
}

func (f *fakePay) IncProductSold(_ context.Context, id uint64) error {
	if p, ok := f.products[id]; ok {
		p.SoldCount++
	}
	return nil
}

// applyOrderFields 把 UpdateOrder / UpdateOrderIf 的列更新应用到内存订单上，只支持 service 用到的列。
func applyOrderFields(o *model.PaymentOrder, fields map[string]any) {
	for k, v := range fields {
		switch k {
		case "status":
			o.Status = v.(string)
		case "checkout_mode":
			o.CheckoutMode = v.(string)
		case "checkout_value":
			o.CheckoutValue = v.(string)
		case "provider_trade_no":
			o.ProviderTradeNo = v.(string)
		case "paid_amount_fen":
			o.PaidAmountFen = v.(int)
		case "paid_at":
			t := v.(time.Time)
			o.PaidAt = &t
		case "credited_at":
			t := v.(time.Time)
			o.CreditedAt = &t
		case "closed_at":
			t := v.(time.Time)
			o.ClosedAt = &t
		case "close_reason":
			o.CloseReason = v.(string)
		case "late_paid":
			o.LatePaid = v.(bool)
		case "abnormal_reason":
			o.AbnormalReason = v.(string)
		case "fail_reason":
			o.FailReason = v.(string)
		case "last_queried_at":
			t := v.(time.Time)
			o.LastQueriedAt = &t
		}
	}
}

// ---- PaymentOrderRepo ----

func (f *fakePay) FindOrder(_ context.Context, orderNo string) (*model.PaymentOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if o := f.orderByNo(orderNo); o != nil {
		c := *o
		return &c, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakePay) FindUserOrder(ctx context.Context, userID uint64, orderNo string) (*model.PaymentOrder, error) {
	o, err := f.FindOrder(ctx, orderNo)
	if err != nil || o.UserID != userID {
		return nil, repository.ErrNotFound
	}
	return o, nil
}

func (f *fakePay) FindByIdempotencyKey(_ context.Context, userID uint64, key string) (*model.PaymentOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.orders {
		if o.UserID == userID && o.IdempotencyKey == key {
			c := *o
			return &c, nil
		}
	}
	return nil, repository.ErrNotFound
}

func isOpenStatus(s string) bool { return s == model.PayOrderCreating || s == model.PayOrderPending }

func (f *fakePay) CountOpenOrders(_ context.Context, userID uint64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, o := range f.orders {
		if o.UserID == userID && isOpenStatus(o.Status) {
			n++
		}
	}
	return n, nil
}

func (f *fakePay) LatestOpenOrder(_ context.Context, userID uint64) (*model.PaymentOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best *model.PaymentOrder
	for _, o := range f.orders {
		if o.UserID == userID && isOpenStatus(o.Status) && (best == nil || o.ID > best.ID) {
			best = o
		}
	}
	if best == nil {
		return nil, repository.ErrNotFound
	}
	c := *best
	return &c, nil
}

func (f *fakePay) InsertOrder(_ context.Context, o *model.PaymentOrder) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.orders {
		if e.UserID == o.UserID && e.IdempotencyKey == o.IdempotencyKey {
			return false, nil
		}
	}
	f.nextID++
	o.ID = f.nextID
	c := *o
	f.orders[o.ID] = &c
	return true, nil
}

func (f *fakePay) UpdateOrderIf(_ context.Context, id uint64, from []string, fields map[string]any) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.orders[id]
	if !ok {
		return false, nil
	}
	for _, s := range from {
		if o.Status == s {
			applyOrderFields(o, fields)
			return true, nil
		}
	}
	return false, nil
}

func (f *fakePay) pick(keep func(*model.PaymentOrder) bool, limit int) []model.PaymentOrder {
	var out []model.PaymentOrder
	for _, o := range f.orders {
		if keep(o) {
			out = append(out, *o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (f *fakePay) ListDueOrders(_ context.Context, now time.Time, limit int) ([]model.PaymentOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pick(func(o *model.PaymentOrder) bool {
		return o.Status == model.PayOrderPending && !o.ExpiresAt.After(now)
	}, limit), nil
}

func (f *fakePay) ListUnconfirmedOrders(_ context.Context, createdBefore, queriedBefore time.Time, limit int) ([]model.PaymentOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pick(func(o *model.PaymentOrder) bool {
		return o.Status == model.PayOrderPending && o.CreatedAt.Before(createdBefore) &&
			(o.LastQueriedAt == nil || o.LastQueriedAt.Before(queriedBefore))
	}, limit), nil
}

func (f *fakePay) ListStuckCreating(_ context.Context, before time.Time, limit int) ([]model.PaymentOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pick(func(o *model.PaymentOrder) bool {
		return o.Status == model.PayOrderCreating && o.CreatedAt.Before(before)
	}, limit), nil
}

func (f *fakePay) InsertNotification(_ context.Context, n *model.PaymentNotification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.notifs {
		if e.Provider == n.Provider && e.EventKey == n.EventKey {
			return nil
		}
	}
	f.notifs = append(f.notifs, *n)
	return nil
}

// ---- 渠道与商品 ----

func (f *fakePay) ListChannels(context.Context) ([]model.PaymentChannel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.PaymentChannel
	for _, c := range f.channels {
		out = append(out, *c)
	}
	return out, nil
}

func (f *fakePay) GetChannel(_ context.Context, provider string) (*model.PaymentChannel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.channels[provider]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakePay) ListProducts(_ context.Context, onSaleOnly bool) ([]model.RechargeProduct, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.RechargeProduct
	for _, p := range f.products {
		if !onSaleOnly || p.OnSale {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (f *fakePay) GetProduct(_ context.Context, id uint64) (*model.RechargeProduct, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.products[id]; ok {
		c := *p
		return &c, nil
	}
	return nil, repository.ErrNotFound
}

// ---------- 可配置的渠道适配器 ----------

type fakeProvider struct {
	mu           sync.Mutex
	key          string
	caps         payment.Capabilities
	identity     string
	fields       []payment.Field // 配置字段定义
	validateErr  error           // ValidateConfig 的返回
	probeErr     error           // Probe 的返回
	validatedCfg payment.Config  // 最近一次 ValidateConfig 收到的完整配置

	createRes payment.Checkout
	createErr error
	createReq []payment.CreateReq

	queryRes   payment.TradeResult
	queryMap   map[string]payment.TradeResult           // 按订单号指定查单结果，优先于 queryRes
	queryHook  func(orderNo string) payment.TradeResult // 非空时由它决定查单结果（可按调用次数变化），优先于 queryMap
	queryErr   error
	queryCalls int

	closeErr   error
	closeCalls int

	verifyRes payment.TradeResult
	verifyErr error
}

func newFakeProvider(key string, method string) *fakeProvider {
	return &fakeProvider{
		key:       key,
		caps:      payment.Capabilities{Methods: []string{method}, Checkout: "qrcode", CanQuery: true, CanCloseUpstream: true},
		identity:  "mch-1",
		createRes: payment.Checkout{Mode: payment.CheckoutQRCode, Value: "weixin://pay/" + key},
		queryRes:  payment.TradeResult{State: payment.StatePending},
	}
}

func (p *fakeProvider) Key() string                        { return p.key }
func (p *fakeProvider) Capabilities() payment.Capabilities { return p.caps }
func (p *fakeProvider) Fields() []payment.Field            { return p.fields }
func (p *fakeProvider) Identity(payment.Config, string) string {
	return p.identity
}
func (p *fakeProvider) ValidateConfig(cfg payment.Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.validatedCfg = cfg
	return p.validateErr
}
func (p *fakeProvider) Probe(context.Context, payment.Config) error { return p.probeErr }

func (p *fakeProvider) CreateOrder(_ context.Context, _ payment.Config, req payment.CreateReq) (payment.Checkout, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.createReq = append(p.createReq, req)
	return p.createRes, p.createErr
}

func (p *fakeProvider) QueryOrder(_ context.Context, _ payment.Config, ref payment.OrderRef) (payment.TradeResult, error) {
	orderNo := ref.OrderNo
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queryCalls++
	r := p.queryRes
	if m, ok := p.queryMap[orderNo]; ok {
		r = m
	}
	if p.queryHook != nil {
		r = p.queryHook(orderNo)
	}
	if r.OrderNo == "" {
		r.OrderNo = orderNo
	}
	return r, p.queryErr
}

func (p *fakeProvider) CloseOrder(context.Context, payment.Config, payment.OrderRef) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeCalls++
	return p.closeErr
}

func (p *fakeProvider) VerifyNotify(context.Context, payment.Config, payment.NotifyRequest) (payment.TradeResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.verifyRes, p.verifyErr
}

func (p *fakeProvider) NotifyAck(ok bool) payment.Ack {
	if ok {
		return payment.Ack{Status: 200, Body: "success"}
	}
	return payment.Ack{Status: 400, Body: "fail"}
}

// ---------- 其它依赖 ----------

type fakeSecrets struct {
	mu sync.Mutex
	kv map[string]string
}

func (s *fakeSecrets) SetSecret(_ context.Context, name, value string, _ uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kv == nil {
		s.kv = map[string]string{}
	}
	s.kv[name] = value
	return nil
}

func (s *fakeSecrets) Get(_ context.Context, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.kv[name]; ok {
		return v, nil
	}
	return "", ErrAISecretNotSet
}

type fakeInitialCredits struct{ n int }

func (f fakeInitialCredits) InitialCredits(context.Context) (int, error) { return f.n, nil }

// recordingBroadcaster 记录发布的 WebSocket 消息。
type recordingBroadcaster struct {
	mu   sync.Mutex
	msgs []struct {
		channel string
		msg     ws.Message
	}
}

func (b *recordingBroadcaster) Publish(_ context.Context, channel string, msg ws.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.msgs = append(b.msgs, struct {
		channel string
		msg     ws.Message
	}{channel, msg})
}

func (b *recordingBroadcaster) count(typ string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, m := range b.msgs {
		if m.msg.Type == typ {
			n++
		}
	}
	return n
}

// payClock 是可手动拨动的时钟。
type payClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *payClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *payClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// ---------- 装配 ----------

const (
	payTestUser   = uint64(7)
	payTestBase   = "https://pay.example.com"
	payTestIdent  = "mch-1"
	payTestWechat = model.ProviderWechatNative
)

// payEnv 是一套装配好的支付服务：微信由 wechat_native 承接，支付宝由 alipay_page 承接，
// 商品 1 是 ¥100 → 10,000 积分 + 赠送 500，商品 2 已下架。
type payEnv struct {
	t      *testing.T
	svc    *PaymentService
	store  *fakePay
	wx     *fakeProvider
	ali    *fakeProvider
	kv     *fakeSettingsRepo
	secret *fakeSecrets
	ws     *recordingBroadcaster
	audit  *fakeAudit
	clock  *payClock
}

func newPayEnv(t *testing.T) *payEnv {
	t.Helper()
	e := &payEnv{
		t: t, store: newFakePay(), kv: newFakeSettingsRepo(), secret: &fakeSecrets{}, ws: &recordingBroadcaster{}, audit: &fakeAudit{},
		wx:    newFakeProvider(model.ProviderWechatNative, payment.MethodWechat),
		ali:   newFakeProvider(model.ProviderAlipayPage, payment.MethodAlipay),
		clock: &payClock{t: time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)},
	}
	e.kv.kv = map[string]string{
		model.SettingPaymentEnabled: "true", model.SettingPaymentPublicBaseURL: payTestBase,
		model.SettingPaymentMethodWechat: model.ProviderWechatNative, model.SettingPaymentMethodAlipay: model.ProviderAlipayPage,
	}
	cfg, _ := json.Marshal(map[string]string{"app_id": "wx-app"})
	for _, key := range []string{model.ProviderWechatNative, model.ProviderAlipayPage} {
		e.store.channels[key] = &model.PaymentChannel{
			ID: 1, Provider: key, Enabled: true, ConfigJSON: cfg, OrderTimeoutMinutes: 15, Version: 3,
		}
	}
	e.secret.kv = map[string]string{
		"payment:" + model.ProviderWechatNative + ":api_key": "k", "payment:" + model.ProviderAlipayPage + ":api_key": "k",
	}
	e.store.products[1] = &model.RechargeProduct{ID: 1, Name: "10,000 积分", PriceFen: 10000, BaseCredits: 10000, BonusCredits: 500, OnSale: true}
	e.store.products[2] = &model.RechargeProduct{ID: 2, Name: "已下架", PriceFen: 500, BaseCredits: 500, OnSale: false}
	e.svc = NewPaymentService(PaymentDeps{
		Orders: e.store, Channels: e.store, Products: e.store, Settings: e.kv, Secrets: e.secret,
		Registry: payment.NewRegistry(e.wx, e.ali), Credits: fakeInitialCredits{n: 50},
		Broadcaster: e.ws, Audit: e.audit, Now: e.clock.Now,
	})
	return e
}

// order 用 payTestUser 下一笔微信订单（商品 1），失败直接终止测试。
func (e *payEnv) order(key string) *model.PaymentOrderView {
	e.t.Helper()
	v, err := e.svc.CreateOrder(context.Background(), payTestUser,
		&model.CreatePaymentOrderReq{ProductID: 1, Method: model.PayMethodWechat, IdempotencyKey: key}, "1.2.3.4")
	if err != nil {
		e.t.Fatalf("下单失败：%v", err)
	}
	return v
}

// paid 让微信渠道的验签结果变成「订单 no 已付 fen 分」。
func (e *payEnv) paid(no string, fen int) {
	e.wx.verifyRes = payment.TradeResult{
		OrderNo: no, TradeNo: "T-" + no, Identity: payTestIdent, State: payment.StatePaid, PaidFen: fen,
		PaidAt: e.clock.Now(), EventKey: "evt-" + no,
	}
}

func (e *payEnv) notify(key string) payment.Ack {
	return e.svc.HandleNotify(context.Background(), key, payment.NotifyRequest{Method: "POST", Body: []byte("{}")})
}
