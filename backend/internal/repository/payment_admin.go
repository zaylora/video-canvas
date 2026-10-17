package repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// ---------- 渠道配置 ----------

// CreateChannel 创建渠道配置行；该 provider 已存在（并发的第一次保存）返回 ErrRevisionConflict。
func (r *PaymentRepository) CreateChannel(ctx context.Context, ch *model.PaymentChannel) error {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(ch)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrRevisionConflict
	}
	return nil
}

// UpdateChannelIf 乐观锁更新渠道配置：WHERE provider=? AND version=?，同时 version+1。版本不一致返回 ErrRevisionConflict。
func (r *PaymentRepository) UpdateChannelIf(ctx context.Context, provider string, version int, fields map[string]any) error {
	fields["version"] = gorm.Expr("version + 1")
	fields["updated_at"] = time.Now()
	res := r.db.WithContext(ctx).Model(&model.PaymentChannel{}).Where("provider = ? AND version = ?", provider, version).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrRevisionConflict
	}
	return nil
}

// SetChannelValidated 记录校验通过的时间。带 version 条件：校验期间配置被改过（version 变了）就不记，
// 否则会把「新配置」标成已校验。没命中返回 ErrRevisionConflict。
func (r *PaymentRepository) SetChannelValidated(ctx context.Context, provider string, version int, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.PaymentChannel{}).
		Where("provider = ? AND version = ?", provider, version).UpdateColumn("validated_at", at)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrRevisionConflict
	}
	return nil
}

// SetChannelEnabled 启用或停用渠道，渠道不存在返回 ErrNotFound。不改 version：启停不是配置变更。
func (r *PaymentRepository) SetChannelEnabled(ctx context.Context, provider string, enabled bool) error {
	res := r.db.WithContext(ctx).Model(&model.PaymentChannel{}).Where("provider = ?", provider).
		Updates(map[string]any{"enabled": enabled, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountOpenOrdersByProvider 统计某渠道进行中（creating / pending）的订单数。
func (r *PaymentRepository) CountOpenOrdersByProvider(ctx context.Context, provider string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.PaymentOrder{}).
		Where("provider = ? AND status IN ?", provider, openStatuses()).Count(&n).Error
	return n, err
}

// ---------- 充值商品 ----------

// CreateProduct 新建商品。
func (r *PaymentRepository) CreateProduct(ctx context.Context, p *model.RechargeProduct) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// UpdateProductIf 乐观锁更新商品：WHERE id=? AND version=?，同时 version+1。商品不存在返回 ErrNotFound，版本不一致返回 ErrRevisionConflict。
func (r *PaymentRepository) UpdateProductIf(ctx context.Context, id uint64, version int, fields map[string]any) error {
	fields["version"] = gorm.Expr("version + 1")
	fields["updated_at"] = time.Now()
	res := r.db.WithContext(ctx).Model(&model.RechargeProduct{}).Where("id = ? AND version = ?", id, version).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	if _, err := r.GetProduct(ctx, id); err != nil {
		return err
	}
	return ErrRevisionConflict
}

// DeleteProduct 删除商品。商品已有订单返回 ErrInUse（订单页会出现孤儿商品，只能下架）；不存在返回 ErrNotFound。
// 检查和删除在同一个事务里，且先锁商品行，避免「刚检查完没有订单、同时有人下了单」。
func (r *PaymentRepository) DeleteProduct(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p model.RechargeProduct
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&p).Error; err != nil {
			return translate(err)
		}
		var n int64
		if err := tx.Model(&model.PaymentOrder{}).Where("product_id = ?", id).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return ErrInUse
		}
		return tx.Delete(&model.RechargeProduct{}, id).Error
	})
}

// SetProductSort 按 ids 的顺序重写商品的 sort（第一个是 0）。ids 必须恰好是全部商品 id 的一个排列，否则返回 ErrOrderMismatch 且不写任何数据。
func (r *PaymentRepository) SetProductSort(ctx context.Context, ids []uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing []uint64
		if err := tx.Model(&model.RechargeProduct{}).Clauses(clause.Locking{Strength: "UPDATE"}).Pluck("id", &existing).Error; err != nil {
			return err
		}
		if len(existing) != len(ids) {
			return ErrOrderMismatch
		}
		have := make(map[uint64]bool, len(existing))
		for _, id := range existing {
			have[id] = true
		}
		for i, id := range ids {
			if !have[id] {
				return ErrOrderMismatch
			}
			delete(have, id) // 重复的 id 第二次出现时找不到，下一轮会报错
			if err := tx.Model(&model.RechargeProduct{}).Where("id = ?", id).UpdateColumn("sort", i).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ---------- 订单查询（后台） ----------

// PaymentOrderFilter 是后台订单列表的筛选与分页条件。
type PaymentOrderFilter struct {
	Q        string     // 订单号（精确）或用户名（模糊）
	Status   string     // 为空不筛
	Method   string     // 为空不筛
	Provider string     // 为空不筛
	From     *time.Time // 创建时间下限（含）
	To       *time.Time // 创建时间上限（不含）
	Offset   int
	Limit    int
}

// ListOrders 分页查询订单，联表带出用户名，按 id 倒序。
func (r *PaymentRepository) ListOrders(ctx context.Context, f PaymentOrderFilter) ([]model.AdminPaymentOrderRow, int64, error) {
	base := r.db.WithContext(ctx).Table("payment_orders o").Joins("LEFT JOIN users u ON u.id = o.user_id")
	if q := strings.TrimSpace(f.Q); q != "" {
		base = base.Where(`(o.order_no = ? OR u.username ILIKE ? ESCAPE '\')`, q, "%"+likeEscaper.Replace(q)+"%")
	}
	for col, val := range map[string]string{"o.status": f.Status, "o.method": f.Method, "o.provider": f.Provider} {
		if val != "" {
			base = base.Where(col+" = ?", val)
		}
	}
	if f.From != nil {
		base = base.Where("o.created_at >= ?", *f.From)
	}
	if f.To != nil {
		base = base.Where("o.created_at < ?", *f.To)
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.AdminPaymentOrderRow
	err := base.Session(&gorm.Session{}).Select("o.*, COALESCE(u.username, '') AS username").
		Order("o.id DESC").Offset(f.Offset).Limit(f.Limit).Scan(&rows).Error
	return rows, total, err
}

// FindOrderWithUser 按订单号查订单并带出用户名，不存在返回 ErrNotFound。
func (r *PaymentRepository) FindOrderWithUser(ctx context.Context, orderNo string) (*model.AdminPaymentOrderRow, error) {
	var row model.AdminPaymentOrderRow
	err := r.db.WithContext(ctx).Table("payment_orders o").Joins("LEFT JOIN users u ON u.id = o.user_id").
		Select("o.*, COALESCE(u.username, '') AS username").Where("o.order_no = ?", orderNo).Take(&row).Error
	if err != nil {
		return nil, translate(err)
	}
	return &row, nil
}

// ListNotifications 返回某订单收到的渠道通知，按时间升序。
func (r *PaymentRepository) ListNotifications(ctx context.Context, orderNo string) ([]model.PaymentNotification, error) {
	var out []model.PaymentNotification
	err := r.db.WithContext(ctx).Where("order_no = ?", orderNo).Order("id ASC").Find(&out).Error
	return out, err
}
