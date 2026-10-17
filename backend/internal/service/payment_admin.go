package service

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// PaymentChannelAdminRepo 是后台管理渠道配置用的数据访问接口（真实实现是 repository.PaymentRepository）。
type PaymentChannelAdminRepo interface {
	PaymentChannelRepo
	// CreateChannel 创建渠道配置行；已存在（并发的第一次保存）返回 repository.ErrRevisionConflict。
	CreateChannel(ctx context.Context, ch *model.PaymentChannel) error
	// UpdateChannelIf 乐观锁更新：WHERE provider=? AND version=?，同时 version+1；版本不一致返回 repository.ErrRevisionConflict。
	UpdateChannelIf(ctx context.Context, provider string, version int, fields map[string]any) error
	// SetChannelValidated 记录校验通过的时间，只在 version 没变时生效，否则返回 repository.ErrRevisionConflict。
	SetChannelValidated(ctx context.Context, provider string, version int, at time.Time) error
	// SetChannelEnabled 启用或停用渠道，渠道不存在返回 repository.ErrNotFound。
	SetChannelEnabled(ctx context.Context, provider string, enabled bool) error
	// CountOpenOrdersByProvider 统计某渠道进行中（creating / pending）的订单数。
	CountOpenOrdersByProvider(ctx context.Context, provider string) (int64, error)
}

// PaymentProductAdminRepo 是后台管理充值商品用的数据访问接口。
type PaymentProductAdminRepo interface {
	PaymentProductRepo
	// CreateProduct 新建商品。
	CreateProduct(ctx context.Context, p *model.RechargeProduct) error
	// UpdateProductIf 乐观锁更新：不存在返回 repository.ErrNotFound，版本不一致返回 repository.ErrRevisionConflict。
	UpdateProductIf(ctx context.Context, id uint64, version int, fields map[string]any) error
	// DeleteProduct 删除商品；已有订单返回 repository.ErrInUse，不存在返回 repository.ErrNotFound。
	DeleteProduct(ctx context.Context, id uint64) error
	// SetProductSort 按 ids 的顺序重写 sort；ids 不是全部商品的一个排列返回 repository.ErrOrderMismatch。
	SetProductSort(ctx context.Context, ids []uint64) error
}

// PaymentOrderAdminRepo 是后台查看订单用的数据访问接口。
type PaymentOrderAdminRepo interface {
	// ListOrders 分页查询订单（带用户名），按 id 倒序。
	ListOrders(ctx context.Context, f repository.PaymentOrderFilter) ([]model.AdminPaymentOrderRow, int64, error)
	// FindOrderWithUser 按订单号查订单并带出用户名，不存在返回 repository.ErrNotFound。
	FindOrderWithUser(ctx context.Context, orderNo string) (*model.AdminPaymentOrderRow, error)
	// ListNotifications 返回某订单收到的渠道通知，按时间升序。
	ListNotifications(ctx context.Context, orderNo string) ([]model.PaymentNotification, error)
}

// PaymentAdminDeps 是 PaymentAdminService 的依赖。
type PaymentAdminDeps struct {
	Channels PaymentChannelAdminRepo
	Products PaymentProductAdminRepo
	Orders   PaymentOrderAdminRepo
	Settings SystemSettingRepo
	Secrets  PaymentSecretStore
	Registry PaymentRegistry
	Audit    AdminAuditWriter // 为 nil 时不写审计
	Now      func() time.Time // 为 nil 时用 time.Now
}

// PaymentAdminService 是后台「支付管理」：全局设置、渠道配置、充值商品、订单查看。
// 订单的查单和关单在 PaymentService（它们要走入账事务）。写操作都记审计，审计里只有字段名，没有密钥。
type PaymentAdminService struct {
	providerLoader
	channelsAdmin PaymentChannelAdminRepo
	products      PaymentProductAdminRepo
	orders        PaymentOrderAdminRepo
	settings      SystemSettingRepo
	audit         AdminAuditWriter
	now           func() time.Time
}

// NewPaymentAdminService 创建后台支付管理服务。
func NewPaymentAdminService(d PaymentAdminDeps) *PaymentAdminService {
	s := &PaymentAdminService{
		providerLoader: providerLoader{registry: d.Registry, channels: d.Channels, secrets: d.Secrets},
		channelsAdmin:  d.Channels, products: d.Products, orders: d.Orders, settings: d.Settings, audit: d.Audit, now: d.Now,
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// settingsView 把解析后的设置转成视图。
func settingsView(st *paymentSettings) *model.PaymentSettingsView {
	return &model.PaymentSettingsView{
		Enabled: st.Enabled, PublicBaseURL: st.BaseURL, MaxPending: st.MaxPending,
		MethodWechat: st.Methods[model.PayMethodWechat], MethodAlipay: st.Methods[model.PayMethodAlipay],
	}
}

// Settings 返回当前的全局设置。
func (s *PaymentAdminService) Settings(ctx context.Context) (*model.PaymentSettingsView, error) {
	kv, err := s.settings.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	return settingsView(parsePaymentSettings(kv)), nil
}

// UpdateSettings 保存全局设置（仅 super_admin，由路由保证）：支付总开关、公网地址、两种方式的承接渠道、未支付订单上限。写审计。
func (s *PaymentAdminService) UpdateSettings(ctx context.Context, actorID uint64, req *model.UpdatePaymentSettingsReq) (*model.PaymentSettingsView, error) {
	// 1. 公网地址：http(s) 根地址；开启支付必须先有它（回调地址和跳回地址都靠它拼）
	base, err := normalizeBaseURL(req.PublicBaseURL)
	if err != nil {
		return nil, err
	}
	if req.Enabled && base == "" {
		return nil, errcode.ErrPayBaseURLMissing
	}
	if req.MaxPending < 1 || req.MaxPending > 10 {
		return nil, errcode.ErrInvalidParams.WithMsg("每个用户的未支付订单上限必须在 1 到 10 之间")
	}
	// 2. 支付方式的承接渠道：必须是已注册、支持该方式、且已启用的渠道，否则前台会出现点了必失败的按钮
	for method, key := range map[string]string{model.PayMethodWechat: req.MethodWechat, model.PayMethodAlipay: req.MethodAlipay} {
		if err := s.checkMethodChannel(ctx, method, key); err != nil {
			return nil, err
		}
	}
	// 3. 记下改前的值用于审计，再一次性写库
	before, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	err = s.settings.SetMany(ctx, map[string]string{
		model.SettingPaymentEnabled: strconv.FormatBool(req.Enabled), model.SettingPaymentPublicBaseURL: base,
		model.SettingPaymentMethodWechat: req.MethodWechat, model.SettingPaymentMethodAlipay: req.MethodAlipay,
		model.SettingPaymentMaxPending: strconv.Itoa(req.MaxPending),
	}, actorID)
	if err != nil {
		return nil, err
	}
	after := &model.PaymentSettingsView{Enabled: req.Enabled, PublicBaseURL: base, MethodWechat: req.MethodWechat, MethodAlipay: req.MethodAlipay, MaxPending: req.MaxPending}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditPaySettings, model.AdminAuditTargetPayment, 0, map[string]any{"before": before, "after": after})
	return after, nil
}

// checkMethodChannel 校验「某支付方式由某渠道承接」：key 为空表示不开放，直接通过。
func (s *PaymentAdminService) checkMethodChannel(ctx context.Context, method, key string) error {
	if key == "" {
		return nil
	}
	p, ok := s.registry.Get(key)
	if !ok || !slices.Contains(p.Capabilities().Methods, method) {
		return errcode.ErrInvalidParams.WithMsg("承接渠道不存在，或不支持" + methodLabel(method))
	}
	ch, err := s.channels.GetChannel(ctx, key)
	if (err != nil && isNotFound(err)) || (err == nil && !ch.Enabled) {
		return errcode.ErrPayChannelConfig.WithMsg(model.PaymentProviderNames[key] + " 还没有启用，请先配置并启用它")
	}
	return err
}

func methodLabel(method string) string {
	if method == model.PayMethodWechat {
		return "微信支付"
	}
	return "支付宝"
}

// normalizeBaseURL 校验并规范公网地址：空串合法（表示未配置）；否则必须是 http(s)://主机[:端口]，不带路径、参数、用户名密码；去掉末尾斜杠。
func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	bad := err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != ""
	if bad {
		return "", errcode.ErrInvalidParams.WithMsg("公网地址必须是 http:// 或 https:// 开头的站点根地址，不带路径和参数，例如 https://example.com")
	}
	return raw, nil
}
