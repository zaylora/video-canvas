package model

import (
	"time"

	"gorm.io/datatypes"
)

// CanvasCollaborationComment 保存协作会话中的节点评论和提及对象。
type CanvasCollaborationComment struct {
	ID        uint64         `gorm:"primaryKey;index:idx_canvas_collab_comment_thread,priority:4;index:idx_canvas_collab_comment_replay,priority:3"`
	Canvas    CanvasProject  `gorm:"foreignKey:CanvasID;references:ID;constraint:OnDelete:CASCADE"`
	CanvasID  uint64         `gorm:"not null;uniqueIndex:idx_canvas_collab_comment_request,priority:1;index:idx_canvas_collab_comment_thread,priority:1;index:idx_canvas_collab_comment_replay,priority:1"`
	Epoch     uint64         `gorm:"not null;uniqueIndex:idx_canvas_collab_comment_request,priority:2;index:idx_canvas_collab_comment_thread,priority:2;index:idx_canvas_collab_comment_replay,priority:2"`
	NodeID    string         `gorm:"size:128;not null;index:idx_canvas_collab_comment_thread,priority:3"`
	ActorID   uint64         `gorm:"not null;uniqueIndex:idx_canvas_collab_comment_request,priority:3"`
	OpID      string         `gorm:"size:64;not null;uniqueIndex:idx_canvas_collab_comment_request,priority:4"`
	Body      string         `gorm:"type:text;not null"`
	Mentions  datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	CreatedAt time.Time      `gorm:"not null;default:now()"`
}

// TableName 返回节点评论表名。
func (CanvasCollaborationComment) TableName() string { return "canvas_collaboration_comments" }

// CanvasCommentRequest 描述一条可安全重试的节点评论。
type CanvasCommentRequest struct {
	Epoch    uint64   `json:"epoch" binding:"required"`
	NodeID   string   `json:"nodeId" binding:"required,max=128"`
	OpID     string   `json:"opId" binding:"required,max=64"`
	Body     string   `json:"body" binding:"required,max=2000"`
	Mentions []string `json:"mentions" binding:"max=10"`
}
