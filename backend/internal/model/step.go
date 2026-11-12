package model

import (
	"time"

	"gorm.io/datatypes"
)

// 环节协议层（stepkit）的审计动作和目标类型。
const (
	AdminAuditStepTrial    = "step.trial"    // 发起试跑
	AdminAuditStepBinding  = "step.binding"  // 设为生效（改绑定）
	AdminAuditStepFallback = "step.fallback" // 绑定的技能被停用或删除，环节自动退回内置（操作人为 0）

	AdminAuditTargetStep = "step"
)

// 试跑状态。failed 表示调用本身出错；输出不合格是 succeeded + passed=false。
const (
	StepTrialQueued    = "queued"
	StepTrialRunning   = "running"
	StepTrialSucceeded = "succeeded"
	StepTrialFailed    = "failed"
)

// StepBinding 是环节当前的方法绑定，每个环节最多一行；没有记录等于内置。
// 不建 skill_id 外键：技能被整个删除时绑定留着，运行时按「技能已删除」退回内置。
type StepBinding struct {
	StepKey      string    `gorm:"primaryKey;type:text" json:"step_key"` // 如 marketing.reverse
	Source       string    `gorm:"type:text;not null" json:"source"`     // builtin / skill
	SkillID      *uint64   `json:"skill_id"`                             // source=skill 时的技能 id
	SkillVersion *int      `json:"skill_version"`                        // 钉死的版本号
	TrialID      *uint64   `json:"trial_id"`                             // 生效依据的试跑
	UpdatedBy    uint64    `gorm:"not null;default:0" json:"updated_by"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName 返回表名。
func (StepBinding) TableName() string { return "step_bindings" }

// StepBindingHistory 是绑定的修改历史，只增不改。from / to 是 StepMethodRef 的 JSON。
type StepBindingHistory struct {
	ID          uint64         `gorm:"primaryKey" json:"id"`
	StepKey     string         `gorm:"type:text;not null;index:idx_step_binding_history_step,priority:1" json:"step_key"`
	FromBinding datatypes.JSON `gorm:"type:jsonb;not null" json:"from"`
	ToBinding   datatypes.JSON `gorm:"type:jsonb;not null" json:"to"`
	TrialID     *uint64        `json:"trial_id"`
	ActorID     uint64         `gorm:"not null;default:0" json:"actor_id"`
	CreatedAt   time.Time      `gorm:"index:idx_step_binding_history_step,priority:2,sort:desc" json:"created_at"`
}

// TableName 返回表名。
func (StepBindingHistory) TableName() string { return "step_binding_history" }

// StepTrial 是一次试跑：用某个候选方法在某个样例上跑一遍环节，看格式校验能否通过。
// 对比试跑时同批两条互指 compare_of。frozen 是冻结在操作管理员账户上的积分，结束时按 credits 结算。
type StepTrial struct {
	ID           uint64         `gorm:"primaryKey" json:"id"`
	StepKey      string         `gorm:"type:text;not null;index:idx_step_trials_step_created,priority:1" json:"step_key"`
	Source       string         `gorm:"type:text;not null" json:"source"` // 候选来源 builtin / skill
	SkillID      *uint64        `json:"skill_id"`
	SkillVersion *int           `json:"skill_version"`
	SampleKey    string         `gorm:"type:text;not null" json:"sample_key"`
	TextModelID  string         `gorm:"type:text;not null" json:"text_model_id"` // 文本模型 key
	CompareOf    *uint64        `json:"compare_of"`
	Status       string         `gorm:"type:text;not null;default:queued" json:"status"`
	Passed       bool           `gorm:"not null;default:false" json:"passed"` // 格式校验是否通过
	Errors       datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"errors"`
	Flags        datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"flags"`
	Output       datatypes.JSON `gorm:"type:jsonb" json:"output"`
	Method       datatypes.JSON `gorm:"type:jsonb" json:"method"`                   // 运行时的方法快照
	Error        string         `gorm:"type:text;not null;default:''" json:"error"` // 调用失败的原因（给管理员看）
	Attempts     int            `gorm:"not null;default:0" json:"attempts"`
	DurationMS   int            `gorm:"not null;default:0" json:"duration_ms"`
	InputTokens  int            `gorm:"not null;default:0" json:"input_tokens"`
	OutputTokens int            `gorm:"not null;default:0" json:"output_tokens"`
	Credits      int            `gorm:"not null;default:0" json:"credits"` // 实际扣的积分
	Frozen       int            `gorm:"not null;default:0" json:"frozen"`  // 提交时冻结的积分
	CreatedBy    uint64         `gorm:"not null" json:"created_by"`
	CreatedAt    time.Time      `gorm:"index:idx_step_trials_step_created,priority:2,sort:desc" json:"created_at"`
	FinishedAt   *time.Time     `json:"finished_at"`
}

// TableName 返回表名。
func (StepTrial) TableName() string { return "step_trials" }

// StepMethodRef 是绑定历史里 from / to 的存储形状：只记 id 和版本，显示名查询时再取。
type StepMethodRef struct {
	Source       string `json:"source"`
	SkillID      uint64 `json:"skill_id,omitempty"`
	SkillName    string `json:"skill_name,omitempty"`
	SkillVersion int    `json:"version,omitempty"`
}

// StepCandidateReq 是请求里的候选方法：内置，或技能名加版本（版本省略取技能当前生效版本）。
type StepCandidateReq struct {
	Source    string `json:"source" binding:"required,oneof=builtin skill" label:"来源"`
	SkillName string `json:"skill_name" binding:"required_if=Source skill,max=64" label:"技能名"`
	Version   int    `json:"version" binding:"omitempty,min=1" label:"版本"`
}

// CreateStepTrialReq 是发起试跑的请求。
type CreateStepTrialReq struct {
	Candidate StepCandidateReq `json:"candidate" binding:"required" label:"候选"`
	SampleKey string           `json:"sample_key" binding:"required,max=64" label:"样例"`
	TextModel string           `json:"text_model" binding:"required,max=128" label:"文本模型"`
	Compare   bool             `json:"compare" label:"对比当前绑定"`
}

// SetStepBindingReq 是「设为生效」的请求：候选加依据的试跑 id。
type SetStepBindingReq struct {
	Source    string `json:"source" binding:"required,oneof=builtin skill" label:"来源"`
	SkillName string `json:"skill_name" binding:"required_if=Source skill,max=64" label:"技能名"`
	Version   int    `json:"version" binding:"omitempty,min=1" label:"版本"`
	TrialID   string `json:"trial_id" binding:"required" label:"试跑"`
}

// StepMethodView 是接口里的一个方法：绑定、生效方法、历史的 from / to 都用它。
type StepMethodView struct {
	Source         string `json:"source"`
	SkillName      string `json:"skill_name,omitempty"`
	SkillTitle     string `json:"skill_title,omitempty"`
	Version        int    `json:"version,omitempty"`
	Fallback       bool   `json:"fallback,omitempty"`        // 绑定的技能不可用，已退回内置
	FallbackReason string `json:"fallback_reason,omitempty"` // 退回原因
}

// StepLastTrial 是环节最近一次试跑的摘要。
type StepLastTrial struct {
	ID        string    `json:"id"`
	Passed    bool      `json:"passed"`
	CreatedAt time.Time `json:"created_at"`
}

// StepSummary 是环节列表的一行。
type StepSummary struct {
	Key          string         `json:"key"`
	Title        string         `json:"title"`
	Studio       string         `json:"studio"`
	Media        string         `json:"media"`
	Binding      StepMethodView `json:"binding"`
	Effective    StepMethodView `json:"effective"`
	NewerVersion *int           `json:"newer_version"` // 绑定技能有更新的生效版本时给出
	LastTrial    *StepLastTrial `json:"last_trial"`
	FailRate7d   float64        `json:"fail_rate_7d"`
}

// StepInputVar 是协议详情里的一个输入变量。
type StepInputVar struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Desc string `json:"desc"`
}

// StepRulesView 是协议详情里的校验规则说明。
type StepRulesView struct {
	Hard []string `json:"hard"`
	Soft []string `json:"soft"`
}

// StepSampleView 是协议详情里的试跑样例。
type StepSampleView struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	NeedsRef bool   `json:"needs_ref"`
}

// StepHistoryView 是绑定历史的一条。
type StepHistoryView struct {
	ID        string         `json:"id"`
	From      StepMethodView `json:"from"`
	To        StepMethodView `json:"to"`
	Actor     string         `json:"actor"` // 操作人用户名
	TrialID   *string        `json:"trial_id"`
	CreatedAt time.Time      `json:"created_at"`
}

// StepCandidateView 是后台候选下拉的一项：内置方法（只有 source），或一个导入技能。
type StepCandidateView struct {
	Source        string `json:"source"`
	SkillName     string `json:"skill_name,omitempty"`
	SkillTitle    string `json:"skill_title,omitempty"`
	Versions      []int  `json:"versions,omitempty"`
	ActiveVersion int    `json:"active_version,omitempty"`
	HasScripts    *bool  `json:"has_scripts,omitempty"`
	Chars         *int   `json:"chars,omitempty"` // 生效版本 SKILL.md 正文字数
	Enabled       *bool  `json:"enabled,omitempty"`
}

// StepDetail 是环节详情：摘要加协议、样例、历史和候选。
type StepDetail struct {
	StepSummary
	Duty       string              `json:"duty"`
	Inputs     []StepInputVar      `json:"inputs"`
	Schema     string              `json:"schema"` // JSON 字符串
	Rules      StepRulesView       `json:"rules"`
	Samples    []StepSampleView    `json:"samples"`
	History    []StepHistoryView   `json:"history"`
	Candidates []StepCandidateView `json:"candidates"`
}

// StepTrialCandidate 是试跑视图里的候选。
type StepTrialCandidate struct {
	Source    string `json:"source"`
	SkillName string `json:"skill_name,omitempty"`
	Version   int    `json:"version,omitempty"`
}

// StepTrialView 是试跑结果。output 是原始输出 JSON，解析失败时为 null。
type StepTrialView struct {
	ID           string             `json:"id"`
	StepKey      string             `json:"step_key"`
	Candidate    StepTrialCandidate `json:"candidate"`
	SampleKey    string             `json:"sample_key"`
	TextModel    string             `json:"text_model"`
	CompareOf    *string            `json:"compare_of"`
	Status       string             `json:"status"`
	Passed       bool               `json:"passed"`
	Errors       []string           `json:"errors"`
	Flags        datatypes.JSON     `json:"flags"`
	Preview      string             `json:"preview"`
	Output       datatypes.JSON     `json:"output"`
	Error        string             `json:"error,omitempty"` // status=failed 时的调用失败原因
	DurationMS   int                `json:"duration_ms"`
	InputTokens  int                `json:"input_tokens"`
	OutputTokens int                `json:"output_tokens"`
	Credits      int                `json:"credits"`
	CreatedAt    time.Time          `json:"created_at"`
}

// StepTrialList 是发起试跑的返回：1 条，对比时 2 条。
type StepTrialList struct {
	Trials []StepTrialView `json:"trials"`
}
