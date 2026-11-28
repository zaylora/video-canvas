package model

import "time"

// 混合存储策略的持久化阶段。
const (
	StorageHybridOff       = "off"
	StorageHybridPreparing = "preparing"
	StorageHybridActive    = "active"
	StorageHybridFailed    = "failed"
)

// StorageHybridPolicy 是当前部署的混合存储策略和待执行的启用任务。
type StorageHybridPolicy struct {
	ID         int       `gorm:"primaryKey;autoIncrement:false" json:"-"`
	State      string    `gorm:"size:16;not null" json:"state"`
	CloudID    uint64    `gorm:"not null" json:"cloud_id"`
	LocalID    uint64    `gorm:"not null" json:"local_id"`
	LimitBytes int64     `gorm:"not null" json:"limit_bytes"`
	Error      string    `gorm:"type:text;not null" json:"error"`
	LeaseToken string    `gorm:"size:36;not null" json:"-"`
	LeaseUntil time.Time `gorm:"not null" json:"-"`
}

// TableName 返回混合策略表。
func (StorageHybridPolicy) TableName() string { return "storage_hybrid_policy" }
