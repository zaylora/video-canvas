package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// 任务模式：决定追加的系统提示词、默认挂载的技能和可用工具子集（见 docs/design/画布Agent助手设计）。
const (
	AgentModeAll        = "all"        // 全能创作
	AgentModeScript     = "script"     // 剧本创编
	AgentModeStoryboard = "storyboard" // 分镜搭建
	AgentModePrompt     = "prompt"     // 提示词优化
)

// ValidAgentMode 判断是不是认识的任务模式。
func ValidAgentMode(m string) bool {
	switch m {
	case AgentModeAll, AgentModeScript, AgentModeStoryboard, AgentModePrompt:
		return true
	}
	return false
}

// Agent 运行状态。活跃状态受「每个画布同时只有一个」的部分唯一索引约束。
const (
	RunQueued          = "queued"           // 已创建，等待 runtime 接手
	RunRunning         = "running"          // 运行中
	RunWaitingApproval = "waiting_approval" // 等用户批准生成或删除
	RunWaitingInput    = "waiting_input"    // 等用户回答提问
	RunSucceeded       = "succeeded"        // 完成
	RunFailed          = "failed"           // 失败
	RunCanceled        = "canceled"         // 用户停止
	RunBudgetExhausted = "budget_exhausted" // 本轮积分预算用尽，暂停
	RunStepLimit       = "step_limit"       // 达到步数上限，暂停
	RunTimeout         = "timeout"          // 超时
	RunInterrupted     = "interrupted"      // 服务重启或 runtime 崩溃，可以继续
	RunExpired         = "expired"          // 等待用户响应超过 24 小时
)

// ActiveRunStatuses 是占用画布的运行状态：同一个画布同时只能有一个。
var ActiveRunStatuses = []string{RunQueued, RunRunning, RunWaitingApproval, RunWaitingInput}

// ResumableRunStatuses 是可以点「继续」的状态。
var ResumableRunStatuses = []string{RunInterrupted, RunBudgetExhausted, RunStepLimit}

// IsActiveRun 判断运行是否仍占用画布。
func IsActiveRun(status string) bool {
	for _, s := range ActiveRunStatuses {
		if s == status {
			return true
		}
	}
	return false
}

// 审批种类。
const (
	ApprovalGenerate = "generate" // 发起生成
	ApprovalDelete   = "delete"   // 删除节点或连线
	ApprovalAsk      = "ask"      // 向用户提问（选项或选模型）
)

// 审批状态。
const (
	ApprovalPending  = "pending"            // 等待用户决定
	ApprovalApproved = "approved"           // 已批准（整体）
	ApprovalPartial  = "partially_approved" // 部分批准
	ApprovalRejected = "rejected"           // 已拒绝
	ApprovalExpired  = "expired"            // 超时或本轮被停止
	ApprovalExecuted = "executed"           // 已批准并执行完
	ApprovalFailed   = "failed"             // 已批准但执行失败
)

// 改动种类。
const (
	MutationApplyOps = "apply_ops" // 建改节点、连线、组
	MutationArrange  = "arrange"   // 排列
	MutationDelete   = "delete"    // 批准后的删除
	MutationBind     = "bind"      // 批准生成后，把任务绑定到节点
	MutationUndo     = "undo"      // 撤销本轮
)

// AgentSession 是画布上的一个 Agent 对话。会话不写进 payload_json，避免拖大画布保存。
type AgentSession struct {
	ID           uint64         `gorm:"primaryKey" json:"id"`                                                   // 会话 ID
	UserID       uint64         `gorm:"not null;index:idx_agent_sessions_canvas,priority:1" json:"user_id"`     // 所属用户
	CanvasID     uint64         `gorm:"not null;index:idx_agent_sessions_canvas,priority:2" json:"canvas_id"`   // 所属画布
	Title        string         `gorm:"size:100;not null;default:''" json:"title"`                              // 标题
	Mode         string         `gorm:"size:16;not null;default:'all'" json:"mode"`                             // 任务模式，按会话记忆
	ModelKey     string         `gorm:"size:128;not null;default:''" json:"model_key"`                          // Agent 大语言模型 key
	SessionJSONL string         `gorm:"column:session_jsonl;type:text;not null;default:''" json:"-"`            // pi 会话 JSONL（含压缩摘要），不含密钥
	LastSeq      int64          `gorm:"not null;default:0" json:"last_seq"`                                     // 最近一条事件的序号
	CreatedAt    time.Time      `json:"created_at"`                                                             // 创建时间
	UpdatedAt    time.Time      `gorm:"index:idx_agent_sessions_canvas,priority:3,sort:desc" json:"updated_at"` // 更新时间
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`                                                         // 软删除
}

// TableName 表名。
func (AgentSession) TableName() string { return "agent_sessions" }

// AgentRun 是用户发一条消息触发的一次运行。
type AgentRun struct {
	ID            uint64     `gorm:"primaryKey" json:"id"`                                                                                                                     // 运行 ID
	SessionID     uint64     `gorm:"not null;index" json:"session_id"`                                                                                                         // 所属会话
	CanvasID      uint64     `gorm:"not null;uniqueIndex:uk_agent_runs_active,where:status IN ('queued'\\,'running'\\,'waiting_approval'\\,'waiting_input')" json:"canvas_id"` // 所属画布；活跃运行在同一画布上唯一
	UserID        uint64     `gorm:"not null;index" json:"user_id"`                                                                                                            // 所属用户
	Status        string     `gorm:"size:20;not null" json:"status"`                                                                                                           // 运行状态，取值见 Run* 常量
	Mode          string     `gorm:"size:16;not null;default:'all'" json:"mode"`                                                                                               // 本轮的任务模式
	BudgetCredits int        `gorm:"not null;default:0" json:"budget_credits"`                                                                                                 // 本轮积分预算
	SpentCredits  int        `gorm:"not null;default:0" json:"spent_credits"`                                                                                                  // 已花积分（对话 token + 已批准的生成）
	Steps         int        `gorm:"not null;default:0" json:"steps"`                                                                                                          // 已执行的步数
	MaxSteps      int        `gorm:"not null;default:40" json:"max_steps"`                                                                                                     // 步数上限
	ErrorCode     string     `gorm:"size:64;not null;default:''" json:"error_code"`                                                                                            // 失败时的错误码
	ErrorMessage  string     `gorm:"size:512;not null;default:''" json:"error_message"`                                                                                        // 失败时给用户看的文案
	LeaseUntil    time.Time  `json:"lease_until"`                                                                                                                              // runtime 租约到期时间，过期未续约视为中断
	CreatedAt     time.Time  `json:"created_at"`                                                                                                                               // 创建时间
	UpdatedAt     time.Time  `json:"updated_at"`                                                                                                                               // 更新时间
	EndedAt       *time.Time `json:"ended_at"`                                                                                                                                 // 结束时间
}

// TableName 表名。
func (AgentRun) TableName() string { return "agent_runs" }

// AgentEvent 是会话里的一条事件，客户端按 seq 回放，断线重连后用 after=seq 对账。
type AgentEvent struct {
	ID          uint64         `gorm:"primaryKey" json:"id"`                                                  // 事件 ID
	SessionID   uint64         `gorm:"not null;uniqueIndex:uk_agent_events_seq,priority:1" json:"session_id"` // 所属会话
	RunID       uint64         `gorm:"not null;default:0" json:"run_id"`                                      // 所属运行，会话级事件为 0
	Seq         int64          `gorm:"not null;uniqueIndex:uk_agent_events_seq,priority:2" json:"seq"`        // 会话内的递增序号
	Type        string         `gorm:"size:32;not null" json:"type"`                                          // 事件类型，如 message.delta、tool.end
	PayloadJSON datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"payload"`                       // 事件内容
	CreatedAt   time.Time      `gorm:"index" json:"created_at"`                                               // 创建时间，按保留期清理
}

// TableName 表名。
func (AgentEvent) TableName() string { return "agent_events" }

// AgentMutation 是一次画布写入的记录，撤销本轮和前端三方合并都靠它。
type AgentMutation struct {
	ID             uint64         `gorm:"primaryKey" json:"id"`                                                 // 改动 ID
	RunID          uint64         `gorm:"not null;uniqueIndex:uk_agent_mutations_seq,priority:1" json:"run_id"` // 所属运行
	CanvasID       uint64         `gorm:"not null;index" json:"canvas_id"`                                      // 所属画布
	UserID         uint64         `gorm:"not null" json:"user_id"`                                              // 所属用户
	Seq            int            `gorm:"not null;uniqueIndex:uk_agent_mutations_seq,priority:2" json:"seq"`    // 运行内的序号
	ToolCallID     string         `gorm:"size:64;not null;default:''" json:"tool_call_id"`                      // 触发它的工具调用
	Kind           string         `gorm:"size:16;not null" json:"kind"`                                         // 改动种类，取值见 Mutation* 常量
	ChangesJSON    datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"changes"`                      // canvasgraph.Change 列表
	RevisionBefore uint64         `gorm:"not null" json:"revision_before"`                                      // 写入前的画布 revision
	RevisionAfter  uint64         `gorm:"not null" json:"revision_after"`                                       // 写入后的画布 revision
	UndoneAt       *time.Time     `json:"undone_at"`                                                            // 被撤销的时间
	CreatedAt      time.Time      `json:"created_at"`                                                           // 创建时间
}

// TableName 表名。
func (AgentMutation) TableName() string { return "agent_mutations" }

// AgentApproval 是需要用户决定的事：批准生成、批准删除，或回答提问。
// 它的 id 同时作为生成任务的幂等键来源，崩溃续跑时不会重复扣费。
type AgentApproval struct {
	ID           uint64         `gorm:"primaryKey" json:"id"`                                                    // 审批 ID
	RunID        uint64         `gorm:"not null;index:idx_agent_approvals_run,priority:1" json:"run_id"`         // 所属运行
	SessionID    uint64         `gorm:"not null" json:"session_id"`                                              // 所属会话
	CanvasID     uint64         `gorm:"not null" json:"canvas_id"`                                               // 所属画布
	UserID       uint64         `gorm:"not null;index" json:"user_id"`                                           // 所属用户
	Kind         string         `gorm:"size:16;not null" json:"kind"`                                            // 审批种类，取值见 Approval* 常量
	ToolCallID   string         `gorm:"size:64;not null;default:''" json:"tool_call_id"`                         // 触发它的工具调用
	PayloadJSON  datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"payload"`                         // 待决定的内容：条目清单、问题和选项
	QuoteCredits int            `gorm:"not null;default:0" json:"quote_credits"`                                 // 预估积分
	Status       string         `gorm:"size:20;not null;index:idx_agent_approvals_run,priority:2" json:"status"` // 状态，取值见 Approval* 常量
	DecisionJSON datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"decision"`                        // 用户的决定
	ResultJSON   datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"result"`                          // 执行结果
	DecidedAt    *time.Time     `json:"decided_at"`                                                              // 决定时间
	ExpiresAt    time.Time      `json:"expires_at"`                                                              // 过期时间（创建后 24 小时）
	CreatedAt    time.Time      `json:"created_at"`                                                              // 创建时间
	UpdatedAt    time.Time      `json:"updated_at"`                                                              // 更新时间
}

// TableName 表名。
func (AgentApproval) TableName() string { return "agent_approvals" }

// Agent 对话调用的状态。
const (
	ModelCallPending = "pending" // 已发出，还没结算
	ModelCallSettled = "settled" // 已按实际用量结算（扣费 0 也算）
)

// AgentModelCall 是一次大模型对话调用的计费记录：Agent 每向模型发一次请求就有一条。
// 调用结束后按实际用量扣费并写积分流水；流水里的 agent_call_id 指向它，保证同一次调用只扣一次。
type AgentModelCall struct {
	ID             uint64     `gorm:"primaryKey" json:"id"`                          // 调用 ID
	RunID          uint64     `gorm:"not null;index" json:"run_id"`                  // 所属运行
	UserID         uint64     `gorm:"not null;index" json:"user_id"`                 // 所属用户
	ModelKey       string     `gorm:"size:128;not null" json:"model_key"`            // Agent 模型 key
	Status         string     `gorm:"size:16;not null" json:"status"`                // 状态，取值见 ModelCall* 常量
	InputTokens    int        `gorm:"not null;default:0" json:"input_tokens"`        // 输入 Token
	OutputTokens   int        `gorm:"not null;default:0" json:"output_tokens"`       // 输出 Token
	CachedTokens   int        `gorm:"not null;default:0" json:"cached_tokens"`       // 输入里命中缓存的 Token
	UsageEstimated bool       `gorm:"not null;default:false" json:"usage_estimated"` // 上游没给用量，Token 数是按字数估的
	Credits        int        `gorm:"not null;default:0" json:"credits"`             // 按用量算出的应收积分
	Charged        int        `gorm:"not null;default:0" json:"charged"`             // 实际扣的积分，用户余额不够时小于 Credits
	Error          string     `gorm:"size:255;not null;default:''" json:"error"`     // 调用失败或中断的原因（已脱敏）
	CreatedAt      time.Time  `json:"created_at"`                                    // 创建时间
	SettledAt      *time.Time `json:"settled_at"`                                    // 结算时间
}

// TableName 表名。
func (AgentModelCall) TableName() string { return "agent_model_calls" }
