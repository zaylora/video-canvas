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

// channelLoadsSQL 按渠道统计当前负载。口径与 ClaimDue 的占用数一致：queued / running 与租约有效的 pending 算“生成中”，
// 其余 pending 算“排队”；终态与 finalizing（上游已出结果、只剩转存）都不算。
const channelLoadsSQL = `
SELECT provider AS channel,
       COUNT(*) FILTER (WHERE status IN ('queued','running') OR (status = 'pending' AND lease_until >= @now)) AS running,
       COUNT(*) FILTER (WHERE status = 'pending' AND (lease_until IS NULL OR lease_until < @now)) AS waiting
FROM generation_tasks
WHERE status IN ('pending','queued','running')
GROUP BY provider
ORDER BY provider`

// ChannelLoads 统计每个有未完成任务的渠道的负载（没有任务的渠道不在结果里，调用方按 0 处理），按渠道 key 升序。
func (r *AIChannelRepository) ChannelLoads(ctx context.Context, now time.Time) ([]model.ChannelLoad, error) {
	var list []model.ChannelLoad
	err := r.db.WithContext(ctx).Raw(channelLoadsSQL, map[string]any{"now": now}).Scan(&list).Error
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

// ChannelModelRef 是一个引用渠道的模型：Label 取已发布版本正文里的 label，没有已发布版本引用它时取草稿的；可能为空串。
type ChannelModelRef struct {
	Key   string
	Label string
}

// ChannelRefs 是一个渠道被引用的情况，删除前检查用。
type ChannelRefs struct {
	Models      []ChannelModelRef // 最新草稿或已发布版本的 channels[].channel 指向它的模型，按 key 升序
	ActiveTasks int64             // 快照引用了这个渠道的非终态任务数（config_snapshot->'channel'->>'key'）
}

// InUse 判断渠道是否仍被引用。
func (r ChannelRefs) InUse() bool { return len(r.Models) > 0 || r.ActiveTasks > 0 }

// channelModelRefRow 是引用查询的一行。
type channelModelRefRow struct {
	Key   string
	Label string
}

// modelRefsChannelSQL 匹配“正文 channels 数组里有一项的 channel 等于某渠道 key”的 revision。
// body_json 是 json 列（库里保证是合法 JSON），转 jsonb 后用 @> 做包含判断；channels 不是数组时只是不匹配，不会报错。
const modelRefsChannelSQL = "(r.body_json::jsonb -> 'channels') @> jsonb_build_array(jsonb_build_object('channel', ?::text))"

// taskRefsChannelSQL 匹配“快照冻结了某个渠道的非终态任务”。快照缺字段时 #>> 得到 NULL，只是不匹配。
const taskRefsChannelSQL = "status IN ? AND config_snapshot #>> '{channel,key}' = ?"

// CountChannelRefs 统计渠道被引用的情况（见 ChannelRefs）；渠道不存在返回 ErrNotFound。
func (r *AIChannelRepository) CountChannelRefs(ctx context.Context, key string) (ChannelRefs, error) {
	db := r.db.WithContext(ctx)
	var exists int64
	if err := db.Model(&model.AIChannel{}).Where("key = ?", key).Count(&exists).Error; err != nil {
		return ChannelRefs{}, err
	}
	if exists == 0 {
		return ChannelRefs{}, ErrNotFound
	}
	return countChannelRefs(db, key)
}

// countChannelRefs 统计渠道的引用，db 可以是普通连接也可以是事务。
// 模型口径：target=model、指针行存在（每个模型只有一份配置）。
func countChannelRefs(db *gorm.DB, key string) (ChannelRefs, error) {
	var rows []channelModelRefRow
	err := db.Table("ai_config_revisions AS r").
		Select("r.target_key AS key, COALESCE(r.body_json::jsonb ->> 'label', '') AS label").
		Joins("JOIN ai_models AS m ON m.key = r.target_key").
		Where("r.target = ?", model.ConfigTargetModel).
		Where(modelRefsChannelSQL, key).
		Order("r.target_key ASC").Scan(&rows).Error
	if err != nil {
		return ChannelRefs{}, err
	}
	refs := ChannelRefs{Models: channelModelRefs(rows)}
	err = db.Model(&model.GenerationTask{}).Where(taskRefsChannelSQL, activeStatuses(), key).Count(&refs.ActiveTasks).Error
	if err != nil {
		return ChannelRefs{}, err
	}
	return refs, nil
}

// channelModelRefs 把查询行转成引用项（rows 已按 key 升序，每个模型只有一行）。
func channelModelRefs(rows []channelModelRefRow) []ChannelModelRef {
	out := make([]ChannelModelRef, 0, len(rows))
	for _, row := range rows {
		out = append(out, ChannelModelRef(row))
	}
	return out
}

// DeleteChannel 在一个事务里删除渠道：锁住渠道行（FOR UPDATE）后重新统计引用，仍被模型或非终态任务引用时返回当时的引用与 ErrInUse；
// 渠道不存在返回 ErrNotFound。删除时连同 ai_secrets 里它的 Key（channel:<key>）一起删掉，避免留下没有主人的凭证。
func (r *AIChannelRepository) DeleteChannel(ctx context.Context, key string) (ChannelRefs, error) {
	var refs ChannelRefs
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 锁渠道行：与更新、设 Key 等写操作串行
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("key").
			Where("key = ?", key).First(&model.AIChannel{}).Error; err != nil {
			return translate(err)
		}
		// 2. 锁内重新统计引用：service 层的预检结果可能已过期
		var err error
		refs, err = countChannelRefs(tx, key)
		if err != nil {
			return err
		}
		if refs.InUse() {
			return ErrInUse
		}
		// 3. 删渠道行与它的 Key
		if err := tx.Where("key = ?", key).Delete(&model.AIChannel{}).Error; err != nil {
			return err
		}
		return tx.Where("name = ?", model.ChannelSecretName(key)).Delete(&model.AISecret{}).Error
	})
	return refs, err
}
