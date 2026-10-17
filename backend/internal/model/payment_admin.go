package model

import "time"

// PaymentProviderNames 是渠道的显示名，后台列表和审计用。
var PaymentProviderNames = map[string]string{
	ProviderAlipayPage:   "支付宝电脑网站支付",
	ProviderWechatNative: "微信支付 Native",
	ProviderEpay:         "易支付",
	ProviderXunhupay:     "虎皮椒",
}

// PaymentSettingsView 是后台「全局设置」：支付总开关、站点公网地址、两种支付方式各由哪个渠道承接。
type PaymentSettingsView struct {
	Enabled       bool   `json:"enabled"`              // 支付总开关
	PublicBaseURL string `json:"public_base_url"`      // 站点公网地址，拼回调和跳回地址，如 https://example.com
	MethodWechat  string `json:"method_wechat"`        // 微信由哪个渠道承接（provider key），空表示不开放
	MethodAlipay  string `json:"method_alipay"`        // 支付宝由哪个渠道承接
	MaxPending    int    `json:"max_pending_per_user"` // 每个用户最多的未支付订单数
}

// UpdatePaymentSettingsReq 是保存全局设置的请求，字段含义同 PaymentSettingsView。
type UpdatePaymentSettingsReq struct {
	Enabled       bool   `json:"enabled"`
	PublicBaseURL string `json:"public_base_url" binding:"max=200" label:"公网地址"`
	MethodWechat  string `json:"method_wechat" binding:"max=32" label:"微信承接渠道"`
	MethodAlipay  string `json:"method_alipay" binding:"max=32" label:"支付宝承接渠道"`
	MaxPending    int    `json:"max_pending_per_user" binding:"min=1,max=10" label:"未支付订单上限"`
}

// PaymentFieldView 是渠道配置的一个字段：定义 + 当前值。敏感字段永远不返回值，只说明是否已设置。
type PaymentFieldView struct {
	Key         string `json:"key"`                   // 配置键
	Label       string `json:"label"`                 // 显示名
	Secret      bool   `json:"secret"`                // 敏感字段：只写不读
	Identity    bool   `json:"identity"`              // 商户身份字段：渠道有未完成订单时禁止修改
	Optional    bool   `json:"optional"`              // 可以不填
	Default     string `json:"default,omitempty"`     // 缺省值
	Placeholder string `json:"placeholder,omitempty"` // 输入提示
	Value       string `json:"value"`                 // 当前值；敏感字段恒为空串
	SecretSet   bool   `json:"secret_set"`            // 敏感字段是否已设置
}

// PaymentChannelView 是后台渠道列表里的一项。
type PaymentChannelView struct {
	Provider         string             `json:"provider"`           // 渠道标识
	Name             string             `json:"name"`               // 显示名
	Methods          []string           `json:"methods"`            // 能承接的支付方式
	Checkout         string             `json:"checkout"`           // 收银形态说明：qrcode / redirect / mixed
	CanQuery         bool               `json:"can_query"`          // 能主动查单
	CanCloseUpstream bool               `json:"can_close_upstream"` // 能在渠道侧关单
	Fields           []PaymentFieldView `json:"fields"`             // 配置字段
	Configured       bool               `json:"configured"`         // 是否保存过配置
	Enabled          bool               `json:"enabled"`            // 是否启用
	Validated        bool               `json:"validated"`          // 当前配置是否已通过校验
	ValidatedAt      *time.Time         `json:"validated_at"`       // 最近一次校验通过的时间
	Version          int                `json:"version"`            // 乐观锁版本，保存时必须带上；未配置过为 0
	TimeoutMinutes   int                `json:"order_timeout_minutes"`
	OpenOrders       int64              `json:"open_orders"`   // 未完成订单数
	NotifyURL        string             `json:"notify_url"`    // 需要在渠道商户后台登记的回调地址；未配置公网地址时为空
	Serving          []string           `json:"serving"`       // 当前由它承接的支付方式
	ProbeSkipped     bool               `json:"probe_skipped"` // 仅校验接口返回：该渠道无法在线探测，只做了本地校验
	UpdatedAt        *time.Time         `json:"updated_at"`    // 最近保存时间
}

// UpdatePaymentChannelReq 是保存渠道配置的请求。Config 是非敏感字段的完整取值（缺少的字段视为清空）；
// Secrets 只传要修改的敏感字段，不传或传空串表示保持原值。Version 是上次读到的版本，未配置过传 0。
type UpdatePaymentChannelReq struct {
	Version        int               `json:"version" binding:"min=0"`
	Config         map[string]string `json:"config"`
	Secrets        map[string]string `json:"secrets"`
	TimeoutMinutes *int              `json:"order_timeout_minutes" binding:"omitempty,min=5,max=120" label:"未支付自动关闭时间"`
}

// PaymentProductReq 是新建 / 修改充值商品的请求。修改时必须带 Version（乐观锁），新建时忽略。
type PaymentProductReq struct {
	Name         string `json:"name" binding:"required,max=64" label:"名称"`
	PriceFen     int    `json:"price_fen" binding:"required,min=1,max=100000000" label:"售价"`
	BaseCredits  int    `json:"base_credits" binding:"required,min=1,max=1000000000" label:"基础积分"`
	BonusCredits int    `json:"bonus_credits" binding:"min=0,max=1000000000" label:"赠送积分"`
	Badge        string `json:"badge" binding:"max=8" label:"角标"`
	OnSale       *bool  `json:"on_sale"`
	Version      int    `json:"version" binding:"min=0"`
}

// SortPaymentProductsReq 是商品排序请求：ids 必须是全部商品 id 的一个排列。
type SortPaymentProductsReq struct {
	IDs []uint64 `json:"ids" binding:"required,min=1,max=200"`
}

// ListPaymentOrdersReq 是 GET /admin/payment/orders 的查询参数；关键字匹配订单号和用户名。
type ListPaymentOrdersReq struct {
	AdminListReq
	Status   string `form:"status" binding:"omitempty,oneof=creating pending credited closed create_failed abnormal" label:"状态"`
	Method   string `form:"method" binding:"omitempty,oneof=wechat alipay" label:"支付方式"`
	Provider string `form:"provider" binding:"max=32" label:"渠道"`
	From     string `form:"from" binding:"max=40" label:"开始时间"` // RFC3339
	To       string `form:"to" binding:"max=40" label:"结束时间"`   // RFC3339
}

// AdminPaymentOrderRow 是订单列表的一行：订单加上用户名（联表查出）。
type AdminPaymentOrderRow struct {
	PaymentOrder
	Username string `json:"username"` // 下单用户的用户名
}

// AdminPaymentOrderView 是后台看到的订单：比用户侧多了用户、渠道、商品快照和各时间点。
type AdminPaymentOrderView struct {
	OrderNo         string     `json:"order_no"`
	UserID          uint64     `json:"user_id"`
	Username        string     `json:"username"`
	ProductName     string     `json:"product_name"` // 取自下单时的商品快照
	AmountFen       int        `json:"amount_fen"`
	Credits         int        `json:"credits"`
	Method          string     `json:"method"`
	Provider        string     `json:"provider"`
	ProviderName    string     `json:"provider_name"`
	Status          string     `json:"status"`
	ProviderTradeNo string     `json:"provider_trade_no"`
	PaidAmountFen   int        `json:"paid_amount_fen"`
	LatePaid        bool       `json:"late_paid"`
	CloseReason     string     `json:"close_reason"`
	AbnormalReason  string     `json:"abnormal_reason"`
	FailReason      string     `json:"fail_reason"`
	ChannelVersion  int        `json:"channel_version"`
	ExpiresAt       time.Time  `json:"expires_at"`
	PaidAt          *time.Time `json:"paid_at"`
	CreditedAt      *time.Time `json:"credited_at"`
	ClosedAt        *time.Time `json:"closed_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

// AdminPaymentOrderDetail 是订单详情：订单 + 收到的渠道通知记录（用来排查「付了没到账」）。
type AdminPaymentOrderDetail struct {
	AdminPaymentOrderView
	Notifications []PaymentNotification `json:"notifications"`
}
