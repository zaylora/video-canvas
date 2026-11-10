package model

import (
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/pkg/idcodec" //nolint:depguard // 对外 ID 与短剧项目沿用十六进制契约
)

// DramaSegment 单集内可独立生成的片段，任务和素材保留各自的生命周期。
type DramaSegment struct {
	BaseModel
	UserID       uint64 `gorm:"not null;index"`
	EpisodeID    uint64 `gorm:"not null;index:idx_drama_segment_episode_seq,priority:1"`
	Seq          int    `gorm:"not null;default:0;index:idx_drama_segment_episode_seq,priority:2"`
	Seconds      int    `gorm:"not null;default:5"`
	Prompt       string `gorm:"type:text;not null;default:''"`
	ChosenTaskID *uint64
	// Adaptations 按格式（h3 / generic）存原稿的转写稿，值为 DramaSegmentAdaptation；原稿就是 Seedance，不存 seedance 键
	Adaptations datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	Revision    int64          `gorm:"not null;default:1"`
}

// DramaSegmentAdaptation 是一份转写稿；SourceHash 是转写时原稿的 SHA-256，与当前原稿不同即已过期。
type DramaSegmentAdaptation struct {
	Prompt     string    `json:"prompt"`
	SourceHash string    `json:"source_hash"`
	Edited     bool      `json:"edited"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// TableName 返回片段表名。
func (DramaSegment) TableName() string { return "drama_segments" }

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
	ID           idcodec.ID                        `json:"id"`
	EpisodeNo    int                               `json:"episode_no"`
	Seq          int                               `json:"seq"`
	Seconds      int                               `json:"seconds"`
	Prompt       string                            `json:"prompt"`
	Adaptations  map[string]DramaSegmentAdaptation `json:"adaptations"`
	Takes        []DramaSegmentTakeView            `json:"takes"`
	ChosenTaskID string                            `json:"chosen_task_id"`
	Status       string                            `json:"status"`
	Revision     int64                             `json:"revision"`
	UpdatedAt    time.Time                         `json:"updated_at"`
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
	// Adaptation 手改某个格式的转写稿；该格式必须已经转写过
	Adaptation *DramaSegmentAdaptationEdit `json:"adaptation"`
}

// DramaSegmentAdaptationEdit 是对一份转写稿的手改。
type DramaSegmentAdaptationEdit struct {
	Format string `json:"format" binding:"required,oneof=h3 generic" label:"转写格式"`
	Prompt string `json:"prompt"`
}

// AdaptDramaSegmentReq 把片段原稿转写成目标格式；revision 用来确认转写的是用户眼前这一版。
type AdaptDramaSegmentReq struct {
	Format   string `json:"format" binding:"required,oneof=h3 generic" label:"转写格式"`
	Revision int64  `json:"revision" binding:"required,min=1" label:"版本"`
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
