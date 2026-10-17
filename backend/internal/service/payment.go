package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
)

// 支付流程里的时间参数。
const (
	defaultMaxPendingOrders = 3                // 每个用户最多的未支付订单数（后台可改，1–10）
	payCheckMinInterval     = 5 * time.Second  // 同一笔订单两次主动查单的最小间隔
	payUpstreamTimeout      = 15 * time.Second // 单次渠道请求（下单、查单、关单）的超时
	payRecheckAfter         = 2 * time.Minute  // 订单创建超过这么久还没结果，补单任务开始向渠道查单；两次补查之间也隔这么久
	payBatchLimit           = 100              // 补单任务每一轮每类订单最多处理的笔数
	payOrderNoRetries       = 3                // 订单号撞库时的重试次数
	payFailReasonMax        = 200              // 下单失败原因入库的最大字数
)

// PaymentDeps 是 PaymentService 的依赖。
type PaymentDeps struct {
	Orders      PaymentOrderRepo     // 订单与入账事务
	Channels    PaymentChannelRepo   // 渠道配置
	Products    PaymentProductRepo   // 充值商品
	Settings    SystemSettingRepo    // 支付总开关、公网地址、方式映射
	Secrets     PaymentSecretStore   // 渠道密钥（加密存储）
	Registry    PaymentRegistry      // 渠道适配器
	Credits     InitialCreditsPolicy // 新积分账户的初始积分
	Broadcaster ws.Broadcaster       // 为 nil 时不推送
	Audit       AdminAuditWriter     // 为 nil 时不写审计
	Now         func() time.Time     // 为 nil 时用 time.Now，测试注入假时钟
}

// providerLoader 负责「按渠道标识装载适配器 + 完整配置」，用户侧服务和后台服务共用。
type providerLoader struct {
	registry PaymentRegistry
	channels PaymentChannelRepo
	secrets  PaymentSecretStore
}

// PaymentService 是积分充值：商城、下单、回调入账、查单、关单和补单。
// 它是「钱的真相」的唯一入口：订单变成 credited 只有一条路 settleTrade，回调、主动查单、补单任务、管理员查单都汇到它。
type PaymentService struct {
	providerLoader
	orders   PaymentOrderRepo
	products PaymentProductRepo
	settings SystemSettingRepo
	credits  InitialCreditsPolicy
	bcast    ws.Broadcaster
	audit    AdminAuditWriter
	now      func() time.Time
}

// NewPaymentService 创建支付服务；Broadcaster 为 nil 时换成空实现，Now 为 nil 时用系统时钟。
func NewPaymentService(d PaymentDeps) *PaymentService {
	s := &PaymentService{
		providerLoader: providerLoader{registry: d.Registry, channels: d.Channels, secrets: d.Secrets},
		orders:         d.Orders, products: d.Products, settings: d.Settings, credits: d.Credits, bcast: d.Broadcaster, audit: d.Audit, now: d.Now,
	}
	if s.bcast == nil {
		s.bcast = ws.NopBroadcaster{}
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// paymentSettings 是 system_settings 里的支付设置。
type paymentSettings struct {
	Enabled    bool              // 支付总开关
	BaseURL    string            // 站点公网地址，不带末尾斜杠
	Methods    map[string]string // 支付方式 → 承接渠道 provider key
	MaxPending int               // 每个用户最多的未支付订单数
}

// loadSettings 读取支付设置；库值缺失或非法时用缺省（总开关关、不开放任何方式、最多 3 笔未支付）。
func (s *PaymentService) loadSettings(ctx context.Context) (*paymentSettings, error) {
	kv, err := s.settings.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	return parsePaymentSettings(kv), nil
}

// parsePaymentSettings 把 system_settings 的键值解析成支付设置，库值损坏时忽略而不是报错：
// 设置读取在下单热路径上，不能因为一行脏数据让支付整体不可用。
func parsePaymentSettings(kv map[string]string) *paymentSettings {
	v := &paymentSettings{
		Enabled: kv[model.SettingPaymentEnabled] == "true",
		BaseURL: strings.TrimRight(strings.TrimSpace(kv[model.SettingPaymentPublicBaseURL]), "/"),
		Methods: map[string]string{
			model.PayMethodWechat: kv[model.SettingPaymentMethodWechat],
			model.PayMethodAlipay: kv[model.SettingPaymentMethodAlipay],
		},
		MaxPending: defaultMaxPendingOrders,
	}
	if n, err := strconv.Atoi(kv[model.SettingPaymentMaxPending]); err == nil && n >= 1 && n <= 10 {
		v.MaxPending = n
	}
	return v
}

// paymentSecretName 是渠道某个敏感字段在 ai_secrets 里的凭证名。每个敏感字段单独一条密文：
// 单条凭证明文最大 4096 字节，把一个渠道的私钥和公钥打包在一起，4096 位 RSA 密钥就会超限；分开存还让「只改一个密钥」不用先读后合并。
func paymentSecretName(provider, field string) string { return "payment:" + provider + ":" + field }

// loadedProvider 是装载好的渠道：适配器、完整配置（非敏感 + 敏感合并）和渠道行。
type loadedProvider struct {
	provider payment.Provider
	cfg      payment.Config
	channel  *model.PaymentChannel
}

// loadProvider 读取渠道行和各个敏感字段的密钥，合并成适配器要用的完整配置。渠道没有配置过返回 repository.ErrNotFound；
// 适配器没有注册返回 errUnknownProvider。不检查是否启用：已有订单的回调、查单、补单在渠道停用后仍要能工作。
func (l providerLoader) loadProvider(ctx context.Context, key string) (*loadedProvider, error) {
	p, ok := l.registry.Get(key)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errUnknownProvider, key)
	}
	ch, err := l.channels.GetChannel(ctx, key)
	if err != nil {
		return nil, err
	}
	cfg := payment.Config{}
	if len(ch.ConfigJSON) > 0 {
		if err := json.Unmarshal(ch.ConfigJSON, &cfg); err != nil {
			return nil, fmt.Errorf("渠道 %s 的配置损坏：%w", key, err)
		}
	}
	for _, f := range p.Fields() {
		if !f.Secret {
			continue
		}
		v, err := l.secrets.Get(ctx, paymentSecretName(key, f.Key))
		switch {
		case errors.Is(err, ErrAISecretNotSet): // 这个密钥没设置过：配置里就没有它，校验阶段会因缺必填字段而失败
		case err != nil:
			return nil, err
		default:
			cfg[f.Key] = v
		}
	}
	return &loadedProvider{provider: p, cfg: cfg, channel: ch}, nil
}

// errUnknownProvider 表示渠道标识没有对应的适配器。
var errUnknownProvider = errors.New("未知的支付渠道")

// sensitiveRe 匹配错误文本里形如 secret=xxx / api_key: xxx 的片段，入库和日志前把值替换掉。
var sensitiveRe = regexp.MustCompile(`(?i)([a-z_]*(?:secret|key|token|password|sign)[a-z_]*)\s*[=:]\s*\S+`)

// failReason 把渠道错误整理成可入库的失败原因：去掉疑似密钥的片段并截断。
func failReason(err error) string {
	msg := sensitiveRe.ReplaceAllString(err.Error(), "$1=***")
	if r := []rune(msg); len(r) > payFailReasonMax {
		msg = string(r[:payFailReasonMax])
	}
	return msg
}

// orderView 把订单转成用户侧视图；只有 pending 订单才带收银信息。
func orderView(o *model.PaymentOrder) *model.PaymentOrderView {
	v := &model.PaymentOrderView{
		OrderNo: o.OrderNo, Status: o.Status, AmountFen: o.AmountFen, Credits: o.Credits, Method: o.Method,
		ExpiresAt: o.ExpiresAt, CreditedAt: o.CreditedAt, CloseReason: o.CloseReason, LatePaid: o.LatePaid,
		AbnormalReason: o.AbnormalReason, CreatedAt: o.CreatedAt,
	}
	if o.Status == model.PayOrderPending {
		v.Checkout = &model.PaymentCheckoutView{Mode: o.CheckoutMode, Value: o.CheckoutValue}
	}
	return v
}

// publishOrder 把订单状态变化推给下单用户：前端据此刷新商城、收银面板和余额。推送可以丢，前端有轮询兜底。
func (s *PaymentService) publishOrder(ctx context.Context, o *model.PaymentOrder) {
	s.bcast.Publish(ctx, ws.UserChannel(o.UserID), ws.Message{
		Type: ws.TypePaymentUpdated,
		Data: map[string]any{"order_no": o.OrderNo, "status": o.Status, "credits": o.Credits},
	})
}

// reload 重新读取订单；读不到（刚被删除之类）按订单不存在处理。
func (s *PaymentService) reload(ctx context.Context, orderNo string) (*model.PaymentOrder, error) {
	o, err := s.orders.FindOrder(ctx, orderNo)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrPayOrderNotFound
	}
	return o, err
}

// warnSwallowed 记录被有意吞掉的错误（用户可以重试、后台任务下一轮会再来），避免静默失败。
func warnSwallowed(msg, orderNo string, err error) {
	logger.Warn(msg, zap.String("order_no", orderNo), zap.Error(err))
}
