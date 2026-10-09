package model

import (
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/pkg/idcodec"
)

// 任务状态。终态：succeeded / failed / canceled / expired。
const (
	TaskPending    = "pending"    // 已入库，等待 worker 提交给平台
	TaskQueued     = "queued"     // 平台已受理，排队中
	TaskRunning    = "running"    // 平台生成中
	TaskFinalizing = "finalizing" // 平台已出片，正在转存到自有存储
	TaskSucceeded  = "succeeded"  // 成功
	TaskFailed     = "failed"     // 失败
	TaskCanceled   = "canceled"   // 已取消
	TaskExpired    = "expired"    // 超过截止时间
)

// 生成种类。
const (
	KindVideo = "video" // 视频
	KindImage = "image" // 图片
	KindAudio = "audio" // 音频
	KindText  = "text"  // 文本（同步出正文，没有素材转存）
)

// ActiveTaskStatuses 是所有非终态，用于并发统计、worker 调度和对账。
var ActiveTaskStatuses = []string{TaskPending, TaskQueued, TaskRunning, TaskFinalizing}

// IsTerminalStatus 判断状态是否为终态。
func IsTerminalStatus(s string) bool {
	switch s {
	case TaskSucceeded, TaskFailed, TaskCanceled, TaskExpired:
		return true
	}
	return false
}

// GenerationTask 通用生成任务，是任务状态的唯一事实来源（见 docs/design/长任务生成设计.md 7.1）。
// 不使用软删除：任务是审计和对账的依据，不允许被删掉。
type GenerationTask struct {
	ID              uint64         `gorm:"primaryKey" json:"id"`                                                                                                                                                      // 任务 ID
	UserID          uint64         `gorm:"not null;index:idx_task_user_status,priority:1;index:idx_task_user_created,priority:1;uniqueIndex:uk_task_user_idem,priority:1,where:idempotency_key <> ''" json:"user_id"` // 所属用户
	CanvasProjectID *uint64        `gorm:"index" json:"canvas_project_id"`                                                                                                                                            // 所属画布，可空
	NodeID          string         `gorm:"size:64;not null;default:''" json:"node_id"`                                                                                                                                // 前端节点 id
	Kind            string         `gorm:"size:16;not null" json:"kind"`                                                                                                                                              // video / image / audio / text
	ModelKey        string         `gorm:"column:model_id;size:128;not null" json:"model_id"`                                                                                                                         // 模型 key（ai_models.key）
	TaskRef         string         `gorm:"size:32;not null;default:'';uniqueIndex:uk_task_ref,where:task_ref <> ''" json:"task_ref"`                                                                                  // 任务编号：id 的十六进制编码，界面展示、日志 task_id 都是它；创建时写入，库里可按它直接查原始数据，唯一
	Provider        string         `gorm:"size:64;not null" json:"provider"`                                                                                                                                          // 平台 key
	ProviderTaskID  string         `gorm:"size:128;not null;default:'';index:idx_task_provider,priority:2" json:"-"`                                                                                                  // 上游任务 id，提交成功后才有
	Status          string         `gorm:"size:16;not null;index:idx_task_user_status,priority:2" json:"status"`                                                                                                      // 任务状态，取值见 Task* 常量
	Progress        *int           `json:"progress"`                                                                                                                                                                  // 0–100，平台不提供时为空
	InputJSON       datatypes.JSON `gorm:"type:jsonb;not null" json:"-"`                                                                                                                                              // 已校验的规范化输入
	OutputJSON      datatypes.JSON `gorm:"type:jsonb" json:"-"`                                                                                                                                                       // []TaskOutput
	ErrorCode       string         `gorm:"size:64;not null;default:''" json:"error_code"`                                                                                                                             // 统一错误码
	ErrorMessage    string         `gorm:"size:512;not null;default:''" json:"error_message"`                                                                                                                         // 给用户看的文案
	Credits         int            `gorm:"not null;default:0" json:"credits"`                                                                                                                                         // 冻结的积分（下单时按定价算出）
	ChargedCredits  *int           `json:"charged_credits"`                                                                                                                                                           // 成功时实际扣的积分；Token 计费按用量结算，可能小于冻结额，未结算为空
	Version         int64          `gorm:"not null;default:1" json:"version"`                                                                                                                                         // 每次状态变化 +1
	IdempotencyKey  string         `gorm:"size:128;not null;default:'';uniqueIndex:uk_task_user_idem,priority:2,where:idempotency_key <> ''" json:"-"`                                                                // 幂等键（Idempotency-Key 请求头），同一用户下唯一
	ConfigSnapshot  datatypes.JSON `gorm:"type:jsonb;not null" json:"-"`                                                                                                                                              // 创建时 model revision + 渠道配置 + 插件版本哈希的快照（provider.Snapshot，不含 Key）
	ProviderState   datatypes.JSON `gorm:"type:jsonb" json:"-"`                                                                                                                                                       // 插件私有状态（≤64KB），由宿主持久化后下次传回插件
	ProviderResult  datatypes.JSON `gorm:"type:jsonb" json:"-"`                                                                                                                                                       // 同步接口的即时结果（[]provider.Output），转存前落库，重启后继续转存而不重复调用上游
	TraceJSON       datatypes.JSON `gorm:"type:jsonb" json:"-"`                                                                                                                                                       // 仅 is_test：插件钩子与 HTTP 的执行追踪（已脱敏），管理端试跑面板展示
	IsTest          bool           `gorm:"not null;default:false" json:"is_test"`                                                                                                                                     // 运营试跑：不扣积分、不推送
	NextPollAt      time.Time      `gorm:"not null" json:"-"`                                                                                                                                                         // worker 调度依据（部分索引由迁移补建）
	PollAttempts    int            `gorm:"not null;default:0" json:"-"`                                                                                                                                               // 已轮询次数
	LeaseUntil      *time.Time     `json:"-"`                                                                                                                                                                         // worker 租约
	DeadlineAt      time.Time      `gorm:"not null" json:"deadline_at"`                                                                                                                                               // 超过则 expired
	SubmittedAt     *time.Time     `json:"submitted_at"`                                                                                                                                                              // 提交给平台的时间
	FinishedAt      *time.Time     `json:"finished_at"`                                                                                                                                                               // 进入终态的时间
	CreatedAt       time.Time      `gorm:"index:idx_task_user_created,priority:2;index:idx_task_created" json:"created_at"`                                                                                           // 创建时间（与 user_id 组成热力图 / 近 7 天统计用的索引；单列索引 idx_task_created 给后台总览的全站按时间聚合用）
	UpdatedAt       time.Time      `json:"updated_at"`                                                                                                                                                                // 更新时间
}

func (GenerationTask) TableName() string { return "generation_tasks" }

// TaskOutput 是 output_json 里的一项：已转存到自有存储的产物。
type TaskOutput struct {
	AssetID      uint64 `json:"asset_id,omitempty"`       // 素材 ID（text 没有）
	URL          string `json:"url,omitempty"`            // 访问地址（text 没有）
	MediaType    string `json:"media_type"`               // video / image / audio / text
	Text         string `json:"text,omitempty"`           // media_type=text 时的正文
	DurationMs   int64  `json:"duration_ms,omitempty"`    // 时长（毫秒）
	Width        int    `json:"width,omitempty"`          // 宽度（像素）
	Height       int    `json:"height,omitempty"`         // 高度（像素）
	CoverAssetID uint64 `json:"cover_asset_id,omitempty"` // 封面素材 ID
}

// GenerationTaskView 是返回给前端的任务快照（HTTP 响应与 WebSocket 推送共用），不含快照、输入等内部字段。
type GenerationTaskView struct {
	ID              uint64       `json:"id"`              // 任务 ID
	TaskRef         string       `json:"task_ref"`        // 任务编号：id 的十六进制编码，界面展示，日志里的 task_id 就是它，用来定位问题
	CanvasProjectID *idcodec.ID  `json:"canvas_id"`       // 所属画布（十六进制串）
	NodeID          string       `json:"node_id"`         // 前端节点 id
	Kind            string       `json:"kind"`            // video / image / audio / text
	ModelID         string       `json:"model_id"`        // 模型 key
	Status          string       `json:"status"`          // 任务状态
	Progress        *int         `json:"progress"`        // 0–100，平台不提供时为空
	Outputs         []TaskOutput `json:"outputs"`         // 已转存的产物列表
	ErrorCode       string       `json:"error_code"`      // 统一错误码
	ErrorMessage    string       `json:"error_message"`   // 给用户看的文案
	Credits         int          `json:"credits"`         // 冻结的积分
	ChargedCredits  *int         `json:"charged_credits"` // 实际扣的积分，未结算为 null
	Version         int64        `json:"version"`         // 状态版本号，前端据此丢弃过期推送
	DeadlineAt      time.Time    `json:"deadline_at"`     // 截止时间，超过则 expired
	CreatedAt       time.Time    `json:"created_at"`      // 创建时间
	SubmittedAt     *time.Time   `json:"submitted_at"`    // 提交给平台（开始调用上游）的时间；还在排队时为空
	FinishedAt      *time.Time   `json:"finished_at"`     // 进入终态的时间
}

// CreateGenerationTaskReq 提交任务。Idempotency-Key 走请求头，不在 body 里。
type CreateGenerationTaskReq struct {
	Kind     string         `json:"kind" binding:"required,oneof=video image audio text" label:"生成种类"` // video / image / audio / text
	ModelID  string         `json:"model_id" binding:"required,max=128" label:"模型"`                    // 模型 key
	CanvasID idcodec.ID     `json:"canvas_id" label:"画布"`                                              // 所属画布（十六进制串），可不传
	NodeID   string         `json:"node_id" binding:"max=64" label:"节点"`                               // 前端节点 id（只生成 1 个时可以只传它）
	NodeIDs  []string       `json:"node_ids" binding:"omitempty,max=8,dive,max=64" label:"节点"`         // 每个任务绑定的节点，长度等于生成数量；第 i 个任务绑定第 i 个节点
	Input    map[string]any `json:"input" binding:"required" label:"生成参数"`                             // 生成参数，按模型的 capabilities 校验
}

// CreateGenerationTaskResp 是提交的结果：按节点顺序逐项给出创建好的任务，或这个节点的错误。
type CreateGenerationTaskResp struct {
	Items []CreateTaskItem `json:"items"`
}

// CreateTaskItem 是一个节点的提交结果，task 与 error 二选一。
type CreateTaskItem struct {
	NodeID string              `json:"node_id"`
	Task   *GenerationTaskView `json:"task,omitempty"`
	Error  *TaskItemError      `json:"error,omitempty"`
}

// TaskItemError 是单个节点提交失败的原因，含义与整体请求的 HTTP 错误一致（402 积分不足、429 并发已满……）。
type TaskItemError struct {
	Status  int    `json:"status"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ListGenerationTaskReq 对账查询：ids（逗号分隔，最多 100 个）与 status=active 二选一。
type ListGenerationTaskReq struct {
	IDs    string `form:"ids" binding:"max=2048" label:"任务ID"`                // 任务 ID，逗号分隔
	Status string `form:"status" binding:"omitempty,oneof=active" label:"状态"` // 状态过滤，目前仅支持 active（非终态）
}
