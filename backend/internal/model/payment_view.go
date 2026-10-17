package model

import "time"

// PaymentProductView 是积分商城里的一档商品（用户侧）。
type PaymentProductView struct {
	ID           uint64 `json:"id"`            // 商品 ID
	Name         string `json:"name"`          // 名称
	PriceFen     int    `json:"price_fen"`     // 售价，单位分
	BaseCredits  int    `json:"base_credits"`  // 基础积分
	BonusCredits int    `json:"bonus_credits"` // 赠送积分
	Badge        string `json:"badge"`         // 角标，空表示没有
}

// PaymentMethodView 是一种支付方式当前是否可用，不可用时 Reason 说明原因（前台置灰并显示）。
type PaymentMethodView struct {
	Method    string `json:"method"`           // wechat / alipay
	Available bool   `json:"available"`        // 是否可用
	Reason    string `json:"reason,omitempty"` // 不可用的原因
}

// PaymentStoreView 是积分商城的数据。Enabled=false 时前端不打开商城，积分胶囊保持原来的余额小卡片。
type PaymentStoreView struct {
	Enabled  bool                 `json:"enabled"`  // 支付总开关
	Products []PaymentProductView `json:"products"` // 上架商品，按 sort 升序
	Methods  []PaymentMethodView  `json:"methods"`  // 两种支付方式的可用状态
}

// PaymentCheckoutView 是收银信息，只在订单 pending 时返回。
type PaymentCheckoutView struct {
	Mode  string `json:"mode"`  // qrcode / qrcode_image / redirect
	Value string `json:"value"` // 二维码内容、二维码图片地址或付款页地址
}

// PaymentOrderView 是用户侧看到的订单。
type PaymentOrderView struct {
	OrderNo        string               `json:"order_no"`                  // 订单号
	Status         string               `json:"status"`                    // 见 PayOrder* 常量
	AmountFen      int                  `json:"amount_fen"`                // 应付金额，单位分
	Credits        int                  `json:"credits"`                   // 到账积分
	Method         string               `json:"method"`                    // wechat / alipay
	Checkout       *PaymentCheckoutView `json:"checkout,omitempty"`        // 仅 pending 时有
	ExpiresAt      time.Time            `json:"expires_at"`                // 到期时间
	CreditedAt     *time.Time           `json:"credited_at"`               // 入账时间
	CloseReason    string               `json:"close_reason,omitempty"`    // 关单原因
	LatePaid       bool                 `json:"late_paid"`                 // 关单后才收到付款
	AbnormalReason string               `json:"abnormal_reason,omitempty"` // 异常原因
	CreatedAt      time.Time            `json:"created_at"`                // 创建时间
}

// CreatePaymentOrderReq 是下单请求。幂等键由前端生成（crypto.randomUUID），同一个键重复提交只会产生一笔订单。
type CreatePaymentOrderReq struct {
	ProductID      uint64 `json:"product_id" binding:"required"`                   // 商品 ID
	Method         string `json:"method" binding:"required,oneof=wechat alipay"`   // 支付方式
	IdempotencyKey string `json:"idempotency_key" binding:"required,min=8,max=64"` // 幂等键
}
