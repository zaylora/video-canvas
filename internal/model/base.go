package model

import (
	"time"

	"gorm.io/gorm"
)

// BaseModel 是所有表的公共字段，DeletedAt 开启 GORM 软删除。
type BaseModel struct {
	ID        uint64         `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// All 返回需要自动迁移的模型，新增表时在这里注册。
func All() []any {
	return []any{
		&User{},
	}
}
