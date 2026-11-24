package model

import (
	"time"

	"gorm.io/datatypes"
)

// CanvasComposeJob 是一次「画布成片合成」的异步任务：把一组已生成的视频按顺序拼成一个 mp4。
//
// 顺序不持久化在画布上：提交时前端把有序的 assetIds 放进 InputJSON 冻结，之后改顺序就是再提交一次。
// 这样画布图结构和存档格式都不用动，也不需要引入「时间线」「序列」这类新概念。
type CanvasComposeJob struct {
	ID            uint64         `gorm:"primaryKey" json:"id"`
	UserID        uint64         `gorm:"not null;index:idx_canvas_compose_user" json:"user_id"`
	CanvasID      uint64         `gorm:"not null;uniqueIndex:uk_canvas_compose_active,where:status IN ('queued'\\,'running')" json:"canvas_id"`
	NodeID        string         `gorm:"size:64;not null;uniqueIndex:uk_canvas_compose_active,where:status IN ('queued'\\,'running')" json:"node_id"`
	Mode          string         `gorm:"size:16;not null;default:''" json:"mode"` // storyboard / selection：只用于区分触发方式，不影响执行
	Status        string         `gorm:"size:16;not null;default:queued" json:"status"`
	InputJSON     datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"-"`
	Fingerprint   string         `gorm:"size:64;not null;default:''" json:"fingerprint"`
	ResultAssetID *uint64        `json:"result_asset_id,omitempty"`
	DurationMS    int64          `gorm:"not null;default:0" json:"duration_ms"`
	SegmentCount  int            `gorm:"not null;default:0" json:"segment_count"`
	Error         string         `gorm:"type:text;not null;default:''" json:"error,omitempty"`
	Attempts      int            `gorm:"not null;default:0" json:"-"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// TableName 指定表名。
func (CanvasComposeJob) TableName() string { return "canvas_compose_jobs" }

// CanvasComposeJob 的状态。全链路一律用 queued，不沿用拆分镜范本的 pending：
// 部分唯一索引与条件更新都以这里为准，两套命名混用会让唯一性失效。
const (
	ComposeJobQueued    = "queued"
	ComposeJobRunning   = "running"
	ComposeJobSucceeded = "succeeded"
	ComposeJobFailed    = "failed"
	ComposeJobCanceled  = "canceled"
)

// CanvasComposeInput 是冻结在任务行里的执行输入：有序的素材 id 与来源镜头序号。
type CanvasComposeInput struct {
	AssetIDs []uint64 `json:"asset_ids"`
	// Seqs 是这些素材对应的镜头序号（从 0 起），只用于展示「第几镜」，不参与执行。
	Seqs []int `json:"seqs,omitempty"`
}

// CanvasComposeReq 是提交合成的请求体：nodeId 决定成片挂在哪、也用于同节点去重。
// assetIds 的顺序就是成片顺序，服务端不再猜测。
type CanvasComposeReq struct {
	NodeID   string   `json:"node_id" binding:"required,max=64" label:"节点"`
	AssetIDs []string `json:"asset_ids" binding:"required,min=1,max=50,dive,max=32" label:"素材"`
	Seqs     []int    `json:"seqs" binding:"omitempty,max=50" label:"镜头序号"`
	Mode     string   `json:"mode" binding:"omitempty,oneof=storyboard compose selection" label:"来源"`
}

// CanvasComposeResp 是任务摘要：接口返回与 WebSocket 推送共用同一份结构，前端不用分两套解析。
type CanvasComposeResp struct {
	ID            string `json:"id"`
	CanvasID      string `json:"canvas_id"`
	NodeID        string `json:"node_id"`
	Mode          string `json:"mode,omitempty"`
	Status        string `json:"status"`
	Fingerprint   string `json:"fingerprint"`
	ResultAssetID string `json:"result_asset_id,omitempty"`
	// ResultAssetURL 是成片的稳定访问地址（/files/...），前端直接拿来播放和下载，不用自己拼。
	ResultAssetURL string `json:"result_asset_url,omitempty"`
	DurationMS     int64  `json:"duration_ms"`
	SegmentCount   int    `json:"segment_count"`
	Error          string `json:"error,omitempty"`
	// Stale 只在 active 查询里出现：当前输入与这条任务的输入不一致，成片已经过期。
	Stale *bool `json:"stale,omitempty"`
}
