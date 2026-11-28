package model

import (
	"time"

	"gorm.io/datatypes"
)

// CanvasComposeReq 固定成片输入节点的顺序和观察版本。
type CanvasComposeReq struct {
	Epoch    uint64   `json:"epoch" binding:"required"`
	Revision uint64   `json:"revision" binding:"required"`
	OpID     string   `json:"opId" binding:"required"`
	Nodes    []string `json:"nodes" binding:"required,min=1,max=24"`
}

// CanvasComposeClip 保存入队时已确认的节点和素材。
type CanvasComposeClip struct {
	Node  string `json:"node"`
	Asset uint64 `json:"asset"`
}

// CanvasComposeJob 是成片任务和队列恢复的持久化记录。
type CanvasComposeJob struct {
	ID          uint64         `gorm:"primaryKey"`
	CanvasID    uint64         `gorm:"not null;uniqueIndex:idx_canvas_compose_request,priority:1"`
	UserID      uint64         `gorm:"not null;uniqueIndex:idx_canvas_compose_request,priority:2"`
	OpID        string         `gorm:"size:64;not null;uniqueIndex:idx_canvas_compose_request,priority:3"`
	Epoch       uint64         `gorm:"not null"`
	Revision    uint64         `gorm:"not null"`
	RequestHash string         `gorm:"size:64;not null"`
	Inputs      datatypes.JSON `gorm:"not null"`
	Status      string         `gorm:"size:16;not null"`
	Error       string         `gorm:"not null;default:''"`
	Attempt     int            `gorm:"not null;default:0"`
	AssetID     uint64         `gorm:"not null;default:0"`
	ResultNode  string         `gorm:"size:128;not null;default:''"`
	CreatedAt   time.Time      `gorm:"not null"`
	UpdatedAt   time.Time      `gorm:"not null"`
}
