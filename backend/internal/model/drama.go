package model

import (
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/pkg/idcodec" //nolint:depguard // 对外 id 用十六进制串，与画布、对话（存量代码）一致
)

// DramaProject 短剧项目，原文保留以支持重新研读。
type DramaProject struct {
	BaseModel
	UpdatedAt      time.Time `gorm:"index:idx_drama_user_updated,priority:2,sort:desc"`
	UserID         uint64    `gorm:"not null;index;index:idx_drama_user_updated,priority:1"`
	CanvasID       uint64    `gorm:"column:canvas_project_id;not null;uniqueIndex:uk_drama_canvas"`
	SourceAssetID  *uint64
	TextModelID    *uint64
	Title          string `gorm:"size:255;not null"`
	SourceText     string `gorm:"type:text;not null"`
	SourceKind     string `gorm:"size:16;not null;default:''"`
	SourceSummary  string `gorm:"type:text;not null;default:''"`
	Genre          string `gorm:"size:32;not null;default:''"`
	StyleKey       string `gorm:"size:32;not null"`
	StyleCustom    string `gorm:"type:text;not null;default:''"`
	Ratio          string `gorm:"size:8;not null;default:''"`
	TargetEpisodes int    `gorm:"not null;default:0"`
	EpisodeSeconds int    `gorm:"not null;default:60"`
	TextModel      string `gorm:"size:128;not null;default:''"`
	OutlineMD      string `gorm:"type:text;not null;default:''"`
	CharactersMD   string `gorm:"type:text;not null;default:''"`
	Status         string `gorm:"size:16;not null;default:analyzing"`
	Revision       int64  `gorm:"not null;default:1"`
}

// TableName 返回 DramaProject 对应的表名。
func (DramaProject) TableName() string { return "drama_projects" }

// DramaEpisode 集号定位的剧本，拆解后锁定正文和梗概。
type DramaEpisode struct {
	BaseModel
	UserID           uint64 `gorm:"not null;index"`
	DramaID          uint64 `gorm:"not null;uniqueIndex:uk_drama_episode_no,priority:1"`
	No               int    `gorm:"not null;uniqueIndex:uk_drama_episode_no,priority:2"`
	Title            string `gorm:"size:255;not null;default:''"`
	Synopsis         string `gorm:"type:text;not null;default:''"`
	ScriptMD         string `gorm:"type:text;not null;default:''"`
	ScriptStatus     string `gorm:"size:16;not null;default:empty"`
	StoryboardStatus string `gorm:"size:16;not null;default:empty"`
	FinalAssetID     *uint64
	LockedAt         *time.Time
	Revision         int64 `gorm:"not null;default:1"`
}

// TableName 返回 DramaEpisode 对应的表名。
func (DramaEpisode) TableName() string { return "drama_episodes" }

// DramaJob 一次流水线步骤，与一个 River job 一一对应。
type DramaJob struct {
	BaseModel
	UserID         uint64                   `gorm:"not null;index"`
	DramaID        uint64                   `gorm:"not null;index;uniqueIndex:uk_drama_active_job,where:status = 'queued' OR status = 'running'"`
	Step           string                   `gorm:"size:16;not null"`
	Episodes       datatypes.JSONSlice[int] `gorm:"type:jsonb;not null;default:'[]'"`
	Status         string                   `gorm:"size:16;not null;default:queued"`
	Progress       int                      `gorm:"not null;default:0"`
	Credits        int                      `gorm:"not null;default:0"`
	FrozenCredits  int                      `gorm:"not null;default:0"`
	ErrorCode      int                      `gorm:"not null;default:0"`
	ErrorMessage   string                   `gorm:"type:text;not null;default:''"`
	ConfigSnapshot datatypes.JSON           `gorm:"type:jsonb;not null;default:'{}'"`
	InputJSON      datatypes.JSON           `gorm:"type:jsonb;not null;default:'{}'"`
	ModelID        uint64                   `gorm:"not null;default:0"`
	Output         datatypes.JSON           `gorm:"type:jsonb;not null;default:'{}'"`
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

// TableName 返回 DramaJob 对应的表名。
func (DramaJob) TableName() string { return "drama_jobs" }

// CreateDramaReq 新建短剧的表单字段；文件由 handler 提取为 Filename/Data。
type CreateDramaReq struct {
	Text           string `form:"text"`
	Filename       string `form:"-"`
	Data           []byte `form:"-"`
	Title          string `form:"title" binding:"max=255"`
	Genre          string `form:"genre" binding:"max=32"`
	StyleKey       string `form:"style_key" binding:"required,max=32"`
	StyleCustom    string `form:"style_custom" binding:"max=500"`
	Ratio          string `form:"ratio" binding:"omitempty,oneof=9:16 16:9 21:9 3:4 4:3 1:1"`
	TargetEpisodes int    `form:"target_episodes" binding:"min=0,max=100"`
	EpisodeSeconds int    `form:"episode_seconds" binding:"omitempty,min=30,max=180"`
}

// UpdateDramaReq 部分更新项目，revision 用于乐观锁。
type UpdateDramaReq struct {
	Revision     int64   `json:"revision" binding:"required,min=1"`
	Title        *string `json:"title" binding:"omitempty,min=1,max=255"`
	Genre        *string `json:"genre" binding:"omitempty,max=32"`
	StyleKey     *string `json:"style_key" binding:"omitempty,min=1,max=32"`
	StyleCustom  *string `json:"style_custom" binding:"omitempty,max=500"`
	Ratio        *string `json:"ratio" binding:"omitempty,oneof=9:16 16:9 21:9 3:4 4:3 1:1"`
	TextModel    *string `json:"text_model" binding:"omitempty,max=128"`
	OutlineMD    *string `json:"outline_md"`
	CharactersMD *string `json:"characters_md"`
}

// CreateDramaEpisodeReq 追加空白集。
type CreateDramaEpisodeReq struct {
	Title    string `json:"title" binding:"max=255"`
	Synopsis string `json:"synopsis"`
}

// UpdateDramaEpisodeReq 集的部分更新。
type UpdateDramaEpisodeReq struct {
	Revision int64   `json:"revision" binding:"required,min=1"`
	Title    *string `json:"title" binding:"omitempty,max=255"`
	Synopsis *string `json:"synopsis"`
	ScriptMD *string `json:"script_md"`
}

// DramaJobReq 提交或估价的步骤与范围。
type DramaJobReq struct {
	Step     string `json:"step" binding:"required,oneof=analyze outline scripts breakdown asset_prompts"`
	Episodes []int  `json:"episodes" binding:"max=100,dive,min=1,max=100"`
	Ratio    string `json:"ratio" binding:"omitempty,oneof=9:16 16:9 21:9 3:4 4:3 1:1"`
}

// DramaSummary 列表项。
type DramaSummary struct {
	ID           idcodec.ID `json:"id"`
	Title        string     `json:"title"`
	Status       string     `json:"status"`
	EpisodeCount int        `json:"episode_count"`
	LockedCount  int        `json:"locked_count"`
	CoverURL     string     `json:"cover_url"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// DramaEpisodeSummary 集概要。
type DramaEpisodeSummary struct {
	No           int       `json:"no"`
	Title        string    `json:"title"`
	Synopsis     string    `json:"synopsis"`
	ScriptStatus string    `json:"script_status"`
	Locked       bool      `json:"locked"`
	Revision     int64     `json:"revision"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// DramaEpisodeView 单集正文。
type DramaEpisodeView struct {
	DramaEpisodeSummary
	ScriptMD string `json:"script_md"`
}

// DramaJobView 任务状态与结算结果。
type DramaJobView struct {
	ID           idcodec.ID `json:"id"`
	Step         string     `json:"step"`
	Episodes     []int      `json:"episodes"`
	Status       string     `json:"status"`
	Progress     int        `json:"progress"`
	Credits      int        `json:"credits"`
	ErrorCode    int        `json:"error_code"`
	ErrorMessage string     `json:"error_message"`
	CreatedAt    time.Time  `json:"created_at"`
	FinishedAt   *time.Time `json:"finished_at"`
}

// DramaView 项目详情。
type DramaView struct {
	ID             idcodec.ID            `json:"id"`
	Title          string                `json:"title"`
	CanvasID       idcodec.ID            `json:"canvas_id"`
	Status         string                `json:"status"`
	SourceKind     string                `json:"source_kind"`
	Genre          string                `json:"genre"`
	StyleKey       string                `json:"style_key"`
	StyleCustom    string                `json:"style_custom"`
	Ratio          string                `json:"ratio"`
	TargetEpisodes int                   `json:"target_episodes"`
	EpisodeSeconds int                   `json:"episode_seconds"`
	TextModel      string                `json:"text_model"`
	OutlineMD      string                `json:"outline_md"`
	CharactersMD   string                `json:"characters_md"`
	Revision       int64                 `json:"revision"`
	Episodes       []DramaEpisodeSummary `json:"episodes"`
	ActiveJobs     []DramaJobView        `json:"active_jobs"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
}
