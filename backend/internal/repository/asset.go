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
