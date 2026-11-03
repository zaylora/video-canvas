package repository

import (
	"context"
	"strconv"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// PricingTx 是单个定价设置变更的事务原语。
type PricingTx interface {
	GetAll(ctx context.Context) (map[string]string, error)
	SetMany(ctx context.Context, kv map[string]string, actorID uint64) error
	Insert(ctx context.Context, audit *model.AdminAuditLog) error
	FindApplied(ctx context.Context, approvalID string, index int) (*model.AdminAuditLog, error)
}

// WithPricingTx 串行化定价配置的读、比较、更新与审计（包括设置尚未创建的情况）。
func (r *SystemSettingRepository) WithPricingTx(ctx context.Context, fn func(PricingTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(73102601)").Error; err != nil {
			return err
		}
		return fn(&pricingTx{SystemSettingRepository: &SystemSettingRepository{db: tx}, db: tx})
	})
}

type pricingTx struct {
	*SystemSettingRepository
	db *gorm.DB
}

// Insert 在配置更新的同一事务写审计，失败会回滚配置。
func (t *pricingTx) Insert(ctx context.Context, audit *model.AdminAuditLog) error {
	return t.db.WithContext(ctx).Create(audit).Error
}

// FindApplied 找到已执行条目供重试复用；没有返回 ErrNotFound。
func (t *pricingTx) FindApplied(ctx context.Context, approvalID string, index int) (*model.AdminAuditLog, error) {
	var row model.AdminAuditLog
	err := t.db.WithContext(ctx).Where("action = ? AND detail_json->>'approval_id' = ? AND detail_json->>'index' = ?", model.AdminAuditSettingsPricing, approvalID, strconv.Itoa(index)).First(&row).Error
	return &row, translate(err)
}
