package repository

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// ShowcaseTx 是调整展示顺序时要用到的 SQL 原语；ShowcaseRepository 自身实现它，WithTx 把它绑定到事务上传给回调。
type ShowcaseTx interface {
	// ListIDs 加行锁（FOR UPDATE）返回全部未删除条目的 id，按 sort、id 升序；锁在事务结束时释放，避免重排与并发的增删交错。
	ListIDs(ctx context.Context) ([]uint64, error)
	// SetSort 把某个条目的 sort 设为 sort；条目不存在返回 ErrNotFound。
	SetSort(ctx context.Context, id uint64, sort int) error
}

// ShowcaseRepository 登录页展示条目表（showcase_items）的数据访问。
type ShowcaseRepository struct {
	db *gorm.DB
}

// NewShowcaseRepository 创建登录页展示条目仓储。
func NewShowcaseRepository(db *gorm.DB) *ShowcaseRepository {
	return &ShowcaseRepository{db: db}
}

var _ ShowcaseTx = (*ShowcaseRepository)(nil)

// WithTx 在一个事务里执行 fn：fn 返回错误整体回滚，否则提交。回调里只能用传入的 tx。
func (r *ShowcaseRepository) WithTx(ctx context.Context, fn func(tx ShowcaseTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&ShowcaseRepository{db: tx})
	})
}

// ListAll 返回全部未删除的条目（含禁用），按 sort 升序，sort 相同按 id 升序。
func (r *ShowcaseRepository) ListAll(ctx context.Context) ([]model.ShowcaseItem, error) {
	var rows []model.ShowcaseItem
	if err := r.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListEnabled 返回 enabled=true 的条目，排序规则同 ListAll。
func (r *ShowcaseRepository) ListEnabled(ctx context.Context) ([]model.ShowcaseItem, error) {
	var rows []model.ShowcaseItem
	if err := r.db.WithContext(ctx).Where("enabled").Order("sort ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// GetByID 按 id 查询（不带用户条件：条目是全站共享的展示内容，不属于某个用户），不存在返回 ErrNotFound。
func (r *ShowcaseRepository) GetByID(ctx context.Context, id uint64) (*model.ShowcaseItem, error) {
	var it model.ShowcaseItem
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&it).Error; err != nil {
		return nil, translate(err)
	}
	return &it, nil
}

// MaxSort 返回当前最大的 sort；没有任何条目时返回 -1，这样“最大值 + 1”恰好是第一个条目的 0。
func (r *ShowcaseRepository) MaxSort(ctx context.Context) (int, error) {
	var top int
	err := r.db.WithContext(ctx).Model(&model.ShowcaseItem{}).Select("COALESCE(MAX(sort), -1)").Scan(&top).Error
	return top, err
}

// Create 插入条目，成功后 it.ID 已回填。
func (r *ShowcaseRepository) Create(ctx context.Context, it *model.ShowcaseItem) error {
	return r.db.WithContext(ctx).Create(it).Error
}

// Update 按 id 更新指定列（fields 的 key 是列名，值为 nil 表示置 NULL），updated_at 由 gorm 自动刷新；条目不存在返回 ErrNotFound。
func (r *ShowcaseRepository) Update(ctx context.Context, id uint64, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.ShowcaseItem{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 软删除条目（只删条目行，不碰素材）；条目不存在返回 ErrNotFound。
func (r *ShowcaseRepository) Delete(ctx context.Context, id uint64) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.ShowcaseItem{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ReferencedAssetIDs 返回 assetIDs 里已被未删除条目作为视频引用的素材 id（含禁用的条目；去重，顺序不保证）；assetIDs 为空返回 nil。
func (r *ShowcaseRepository) ReferencedAssetIDs(ctx context.Context, assetIDs []uint64) ([]uint64, error) {
	if len(assetIDs) == 0 {
		return nil, nil
	}
	var ids []uint64
	err := r.db.WithContext(ctx).Model(&model.ShowcaseItem{}).Where("asset_id IN ?", assetIDs).Distinct().Pluck("asset_id", &ids).Error
	return ids, err
}

// ListIDs 见 ShowcaseTx.ListIDs。
func (r *ShowcaseRepository) ListIDs(ctx context.Context) ([]uint64, error) {
	var ids []uint64
	err := r.db.WithContext(ctx).Model(&model.ShowcaseItem{}).Clauses(clause.Locking{Strength: "UPDATE"}).
		Order("sort ASC, id ASC").Pluck("id", &ids).Error
	return ids, err
}

// SetSort 见 ShowcaseTx.SetSort。
func (r *ShowcaseRepository) SetSort(ctx context.Context, id uint64, sort int) error {
	res := r.db.WithContext(ctx).Model(&model.ShowcaseItem{}).Where("id = ?", id).Update("sort", sort)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
