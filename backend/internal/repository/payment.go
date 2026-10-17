package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// PaymentTx 是支付订单与积分在同一个事务里的 SQL 原语。入账（订单转 credited + 写 recharge 流水 + 加余额）
// 必须在一个事务里完成，所以积分原语和订单原语放在同一个接口上；事务内不做任何网络请求。
type PaymentTx interface {
	// LockOrder 按订单号读取订单并加行锁（SELECT … FOR UPDATE），不存在返回 ErrNotFound。
	LockOrder(ctx context.Context, orderNo string) (*model.PaymentOrder, error)
	// UpdateOrder 更新订单的指定列，订单不存在返回 ErrNotFound。
	UpdateOrder(ctx context.Context, id uint64, fields map[string]any) error
	// TradeNoTaken 判断同一渠道下，是否已有别的订单（id 不等于 exceptID）占用了这个渠道交易号。
	TradeNoTaken(ctx context.Context, provider, tradeNo string, exceptID uint64) (bool, error)
	// EnsureCredit 积分账户不存在时以 initial 惰性创建（同时写 initial 流水），已存在则什么都不做。
	EnsureCredit(ctx context.Context, userID uint64, initial int) error
	// AddCredit 对积分账户做增量更新，账户不存在返回 ErrNotFound。
	AddCredit(ctx context.Context, userID uint64, dBalance, dFrozen int) error
	// InsertLedger 写一条流水（ON CONFLICT DO NOTHING）。inserted=false 表示 (payment_order_id, type) 已存在，即已入过账。
	InsertLedger(ctx context.Context, entry *model.CreditLedger) (inserted bool, err error)
	// IncProductSold 商品已售笔数 +1。
	IncProductSold(ctx context.Context, productID uint64) error
}

// PaymentRepository 是支付订单、渠道配置和充值商品的数据访问层，只做 SQL，不含业务规则。
type PaymentRepository struct {
	db *gorm.DB
}

// NewPaymentRepository 创建支付仓储。
func NewPaymentRepository(db *gorm.DB) *PaymentRepository {
	return &PaymentRepository{db: db}
}

var _ PaymentTx = (*PaymentRepository)(nil)

// WithTx 在一个数据库事务里执行 fn：fn 返回错误则整体回滚，否则提交。传给 fn 的仓储绑定在该事务上。
func (r *PaymentRepository) WithTx(ctx context.Context, fn func(tx PaymentTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&PaymentRepository{db: tx})
	})
}

// credits 返回绑定在同一个 db（事务或普通连接）上的积分原语，复用任务服务那一套，保证记账规则只有一份。
func (r *PaymentRepository) credits() *GenerationTaskRepository {
	return &GenerationTaskRepository{db: r.db}
}

// ---------- 事务原语 ----------

// LockOrder 按订单号读取订单并加行锁（SELECT … FOR UPDATE）；必须在事务里调用才有意义。不存在返回 ErrNotFound。
func (r *PaymentRepository) LockOrder(ctx context.Context, orderNo string) (*model.PaymentOrder, error) {
	var o model.PaymentOrder
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_no = ?", orderNo).First(&o).Error
	if err != nil {
		return nil, translate(err)
	}
	return &o, nil
}

// UpdateOrder 更新订单的指定列（WHERE id=?），订单不存在返回 ErrNotFound。
func (r *PaymentRepository) UpdateOrder(ctx context.Context, id uint64, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.PaymentOrder{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// TradeNoTaken 查同一渠道下是否有别的订单占用了这个渠道交易号。
func (r *PaymentRepository) TradeNoTaken(ctx context.Context, provider, tradeNo string, exceptID uint64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.PaymentOrder{}).
		Where("provider = ? AND provider_trade_no = ? AND id <> ?", provider, tradeNo, exceptID).Count(&n).Error
	return n > 0, err
}

// EnsureCredit 积分账户不存在时以 initial 惰性创建，已存在则不改动。
func (r *PaymentRepository) EnsureCredit(ctx context.Context, userID uint64, initial int) error {
	return r.credits().EnsureCredit(ctx, userID, initial)
}

// AddCredit 对积分账户做增量更新，账户不存在返回 ErrNotFound。
func (r *PaymentRepository) AddCredit(ctx context.Context, userID uint64, dBalance, dFrozen int) error {
	return r.credits().AddCredit(ctx, userID, dBalance, dFrozen)
}

// InsertLedger 写一条积分流水（ON CONFLICT DO NOTHING）。(payment_order_id, type) 唯一索引命中时 inserted=false。
func (r *PaymentRepository) InsertLedger(ctx context.Context, entry *model.CreditLedger) (bool, error) {
	return r.credits().InsertLedger(ctx, entry)
}

// IncProductSold 商品已售笔数 +1（由数据库原子完成）。
func (r *PaymentRepository) IncProductSold(ctx context.Context, productID uint64) error {
	return r.db.WithContext(ctx).Model(&model.RechargeProduct{}).Where("id = ?", productID).
		UpdateColumn("sold_count", gorm.Expr("sold_count + 1")).Error
}

// ---------- 订单查询与状态迁移 ----------

// FindOrder 按订单号查订单，不存在返回 ErrNotFound。
func (r *PaymentRepository) FindOrder(ctx context.Context, orderNo string) (*model.PaymentOrder, error) {
	var o model.PaymentOrder
	if err := r.db.WithContext(ctx).Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		return nil, translate(err)
	}
	return &o, nil
}

// FindUserOrder 按订单号查订单，条件带 user_id：不存在和不属于该用户都返回 ErrNotFound（A7）。
func (r *PaymentRepository) FindUserOrder(ctx context.Context, userID uint64, orderNo string) (*model.PaymentOrder, error) {
	var o model.PaymentOrder
	if err := r.db.WithContext(ctx).Where("order_no = ? AND user_id = ?", orderNo, userID).First(&o).Error; err != nil {
		return nil, translate(err)
	}
	return &o, nil
}

// FindByIdempotencyKey 按 (user_id, idempotency_key) 查订单，不存在返回 ErrNotFound。
func (r *PaymentRepository) FindByIdempotencyKey(ctx context.Context, userID uint64, key string) (*model.PaymentOrder, error) {
	var o model.PaymentOrder
	if err := r.db.WithContext(ctx).Where("user_id = ? AND idempotency_key = ?", userID, key).First(&o).Error; err != nil {
		return nil, translate(err)
	}
	return &o, nil
}

// openStatuses 是「进行中」的订单状态：占用用户的未支付名额。
func openStatuses() []string {
	return []string{model.PayOrderCreating, model.PayOrderPending}
}

// CountOpenOrders 统计用户进行中（creating / pending）的订单数。
func (r *PaymentRepository) CountOpenOrders(ctx context.Context, userID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.PaymentOrder{}).
		Where("user_id = ? AND status IN ?", userID, openStatuses()).Count(&n).Error
	return n, err
}

// LatestOpenOrder 返回用户最近一笔进行中的订单，没有返回 ErrNotFound。
func (r *PaymentRepository) LatestOpenOrder(ctx context.Context, userID uint64) (*model.PaymentOrder, error) {
	var o model.PaymentOrder
	err := r.db.WithContext(ctx).Where("user_id = ? AND status IN ?", userID, openStatuses()).Order("id DESC").First(&o).Error
	if err != nil {
		return nil, translate(err)
	}
	return &o, nil
}

// InsertOrder 插入订单。用 ON CONFLICT DO NOTHING 而不是捕获唯一冲突：冲突错误会让事务进入 aborted 状态；
// inserted=false 表示命中了 (user_id, idempotency_key) 或 order_no 的唯一索引，由 service 区分并处理。
func (r *PaymentRepository) InsertOrder(ctx context.Context, o *model.PaymentOrder) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(o)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// UpdateOrderIf 是状态迁移的 CAS：UPDATE … WHERE id=? AND status IN (from)，没命中返回 updated=false。
func (r *PaymentRepository) UpdateOrderIf(ctx context.Context, id uint64, from []string, fields map[string]any) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.PaymentOrder{}).Where("id = ? AND status IN ?", id, from).Updates(fields)
	return res.RowsAffected > 0, res.Error
}

// ListDueOrders 返回已到期（expires_at <= now）的 pending 订单，按到期时间升序，最多 limit 条。
func (r *PaymentRepository) ListDueOrders(ctx context.Context, now time.Time, limit int) ([]model.PaymentOrder, error) {
	var out []model.PaymentOrder
	err := r.db.WithContext(ctx).Where("status = ? AND expires_at <= ?", model.PayOrderPending, now).
		Order("expires_at ASC").Limit(limit).Find(&out).Error
	return out, err
}

// ListUnconfirmedOrders 返回创建早于 createdBefore、且最近一次查单早于 queriedBefore（或从未查过）的 pending 订单。
func (r *PaymentRepository) ListUnconfirmedOrders(ctx context.Context, createdBefore, queriedBefore time.Time, limit int) ([]model.PaymentOrder, error) {
	var out []model.PaymentOrder
	err := r.db.WithContext(ctx).
		Where("status = ? AND created_at < ? AND (last_queried_at IS NULL OR last_queried_at < ?)", model.PayOrderPending, createdBefore, queriedBefore).
		Order("created_at ASC").Limit(limit).Find(&out).Error
	return out, err
}

// ListStuckCreating 返回创建早于 before、仍停在 creating 的订单。
func (r *PaymentRepository) ListStuckCreating(ctx context.Context, before time.Time, limit int) ([]model.PaymentOrder, error) {
	var out []model.PaymentOrder
	err := r.db.WithContext(ctx).Where("status = ? AND created_at < ?", model.PayOrderCreating, before).
		Order("created_at ASC").Limit(limit).Find(&out).Error
	return out, err
}

// InsertNotification 记一条通知，(provider, event_key) 已存在时什么都不做（重复通知只记第一次）。
func (r *PaymentRepository) InsertNotification(ctx context.Context, n *model.PaymentNotification) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(n).Error
}
