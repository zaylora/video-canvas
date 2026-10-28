package model

import (
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/pkg/idcodec" //nolint:depguard // 对外 ID 与短剧项目沿用十六进制契约
)

// DramaSegment 单集内可独立生成的片段，任务和素材保留各自的生命周期。
type DramaSegment struct {
	BaseModel
	UserID          uint64         `gorm:"not null;index"`
	EpisodeID       uint64         `gorm:"not null;index:idx_drama_segment_episode_seq,priority:1"`
	Seq             int            `gorm:"not null;default:0;index:idx_drama_segment_episode_seq,priority:2"`
	Seconds         int            `gorm:"not null;default:5"`
	Prompt          string         `gorm:"type:text;not null;default:''"`
	Shots           datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	KeyframeAssetID *uint64
	ChosenTaskID    *uint64
	Revision        int64 `gorm:"not null;default:1"`
}

// TableName 返回片段表名。
func (DramaSegment) TableName() string { return "drama_segments" }

// DramaSegmentShot 一条分镜的时长、摄影和表演信息。
type DramaSegmentShot struct {
	Seconds  int    `json:"seconds"`
	ShotSize string `json:"shot_size"`
	Camera   string `json:"camera"`
	Action   string `json:"action"`
	Dialogue string `json:"dialogue"`
}

// DramaSegmentTakeView 从生成任务读取的结果，不复制保存任务状态。
type DramaSegmentTakeView struct {
	TaskID       idcodec.ID `json:"task_id"`
	Status       string     `json:"status"`
	Asset        *AssetView `json:"asset"`
	ErrorMessage string     `json:"error_message"`
	CreatedAt    time.Time  `json:"created_at"`
}

// DramaSegmentView 是片段工作区和 WebSocket 共用视图。
type DramaSegmentView struct {
	ID           idcodec.ID             `json:"id"`
	EpisodeNo    int                    `json:"episode_no"`
	Seq          int                    `json:"seq"`
	Seconds      int                    `json:"seconds"`
	Prompt       string                 `json:"prompt"`
	Shots        []DramaSegmentShot     `json:"shots"`
	Keyframe     *AssetView             `json:"keyframe"`
	Takes        []DramaSegmentTakeView `json:"takes"`
	ChosenTaskID string                 `json:"chosen_task_id"`
	Status       string                 `json:"status"`
	Revision     int64                  `json:"revision"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// CreateDramaSegmentReq 新增片段，after_seq 未传则追加到末尾。
type CreateDramaSegmentReq struct {
	Prompt   string `json:"prompt"`
	Seconds  *int   `json:"seconds" binding:"omitempty,min=1" label:"片段时长"`
	AfterSeq *int   `json:"after_seq" binding:"omitempty,min=0" label:"插入位置"`
}

// UpdateDramaSegmentReq 带版本部分更新，采用结果必须属于本段且已成功。
type UpdateDramaSegmentReq struct {
	Revision     int64       `json:"revision" binding:"required,min=1" label:"版本"`
	Prompt       *string     `json:"prompt"`
	Seconds      *int        `json:"seconds" binding:"omitempty,min=1" label:"片段时长"`
	ChosenTaskID *idcodec.ID `json:"chosen_task_id"`
}

// OrderDramaSegmentsReq 必须包含该集完整且不重复的片段集合。
type OrderDramaSegmentsReq struct {
	IDs []idcodec.ID `json:"ids" binding:"required" label:"片段列表"`
}

// GenerateDramaSegmentReq 复用生成任务的输入结构，种类、画布与节点由服务指定。
type GenerateDramaSegmentReq struct {
	ModelID string         `json:"model_id" binding:"required,max=128" label:"模型"`
	Input   map[string]any `json:"input" binding:"required" label:"生成参数"`
}
