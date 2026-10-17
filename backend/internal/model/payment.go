package model

import (
	"time"

	"gorm.io/datatypes"
)

// 支付方式：用户在积分商城里看到的只有这两个，由后台指定各自的承接渠道。
const (
	PayMethodWechat = "wechat" // 微信支付
	PayMethodAlipay = "alipay" // 支付宝
)

// PayMethods 是全部支付方式，顺序即前台展示顺序。
var PayMethods = []string{PayMethodWechat, PayMethodAlipay}

// 支付渠道（provider）：Go 内置适配器，固定四个，后台只能配置和启停，不能新增。
const (
	ProviderAlipayPage   = "alipay_page"   // 支付宝电脑网站支付
	ProviderWechatNative = "wechat_native" // 微信支付 Native（APIv3）
	ProviderEpay         = "epay"          // 易支付（彩虹易支付协议 V1，MD5）
	ProviderXunhupay     = "xunhupay"      // 虎皮椒
)

// 订单状态。creating / pending 是进行中；credited / create_failed / abnormal 是终态；
// closed 在收到迟到的已付通知后还能转 credited（此时 late_paid=true）。
const (
	PayOrderCreating     = "creating"      // 已落库，正在向渠道下单
	PayOrderPending      = "pending"       // 等待用户付款
	PayOrderCredited     = "credited"      // 已付款且积分已入账（终态）
	PayOrderClosed       = "closed"        // 超时、用户取消或管理员关单
	PayOrderCreateFailed = "create_failed" // 渠道下单失败（终态）
	PayOrderAbnormal     = "abnormal"      // 金额不符、商户身份不符或交易号冲突，不自动入账（终态，人工处理）
)

// 订单关闭原因。
const (
	PayCloseTimeout = "timeout" // 到期
	PayCloseUser    = "user"    // 用户取消
	PayCloseAdmin   = "admin"   // 管理员关单
)

// 异常原因（abnormal_reason）。
const (
	PayAbnormalAmount   = "amount_mismatch"   // 实付金额与应付不符
	PayAbnormalIdentity = "identity_mismatch" // 回调里的商户身份与渠道配置不符
	PayAbnormalTradeNo  = "trade_no_conflict" // 渠道交易号已被别的订单占用
)

// 收银形态（checkout_mode）：决定前端怎么展示。
const (
	CheckoutQRCode      = "qrcode"       // value 是二维码内容，前端画成二维码
	CheckoutQRCodeImage = "qrcode_image" // value 是渠道已经画好的二维码图片地址，前端直接显示
	CheckoutRedirect    = "redirect"     // value 是付款页地址，前端新开标签页
)

// 通知处理结果（payment_notifications.result）。
const (
	PayNotifyCredited         = "credited"          // 本次通知完成了入账
	PayNotifyDuplicate        = "duplicate"         // 订单早已入账，本次是重复通知
	PayNotifyUnpaid           = "unpaid"            // 通知表示未付款（例如已关闭）
	PayNotifyAmountMismatch   = "amount_mismatch"   // 金额不符，订单转 abnormal
	PayNotifyIdentityMismatch = "identity_mismatch" // 商户身份不符，订单转 abnormal
	PayNotifyTradeNoConflict  = "trade_no_conflict" // 渠道交易号已被别的订单占用，订单转 abnormal
	PayNotifyNotFound         = "not_found"         // 订单号不存在
	PayNotifyStateRejected    = "state_rejected"    // 订单当前状态不允许入账（create_failed / abnormal）
)

// 支付相关的 system_settings 键。
const (
	SettingPaymentEnabled       = "payment.enabled"              // "true" / "false"，缺省关
	SettingPaymentPublicBaseURL = "payment.public_base_url"      // 站点公网地址，拼回调和跳回地址
	SettingPaymentMethodWechat  = "payment.method.wechat"        // 微信由哪个渠道承接（provider key），空表示不开放
	SettingPaymentMethodAlipay  = "payment.method.alipay"        // 支付宝由哪个渠道承接
	SettingPaymentMaxPending    = "payment.max_pending_per_user" // 每个用户最多的未支付订单数，缺省 3
)

// PaymentChannel 支付渠道配置：每个 provider 一行。非敏感字段在 ConfigJSON；
// 敏感字段整体加密后存进 ai_secrets（名 payment:<provider>），这里只记每个字段是否已设置。
type PaymentChannel struct {
	ID                  uint64         `gorm:"primaryKey" json:"id"`                                                      // 渠道 ID
	Provider            string         `gorm:"size:32;not null;uniqueIndex:uk_payment_channels_provider" json:"provider"` // 内置适配器的 key
	Enabled             bool           `gorm:"not null;default:false" json:"enabled"`                                     // 是否启用；启用前必须通过校验
	ConfigJSON          datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"config"`                            // 非敏感配置
	SecretSet           datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"secret_set"`                        // 每个敏感字段是否已设置，不含密文
	OrderTimeoutMinutes int            `gorm:"not null;default:15" json:"order_timeout_minutes"`                          // 未支付订单自动关闭的分钟数（5–120）
	ValidatedAt         *time.Time     `json:"validated_at"`                                                              // 最近一次校验通过的时间；修改配置后清空
	Version             int            `gorm:"not null;default:1" json:"version"`                                         // 乐观锁，每次保存 +1，订单下单时记下它
	UpdatedBy           uint64         `gorm:"not null;default:0" json:"updated_by"`                                      // 最后修改人
	CreatedAt           time.Time      `json:"created_at"`                                                                // 创建时间
	UpdatedAt           time.Time      `json:"updated_at"`                                                                // 更新时间
}

// TableName 表名。
func (PaymentChannel) TableName() string { return "payment_channels" }

// RechargeProduct 充值商品（积分商城里的一档）。价格和积分在下单时快照进订单，之后改商品不影响已下的订单。
type RechargeProduct struct {
	ID           uint64    `gorm:"primaryKey" json:"id"`                     // 商品 ID
	Name         string    `gorm:"size:64;not null" json:"name"`             // 名称，如「10,000 积分」
	PriceFen     int       `gorm:"not null" json:"price_fen"`                // 售价，单位分，必须 > 0
	BaseCredits  int       `gorm:"not null" json:"base_credits"`             // 基础积分，必须 > 0
	BonusCredits int       `gorm:"not null;default:0" json:"bonus_credits"`  // 赠送积分，>= 0
	Badge        string    `gorm:"size:16;not null;default:''" json:"badge"` // 角标，如「热门」，空表示没有
	Sort         int       `gorm:"not null;default:0" json:"sort"`           // 排序，越小越靠前
	OnSale       bool      `gorm:"not null;default:true" json:"on_sale"`     // 是否上架
	SoldCount    int       `gorm:"not null;default:0" json:"sold_count"`     // 已成功入账的笔数
	Version      int       `gorm:"not null;default:1" json:"version"`        // 乐观锁
	CreatedAt    time.Time `json:"created_at"`                               // 创建时间
	UpdatedAt    time.Time `json:"updated_at"`                               // 更新时间
}

// TableName 表名。
func (RechargeProduct) TableName() string { return "recharge_products" }

// PaymentOrder 支付订单。商品快照、承接渠道和渠道配置版本都在下单时锁定。
type PaymentOrder struct {
	ID              uint64         `gorm:"primaryKey" json:"id"`                                                                                                              // 订单 ID
	OrderNo         string         `gorm:"size:32;not null;uniqueIndex:uk_payment_orders_no" json:"order_no"`                                                                 // 订单号，发给渠道的商户订单号
	UserID          uint64         `gorm:"not null;index:idx_payment_orders_user,priority:1;uniqueIndex:uk_payment_orders_idem,priority:1" json:"user_id"`                    // 下单用户
	IdempotencyKey  string         `gorm:"size:64;not null;uniqueIndex:uk_payment_orders_idem,priority:2" json:"-"`                                                           // 客户端幂等键；重复提交返回同一笔订单
	ProductID       uint64         `gorm:"not null" json:"product_id"`                                                                                                        // 商品 ID
	ProductSnapshot datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"product_snapshot"`                                                                          // 商品快照 {name, price_fen, base_credits, bonus_credits}
	AmountFen       int            `gorm:"not null" json:"amount_fen"`                                                                                                        // 应付金额，单位分
	Credits         int            `gorm:"not null" json:"credits"`                                                                                                           // 到账积分 = 基础 + 赠送
	Method          string         `gorm:"size:16;not null" json:"method"`                                                                                                    // wechat / alipay
	Provider        string         `gorm:"size:32;not null;uniqueIndex:uk_payment_orders_trade,priority:1,where:provider_trade_no <> ''" json:"provider"`                     // 下单时承接的渠道
	ChannelVersion  int            `gorm:"not null;default:0" json:"channel_version"`                                                                                         // 下单时的渠道配置版本
	Status          string         `gorm:"size:16;not null;index:idx_payment_orders_status_exp,priority:1" json:"status"`                                                     // 见 PayOrder* 常量
	CheckoutMode    string         `gorm:"size:16;not null;default:''" json:"checkout_mode"`                                                                                  // 见 Checkout* 常量
	CheckoutValue   string         `gorm:"type:text;not null;default:''" json:"-"`                                                                                            // 二维码内容或付款页地址；只在 pending 时对用户返回
	ExpiresAt       time.Time      `gorm:"not null;index:idx_payment_orders_status_exp,priority:2" json:"expires_at"`                                                         // 到期时间
	ProviderTradeNo string         `gorm:"size:64;not null;default:'';uniqueIndex:uk_payment_orders_trade,priority:2,where:provider_trade_no <> ''" json:"provider_trade_no"` // 渠道交易号；非空时 (provider, 交易号) 唯一
	PaidAmountFen   int            `gorm:"not null;default:0" json:"paid_amount_fen"`                                                                                         // 渠道报告的实付金额
	PaidAt          *time.Time     `json:"paid_at"`                                                                                                                           // 渠道报告的付款时间
	CreditedAt      *time.Time     `json:"credited_at"`                                                                                                                       // 积分入账时间
	ClosedAt        *time.Time     `json:"closed_at"`                                                                                                                         // 关单时间
	CloseReason     string         `gorm:"size:16;not null;default:''" json:"close_reason"`                                                                                   // 见 PayClose* 常量
	LatePaid        bool           `gorm:"not null;default:false" json:"late_paid"`                                                                                           // 关单后才收到已付通知
	AbnormalReason  string         `gorm:"size:32;not null;default:''" json:"abnormal_reason"`                                                                                // 见 PayAbnormal* 常量
	FailReason      string         `gorm:"size:255;not null;default:''" json:"fail_reason"`                                                                                   // 下单失败原因（已脱敏）
	LastQueriedAt   *time.Time     `json:"last_queried_at"`                                                                                                                   // 最近一次向渠道查单的时间，补单任务和主动查单限频用
	ClientIP        string         `gorm:"size:64;not null;default:''" json:"-"`                                                                                              // 下单时的客户端 IP，只做记录
	CreatedAt       time.Time      `gorm:"index:idx_payment_orders_user,priority:2,sort:desc" json:"created_at"`                                                              // 创建时间
	UpdatedAt       time.Time      `json:"updated_at"`                                                                                                                        // 更新时间
}

// TableName 表名。
func (PaymentOrder) TableName() string { return "payment_orders" }

// PaymentNotification 渠道通知记录：只存验签通过的通知，用来排查「付了没到账」。(provider, event_key) 唯一，重复通知只记第一次。
type PaymentNotification struct {
	ID         uint64         `gorm:"primaryKey" json:"id"`                                                                     // 记录 ID
	Provider   string         `gorm:"size:32;not null;uniqueIndex:uk_payment_notifications_event,priority:1" json:"provider"`   // 渠道
	EventKey   string         `gorm:"size:128;not null;uniqueIndex:uk_payment_notifications_event,priority:2" json:"event_key"` // 通知去重键
	OrderNo    string         `gorm:"size:32;not null;index:idx_payment_notifications_order" json:"order_no"`                   // 订单号
	Result     string         `gorm:"size:24;not null" json:"result"`                                                           // 见 PayNotify* 常量
	Payload    datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"payload"`                                          // 验签后的业务字段，已去掉签名和密钥，最多 8KB
	ReceivedAt time.Time      `gorm:"not null" json:"received_at"`                                                              // 收到时间
}

// TableName 表名。
func (PaymentNotification) TableName() string { return "payment_notifications" }
