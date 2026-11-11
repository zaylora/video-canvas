package repository

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// AnnouncementTx 是调整公告顺序时要用到的 SQL 原语；AnnouncementRepository 自身实现它，WithTx 把它绑定到事务上传给回调。
type AnnouncementTx interface {
	// ListIDs 加行锁（FOR UPDATE）返回全部未删除公告的 id，按 sort、id 升序；锁在事务结束时释放，避免重排与并发的增删交错。
	ListIDs(ctx context.Context) ([]uint64, error)
	// SetSort 把某条公告的 sort 设为 sort；公告不存在返回 ErrNotFound。
	SetSort(ctx context.Context, id uint64, sort int) error
}

// AnnouncementRepository 首页公告表（announcements）的数据访问。公告全站共享，查询不带 user_id。
type AnnouncementRepository struct {
	db *gorm.DB
}

// NewAnnouncementRepository 创建公告仓储。
func NewAnnouncementRepository(db *gorm.DB) *AnnouncementRepository {
	return &AnnouncementRepository{db: db}
}

var _ AnnouncementTx = (*AnnouncementRepository)(nil)

// WithTx 在一个事务里执行 fn：fn 返回错误整体回滚，否则提交。回调里只能用传入的 tx。
func (r *AnnouncementRepository) WithTx(ctx context.Context, fn func(tx AnnouncementTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&AnnouncementRepository{db: tx})
	})
}

// ListAll 返回全部未删除的公告（含停用、未生效、已过期），按 sort 升序，sort 相同按 id 升序。
func (r *AnnouncementRepository) ListAll(ctx context.Context) ([]model.Announcement, error) {
	var rows []model.Announcement
	if err := r.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// GetByID 按 id 查询未删除的公告，不存在返回 ErrNotFound。
func (r *AnnouncementRepository) GetByID(ctx context.Context, id uint64) (*model.Announcement, error) {
	var a model.Announcement
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&a).Error; err != nil {
		return nil, translate(err)
	}
	return &a, nil
}

// MaxSort 返回当前最大的 sort；没有任何公告时返回 -1，这样“最大值 + 1”恰好是第一条的 0。
func (r *AnnouncementRepository) MaxSort(ctx context.Context) (int, error) {
	var top int
	err := r.db.WithContext(ctx).Model(&model.Announcement{}).Select("COALESCE(MAX(sort), -1)").Scan(&top).Error
	return top, err
}

// Create 插入公告，成功后 a.ID、CreatedAt、UpdatedAt 已回填。
func (r *AnnouncementRepository) Create(ctx context.Context, a *model.Announcement) error {
	return r.db.WithContext(ctx).Create(a).Error
}

// Update 按 id 更新指定列（fields 的 key 是列名，值为 nil 表示置 NULL），updated_at 由 gorm 自动刷新；公告不存在返回 ErrNotFound。
func (r *AnnouncementRepository) Update(ctx context.Context, id uint64, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.Announcement{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetEnabled 只改启用开关与修改人，不刷新 updated_at：启停不是内容变化，不应让用户已关闭的公告重新出现；公告不存在返回 ErrNotFound。
func (r *AnnouncementRepository) SetEnabled(ctx context.Context, id uint64, enabled bool, actorID uint64) error {
	res := r.db.WithContext(ctx).Model(&model.Announcement{}).Where("id = ?", id).
		UpdateColumns(map[string]any{"enabled": enabled, "updated_by": actorID})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 软删除公告；公告不存在返回 ErrNotFound。
func (r *AnnouncementRepository) Delete(ctx context.Context, id uint64) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Announcement{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ListIDs 见 AnnouncementTx.ListIDs。
func (r *AnnouncementRepository) ListIDs(ctx context.Context) ([]uint64, error) {
	var ids []uint64
	err := r.db.WithContext(ctx).Model(&model.Announcement{}).Clauses(clause.Locking{Strength: "UPDATE"}).
		Order("sort ASC, id ASC").Pluck("id", &ids).Error
	return ids, err
}

// SetSort 见 AnnouncementTx.SetSort。只改 sort 列，不刷新 updated_at：顺序变化不是内容变化，不应让用户已关闭的公告重新出现。
func (r *AnnouncementRepository) SetSort(ctx context.Context, id uint64, sort int) error {
	res := r.db.WithContext(ctx).Model(&model.Announcement{}).Where("id = ?", id).UpdateColumn("sort", sort)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
