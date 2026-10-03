package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// UploadIntentRepository 浏览器直传上传意图的数据访问。
type UploadIntentRepository struct {
	db *gorm.DB
}

// NewUploadIntentRepository 创建上传意图仓储。
func NewUploadIntentRepository(db *gorm.DB) *UploadIntentRepository {
	return &UploadIntentRepository{db: db}
}

// Create 插入一条上传意图，成功后 in.ID 已回填。
func (r *UploadIntentRepository) Create(ctx context.Context, in *model.AssetUploadIntent) error {
	return r.db.WithContext(ctx).Create(in).Error
}

// GetByID 按 ID 并限定申请人查询；查不到或不属于该用户都返回 ErrNotFound。
func (r *UploadIntentRepository) GetByID(ctx context.Context, userID, id uint64) (*model.AssetUploadIntent, error) {
	var in model.AssetUploadIntent
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&in).Error; err != nil {
		return nil, translate(err)
	}
	return &in, nil
}

// Complete 标记意图已完成登记。只有还没完成的意图才会被标记；已完成或不存在返回 ErrNotFound，
// 这样同一个意图重复提交 complete 只有第一次生效。
func (r *UploadIntentRepository) Complete(ctx context.Context, id uint64, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.AssetUploadIntent{}).
		Where("id = ? AND completed_at IS NULL", id).UpdateColumn("completed_at", at)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ListExpired 返回截至 now 已过期且仍未完成的意图（对象可能已经传到桶里却没人登记），按 ID 升序，最多 limit 条。
func (r *UploadIntentRepository) ListExpired(ctx context.Context, now time.Time, limit int) ([]model.AssetUploadIntent, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	var rows []model.AssetUploadIntent
	err := r.db.WithContext(ctx).Where("completed_at IS NULL AND expires_at <= ?", now).
		Order("id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

// Delete 删除一条意图；不存在不算错误。
func (r *UploadIntentRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&model.AssetUploadIntent{}, id).Error
}
