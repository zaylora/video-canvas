package model

import (
	"time"

	"gorm.io/datatypes"
)

// StoryboardSplitJob 是一次「从剧本拆分镜」的异步任务。
type StoryboardSplitJob struct {
	BudgetGroupID         *uint64                        `gorm:"index:idx_storyboard_split_jobs_budget_group_id,where:budget_group_id IS NOT NULL" json:"-"`
	BudgetRequest         *CanvasGenerationBudgetRequest `gorm:"-" json:"-"`
	ID                    uint64                         `gorm:"primaryKey" json:"id"`
	UserID                uint64                         `gorm:"not null;index:idx_storyboard_split_user;uniqueIndex:uk_storyboard_split_request" json:"user_id"`
	CanvasID              uint64                         `gorm:"not null;index:idx_storyboard_split_collab,priority:1;uniqueIndex:uk_storyboard_split_active,where:status IN ('pending'\\,'running')" json:"canvas_id"`
	NodeID                string                         `gorm:"size:64;not null;uniqueIndex:uk_storyboard_split_active,where:status IN ('pending'\\,'running')" json:"node_id"`
	ModelKey              string                         `gorm:"size:128;not null;default:''" json:"model_key"`
	Script                string                         `gorm:"type:text;not null;default:''" json:"-"` // 不返给前端
	Status                string                         `gorm:"size:16;not null;default:pending" json:"status"`
	Error                 string                         `gorm:"type:text;not null;default:''" json:"error,omitempty"`
	Shots                 datatypes.JSON                 `gorm:"type:jsonb" json:"shots,omitempty"`
	Credits               int                            `gorm:"not null;default:0" json:"credits"`
	InputTokens           int                            `gorm:"not null;default:0" json:"-"`
	OutputTokens          int                            `gorm:"not null;default:0" json:"-"`
	PromptChars           int                            `gorm:"not null;default:0" json:"-"`
	Attempts              int                            `gorm:"not null;default:0" json:"-"`
	CollaborationEpoch    uint64                         `gorm:"not null;default:0;index:idx_storyboard_split_collab,priority:2" json:"-"`
	CollaborationRevision uint64                         `gorm:"not null;default:0" json:"-"`
	Mode                  string                         `gorm:"size:8;not null;default:''" json:"-"`
	RequestKey            *string                        `gorm:"size:64;uniqueIndex:uk_storyboard_split_request" json:"-"`
	RequestHash           string                         `gorm:"size:64;not null;default:''" json:"-"`
	MaxCredits            *int                           `json:"-"`
	CreatedAt             time.Time                      `json:"created_at"`
	UpdatedAt             time.Time                      `json:"updated_at"`
}

// TableName 指定表名。
func (StoryboardSplitJob) TableName() string { return "storyboard_split_jobs" }

// StoryboardSplitJob 的状态。
const (
	SplitJobPending   = "pending"
	SplitJobRunning   = "running"
	SplitJobSucceeded = "succeeded"
	SplitJobFailed    = "failed"
)
