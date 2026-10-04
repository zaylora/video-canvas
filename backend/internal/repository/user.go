package repository

import (
	"context"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// UserRepository 是用户的数据访问层。
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository 创建用户仓储。
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create 新建用户；用户名或邮箱唯一约束冲突返回 ErrDuplicate。
func (r *UserRepository) Create(ctx context.Context, u *model.User) error {
	if err := r.db.WithContext(ctx).Create(u).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// GetByID 按 id 查询用户；不存在返回 ErrNotFound。
func (r *UserRepository) GetByID(ctx context.Context, id uint64) (*model.User, error) {
	var u model.User
	if err := r.db.WithContext(ctx).First(&u, id).Error; err != nil {
		return nil, translate(err)
	}
	return &u, nil
}

// GetByUsername 按用户名查询用户；不存在返回 ErrNotFound。
func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	var u model.User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&u).Error; err != nil {
		return nil, translate(err)
	}
	return &u, nil
}

// List 分页查询用户（id 倒序），同时返回总数。
func (r *UserRepository) List(ctx context.Context, offset, limit int) ([]model.User, int64, error) {
	var (
		users []model.User
		total int64
	)
	db := r.db.WithContext(ctx).Model(&model.User{})
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := db.Order("id DESC").Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// Update 按 id 更新指定字段；用户不存在返回 ErrNotFound。
func (r *UserRepository) Update(ctx context.Context, id uint64, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 按 id 删除用户；用户不存在返回 ErrNotFound。
func (r *UserRepository) Delete(ctx context.Context, id uint64) error {
	res := r.db.WithContext(ctx).Delete(&model.User{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
