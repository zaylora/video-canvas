package repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

type CanvasProjectRepository struct {
	db *gorm.DB
}

func NewCanvasProjectRepository(db *gorm.DB) *CanvasProjectRepository {
	return &CanvasProjectRepository{db: db}
}

func (r *CanvasProjectRepository) Create(ctx context.Context, p *model.CanvasProject) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// GetByID 按 ID 查询，并限定所属用户，查不到或不属于该用户都返回 ErrNotFound。
func (r *CanvasProjectRepository) GetByID(ctx context.Context, userID, id uint64) (*model.CanvasProject, error) {
	var p model.CanvasProject
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&p).Error; err != nil {
		return nil, translate(err)
	}
	return &p, nil
}

// List 分页查询用户的画布列表，不查 payload_json 大字段。
func (r *CanvasProjectRepository) List(ctx context.Context, userID uint64, keyword string, offset, limit int) ([]model.CanvasProjectItem, int64, error) {
	var (
		items []model.CanvasProjectItem
		total int64
	)
	db := r.db.WithContext(ctx).Model(&model.CanvasProject{}).Where("user_id = ?", userID)
	if keyword != "" {
		db = db.Where("title ILIKE ?", "%"+escapeLike(keyword)+"%")
	}
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Select("id", "title", "revision", "created_at", "updated_at").
		Order("updated_at DESC").Offset(offset).Limit(limit).Find(&items).Error
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Update 带乐观锁更新：只有 revision 与数据库一致时才更新，并把 revision +1。
// 记录不存在返回 ErrNotFound，版本号不一致返回 ErrRevisionConflict。
func (r *CanvasProjectRepository) Update(ctx context.Context, userID, id, revision uint64, fields map[string]any) error {
	fields["revision"] = gorm.Expr("revision + 1")
	res := r.db.WithContext(ctx).Model(&model.CanvasProject{}).
		Where("id = ? AND user_id = ? AND revision = ?", id, userID, revision).
		Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	// 没有更新到任何行：区分是记录不存在还是版本冲突
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.CanvasProject{}).
		Where("id = ? AND user_id = ?", id, userID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return ErrRevisionConflict
}

func (r *CanvasProjectRepository) Delete(ctx context.Context, userID, id uint64) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.CanvasProject{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// escapeLike 转义 LIKE 通配符，避免用户输入的 % 和 _ 被当作通配符。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
