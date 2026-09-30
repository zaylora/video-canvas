package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// AIChannelRepository 是渠道（ai_channels）与审计日志（ai_audit_logs）的数据访问。
// 渠道直接改库（没有 revision）；Key 不在这里，存在 ai_secrets（由 AIConfigRepository 读写）。
type AIChannelRepository struct {
	db *gorm.DB
}

// NewAIChannelRepository 创建渠道与审计日志仓储。
func NewAIChannelRepository(db *gorm.DB) *AIChannelRepository {
	return &AIChannelRepository{db: db}
}

// emptyJSONObject 是渠道 settings_json / rate_limit_json 的缺省值（两列都是 NOT NULL）。
const emptyJSONObject = "{}"

// CreateChannel 新建渠道；key 已存在返回 ErrDuplicate。成功后 CreatedAt / UpdatedAt 已回填。
// settings_json / rate_limit_json 为空时按 {} 写入。
// 事务内对 plugin_version_id 指向的版本行加共享锁并确认它存在（不存在返回 ErrNotFound），
// 与 DeleteVersion 的排他锁互斥，避免“渠道刚指向某版本、版本同时被删”的竞态。
func (r *AIChannelRepository) CreateChannel(ctx context.Context, c *model.AIChannel) error {
	fillChannelJSONDefaults(c)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockVersionShared(tx, c.PluginVersionID); err != nil {
			return err
		}
		// enabled 列带 default:true，GORM 会把 Go 零值 false 换成默认值 true（Select 也拦不住），
		// 所以调用方要求停用时，插入后在同一事务里补写 false
		wantEnabled := c.Enabled
		if err := tx.Create(c).Error; err != nil {
			return err
		}
		if wantEnabled {
			return nil
		}
		c.Enabled = false
		return tx.Model(&model.AIChannel{}).Where("key = ?", c.Key).UpdateColumn("enabled", false).Error
	})
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return err
}

// fillChannelJSONDefaults 把空的 JSON 列补成 {}。
func fillChannelJSONDefaults(c *model.AIChannel) {
	if len(c.SettingsJSON) == 0 {
		c.SettingsJSON = model.JSONText(emptyJSONObject)
	}
	if len(c.RateLimitJSON) == 0 {
		c.RateLimitJSON = model.JSONText(emptyJSONObject)
	}
}

// lockVersionShared 对版本行加 FOR SHARE 锁；版本不存在返回 ErrNotFound。
func lockVersionShared(tx *gorm.DB, id uint64) error {
	var v model.AIPluginVersion
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("id = ?", id).First(&v).Error
	return translate(err)
}

// UpdateChannel 更新渠道的可变字段（name、plugin_key、plugin_version_id、base_url、trusted_internal、allow_credentials、
// settings_json、rate_limit_json、enabled、updated_by、updated_at）；key 与 created_at 不变。渠道不存在返回 ErrNotFound。
// settings_json / rate_limit_json 为空时按 {} 写入；事务内对新的 plugin_version_id 加共享锁，
// 版本不存在同样返回 ErrNotFound（语义见 CreateChannel）。成功后 c.UpdatedAt 已回填。
func (r *AIChannelRepository) UpdateChannel(ctx context.Context, c *model.AIChannel) error {
	fillChannelJSONDefaults(c)
	now := time.Now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockVersionShared(tx, c.PluginVersionID); err != nil {
			return err
		}
		res := tx.Model(&model.AIChannel{}).Where("key = ?", c.Key).Updates(map[string]any{
			"name":              c.Name,
			"plugin_key":        c.PluginKey,
			"plugin_version_id": c.PluginVersionID,
			"base_url":          c.BaseURL,
			"trusted_internal":  c.TrustedInternal,
			"allow_credentials": c.AllowCredentials,
			"settings_json":     c.SettingsJSON,
			"rate_limit_json":   c.RateLimitJSON,
			"enabled":           c.Enabled,
			"updated_by":        c.UpdatedBy,
			"updated_at":        now,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		c.UpdatedAt = now
		return nil
	})
}

// GetChannel 按 key 查询渠道；不存在返回 ErrNotFound。
func (r *AIChannelRepository) GetChannel(ctx context.Context, key string) (*model.AIChannel, error) {
	var c model.AIChannel
	if err := r.db.WithContext(ctx).Where("key = ?", key).First(&c).Error; err != nil {
		return nil, translate(err)
	}
	return &c, nil
}

// ListChannels 返回所有渠道，按 key 升序。
func (r *AIChannelRepository) ListChannels(ctx context.Context) ([]model.AIChannel, error) {
	var list []model.AIChannel
	err := r.db.WithContext(ctx).Order("key ASC").Find(&list).Error
	return list, err
}

// SetChannelEnabled 启用 / 停用渠道并记录操作人（updated_by、updated_at）；渠道不存在返回 ErrNotFound。
func (r *AIChannelRepository) SetChannelEnabled(ctx context.Context, key string, enabled bool, updatedBy uint64) error {
	res := r.db.WithContext(ctx).Model(&model.AIChannel{}).Where("key = ?", key).
		Updates(map[string]any{"enabled": enabled, "updated_by": updatedBy, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// InsertAudit 写一条审计日志（只增不改不删）。成功后 ID 与 CreatedAt 已回填。
func (r *AIChannelRepository) InsertAudit(ctx context.Context, log *model.AIAuditLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

// ListAudit 返回最近的审计日志（id 倒序）。targetKey 非空时只看 target_key 等于它的日志；limit<=0 时默认 50。
func (r *AIChannelRepository) ListAudit(ctx context.Context, targetKey string, limit int) ([]model.AIAuditLog, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	db := r.db.WithContext(ctx)
	if targetKey != "" {
		db = db.Where("target_key = ?", targetKey)
	}
	var list []model.AIAuditLog
	err := db.Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}
