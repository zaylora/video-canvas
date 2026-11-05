package model

import (
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/pkg/idcodec" //nolint:depguard // 沿用短剧对外 ID。
)

// DramaSourceDraft 是仅本人可用、有效期二十四小时的预检草稿。
type DramaSourceDraft struct {
	ID          string         `gorm:"primaryKey;size:36"`
	UserID      uint64         `gorm:"not null;index"`
	Filename    string         `gorm:"size:255;not null"`
	Text        string         `gorm:"type:text;not null"`
	UnitsJSON   datatypes.JSON `gorm:"type:jsonb;not null"`
	KindGuess   string         `gorm:"size:16;not null"`
	SignalsJSON datatypes.JSON `gorm:"type:jsonb;not null"`
	Chars       int            `gorm:"not null"`
	SHA256      string         `gorm:"size:64;not null"`
	ExpiresAt   time.Time      `gorm:"not null;index"`
	CreatedAt   time.Time
}

// TableName 返回草稿表名。
func (DramaSourceDraft) TableName() string { return "drama_source_drafts" }

// DramaUnit 按用户和短剧隔离保存完整原文单元。
type DramaUnit struct {
	ID      uint64 `gorm:"primaryKey" json:"-"`
	UserID  uint64 `gorm:"not null;index" json:"-"`
	DramaID uint64 `gorm:"not null;uniqueIndex:uk_drama_unit_no,priority:1" json:"-"`
	No      int    `gorm:"not null;uniqueIndex:uk_drama_unit_no,priority:2" json:"no"`
	Kind    string `gorm:"size:16;not null" json:"-"`
	Title   string `gorm:"size:255;not null" json:"title"`
	Text    string `gorm:"type:text;not null" json:"text,omitempty"`
	Chars   int    `gorm:"not null" json:"chars"`
}

// TableName 返回原文表名。
func (DramaUnit) TableName() string { return "drama_units" }

// DramaPrompt 是用户可复用的正文，任务使用独立快照。
type DramaPrompt struct {
	ID        uint64         `gorm:"primaryKey"`
	UserID    uint64         `gorm:"not null;uniqueIndex:uk_drama_prompt_name,priority:1"`
	Step      string         `gorm:"size:16;not null;uniqueIndex:uk_drama_prompt_name,priority:2"`
	Name      string         `gorm:"size:40;not null;uniqueIndex:uk_drama_prompt_name,priority:3"`
	Bodies    datatypes.JSON `gorm:"type:jsonb;not null"`
	Revision  int64          `gorm:"not null;default:1"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName 返回个人提示词表名。
func (DramaPrompt) TableName() string { return "drama_prompts" }

// DramaPromptChoice 是四环节之一的来源选择。
type DramaPromptChoice struct {
	Source string     `json:"source" binding:"required,oneof=builtin mine skill"`
	ID     idcodec.ID `json:"id"`
	Skill  string     `json:"skill"`
}

// DramaPromptSetting 是项目内持久正文快照；对外视图不包含正文。
type DramaPromptSetting struct {
	Source  string            `json:"source"`
	ID      idcodec.ID        `json:"id,omitempty"`
	Skill   string            `json:"skill,omitempty"`
	Label   string            `json:"label"`
	Version int               `json:"version,omitempty"`
	Bodies  map[string]string `json:"bodies,omitempty"`
	Missing bool              `json:"missing"`
}

// DramaSourceEstimateReq 在草稿基础上重算来源类型、范围与模型报价。
type DramaSourceEstimateReq struct {
	SourceKind     string `json:"source_kind" binding:"required,oneof=script novel"`
	RangeEnd       int    `json:"range_end" binding:"min=0"`
	EpisodeSeconds int    `json:"episode_seconds" binding:"omitempty,min=30,max=180"`
	TextModel      string `json:"text_model" binding:"max=128"`
}

// DramaSourceEstimate 是自动研读与规划的集数、积分、分钟预估。
type DramaSourceEstimate struct {
	EpisodesMin   int  `json:"episodes_min"`
	EpisodesMax   int  `json:"episodes_max"`
	EpisodesExact bool `json:"episodes_exact"`
	Credits       int  `json:"credits"`
	Minutes       int  `json:"minutes"`
}

// SaveDramaPromptReq 创建或覆盖个人提示词，更新须携带 revision。
type SaveDramaPromptReq struct {
	Step     string            `json:"step" binding:"required,oneof=adapt assets asset_prompts video"`
	Name     string            `json:"name"`
	Bodies   map[string]string `json:"bodies"`
	Revision int64             `json:"revision"`
}

// DramaPromptView 返回个人提示词正文和版本。
type DramaPromptView struct {
	ID        idcodec.ID        `json:"id"`
	Step      string            `json:"step"`
	Name      string            `json:"name"`
	Bodies    map[string]string `json:"bodies"`
	Revision  int64             `json:"revision"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// DramaPlanningReq 是范围变更和恢复内置的请求。
type DramaPlanningReq struct {
	RangeEnd int    `json:"range_end" binding:"min=0"`
	Step     string `json:"step" binding:"omitempty,oneof=adapt assets asset_prompts video"`
}

// DramaSkillView 是短剧可选择的技能目录项。
type DramaSkillView struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	HasScripts  bool   `json:"has_scripts"`
}
