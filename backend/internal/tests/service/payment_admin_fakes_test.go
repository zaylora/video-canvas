package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// 本文件给 fakePay 补上后台管理用的仓储方法（渠道配置写入、商品增删改、订单列表），语义和真实仓储一致：
// 乐观锁冲突返回 ErrRevisionConflict，商品有订单返回 ErrInUse，排序 ids 不是全集返回 ErrOrderMismatch。

func (f *fakePay) CreateChannel(_ context.Context, ch *model.PaymentChannel) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.channels[ch.Provider]; ok {
		return repository.ErrRevisionConflict
	}
	c := *ch
	f.channels[ch.Provider] = &c
	return nil
}

func (f *fakePay) UpdateChannelIf(_ context.Context, provider string, version int, fields map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.channels[provider]
	if !ok || c.Version != version {
		return repository.ErrRevisionConflict
	}
	for k, v := range fields {
		switch k {
		case "config_json":
			c.ConfigJSON = asJSON(v)
		case "secret_set":
			c.SecretSet = asJSON(v)
		case "order_timeout_minutes":
			c.OrderTimeoutMinutes = v.(int)
		case "enabled":
			c.Enabled = v.(bool)
		case "validated_at":
			if v == nil {
				c.ValidatedAt = nil
			} else {
				t := v.(time.Time)
				c.ValidatedAt = &t
			}
		case "updated_by":
			c.UpdatedBy = v.(uint64)
		}
	}
	c.Version++
	return nil
}

func (f *fakePay) SetChannelValidated(_ context.Context, provider string, version int, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.channels[provider]
	if !ok || c.Version != version {
		return repository.ErrRevisionConflict
	}
	c.ValidatedAt = &at
	return nil
}

func (f *fakePay) SetChannelEnabled(_ context.Context, provider string, enabled bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.channels[provider]
	if !ok {
		return repository.ErrNotFound
	}
	c.Enabled = enabled
	return nil
}

func (f *fakePay) CountOpenOrdersByProvider(_ context.Context, provider string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, o := range f.orders {
		if o.Provider == provider && isOpenStatus(o.Status) {
			n++
		}
	}
	return n, nil
}

func (f *fakePay) CreateProduct(_ context.Context, p *model.RechargeProduct) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	p.ID = f.nextID
	c := *p
	f.products[p.ID] = &c
	return nil
}

func (f *fakePay) UpdateProductIf(_ context.Context, id uint64, version int, fields map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.products[id]
	if !ok {
		return repository.ErrNotFound
	}
	if p.Version != version {
		return repository.ErrRevisionConflict
	}
	for k, v := range fields {
		switch k {
		case "name":
			p.Name = v.(string)
		case "price_fen":
			p.PriceFen = v.(int)
		case "base_credits":
			p.BaseCredits = v.(int)
		case "bonus_credits":
			p.BonusCredits = v.(int)
		case "badge":
			p.Badge = v.(string)
		case "on_sale":
			p.OnSale = v.(bool)
		}
	}
	p.Version++
	return nil
}

func (f *fakePay) DeleteProduct(_ context.Context, id uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.products[id]; !ok {
		return repository.ErrNotFound
	}
	for _, o := range f.orders {
		if o.ProductID == id {
			return repository.ErrInUse
		}
	}
	delete(f.products, id)
	return nil
}

func (f *fakePay) SetProductSort(_ context.Context, ids []uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(ids) != len(f.products) {
		return repository.ErrOrderMismatch
	}
	seen := map[uint64]bool{}
	for _, id := range ids {
		if _, ok := f.products[id]; !ok || seen[id] {
			return repository.ErrOrderMismatch
		}
		seen[id] = true
	}
	for i, id := range ids {
		f.products[id].Sort = i
	}
	return nil
}

func (f *fakePay) ListOrders(_ context.Context, flt repository.PaymentOrderFilter) ([]model.AdminPaymentOrderRow, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastFilter = flt
	var rows []model.AdminPaymentOrderRow
	for _, o := range f.pick(func(o *model.PaymentOrder) bool { return flt.Status == "" || o.Status == flt.Status }, 0) {
		rows = append(rows, model.AdminPaymentOrderRow{PaymentOrder: o, Username: "user-" + string(rune('a'+o.UserID%26))})
	}
	return rows, int64(len(rows)), nil
}

func (f *fakePay) FindOrderWithUser(_ context.Context, orderNo string) (*model.AdminPaymentOrderRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if o := f.orderByNo(orderNo); o != nil {
		return &model.AdminPaymentOrderRow{PaymentOrder: *o, Username: "alice"}, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakePay) ListNotifications(_ context.Context, orderNo string) ([]model.PaymentNotification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.PaymentNotification
	for _, n := range f.notifs {
		if n.OrderNo == orderNo {
			out = append(out, n)
		}
	}
	return out, nil
}

// ---------- 后台测试装配 ----------

// adminEnv 在 payEnv 的基础上装配后台服务：fake 渠道带上和真实渠道一样形状的字段定义。
type adminEnv struct {
	*payEnv
	admin *PaymentAdminService
}

// testFields 是 fake 渠道的字段：一个身份字段、一个普通字段、两个密钥字段（其中一个可选）。
func testFields() []payment.Field {
	return []payment.Field{
		{Key: "app_id", Label: "AppID", Identity: true},
		{Key: "gateway", Label: "网关", Optional: true, Default: "https://gw.example.com"},
		{Key: "api_key", Label: "密钥", Secret: true},
		{Key: "extra_secret", Label: "备用密钥", Secret: true, Optional: true},
	}
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	e := newPayEnv(t)
	e.wx.fields, e.ali.fields = testFields(), testFields()
	// 渠道行：secret_set 与密钥存储保持一致
	for _, key := range []string{model.ProviderWechatNative, model.ProviderAlipayPage} {
		now := e.clock.Now()
		ch := e.store.channels[key]
		ch.ConfigJSON, _ = json.Marshal(map[string]string{"app_id": "wx-app"})
		ch.SecretSet, _ = json.Marshal(map[string]bool{"api_key": true})
		ch.ValidatedAt = &now
	}
	a := NewPaymentAdminService(PaymentAdminDeps{
		Channels: e.store, Products: e.store, Orders: e.store, Settings: e.kv, Secrets: e.secret,
		Registry: payment.NewRegistry(e.wx, e.ali), Audit: e.audit, Now: e.clock.Now,
	})
	return &adminEnv{payEnv: e, admin: a}
}

// asJSON 取出 service 传给仓储的 JSON 列值（datatypes.JSON 或 []byte）。
func asJSON(v any) datatypes.JSON {
	switch t := v.(type) {
	case datatypes.JSON:
		return t
	case []byte:
		return datatypes.JSON(t)
	}
	panic("config_json / secret_set 必须是 JSON 字节")
}
