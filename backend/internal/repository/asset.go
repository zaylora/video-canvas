package repository

import (
	"context"

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
