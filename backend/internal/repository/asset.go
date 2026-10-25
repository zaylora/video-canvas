package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// AssetRepository 素材表的数据访问。
type AssetRepository struct {
	db *gorm.DB
}

// NewAssetRepository 创建素材仓储。
func NewAssetRepository(db *gorm.DB) *AssetRepository {
	return &AssetRepository{db: db}
}

// Create 插入素材行，成功后 a.ID 已回填；storage_key 重复返回 ErrDuplicate。
func (r *AssetRepository) Create(ctx context.Context, a *model.Asset) error {
	if err := r.db.WithContext(ctx).Create(a).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// GetByStorageKey 按对象 key 查询，不带用户条件：给 /files/<key> 路由反查素材所在的存储用，
// 这条路由本来就不鉴权，靠 key 不可猜测保护。查不到返回 ErrNotFound。
func (r *AssetRepository) GetByStorageKey(ctx context.Context, key string) (*model.Asset, error) {
	var a model.Asset
	if err := r.db.WithContext(ctx).Where("storage_key = ?", key).First(&a).Error; err != nil {
		return nil, translate(err)
	}
	return &a, nil
}

// GetByID 按 ID 查询，并限定所属用户；查不到或不属于该用户都返回 ErrNotFound。
func (r *AssetRepository) GetByID(ctx context.Context, userID, id uint64) (*model.Asset, error) {
	var a model.Asset
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&a).Error; err != nil {
		return nil, translate(err)
	}
	return &a, nil
}

// FindBySha256 按内容哈希与文件大小查找素材，不带用户条件：给画布导入 / 复制按内容复用已有文件用，
// 调用方只取文件本身（存储位置），不会把别人的素材行暴露给用户。按 id 升序最多返回 5 条（取最早的几份即可），
// sha 为空串时不会匹配“未计算”的行，直接返回空切片；查不到返回空切片（非 nil），不返回 ErrNotFound。
func (r *AssetRepository) FindBySha256(ctx context.Context, sha string, byteSize int64) ([]model.Asset, error) {
	rows := []model.Asset{}
	if sha == "" {
		return rows, nil
	}
	if err := r.db.WithContext(ctx).Where("sha256 = ? AND byte_size = ?", sha, byteSize).
		Order("id ASC").Limit(5).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// SetSha256IfEmpty 给还没有哈希的素材回填 sha256：条件 id = ? AND sha256 = ”，已有哈希时不覆盖，所以重复调用是幂等的；
// 素材不存在（影响 0 行）同样返回 nil，不返回 ErrNotFound。不带用户条件，只由后端内部流程调用。
func (r *AssetRepository) SetSha256IfEmpty(ctx context.Context, id uint64, sha string) error {
	return r.db.WithContext(ctx).Model(&model.Asset{}).
		Where("id = ? AND sha256 = ''", id).Update("sha256", sha).Error
}

// ListByIDs 按 id 批量查询素材，不带用户条件：给登录页展示这类“管理员挑选后全站公开”的场景用，
// 归属已在管理员添加条目时校验过。不存在的 id 直接缺席，返回顺序不保证；ids 为空返回 nil。
func (r *AssetRepository) ListByIDs(ctx context.Context, ids []uint64) ([]model.Asset, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []model.Asset
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListByKindSource 按 kind + source 分页查询素材，不带用户条件：给后台“展示作品素材库”列出全平台生成的视频用，
// 按 created_at 倒序、id 倒序（同一时刻生成的素材顺序稳定），同时返回满足条件的总数。没有符合条件的素材时返回空切片与 0。
func (r *AssetRepository) ListByKindSource(ctx context.Context, kind, source string, offset, limit int) ([]model.Asset, int64, error) {
	db := r.db.WithContext(ctx).Model(&model.Asset{}).Where("kind = ? AND source = ?", kind, source)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.Asset
	if err := db.Order("created_at DESC, id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// GeneratedAssetFilter 是生成历史的过滤条件。
type GeneratedAssetFilter struct {
	Kind        string      // 素材类型（image / video / audio）
	Source      string      // all（空等同）/ conversation（任务没有画布）/ canvas（任务有画布）
	CanvasID    uint64      // 非 0 时只看这张画布的产物（隐含 canvas 来源）
	CreatedFrom *time.Time  // 只看不早于这个时间生成的素材，nil 不限
	Asc         bool        // true 最早优先，默认最新优先
	Q           string      // 提示词包含匹配（忽略大小写），空不过滤
	After       *TimeCursor // 上一页最后一条的位置，nil 从头开始
	Limit       int         // 最多返回多少条
}

// ListGeneratedByUser 返回该用户的生成历史：source=generated 的素材，左联所属任务取出提示词、模型和画布来源，
// 排除运营试跑任务（is_test）的产物。按 (created_at, id) 排序并游标翻页，最多 f.Limit 条。
// 用左联而不是内联：个别没有任务记录的生成产物（历史数据）仍然保留在历史里，只是没有提示词与来源。
func (r *AssetRepository) ListGeneratedByUser(ctx context.Context, userID uint64, f GeneratedAssetFilter) ([]model.GeneratedAssetRow, error) {
	db := r.db.WithContext(ctx).Table("assets").
		Joins("LEFT JOIN generation_tasks t ON t.id = assets.task_id").
		Select("assets.*, t.canvas_project_id AS task_canvas_project_id, t.input_json AS task_input_json, "+
			"t.config_snapshot AS task_config_snapshot, COALESCE(t.model_id, '') AS task_model_key").
		Where("assets.user_id = ? AND assets.source = ? AND assets.kind = ?", userID, model.AssetSourceGenerated, f.Kind).
		Where("COALESCE(t.is_test, false) = false")
	switch {
	case f.CanvasID != 0:
		db = db.Where("t.canvas_project_id = ?", f.CanvasID)
	case f.Source == "conversation":
		db = db.Where("t.canvas_project_id IS NULL")
	case f.Source == "canvas":
		db = db.Where("t.canvas_project_id IS NOT NULL")
	}
	if f.CreatedFrom != nil {
		db = db.Where("assets.created_at >= ?", *f.CreatedFrom)
	}
	if f.Q != "" {
		db = db.Where("t.input_json->>'prompt' ILIKE ?", "%"+escapeLike(f.Q)+"%")
	}
	order, cmp := "assets.created_at DESC, assets.id DESC", "<"
	if f.Asc {
		order, cmp = "assets.created_at ASC, assets.id ASC", ">"
	}
	if f.After != nil {
		db = db.Where("(assets.created_at, assets.id) "+cmp+" (?, ?)", f.After.At, f.After.ID)
	}
	var rows []model.GeneratedAssetRow
	if err := db.Order(order).Limit(f.Limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
