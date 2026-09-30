package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// AIPluginRepository 是协议插件（ai_plugins）与插件版本（ai_plugin_versions）的数据访问。
// 版本一经登记不可修改；代码（code 列）只在装载进 runner 时读取，列表类查询都不读它。
type AIPluginRepository struct {
	db *gorm.DB
}

// NewAIPluginRepository 创建插件与插件版本仓储。
func NewAIPluginRepository(db *gorm.DB) *AIPluginRepository {
	return &AIPluginRepository{db: db}
}

// VersionRefs 是一个插件版本被引用的情况，删除前检查用。
type VersionRefs struct {
	Channels    int64 // 固定在这个版本上的渠道数
	ActiveTasks int64 // 快照引用了这个版本的非终态任务数（config_snapshot->'channel'->>'plugin_version_id'）
}

// versionHeadColumns 是版本表里不含 code 的列，列表类与“头信息”查询用它避免读出最大 512KB 的代码。
const versionHeadColumns = "id, plugin_key, version, sha256, meta_json, created_by, created_at"

// taskRefsVersionSQL 匹配“快照冻结了某个插件版本的非终态任务”。
// 用 jsonb 相等比较而不是 ::bigint 强转：个别任务的快照缺字段或格式异常时，强转会让整条查询报错，比较则只是不匹配。
const taskRefsVersionSQL = "status IN ? AND config_snapshot #> '{channel,plugin_version_id}' = to_jsonb(?::bigint)"

// SaveVersion 在一个事务里登记一个新版本：插件行不存在就创建（带 name / source / enabled=true），已存在则只更新 name 与 updated_at
// （不改 source 与 enabled）；再插入版本行。(plugin_key, version) 重复返回 ErrDuplicate，此时整个事务回滚。
// 成功后 v.ID 与 v.CreatedAt 已回填，p 回填为数据库里插件行的当前值（source / enabled 以已有行为准）。
// v.PluginKey 为空时取 p.Key，与 p.Key 不一致视为调用方错误。
func (r *AIPluginRepository) SaveVersion(ctx context.Context, p *model.AIPlugin, v *model.AIPluginVersion) error {
	if v.PluginKey == "" {
		v.PluginKey = p.Key
	}
	if v.PluginKey != p.Key {
		return errors.New("插件版本的 plugin_key 与插件 key 不一致")
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. upsert 插件行：冲突时只改 name 与 updated_at，不让上传动作改掉 source / enabled；
		//    冲突更新同时锁住插件行，与 DeleteVersion 串行化
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"name", "updated_at"}),
		}).Create(p).Error; err != nil {
			return err
		}
		// 2. 插入版本行，(plugin_key, version) 重复由唯一索引兜底
		return tx.Create(v).Error
	})
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return err
}

// GetPlugin 按 key 查询插件；不存在返回 ErrNotFound。
func (r *AIPluginRepository) GetPlugin(ctx context.Context, key string) (*model.AIPlugin, error) {
	var p model.AIPlugin
	if err := r.db.WithContext(ctx).Where("key = ?", key).First(&p).Error; err != nil {
		return nil, translate(err)
	}
	return &p, nil
}

// ListPlugins 返回所有插件，按 key 升序。
func (r *AIPluginRepository) ListPlugins(ctx context.Context) ([]model.AIPlugin, error) {
	var list []model.AIPlugin
	err := r.db.WithContext(ctx).Order("key ASC").Find(&list).Error
	return list, err
}

// ListVersions 返回插件版本（不含 code），按 id 倒序（新到旧）。pluginKey 为空表示所有插件的所有版本。
func (r *AIPluginRepository) ListVersions(ctx context.Context, pluginKey string) ([]model.AIPluginVersion, error) {
	db := r.db.WithContext(ctx).Select(versionHeadColumns)
	if pluginKey != "" {
		db = db.Where("plugin_key = ?", pluginKey)
	}
	var list []model.AIPluginVersion
	err := db.Order("id DESC").Find(&list).Error
	return list, err
}

// GetVersion 按 id 查询版本，含 code；不存在返回 ErrNotFound。只给装载进 runner 用。
func (r *AIPluginRepository) GetVersion(ctx context.Context, id uint64) (*model.AIPluginVersion, error) {
	var v model.AIPluginVersion
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&v).Error; err != nil {
		return nil, translate(err)
	}
	return &v, nil
}

// GetVersionHead 按 id 查询版本，不含 code（meta_json 与 sha256 有）；不存在返回 ErrNotFound。
func (r *AIPluginRepository) GetVersionHead(ctx context.Context, id uint64) (*model.AIPluginVersion, error) {
	var v model.AIPluginVersion
	if err := r.db.WithContext(ctx).Select(versionHeadColumns).Where("id = ?", id).First(&v).Error; err != nil {
		return nil, translate(err)
	}
	return &v, nil
}

// FindVersion 按 (plugin_key, version) 查询版本，不含 code；不存在返回 ErrNotFound。
func (r *AIPluginRepository) FindVersion(ctx context.Context, pluginKey, version string) (*model.AIPluginVersion, error) {
	var v model.AIPluginVersion
	err := r.db.WithContext(ctx).Select(versionHeadColumns).
		Where("plugin_key = ? AND version = ?", pluginKey, version).First(&v).Error
	if err != nil {
		return nil, translate(err)
	}
	return &v, nil
}

// SetPluginEnabled 启用 / 停用插件并更新 updated_at；插件不存在返回 ErrNotFound。
func (r *AIPluginRepository) SetPluginEnabled(ctx context.Context, key string, enabled bool) error {
	res := r.db.WithContext(ctx).Model(&model.AIPlugin{}).Where("key = ?", key).
		Updates(map[string]any{"enabled": enabled, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountVersionRefs 统计版本被引用的情况（渠道 plugin_version_id = id；快照冻结了该版本的非终态任务）；版本不存在返回 ErrNotFound。
func (r *AIPluginRepository) CountVersionRefs(ctx context.Context, id uint64) (VersionRefs, error) {
	db := r.db.WithContext(ctx)
	var exists int64
	if err := db.Model(&model.AIPluginVersion{}).Where("id = ?", id).Count(&exists).Error; err != nil {
		return VersionRefs{}, err
	}
	if exists == 0 {
		return VersionRefs{}, ErrNotFound
	}
	return countVersionRefs(db, id)
}

// countVersionRefs 统计引用数，db 可以是普通连接也可以是事务。
func countVersionRefs(db *gorm.DB, id uint64) (VersionRefs, error) {
	var refs VersionRefs
	if err := db.Model(&model.AIChannel{}).Where("plugin_version_id = ?", id).Count(&refs.Channels).Error; err != nil {
		return VersionRefs{}, err
	}
	err := db.Model(&model.GenerationTask{}).Where(taskRefsVersionSQL, activeStatuses(), id).Count(&refs.ActiveTasks).Error
	if err != nil {
		return VersionRefs{}, err
	}
	return refs, nil
}

// DeleteVersion 在一个事务里删除版本：先锁住版本行并重新统计引用，仍被渠道或非终态任务引用返回 ErrInUse，不存在返回 ErrNotFound。
// 删除后插件没有任何版本时顺带删掉插件行。内置插件的版本由 service 层拒绝，这里不判断来源。
func (r *AIPluginRepository) DeleteVersion(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 先读出所属插件，并按“插件行 → 版本行”的顺序加锁（与 SaveVersion 的加锁顺序一致，避免死锁）；
		//    锁插件行是为了让“删掉同一插件的最后两个版本”的两个并发事务串行，否则双方都看到对方的版本而漏删插件行
		var v model.AIPluginVersion
		if err := tx.Select("id, plugin_key").Where("id = ?", id).First(&v).Error; err != nil {
			return translate(err)
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("key").
			Where("key = ?", v.PluginKey).Find(&[]model.AIPlugin{}).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").
			Where("id = ?", id).First(&model.AIPluginVersion{}).Error; err != nil {
			return translate(err)
		}
		// 2. 锁内重新统计引用：前面的检查可能已过期
		refs, err := countVersionRefs(tx, id)
		if err != nil {
			return err
		}
		if refs.Channels > 0 || refs.ActiveTasks > 0 {
			return ErrInUse
		}
		// 3. 删版本；插件没有任何版本后一并删掉插件行
		if err := tx.Where("id = ?", id).Delete(&model.AIPluginVersion{}).Error; err != nil {
			return err
		}
		return deletePluginIfEmpty(tx, v.PluginKey)
	})
}

// deletePluginIfEmpty 插件下已没有任何版本时删除插件行。
func deletePluginIfEmpty(tx *gorm.DB, pluginKey string) error {
	var left int64
	if err := tx.Model(&model.AIPluginVersion{}).Where("plugin_key = ?", pluginKey).Count(&left).Error; err != nil {
		return err
	}
	if left > 0 {
		return nil
	}
	return tx.Where("key = ?", pluginKey).Delete(&model.AIPlugin{}).Error
}
