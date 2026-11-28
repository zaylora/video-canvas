package model

import "time"

// ReferenceCacheObject 登记已确认的临时对象与尚未确认的容量预留。Key 是一代对象独有的键。
type ReferenceCacheObject struct {
	Key        string    `gorm:"size:512;primaryKey"`
	Namespace  string    `gorm:"size:64;not null;index:idx_reference_cache_identity,priority:1;index:idx_reference_cache_sweep,priority:1"`
	Identity   string    `gorm:"size:64;not null;index:idx_reference_cache_identity,priority:2"`
	Digest     string    `gorm:"size:64;not null"`
	Size       int64     `gorm:"not null"`
	Ready      bool      `gorm:"not null"`
	Deleting   bool      `gorm:"not null"`
	CreatedAt  time.Time `gorm:"not null"`
	LastUsedAt time.Time `gorm:"not null"`
	BusyUntil  time.Time `gorm:"not null;index:idx_reference_cache_sweep,priority:2"`
}

// TableName 返回临时对象登记表。
func (ReferenceCacheObject) TableName() string { return "reference_cache_objects" }

// ReferenceCacheUse 持久记录一个任务对一代对象的使用。任务暂不可查时仍保护未过期的签名。
type ReferenceCacheUse struct {
	ObjectKey    string    `gorm:"size:512;primaryKey"`
	TaskID       uint64    `gorm:"primaryKey;autoIncrement:false"`
	ProtectUntil time.Time `gorm:"not null"`
}

// TableName 返回任务与临时对象的关联表。
func (ReferenceCacheUse) TableName() string { return "reference_cache_uses" }

// ReferenceCacheReservation 描述一次复用或创建临时对象所需的期限和容量。
type ReferenceCacheReservation struct {
	Object     ReferenceCacheObject
	TaskID     uint64
	ReuseAfter time.Time
	MaxBytes   int64
}

// ReferenceCacheSweep 描述一轮回收允许触及的范围与时间窗口。
type ReferenceCacheSweep struct {
	Namespace      string
	Now            time.Time
	UnusedSince    time.Time
	UncertainSince time.Time
	ClaimUntil     time.Time
}
