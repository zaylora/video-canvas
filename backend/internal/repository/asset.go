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

// Create 插入素材行，成功后 a.ID 已回填。
func (r *AssetRepository) Create(ctx context.Context, a *model.Asset) error {
	return r.db.WithContext(ctx).Create(a).Error
}

// GetByID 按 ID 查询，并限定所属用户；查不到或不属于该用户都返回 ErrNotFound。
func (r *AssetRepository) GetByID(ctx context.Context, userID, id uint64) (*model.Asset, error) {
	var a model.Asset
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&a).Error; err != nil {
		return nil, translate(err)
	}
	return &a, nil
}
