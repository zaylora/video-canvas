package model

import (
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/pkg/idcodec" //nolint:depguard // 对外 id 用十六进制串，与短剧、画布一致
)

// 营销轮次状态。
const (
	MarketingRoundScripting = "scripting" // 写脚本中
	MarketingRoundReady     = "ready"     // 至少有一份脚本可用
	MarketingRoundFailed    = "failed"    // 全部失败
	MarketingRoundCanceled  = "canceled"  // 用户取消
)

// 营销环节运行状态。
const (
	MarketingRunQueued    = "queued"
	MarketingRunRunning   = "running"
	MarketingRunSucceeded = "succeeded"
	MarketingRunFailed    = "failed"
	MarketingRunSkipped   = "skipped"  // 依赖的环节失败，本环节不跑
	MarketingRunCanceled  = "canceled" // 轮次被取消
	MarketingRunReused    = "reused"   // 事实卡沿用项目已有的
)

// 营销脚本状态。
const (
	MarketingScriptDraft     = "draft"
	MarketingScriptRewriting = "rewriting" // 重写跟拍中
	MarketingScriptRendering = "rendering"
	MarketingScriptDone      = "done"
	MarketingScriptFailed    = "failed"
)

// MarketingNodePrefix 是营销脚本出片任务的节点前缀（node_id = mkt:{脚本十六进制 id}）。
const MarketingNodePrefix = "mkt:"

// MarketingProject 是营销项目：一个商品一个项目，可以多轮写脚本。
type MarketingProject struct {
	BaseModel
	UpdatedAt       time.Time      `gorm:"index:idx_mkt_project_user_updated,priority:2,sort:desc"`
	UserID          uint64         `gorm:"not null;index:idx_mkt_project_user_updated,priority:1"`
	Title           string         `gorm:"size:255;not null;default:''"`
	Product         datatypes.JSON `gorm:"type:jsonb"` // 事实卡，第一轮事实卡环节成功后才有
	ProductAssetIDs datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	ModelAssetID    *uint64        // 模特图
	CoverAssetID    *uint64        // 列表封面：最新成片的首帧
	Revision        int64          `gorm:"not null;default:1"`
}

// TableName 返回表名。
func (MarketingProject) TableName() string { return "marketing_projects" }

// MarketingRound 是一轮写脚本：设置快照、拆解、环节与脚本的归属。
type MarketingRound struct {
	ID              uint64         `gorm:"primaryKey"`
	UserID          uint64         `gorm:"not null;index"`
	ProjectID       uint64         `gorm:"not null;uniqueIndex:uk_mkt_round_project_no,priority:1"`
	No              int            `gorm:"not null;uniqueIndex:uk_mkt_round_project_no,priority:2"`
	Status          string         `gorm:"size:16;not null;default:scripting"`
	Error           string         `gorm:"type:text;not null;default:''"`
	Warnings        datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	Settings        datatypes.JSON `gorm:"type:jsonb;not null"`
	RefVideoAssetID *uint64
	ModelAssetID    *uint64 // 本轮生效的模特图（新一轮没传时沿用项目已有的）
	PlanCount       int     `gorm:"not null;default:0"`
	Frozen          int     `gorm:"not null;default:0"`
	Charged         int     `gorm:"not null;default:0"`
	// Reverse 是当前拆解（可能被用户改过），ReverseOriginal 是模型原稿，FollowBasis 是当前跟拍据以写成的拆解快照。
	Reverse         datatypes.JSON `gorm:"type:jsonb"`
	ReverseOriginal datatypes.JSON `gorm:"type:jsonb"`
	ReverseRevision int            `gorm:"not null;default:0"`
	FollowBasis     datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TableName 返回表名。
func (MarketingRound) TableName() string { return "marketing_rounds" }

// MarketingStepRun 是一个环节的一次运行；重试新建一条，视图只取每个环节最新的一条。
type MarketingStepRun struct {
	ID          uint64         `gorm:"primaryKey"`
	UserID      uint64         `gorm:"not null"`
	RoundID     uint64         `gorm:"not null;index:idx_mkt_run_round_step,priority:1"`
	StepKey     string         `gorm:"type:text;not null;index:idx_mkt_run_round_step,priority:2;index:idx_mkt_run_step_created,priority:1"` // 如 marketing.reverse
	Method      datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`                                                                     // 方法快照 stepkit.MethodSnapshot
	TextModelID string         `gorm:"type:text;not null;default:''"`                                                                        // 实际用的模型（含能力回退）
	Status      string         `gorm:"size:16;not null;default:queued"`
	Error       string         `gorm:"type:text;not null;default:''"`
	Attempts    int            `gorm:"not null;default:0"`
	Frozen      int            `gorm:"not null;default:0"`
	Charged     int            `gorm:"not null;default:0"`
	StartedAt   *time.Time
	FinishedAt  *time.Time
	CreatedAt   time.Time `gorm:"index:idx_mkt_run_round_step,priority:3;index:idx_mkt_run_step_created,priority:2"`
}

// TableName 返回表名。
func (MarketingStepRun) TableName() string { return "marketing_step_runs" }

// MarketingScript 是一份脚本。Shots / Original 的结构见 marketing.ScriptShot / marketing.Script。
type MarketingScript struct {
	ID        uint64         `gorm:"primaryKey"`
	UserID    uint64         `gorm:"not null;index"`
	ProjectID uint64         `gorm:"not null;index;index:idx_mkt_script_project_created,priority:1"`
	RoundID   uint64         `gorm:"not null;index"`
	StepRunID uint64         `gorm:"not null"`
	Kind      string         `gorm:"size:16;not null"` // follow / create
	Angle     string         `gorm:"size:16;not null"`
	Seq       int            `gorm:"not null;default:0"` // 轮次内的展示顺序，跟拍在前
	Title     string         `gorm:"type:text;not null;default:''"`
	Hook      string         `gorm:"type:text;not null;default:''"`
	Shots     datatypes.JSON `gorm:"type:jsonb;not null"`
	Music     string         `gorm:"type:text;not null;default:''"`
	Voice     string         `gorm:"type:text;not null;default:''"`
	Flags     datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	FollowMap datatypes.JSON `gorm:"type:jsonb"`
	Original  datatypes.JSON `gorm:"type:jsonb;not null"` // 模型原稿，恢复原稿用
	Status    string         `gorm:"size:16;not null;default:draft"`
	TaskIDs   datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"` // 出片任务 id，按时间正序
	Error     string         `gorm:"type:text;not null;default:''"`    // 最近一次出片失败原因
	Revision  int64          `gorm:"not null;default:1"`
	CreatedAt time.Time      `gorm:"index:idx_mkt_script_project_created,priority:2,sort:desc"`
	UpdatedAt time.Time
}

// TableName 返回表名。
func (MarketingScript) TableName() string { return "marketing_scripts" }

// ---- 请求 ----

// MarketingParams 是轮次设置里的视频参数：键名与 POST /generation-tasks 的 input 一致，取值来自模型能力。
type MarketingParams = map[string]any

// MarketingSettings 是轮次设置（请求和视图同形）。
type MarketingSettings struct {
	Platform    string          `json:"platform" binding:"required,oneof=douyin xhs tmall tiktok amazon reels generic" label:"投放平台"`
	Language    string          `json:"language" binding:"required,oneof=zh en" label:"旁白语言"`
	Angles      []string        `json:"angles" binding:"omitempty,max=4,dive,oneof=pain feature lifestyle compare unbox mood" label:"创意角度"`
	AutoCount   int             `json:"auto_count" binding:"min=0,max=4" label:"自动份数"`
	VideoModel  string          `json:"video_model" binding:"required,max=128" label:"视频模型"`
	TextModel   string          `json:"text_model" binding:"max=128" label:"文本模型"`
	Params      MarketingParams `json:"params"`
	Description string          `json:"description" binding:"max=1000" label:"描述"`
}

// CreateMarketingProjectReq 创建营销项目（同时开第一轮）。
type CreateMarketingProjectReq struct {
	ProductAssetIDs []uint64          `json:"product_asset_ids" binding:"required,min=1,max=4,dive,min=1" label:"商品图"`
	ModelAssetID    *uint64           `json:"model_asset_id" binding:"omitempty,min=1" label:"模特图"`
	RefVideoAssetID *uint64           `json:"ref_video_asset_id" binding:"omitempty,min=1" label:"参考视频"`
	Settings        MarketingSettings `json:"settings" binding:"required" label:"设置"`
}

// CreateMarketingRoundReq 新一轮。
type CreateMarketingRoundReq struct {
	ModelAssetID    *uint64           `json:"model_asset_id" binding:"omitempty,min=1" label:"模特图"`
	RefVideoAssetID *uint64           `json:"ref_video_asset_id" binding:"omitempty,min=1" label:"参考视频"`
	Settings        MarketingSettings `json:"settings" binding:"required" label:"设置"`
}

// UpdateMarketingProjectReq 改标题、事实卡或商品图，带 revision。
type UpdateMarketingProjectReq struct {
	Title           *string         `json:"title" binding:"omitempty,max=255" label:"标题"`
	Product         *MarketingFacts `json:"product" label:"事实卡"`
	ProductAssetIDs []uint64        `json:"product_asset_ids" binding:"omitempty,min=1,max=4,dive,min=1" label:"商品图"`
	Revision        int64           `json:"revision" binding:"min=1" label:"版本"`
}

// UpdateMarketingRoundSettingsReq 改一轮的出片设置：视频模型与参数（画幅、时长不能变）、换模特图（不能加也不能去掉）。
type UpdateMarketingRoundSettingsReq struct {
	VideoModel   string          `json:"video_model" binding:"required,max=128" label:"视频模型"`
	Params       MarketingParams `json:"params"`
	ModelAssetID *uint64         `json:"model_asset_id" binding:"omitempty,min=1" label:"模特图"`
}

// MarketingQuoteReq 写脚本预估。
type MarketingQuoteReq struct {
	ProjectID *idcodec.ID       `json:"project_id"`
	HasRef    bool              `json:"has_ref"`
	Settings  MarketingSettings `json:"settings" binding:"required" label:"设置"`
}

// UpdateMarketingReverseReq 改拆解：只认 shots 的 action / dialogue / product_role / key_colors。
type UpdateMarketingReverseReq struct {
	Reverse         MarketingReverse `json:"reverse" binding:"required" label:"拆解"`
	ReverseRevision int              `json:"reverse_revision" binding:"min=0" label:"拆解版本"`
}

// UpdateMarketingScriptReq 改脚本旁白（长度 = 镜头数）。
type UpdateMarketingScriptReq struct {
	Narrations []string `json:"narrations" binding:"required,max=64,dive,max=1000" label:"旁白"`
	Revision   int64    `json:"revision" binding:"min=1" label:"版本"`
}

// RenderMarketingScriptReq 单份出片。
type RenderMarketingScriptReq struct {
	IdempotencyKey string          `json:"idempotency_key" binding:"required,max=100" label:"幂等键"`
	VideoModel     string          `json:"video_model" binding:"max=128" label:"视频模型"`
	Params         MarketingParams `json:"params"`
}

// RenderMarketingRoundReq 全部出片。
type RenderMarketingRoundReq struct {
	IdempotencyKey string `json:"idempotency_key" binding:"required,max=100" label:"幂等键"`
}

// ---- 视图 ----

// MarketingSellingPoint 是事实卡的卖点。
type MarketingSellingPoint struct {
	Text   string `json:"text" binding:"required,max=200" label:"卖点"`
	Proof  string `json:"proof" binding:"max=300" label:"证明"`
	Source string `json:"source" binding:"required,oneof=description image" label:"来源"`
}

// MarketingFacts 是事实卡（ProductFacts）。
type MarketingFacts struct {
	Name          string                  `json:"name" binding:"required,max=120" label:"商品名"`
	Category      string                  `json:"category" binding:"max=60" label:"品类"`
	Anchors       []string                `json:"anchors" binding:"max=12,dive,max=200" label:"外观锚点"`
	SellingPoints []MarketingSellingPoint `json:"selling_points" binding:"max=8,dive" label:"卖点"`
	Prohibited    []string                `json:"prohibited" binding:"max=12,dive,max=200" label:"禁用说法"`
	Audience      string                  `json:"audience" binding:"max=120" label:"人群"`
	CTA           string                  `json:"cta" binding:"max=200" label:"行动引导"`
}

// MarketingReverseShot 是拆解的一个镜头。
type MarketingReverseShot struct {
	ID             int      `json:"id"`
	Start          float64  `json:"start"`
	End            float64  `json:"end"`
	IsOpening3s    bool     `json:"is_opening_3s"`
	ShotSize       string   `json:"shot_size"`
	Camera         string   `json:"camera"`
	Subject        string   `json:"subject"`
	Action         string   `json:"action"`
	Scene          string   `json:"scene"`
	Lighting       string   `json:"lighting"`
	Person         string   `json:"person"`
	ProductInFrame string   `json:"product_in_frame"`
	ProductRole    string   `json:"product_role"`
	Dialogue       string   `json:"dialogue"`
	OnscreenText   string   `json:"onscreen_text"`
	KeyColors      []string `json:"key_colors"`
}

// MarketingReverse 是拆解视图（与 marketing.Reverse 同形）。
type MarketingReverse struct {
	Overall struct {
		Style        string `json:"style"`
		NarrativeArc string `json:"narrative_arc"`
		WhyViral     string `json:"why_viral"`
		Transcript   string `json:"transcript"`
		Category     string `json:"category"`
	} `json:"overall"`
	Shots  []MarketingReverseShot `json:"shots"`
	Brands []string               `json:"brands"`
	Claims []string               `json:"claims"`
}

// MarketingAssetRef 是接口里的素材引用。
type MarketingAssetRef struct {
	ID  uint64 `json:"id"`
	URL string `json:"url"`
}

// MarketingRefVideo 是参考视频引用，duration 单位秒。
type MarketingRefVideo struct {
	ID       uint64  `json:"id"`
	URL      string  `json:"url"`
	Duration float64 `json:"duration"`
}

// MarketingMethodView 是环节方法的展示。
type MarketingMethodView struct {
	Source     string `json:"source"`
	SkillName  string `json:"skill_name,omitempty"`
	SkillTitle string `json:"skill_title,omitempty"`
	Version    int    `json:"version,omitempty"`
	Fallback   bool   `json:"fallback"`
}

// MarketingStepRunView 是一个环节最新一次运行。
type MarketingStepRunView struct {
	ID        idcodec.ID           `json:"id"`
	Step      string               `json:"step"`
	Status    string               `json:"status"`
	Method    *MarketingMethodView `json:"method"`
	TextModel string               `json:"text_model"`
	Error     string               `json:"error"`
	Attempts  int                  `json:"attempts"`
	Charged   int                  `json:"charged"`
}

// MarketingScriptShotView 是脚本镜头。
type MarketingScriptShotView struct {
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Purpose   string  `json:"purpose"`
	Shot      string  `json:"shot"`
	Camera    string  `json:"camera"`
	Scene     string  `json:"scene"`
	Narration string  `json:"narration"`
	RefShot   *int    `json:"ref_shot,omitempty"`
}

// MarketingFlagView 是脚本的软问题；shot 是从 0 起的镜头下标。
type MarketingFlagView struct {
	Shot int    `json:"shot"`
	Type string `json:"type"`
	Text string `json:"text"`
}

// MarketingTaskLite 是出片任务的精简视图。
type MarketingTaskLite struct {
	ID           uint64       `json:"id"`
	TaskRef      string       `json:"task_ref"`
	Status       string       `json:"status"`
	Progress     *int         `json:"progress"`
	Outputs      []TaskOutput `json:"outputs"`
	ErrorMessage string       `json:"error_message"`
}

// MarketingScriptView 是脚本视图。
type MarketingScriptView struct {
	ID            idcodec.ID                `json:"id"`
	RoundID       idcodec.ID                `json:"round_id"`
	Kind          string                    `json:"kind"`
	Angle         string                    `json:"angle"`
	Title         string                    `json:"title"`
	Hook          string                    `json:"hook"`
	Shots         []MarketingScriptShotView `json:"shots"`
	Music         string                    `json:"music"`
	Voice         string                    `json:"voice"`
	Flags         []MarketingFlagView       `json:"flags"`
	Status        string                    `json:"status"`
	Edited        bool                      `json:"edited"`
	Method        *MarketingMethodView      `json:"method"`
	Takes         []MarketingTaskLite       `json:"takes"`
	RenderCredits int                       `json:"render_credits"`
	Error         string                    `json:"error"`
	Revision      int64                     `json:"revision"`
}

// MarketingRoundView 是轮次视图。
type MarketingRoundView struct {
	ID              idcodec.ID             `json:"id"`
	No              int                    `json:"no"`
	Status          string                 `json:"status"`
	Stage           string                 `json:"stage"`
	Error           string                 `json:"error"`
	Warnings        []string               `json:"warnings"`
	Settings        MarketingSettings      `json:"settings"`
	RefVideo        *MarketingRefVideo     `json:"ref_video"`
	ModelAsset      *MarketingAssetRef     `json:"model_asset"`
	Steps           []MarketingStepRunView `json:"steps"`
	PlanCount       int                    `json:"plan_count"`
	Reverse         *MarketingReverse      `json:"reverse"`
	ReverseRevision int                    `json:"reverse_revision"`
	ReverseEdited   bool                   `json:"reverse_edited"`
	ReverseDirty    bool                   `json:"reverse_dirty"`
	Scripts         []MarketingScriptView  `json:"scripts"`
	Frozen          int                    `json:"frozen"`
	Charged         int                    `json:"charged"`
	CreatedAt       time.Time              `json:"created_at"`
}

// MarketingProjectView 是项目详情。
type MarketingProjectView struct {
	ID            idcodec.ID           `json:"id"`
	Title         string               `json:"title"`
	Product       *MarketingFacts      `json:"product"`
	ProductAssets []MarketingAssetRef  `json:"product_assets"`
	ModelAsset    *MarketingAssetRef   `json:"model_asset"`
	Revision      int64                `json:"revision"`
	Rounds        []MarketingRoundView `json:"rounds"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
}

// MarketingScriptCounts 是项目各状态的脚本数。
type MarketingScriptCounts struct {
	Draft     int `json:"draft"`
	Rendering int `json:"rendering"`
	Done      int `json:"done"`
	Failed    int `json:"failed"`
}

// MarketingProjectSummary 是列表项。
type MarketingProjectSummary struct {
	ID              idcodec.ID            `json:"id"`
	Title           string                `json:"title"`
	CoverURL        string                `json:"cover_url"`
	Scripting       bool                  `json:"scripting"`
	Rendering       int                   `json:"rendering"`
	LastRoundFailed bool                  `json:"last_round_failed"`
	ScriptCounts    MarketingScriptCounts `json:"script_counts"`
	Bars            []string              `json:"bars"`
	UpdatedAt       time.Time             `json:"updated_at"`
}

// MarketingOptions 是 GET /marketing/options 的响应。
type MarketingOptions struct {
	Platforms         []MarketingPlatformOption `json:"platforms"`
	Angles            []MarketingAngleOption    `json:"angles"`
	RefVideoMaxSecond int                       `json:"ref_video_max_seconds"`
}

// MarketingPlatformOption 是一个平台预设。
type MarketingPlatformOption struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	AspectRatio string `json:"aspect_ratio"`
	Language    string `json:"language"`
	Style       string `json:"style"`
}

// MarketingAngleOption 是一个创意角度。
type MarketingAngleOption struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// MarketingQuoteStep 是预估里一个环节的积分。
type MarketingQuoteStep struct {
	Step    string `json:"step"`
	Credits int    `json:"credits"`
}

// MarketingQuote 是写脚本预估。
type MarketingQuote struct {
	Credits int                  `json:"credits"`
	Steps   []MarketingQuoteStep `json:"steps"`
}

// MarketingPrompt 是脚本提示词预览。
type MarketingPrompt struct {
	Format string `json:"format"`
	Prompt string `json:"prompt"`
}

// MarketingSkipped 是全部出片时被跳过的脚本。
type MarketingSkipped struct {
	ScriptID idcodec.ID `json:"script_id"`
	Reason   string     `json:"reason"`
}

// MarketingRenderAll 是全部出片的响应。
type MarketingRenderAll struct {
	Round   *MarketingRoundView `json:"round"`
	Skipped []MarketingSkipped  `json:"skipped"`
}

// MarketingCanvasResult 是发到画布的响应。
type MarketingCanvasResult struct {
	CanvasID idcodec.ID `json:"canvas_id"`
}

// FormatterView 是后台「视频提示词格式化器」只读列表的一项。
type FormatterView struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Sample      string `json:"sample"`
}
