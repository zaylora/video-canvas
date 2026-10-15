package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"
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
	Models      int64 // 固定在这个版本上的模型数（channels[0].protocol 的 plugin + version）
	ActiveTasks int64 // 快照引用了这个版本的非终态任务数（config_snapshot->'channel'->>'plugin_version_id'）
}

// modelRefsVersionSQL 匹配“协议固定在某个插件版本上”的模型配置；modelRefsPluginSQL 匹配“协议用了某个插件（任意版本）”的。
// body_json 是 json 列（库里保证是合法 JSON），转 jsonb 后按路径取值；没有协议（Agent / 待配置）的模型取到 NULL，只是不匹配。
const (
	modelRefsVersionSQL = "(r.body_json::jsonb #>> '{channels,0,protocol,plugin}') = ? AND (r.body_json::jsonb #>> '{channels,0,protocol,version}') = ?"
	modelRefsPluginSQL  = "(r.body_json::jsonb #>> '{channels,0,protocol,plugin}') = ?"
)

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
	var v model.AIPluginVersion
	if err := db.Select("plugin_key, version").Where("id = ?", id).First(&v).Error; err != nil {
		return VersionRefs{}, translate(err)
	}
	err := db.Table("ai_config_revisions AS r").Joins("JOIN ai_models AS m ON m.key = r.target_key").
		Where("r.target = ?", model.ConfigTargetModel).Where(modelRefsVersionSQL, v.PluginKey, v.Version).Count(&refs.Models).Error
	if err != nil {
		return VersionRefs{}, err
	}
	err = db.Model(&model.GenerationTask{}).Where(taskRefsVersionSQL, activeStatuses(), id).Count(&refs.ActiveTasks).Error
	if err != nil {
		return VersionRefs{}, err
	}
	return refs, nil
}

// DeleteVersion 在一个事务里删除版本：先锁住版本行并重新统计引用，仍被模型或非终态任务引用返回 ErrInUse，不存在返回 ErrNotFound。
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
		if refs.Models > 0 || refs.ActiveTasks > 0 {
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

// PluginRefs 是整个插件被引用的情况，删除插件前检查用。
type PluginRefs struct {
	Models      []ChannelModelRef // 协议用了它任一版本的模型（key 与展示名），按 key 升序
	ActiveTasks int64             // 快照引用了它任一版本的非终态任务数
}

// InUse 判断插件是否仍被引用。
func (r PluginRefs) InUse() bool { return len(r.Models) > 0 || r.ActiveTasks > 0 }

// taskRefsVersionsSQL 匹配“快照冻结了一组插件版本中任一个的非终态任务”，写法与 taskRefsVersionSQL 一致（jsonb 比较，不强转）。
const taskRefsVersionsSQL = "status IN ? AND config_snapshot #> '{channel,plugin_version_id}' IN (SELECT to_jsonb(v) FROM unnest(?::bigint[]) AS v)"

// CountPluginRefs 统计插件全部版本被模型与非终态任务引用的情况；插件不存在返回 ErrNotFound。
func (r *AIPluginRepository) CountPluginRefs(ctx context.Context, key string) (PluginRefs, error) {
	db := r.db.WithContext(ctx)
	var exists int64
	if err := db.Model(&model.AIPlugin{}).Where("key = ?", key).Count(&exists).Error; err != nil {
		return PluginRefs{}, err
	}
	if exists == 0 {
		return PluginRefs{}, ErrNotFound
	}
	var ids []uint64
	if err := db.Model(&model.AIPluginVersion{}).Where("plugin_key = ?", key).Pluck("id", &ids).Error; err != nil {
		return PluginRefs{}, err
	}
	return countPluginRefs(db, key, ids)
}

// countPluginRefs 统计插件（key）与它的一组版本（ids）的引用，db 可以是普通连接也可以是事务。ids 为空时没有任何引用。
func countPluginRefs(db *gorm.DB, key string, ids []uint64) (PluginRefs, error) {
	refs := PluginRefs{Models: []ChannelModelRef{}}
	if len(ids) == 0 {
		return refs, nil
	}
	var rows []channelModelRefRow
	err := db.Table("ai_config_revisions AS r").
		Select("r.target_key AS key, COALESCE(r.body_json::jsonb ->> 'label', '') AS label").
		Joins("JOIN ai_models AS m ON m.key = r.target_key").
		Where("r.target = ?", model.ConfigTargetModel).
		Where(modelRefsPluginSQL, key).
		Order("r.target_key ASC").Scan(&rows).Error
	if err != nil {
		return PluginRefs{}, err
	}
	refs.Models = channelModelRefs(rows)
	err = db.Model(&model.GenerationTask{}).Where(taskRefsVersionsSQL, activeStatuses(), pgBigintArray(ids)).
		Count(&refs.ActiveTasks).Error
	if err != nil {
		return PluginRefs{}, err
	}
	return refs, nil
}

// pgBigintArray 把 id 列表写成 PostgreSQL 数组字面量（如 {1,2,3}），配合 ?::bigint[] 使用；元素都是整数，不存在注入问题。
func pgBigintArray(ids []uint64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatUint(id, 10)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// DeletePlugin 在一个事务里删除整个插件（全部版本 + 插件行）：按“插件行 → 版本行”的顺序加锁（与 SaveVersion / DeleteVersion 一致，避免死锁），
// 锁内重新统计引用，仍被模型或非终态任务引用时返回当时的引用与 ErrInUse；插件不存在返回 ErrNotFound。
// 内置插件由 service 层拒绝，这里不判断来源。
func (r *AIPluginRepository) DeletePlugin(ctx context.Context, key string) (PluginRefs, error) {
	var refs PluginRefs
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 锁插件行：与同插件的上传（SaveVersion 的冲突更新）、删除版本串行
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("key").
			Where("key = ?", key).First(&model.AIPlugin{}).Error; err != nil {
			return translate(err)
		}
		// 2. 锁全部版本行（按 id 升序加锁，顺序固定）：与其他对版本行的写操作串行
		var ids []uint64
		if err := tx.Model(&model.AIPluginVersion{}).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("plugin_key = ?", key).Order("id ASC").Pluck("id", &ids).Error; err != nil {
			return err
		}
		// 3. 锁内重新统计引用
		var err error
		refs, err = countPluginRefs(tx, key, ids)
		if err != nil {
			return err
		}
		if refs.InUse() {
			return ErrInUse
		}
		// 4. 删全部版本与插件行
		if err := tx.Where("plugin_key = ?", key).Delete(&model.AIPluginVersion{}).Error; err != nil {
			return err
		}
		return tx.Where("key = ?", key).Delete(&model.AIPlugin{}).Error
	})
	return refs, err
}
