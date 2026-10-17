// Package payment 定义支付渠道适配器的契约。适配器只做「签名、拼请求、解析响应、验签」，
// 金额校验、幂等入账、积分记账全部由 service 层负责，适配器碰不到数据库和积分。
package payment

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// 支付方式与收银形态的取值，和 model 里的常量一致；这里重复声明是因为 model 不能被适配器依赖。
const (
	MethodWechat = "wechat"
	MethodAlipay = "alipay"

	CheckoutQRCode      = "qrcode"       // Value 是二维码内容，前端画成二维码
	CheckoutQRCodeImage = "qrcode_image" // Value 是渠道已经画好的二维码图片地址
	CheckoutRedirect    = "redirect"     // Value 是付款页地址，前端新开标签页
)

// 交易状态。
const (
	StatePaid     = "paid"      // 已付款
	StatePending  = "pending"   // 等待付款
	StateClosed   = "closed"    // 渠道侧已关闭
	StateNotExist = "not_exist" // 渠道没有这笔订单（用户还没打开付款页时，支付宝和微信都会这样回答）
)

// ErrVerify 表示通知验签或解密失败：不能信任这个请求，service 不会改动任何订单。
var ErrVerify = errors.New("通知验签失败")

// Config 是一个渠道的完整配置：非敏感字段和敏感字段合并后的键值，键是 Field.Key。
type Config map[string]string

// Field 描述渠道配置的一个字段，后台渠道配置页按它渲染表单。
type Field struct {
	Key         string `json:"key"`                   // 配置键
	Label       string `json:"label"`                 // 显示名
	Secret      bool   `json:"secret"`                // 敏感字段：只写不读，加密存储
	Identity    bool   `json:"identity"`              // 商户身份字段：渠道有未完成订单时禁止修改
	Optional    bool   `json:"optional"`              // 可以不填
	Default     string `json:"default,omitempty"`     // 缺省值
	Placeholder string `json:"placeholder,omitempty"` // 输入提示
}

// Capabilities 是渠道的能力声明，决定超时、取消、丢了回调时系统怎么处理。
type Capabilities struct {
	Methods          []string `json:"methods"`            // 能承接的支付方式
	Checkout         string   `json:"checkout"`           // 默认收银形态说明：qrcode / redirect / mixed
	CanQuery         bool     `json:"can_query"`          // 能主动查单；不能查单的渠道丢了回调就无法自动补单
	CanCloseUpstream bool     `json:"can_close_upstream"` // 能在渠道侧关单；不能的渠道本地关单后用户仍可能付款
	ConfirmNotify    bool     `json:"confirm_notify"`     // 回调验签后还要再向渠道查一次单确认（MD5 签名的渠道）
}

// CreateReq 是下单请求。
type CreateReq struct {
	OrderNo   string    // 商户订单号
	Method    string    // MethodWechat / MethodAlipay
	Subject   string    // 商品标题
	AmountFen int       // 应付金额，单位分
	NotifyURL string    // 异步通知地址
	ReturnURL string    // 付款完成后浏览器跳回的地址
	ExpiresAt time.Time // 订单到期时间
	ClientIP  string    // 下单用户的 IP，部分渠道要求
}

// Checkout 是下单结果：前端据此展示二维码或跳转。
type Checkout struct {
	Mode  string // 见 Checkout* 常量
	Value string
}

// OrderRef 指向渠道里的一笔订单：除了商户订单号，还要带上支付方式，
// 因为有的渠道（虎皮椒）微信和支付宝是两组不同的凭证，要按支付方式选。
type OrderRef struct {
	OrderNo string // 商户订单号
	Method  string // MethodWechat / MethodAlipay
}

// TradeResult 是渠道对一笔交易的描述，来自查单或验签通过的通知。
type TradeResult struct {
	OrderNo  string    // 商户订单号
	TradeNo  string    // 渠道交易号，已付时必须有
	Identity string    // 通知里带的商户身份（appid / mchid / pid），没有就留空；有则 service 会和配置核对
	State    string    // 见 State* 常量
	PaidFen  int       // 实付金额，单位分，已付时有效
	PaidAt   time.Time // 付款时间，渠道没给就留零值
	EventKey string    // 通知去重键；查单结果留空
}

// NotifyRequest 是渠道发来的异步通知（原始请求，适配器自己解析）。
type NotifyRequest struct {
	Method string
	Header http.Header
	Query  url.Values
	Body   []byte
}

// Ack 是给渠道的应答：渠道按各自约定的格式判断「通知已收到」，格式错了会一直重试。
type Ack struct {
	Status      int
	ContentType string
	Body        string
}

// Provider 是支付渠道适配器。实现必须是并发安全的，不持有订单状态。
type Provider interface {
	// Key 返回渠道标识，和 model.Provider* 常量一致。
	Key() string
	// Capabilities 返回渠道能力。
	Capabilities() Capabilities
	// Fields 返回配置字段定义。
	Fields() []Field
	// Identity 返回这个配置下、指定支付方式的商户身份，用于和通知里的身份核对；该渠道没有身份概念返回空。
	Identity(cfg Config, method string) string
	// ValidateConfig 做本地格式校验（必填、私钥能解析、地址合法），不发网络请求。
	ValidateConfig(cfg Config) error
	// Probe 在线探测凭证是否可用（用一个不存在的订单号查单）。无法探测的渠道返回 ErrProbeUnsupported。
	Probe(ctx context.Context, cfg Config) error
	// CreateOrder 向渠道下单。
	CreateOrder(ctx context.Context, cfg Config, req CreateReq) (Checkout, error)
	// QueryOrder 查单。渠道没有这笔订单时返回 State=StateNotExist，而不是错误。
	QueryOrder(ctx context.Context, cfg Config, ref OrderRef) (TradeResult, error)
	// CloseOrder 在渠道侧关单；只有 Capabilities().CanCloseUpstream 为真时才会被调用。订单在渠道侧不存在视为成功。
	CloseOrder(ctx context.Context, cfg Config, ref OrderRef) error
	// VerifyNotify 验签（微信还要解密）并解析通知；验签失败返回 ErrVerify，不得返回任何部分结果。
	VerifyNotify(ctx context.Context, cfg Config, req NotifyRequest) (TradeResult, error)
	// NotifyAck 返回应答：ok 为真表示通知已妥善处理，为假表示请渠道稍后重试。
	NotifyAck(ok bool) Ack
}

// ErrProbeUnsupported 表示该渠道没有可用的在线探测手段，后台只做本地校验并提示用小额真实订单测试。
var ErrProbeUnsupported = errors.New("该渠道不支持在线校验")

// Registry 按 key 查找适配器。
type Registry struct {
	items map[string]Provider
	order []string
}

// NewRegistry 创建注册表，顺序即后台列表的展示顺序。
func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{items: make(map[string]Provider, len(providers))}
	for _, p := range providers {
		r.items[p.Key()] = p
		r.order = append(r.order, p.Key())
	}
	return r
}

// Get 返回指定渠道的适配器。
func (r *Registry) Get(key string) (Provider, bool) {
	p, ok := r.items[key]
	return p, ok
}

// Keys 返回全部渠道 key，按注册顺序。
func (r *Registry) Keys() []string { return append([]string(nil), r.order...) }
