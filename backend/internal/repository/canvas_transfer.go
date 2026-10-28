package repository

import (
	"context"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// CanvasTransferTx 是画布导入事务里要用到的 SQL 原语；CanvasTransferRepository 自身实现它，WithTx 把它绑定到事务上传给回调。
type CanvasTransferTx interface {
	// CreateAsset 插入一条素材记录，成功后回填 id；storage_key 冲突返回 ErrDuplicate。
	CreateAsset(ctx context.Context, a *model.Asset) error
	// CreateCanvas 插入一张画布，成功后回填 id。
	CreateCanvas(ctx context.Context, p *model.CanvasProject) error
}

// CanvasTransferRepository 是画布导入的事务仓储：素材、画布在同一个事务里写入。
type CanvasTransferRepository struct {
	db *gorm.DB
}

// NewCanvasTransferRepository 创建画布导入事务仓储。
func NewCanvasTransferRepository(db *gorm.DB) *CanvasTransferRepository {
	return &CanvasTransferRepository{db: db}
}

var _ CanvasTransferTx = (*CanvasTransferRepository)(nil)

// WithTx 在一个事务里执行 fn：fn 返回错误整体回滚，否则提交。回调里只能用传入的 tx。
func (r *CanvasTransferRepository) WithTx(ctx context.Context, fn func(tx CanvasTransferTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&CanvasTransferRepository{db: tx})
	})
}

// CreateAsset 插入一条素材记录并回填 id；storage_key 唯一索引冲突返回 ErrDuplicate。
func (r *CanvasTransferRepository) CreateAsset(ctx context.Context, a *model.Asset) error {
	if err := r.db.WithContext(ctx).Create(a).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// CreateCanvas 插入一张画布并回填 id。
func (r *CanvasTransferRepository) CreateCanvas(ctx context.Context, p *model.CanvasProject) error {
	return r.db.WithContext(ctx).Create(p).Error
}
