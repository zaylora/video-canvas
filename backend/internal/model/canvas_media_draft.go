package model

import (
	"time"

	"gorm.io/datatypes"
)

// CanvasMediaDraftReq 锁定加工时观察到的来源素材和协作版本。
type CanvasMediaDraftReq struct {
	ID          string
	SourceNode  string `form:"sourceNode" binding:"required,max=128"`
	SourceAsset uint64 `form:"sourceAsset" binding:"required"`
	Epoch       uint64 `form:"epoch" binding:"required"`
	Revision    uint64 `form:"revision" binding:"required"`
}

// CanvasMediaDraft 保存待挂载对象的归属、来源和回收状态。
type CanvasMediaDraft struct {
	CanvasID    uint64         `gorm:"primaryKey;autoIncrement:false"`
	ActorID     uint64         `gorm:"primaryKey;autoIncrement:false"`
	DraftID     string         `gorm:"primaryKey;size:64"`
	Epoch       uint64         `gorm:"not null"`
	Revision    uint64         `gorm:"not null"`
	SourceNode  string         `gorm:"size:128;not null"`
	SourceAsset uint64         `gorm:"not null"`
	State       string         `gorm:"size:16;not null"`
	StorageID   uint64         `gorm:"not null"`
	StorageKey  string         `gorm:"size:512;not null"`
	SHA256      string         `gorm:"size:64;not null"`
	Metadata    datatypes.JSON `gorm:"not null"`
	ExpiresAt   time.Time      `gorm:"index;not null"`
}
