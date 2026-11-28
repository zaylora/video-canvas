package model

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

// 协作提交各层共同使用的容量限制。
const (
	CanvasCollaborationMaxBatchRecords = 100
	CanvasCollaborationMaxBatchBytes   = 256 * 1024
	CanvasCollaborationMaxRequestBytes = 1024 * 1024
)

// CanvasCollaborationBatch 描述同一事务内确认的可重试编辑。
type CanvasCollaborationBatch struct {
	OpID                string                      `json:"opId"`
	Epoch               uint64                      `json:"epoch"`
	BaseRevision        uint64                      `json:"baseRevision"`
	RequireBaseRevision bool                        `json:"requireBaseRevision,omitempty"`
	Changes             []CanvasCollaborationChange `json:"changes"`
}

// CanvasCollaborationChange 描述一个记录上成组提交的字段。
type CanvasCollaborationChange struct {
	Kind             string                     `json:"kind"`
	ID               string                     `json:"id"`
	Action           string                     `json:"action,omitempty"`
	Value            json.RawMessage            `json:"value,omitempty"`
	Fields           map[string]json.RawMessage `json:"fields,omitempty"`
	FieldBases       map[string]uint64          `json:"fieldBases,omitempty"`
	RecordBase       uint64                     `json:"recordBase,omitempty"`
	PreviousRevision uint64                     `json:"previousRevision,omitempty"`
}

// CanvasCollaborationUndo 标识一次可重试的个人撤销。
type CanvasCollaborationUndo struct {
	OpID             string `json:"opId"`
	Epoch            uint64 `json:"epoch"`
	Redo             bool   `json:"redo,omitempty"`
	ExpectedRevision uint64 `json:"expectedRevision,omitempty"`
}

// CanvasCollaborationRecord 存储一个节点、组、连线或其删除墓碑。
type CanvasCollaborationRecord struct {
	Canvas        CanvasProject  `gorm:"foreignKey:CanvasID;references:ID;constraint:OnDelete:CASCADE" json:"-"`
	CanvasID      uint64         `gorm:"primaryKey;autoIncrement:false;index:idx_canvas_collab_records_live_order,priority:1,where:deleted = false"`
	Kind          string         `gorm:"primaryKey;size:8;index:idx_canvas_collab_records_live_order,priority:3,where:deleted = false"`
	RecordID      string         `gorm:"primaryKey;size:128;index:idx_canvas_collab_records_live_order,priority:4,where:deleted = false"`
	Ordinal       int            `gorm:"not null;index:idx_canvas_collab_records_live_order,priority:2,where:deleted = false"`
	Body          datatypes.JSON `gorm:"type:jsonb;not null"`
	FieldVersions datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	Revision      uint64         `gorm:"not null"`
	Deleted       bool           `gorm:"not null;default:false"`
}

// TableName 返回协作记录表名。
func (CanvasCollaborationRecord) TableName() string { return "canvas_collaboration_records" }

// CanvasCollaborationReceipt 保存已处理请求的摘要和确认结果。
type CanvasCollaborationReceipt struct {
	Canvas      CanvasProject  `gorm:"foreignKey:CanvasID;references:ID;constraint:OnDelete:CASCADE"`
	CanvasID    uint64         `gorm:"primaryKey;autoIncrement:false"`
	ActorID     uint64         `gorm:"primaryKey;autoIncrement:false"`
	OpID        string         `gorm:"primaryKey;size:64"`
	RequestHash string         `gorm:"size:64;not null"`
	Result      datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedAt   time.Time      `gorm:"not null;default:now()"`
}

// TableName 返回协作收据表名。
func (CanvasCollaborationReceipt) TableName() string { return "canvas_collaboration_receipts" }

// CanvasCollaborationEvent 保存一个已确认版本的可回放编辑。
type CanvasCollaborationEvent struct {
	Canvas      CanvasProject  `gorm:"foreignKey:CanvasID;references:ID;constraint:OnDelete:CASCADE"`
	CanvasID    uint64         `gorm:"primaryKey;autoIncrement:false"`
	Revision    uint64         `gorm:"primaryKey;autoIncrement:false"`
	Epoch       uint64         `gorm:"not null"`
	ActorID     uint64         `gorm:"not null"`
	Source      string         `gorm:"size:16;not null;default:''"`
	OpID        string         `gorm:"size:64;not null"`
	Changes     datatypes.JSON `gorm:"type:jsonb;not null"`
	Undo        datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt   time.Time      `gorm:"not null;default:now();index:idx_canvas_collaboration_events_pending,where:published_at IS NULL"`
	PublishedAt *time.Time
}

// TableName 返回协作事件表名。
func (CanvasCollaborationEvent) TableName() string { return "canvas_collaboration_events" }

// CanvasCollaborationCheckpoint 保存历史重放的已确认起点。
type CanvasCollaborationCheckpoint struct {
	Canvas      CanvasProject  `gorm:"foreignKey:CanvasID;references:ID;constraint:OnDelete:CASCADE"`
	CanvasID    uint64         `gorm:"primaryKey;autoIncrement:false"`
	Epoch       uint64         `gorm:"primaryKey;autoIncrement:false"`
	Revision    uint64         `gorm:"primaryKey;autoIncrement:false"`
	PayloadJSON datatypes.JSON `gorm:"type:jsonb;not null"`
	Baseline    bool           `gorm:"not null;default:false"`
	CreatedAt   time.Time      `gorm:"not null;default:now()"`
}

// TableName 返回协作检查点表名。
func (CanvasCollaborationCheckpoint) TableName() string { return "canvas_collaboration_checkpoints" }
