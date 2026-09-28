package model

import (
	"time"

	"gorm.io/gorm"
)

// BaseModel 是所有表的公共字段，DeletedAt 开启 GORM 软删除。
type BaseModel struct {
	ID        uint64         `gorm:"primaryKey;comment:主键ID" json:"id"`
	CreatedAt time.Time      `gorm:"comment:创建时间" json:"created_at"`
	UpdatedAt time.Time      `gorm:"comment:更新时间" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index;comment:删除时间（软删除）" json:"-"`
}

// All 返回需要自动迁移的模型，新增表时在这里注册。
func All() []any {
	return []any{
		&User{},
		&CanvasProject{},
	}
}
