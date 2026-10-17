package service

import (
	"context"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/repository"
)

// PaymentTx 是支付订单与积分在同一个事务里的 SQL 原语，定义在 repository 包（和任务服务的 GenerationTaskTx 同样的做法）。
type PaymentTx = repository.PaymentTx

// PaymentOrderRepo 是支付订单的数据访问接口（真实实现是 repository.PaymentRepository）。
type PaymentOrderRepo interface {
	// WithTx 在一个数据库事务里执行 fn：fn 返回错误则整体回滚。
	WithTx(ctx context.Context, fn func(tx PaymentTx) error) error
	// FindOrder 按订单号查订单，不存在返回 repository.ErrNotFound。
	FindOrder(ctx context.Context, orderNo string) (*model.PaymentOrder, error)
	// FindUserOrder 按订单号查订单，并要求属于 userID；不存在或不属于该用户都返回 repository.ErrNotFound。
	FindUserOrder(ctx context.Context, userID uint64, orderNo string) (*model.PaymentOrder, error)
	// FindByIdempotencyKey 按 (user_id, idempotency_key) 查订单，不存在返回 repository.ErrNotFound。
	FindByIdempotencyKey(ctx context.Context, userID uint64, key string) (*model.PaymentOrder, error)
	// CountOpenOrders 统计用户进行中（creating / pending）的订单数。
	CountOpenOrders(ctx context.Context, userID uint64) (int64, error)
	// LatestOpenOrder 返回用户最近一笔进行中的订单，没有返回 repository.ErrNotFound。
	LatestOpenOrder(ctx context.Context, userID uint64) (*model.PaymentOrder, error)
	// InsertOrder 插入订单（ON CONFLICT DO NOTHING）。inserted=false 表示命中了 (user_id, idempotency_key) 唯一索引。
	InsertOrder(ctx context.Context, o *model.PaymentOrder) (inserted bool, err error)
	// UpdateOrderIf 是状态迁移的 CAS：UPDATE … WHERE id=? AND status IN (from)。没命中返回 updated=false。
	UpdateOrderIf(ctx context.Context, id uint64, from []string, fields map[string]any) (updated bool, err error)
	// ListDueOrders 返回已到期（expires_at <= now）的 pending 订单，按到期时间升序，最多 limit 条。
	ListDueOrders(ctx context.Context, now time.Time, limit int) ([]model.PaymentOrder, error)
	// ListUnconfirmedOrders 返回创建早于 createdBefore、且最近一次查单早于 queriedBefore（或从未查过）的 pending 订单。
	ListUnconfirmedOrders(ctx context.Context, createdBefore, queriedBefore time.Time, limit int) ([]model.PaymentOrder, error)
	// ListStuckCreating 返回创建早于 before、仍停在 creating 的订单（进程在下单中途崩溃留下的）。
	ListStuckCreating(ctx context.Context, before time.Time, limit int) ([]model.PaymentOrder, error)
	// InsertNotification 记一条验签通过的通知（ON CONFLICT DO NOTHING，重复通知只记第一次）。
	InsertNotification(ctx context.Context, n *model.PaymentNotification) error
}

// PaymentChannelRepo 是支付渠道配置的数据访问接口。
type PaymentChannelRepo interface {
	// ListChannels 返回已有配置的全部渠道（没配置过的渠道没有行）。
	ListChannels(ctx context.Context) ([]model.PaymentChannel, error)
	// GetChannel 按 provider 查渠道配置，不存在返回 repository.ErrNotFound。
	GetChannel(ctx context.Context, provider string) (*model.PaymentChannel, error)
}

// PaymentProductRepo 是充值商品的数据访问接口。
type PaymentProductRepo interface {
	// ListProducts 返回商品，按 sort、id 升序；onSaleOnly 为真时只返回上架的。
	ListProducts(ctx context.Context, onSaleOnly bool) ([]model.RechargeProduct, error)
	// GetProduct 按 id 查商品，不存在返回 repository.ErrNotFound。
	GetProduct(ctx context.Context, id uint64) (*model.RechargeProduct, error)
}

// PaymentSecretStore 是渠道密钥的存取：复用 AIConfigService 的 AES-256-GCM 加密存储（ai_secrets），只写不读。
type PaymentSecretStore interface {
	// SetSecret 加密保存（覆盖）凭证。
	SetSecret(ctx context.Context, name, value string, adminID uint64) error
	// Get 返回凭证明文，仅供服务端调用渠道时使用；凭证未设置返回 ErrAISecretNotSet。
	Get(ctx context.Context, name string) (string, error)
}

// PaymentRegistry 按 key 查找支付渠道适配器（真实实现是 payment.Registry）。
type PaymentRegistry interface {
	// Get 返回指定渠道的适配器。
	Get(key string) (payment.Provider, bool)
	// Keys 返回全部渠道 key，顺序即后台展示顺序。
	Keys() []string
}

// InitialCreditsPolicy 提供新积分账户的初始积分：老用户第一次充值时账户可能还没建，入账要先惰性创建它。
type InitialCreditsPolicy interface {
	// InitialCredits 返回新用户初始积分。
	InitialCredits(ctx context.Context) (int, error)
}
