package model

import (
	"time"

	"gorm.io/gorm"
)

// BaseModel 是所有表的公共字段，DeletedAt 开启 GORM 软删除。
type BaseModel struct {
	ID        uint64         `gorm:"primaryKey;comment:主键ID" json:"id"` // 主键 ID
	CreatedAt time.Time      `gorm:"comment:创建时间" json:"created_at"`    // 创建时间
	UpdatedAt time.Time      `gorm:"comment:更新时间" json:"updated_at"`    // 更新时间
	DeletedAt gorm.DeletedAt `gorm:"index;comment:删除时间（软删除）" json:"-"`  // 软删除时间，不对外返回
}

// All 返回需要自动迁移的模型，新增表时在这里注册。
func All() []any {
	return []any{
		&User{},
		&CanvasProject{},
		&GenerationTask{},
		&UserCredit{},
		&CreditLedger{},
		&Asset{},
		&StorageConfig{},
		&AssetUploadIntent{},
		&AIPlugin{},
		&AIPluginVersion{},
		&AIChannel{},
		&AIModel{},
		&AIConfigRevision{},
		&AISecret{},
		&AIAuditLog{},
	}
}
