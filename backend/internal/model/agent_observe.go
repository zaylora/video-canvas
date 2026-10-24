package model

import (
	"time"

	"gorm.io/datatypes"
)

// Agent 运行观测：把线上每次运行的轨迹、反馈和 Harness 版本留下来，供后台和本地 AI 分析
// （设计见 docs/design/Agent运行观测/Agent运行观测.md）。

// 用户反馈的评分。
const (
	FeedbackUp   = 1  // 有帮助
	FeedbackDown = -1 // 没帮上忙
)

// 点踩时可选的原因标签。
const (
	FeedbackReasonMisread   = "misread"    // 没理解我的需求
	FeedbackReasonWrongEdit = "wrong_edit" // 画布改错了
	FeedbackReasonSlow      = "slow"       // 步骤太多或太慢
	FeedbackReasonBadOutput = "bad_output" // 生成结果不好
	FeedbackReasonOther     = "other"      // 其他
)

// ValidFeedbackReason 判断是不是认识的反馈原因。
func ValidFeedbackReason(r string) bool {
	switch r {
	case FeedbackReasonMisread, FeedbackReasonWrongEdit, FeedbackReasonSlow, FeedbackReasonBadOutput, FeedbackReasonOther:
		return true
	}
	return false
}

// 分析令牌的权限范围。
const (
	TokenScopeAgentRead = "agent:read" // 只读 Agent 运行记录
	TokenPrefix         = "vca_"       // 令牌明文的固定前缀，中间件靠它区分登录 JWT 和分析令牌
)

// MaxToolResultBytes 是工具调用结果落库的最大长度（字节）；超过就截断并标记。
const MaxToolResultBytes = 32 << 10

// AgentToolCall 是一次工具调用的完整记录：参数、结果和耗时。
// 参数和结果是大字段，保留 30 天后由清理任务置空（purged_at 记录清理时间）。
type AgentToolCall struct {
	ID              uint64         `gorm:"primaryKey" json:"id"`                                                                                        // 记录 ID
	RunID           uint64         `gorm:"not null;index:idx_agent_tool_calls_run,priority:1;uniqueIndex:uk_agent_tool_calls,priority:1" json:"run_id"` // 所属运行
	SessionID       uint64         `gorm:"not null" json:"session_id"`                                                                                  // 所属会话
	ToolCallID      string         `gorm:"size:64;not null;uniqueIndex:uk_agent_tool_calls,priority:2" json:"tool_call_id"`                             // 模型给的调用 id，与运行组成唯一键
	ParentID        string         `gorm:"size:64;not null;default:''" json:"parent_id"`                                                                // codemode 脚本里发起的调用，指向脚本那次调用
	Name            string         `gorm:"size:64;not null" json:"name"`                                                                                // 工具名（gen_ai.tool.name）
	ArgsJSON        datatypes.JSON `gorm:"type:jsonb" json:"args"`                                                                                      // 调用参数；过保留期后为空
	ResultText      *string        `gorm:"type:text" json:"result"`                                                                                     // 返回给模型的文本，最长 MaxToolResultBytes；过保留期后为空
	ResultTruncated bool           `gorm:"not null;default:false" json:"result_truncated"`                                                              // 结果被截断过
	IsError         bool           `gorm:"not null;default:false" json:"is_error"`                                                                      // 工具没做成
	Summary         string         `gorm:"size:255;not null;default:''" json:"summary"`                                                                 // 与 tool.end 事件的摘要一致
	StartedAt       time.Time      `gorm:"not null;index:idx_agent_tool_calls_run,priority:2" json:"started_at"`                                        // 开始时间
	EndedAt         *time.Time     `json:"ended_at"`                                                                                                    // 结束时间；没结束（进程崩溃）为空
	DurationMs      int            `gorm:"not null;default:0" json:"duration_ms"`                                                                       // 耗时（毫秒）
	PurgedAt        *time.Time     `json:"purged_at"`                                                                                                   // 大字段被清理的时间
}

// TableName 表名。
func (AgentToolCall) TableName() string { return "agent_tool_calls" }

// AgentModelCallBody 是一次模型调用实际发给上游的请求体（已脱敏、gzip 压缩），给事后分析"模型当时看到了什么"。
// 请求体是大字段，一次运行几十次调用、每次都带完整上下文，所以单独放一张表，保留 7 天后整行删除。
// 超过 MaxModelRequestBytes 的请求只留摘要（Body 为空），摘要部分长期随这一行一起保留到被清理。
type AgentModelCallBody struct {
	CallID       uint64         `gorm:"primaryKey;autoIncrement:false" json:"call_id"`                        // 对应 agent_model_calls.id
	RunID        uint64         `gorm:"not null;index:idx_agent_model_call_bodies_run" json:"run_id"`         // 所属运行
	MessageCount int            `gorm:"not null;default:0" json:"message_count"`                              // messages 条数
	ToolCount    int            `gorm:"not null;default:0" json:"tool_count"`                                 // tools 个数
	Params       datatypes.JSON `gorm:"type:jsonb" json:"params"`                                             // 除 messages 和 tools 外的顶层字段：model、max_tokens、temperature 等
	RawBytes     int            `gorm:"not null;default:0" json:"raw_bytes"`                                  // 脱敏后的请求体原始字节数（压缩前）
	Body         []byte         `gorm:"type:bytea" json:"-"`                                                  // gzip 压缩的请求体 JSON；超过上限为空
	CreatedAt    time.Time      `gorm:"not null;index:idx_agent_model_call_bodies_created" json:"created_at"` // 创建时间，清理按它算
}

// TableName 表名。
func (AgentModelCallBody) TableName() string { return "agent_model_call_bodies" }

// AgentFeedback 是用户对一次运行的评价：每个运行每人一条，改选就是覆盖。
type AgentFeedback struct {
	ID        uint64         `gorm:"primaryKey" json:"id"`                                             // 记录 ID
	RunID     uint64         `gorm:"not null;uniqueIndex:uk_agent_feedback,priority:1" json:"run_id"`  // 所属运行
	UserID    uint64         `gorm:"not null;uniqueIndex:uk_agent_feedback,priority:2" json:"user_id"` // 评价人
	Rating    int            `gorm:"not null" json:"rating"`                                           // 1 有帮助，-1 没帮上忙
	Reasons   datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"reasons"`                  // 点踩原因标签（字符串数组）
	Comment   string         `gorm:"size:500;not null;default:''" json:"comment"`                      // 补充说明
	CreatedAt time.Time      `json:"created_at"`                                                       // 创建时间
	UpdatedAt time.Time      `json:"updated_at"`                                                       // 更新时间
}

// TableName 表名。
func (AgentFeedback) TableName() string { return "agent_feedback" }

// AgentRunDigest 是一次运行的摘要：运行进入终态或暂停时生成（之后反馈、撤销会更新），长期保留。
// 后台列表、版本对比和本地 CLI 的索引都只查这张表。
type AgentRunDigest struct {
	RunID             uint64         `gorm:"primaryKey;autoIncrement:false" json:"run_id"`                                                  // 运行 ID
	SessionID         uint64         `gorm:"not null" json:"session_id"`                                                                    // 所属会话
	UserID            uint64         `gorm:"not null;index" json:"user_id"`                                                                 // 所属用户
	CanvasID          uint64         `gorm:"not null" json:"canvas_id"`                                                                     // 所属画布
	Mode              string         `gorm:"size:16;not null;default:''" json:"mode"`                                                       // 任务模式
	ModelKey          string         `gorm:"size:128;not null;default:''" json:"model_key"`                                                 // Agent 模型
	PromptVersion     int            `gorm:"not null;default:0" json:"prompt_version"`                                                      // 系统提示词版本
	ToolsHash         string         `gorm:"size:12;not null;default:''" json:"tools_hash"`                                                 // 工具声明哈希
	BuildSHA          string         `gorm:"column:build_sha;size:40;not null;default:''" json:"build_sha"`                                 // 构建的 git commit
	HarnessKey        string         `gorm:"size:12;not null;default:'';index:idx_agent_run_digests_harness,priority:1" json:"harness_key"` // 以上三项合并的短哈希，版本对比按它分组
	Skills            datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"skills"`                                                // 本次读过的技能及版本，如 {"script-breakdown":"v3"}
	Status            string         `gorm:"size:20;not null;default:''" json:"status"`                                                     // 运行状态
	ErrorCode         string         `gorm:"size:64;not null;default:''" json:"error_code"`                                                 // 失败时的错误码
	RequestHead       string         `gorm:"size:200;not null;default:''" json:"request_head"`                                              // 用户请求的前 200 字
	Steps             int            `gorm:"not null;default:0" json:"steps"`                                                               // 步数
	ToolCalls         int            `gorm:"not null;default:0" json:"tool_calls"`                                                          // 工具调用次数
	ToolErrors        int            `gorm:"not null;default:0" json:"tool_errors"`                                                         // 工具报错次数
	ModelCalls        int            `gorm:"not null;default:0" json:"model_calls"`                                                         // 模型调用次数
	InputTokens       int            `gorm:"not null;default:0" json:"input_tokens"`                                                        // 输入 Token 合计
	OutputTokens      int            `gorm:"not null;default:0" json:"output_tokens"`                                                       // 输出 Token 合计
	SpentCredits      int            `gorm:"not null;default:0" json:"spent_credits"`                                                       // 已花积分
	DurationMs        int64          `gorm:"not null;default:0" json:"duration_ms"`                                                         // 从创建到结束的耗时（毫秒）
	ApprovalsTotal    int            `gorm:"not null;default:0" json:"approvals_total"`                                                     // 审批总数
	ApprovalsRejected int            `gorm:"not null;default:0" json:"approvals_rejected"`                                                  // 被拒绝的审批数
	SteerCount        int            `gorm:"not null;default:0" json:"steer_count"`                                                         // 用户插话次数
	MutationCount     int            `gorm:"not null;default:0" json:"mutation_count"`                                                      // 改画布的次数（不含撤销）
	Undone            bool           `gorm:"not null;default:false" json:"undone"`                                                          // 本轮改动被用户撤销过
	Feedback          int            `gorm:"not null;default:0" json:"feedback"`                                                            // 1 有帮助，-1 没帮上忙，0 没评价
	RuntimeTail       *string        `gorm:"type:text" json:"-"`                                                                            // Node 进程 stderr 末尾 4KB；过保留期后为空
	EndedAt           *time.Time     `gorm:"index:idx_agent_run_digests_harness,priority:2;index" json:"ended_at"`                          // 结束时间；未结束为空
	CreatedAt         time.Time      `json:"created_at"`                                                                                    // 运行创建时间
	UpdatedAt         time.Time      `gorm:"index" json:"updated_at"`                                                                       // 摘要更新时间，本地 CLI 据此做增量拉取
}

// TableName 表名。
func (AgentRunDigest) TableName() string { return "agent_run_digests" }

// AdminAPIToken 是后台签发的分析令牌，给本地 agent-trace 只读拉取运行记录。库里只存 sha256，不存明文。
type AdminAPIToken struct {
	ID         uint64     `gorm:"primaryKey" json:"id"`                  // 令牌 ID
	Name       string     `gorm:"size:64;not null" json:"name"`          // 用途备注
	Prefix     string     `gorm:"size:12;not null" json:"prefix"`        // 明文的前 8 位，用来在列表里辨认
	TokenHash  string     `gorm:"size:64;not null;uniqueIndex" json:"-"` // 明文的 sha256 十六进制
	Scope      string     `gorm:"size:32;not null" json:"scope"`         // 权限范围，见 TokenScope*
	CreatedBy  uint64     `gorm:"not null" json:"created_by"`            // 签发人；鉴权时校验其仍是管理员
	ExpiresAt  time.Time  `gorm:"not null" json:"expires_at"`            // 到期时间
	LastUsedAt *time.Time `json:"last_used_at"`                          // 最近使用时间
	RevokedAt  *time.Time `json:"revoked_at"`                            // 注销时间
	CreatedAt  time.Time  `json:"created_at"`                            // 签发时间
}

// TableName 表名。
func (AdminAPIToken) TableName() string { return "admin_api_tokens" }

// AgentToolCallResult 是一次工具调用结束时要补记的内容。
type AgentToolCallResult struct {
	ResultText string    // 返回给模型的文本（已按 MaxToolResultBytes 截断）
	Truncated  bool      // 结果被截断过
	IsError    bool      // 工具没做成
	Summary    string    // 与 tool.end 事件一致的一行摘要
	EndedAt    time.Time // 结束时间
	DurationMs int       // 耗时（毫秒）；为 0 时由存储按开始时间推算
}

// FailedRunStatuses 是版本对比里算「失败结束」的运行状态：运行失败、超时、被中断，
// 以及步数或预算用尽被迫暂停。用户主动停止（canceled）和等用户太久（expired）不算 Agent 的失败。
var FailedRunStatuses = []string{RunFailed, RunTimeout, RunInterrupted, RunStepLimit, RunBudgetExhausted}

// AgentRunFacts 是生成运行摘要需要的原始事实：只含计数和分类用的轻量字段，不含工具参数和结果这类大字段。
type AgentRunFacts struct {
	Run            AgentRun         // 运行本身
	SessionDeleted bool             // 所属会话已被用户删除
	ToolCalls      []AgentToolCall  // 工具调用（不含参数和结果）
	ModelCalls     []AgentModelCall // 模型调用
	Approvals      []AgentApproval  // 审批（只含种类和状态）
	Mutations      []AgentMutation  // 画布改动（只含种类和撤销时间）
	UserMessages   []AgentEvent     // 本轮的 message.user 事件，按序号升序
	Feedback       *AgentFeedback   // 运行所有者的评价；没评价为 nil
	RuntimeTail    string           // Node 进程 stderr 末尾，没有为空
}

// DigestQuery 是运行摘要列表的筛选条件，零值表示不限。
type DigestQuery struct {
	Since, Until  time.Time // 运行创建时间的范围，Since 含、Until 不含
	Statuses      []string  // 运行状态
	Feedback      string    // down / up / none；空或 any 不限
	Undone        bool      // 只要被撤销过的
	HarnessKey    string    // Harness 版本
	PromptVersion *int      // 系统提示词版本
	ModelKey      string    // Agent 模型
	UserID        uint64    // 所属用户
	Cursor        string    // 上一页返回的游标
	Page          int       // 页码（从 1 起）；大于 0 走偏移分页（后台页码翻页用），为 0 走游标分页（本地 CLI 用）
	Limit         int       // 每页条数，默认 100，最大 500
}

// AgentStatsRow 是版本对比里一组的聚合指标；比率是 0–1 的小数，分母为 0 时记 0。
type AgentStatsRow struct {
	Key              string    `json:"key"`                // 分组值
	N                int       `json:"n"`                  // 运行数
	FeedbackRate     float64   `json:"feedback_rate"`      // 有评价的运行占比
	DownRate         float64   `json:"down_rate"`          // 点踩数 ÷ 有评价的运行数
	UndoRate         float64   `json:"undo_rate"`          // 被撤销的运行数 ÷ 改过画布的运行数
	FailEndRate      float64   `json:"fail_end_rate"`      // 失败结束的运行占比，见 FailedRunStatuses
	MedianDurationMs float64   `json:"median_duration_ms"` // 耗时中位数
	MedianSteps      float64   `json:"median_steps"`       // 步数中位数
	AvgCredits       float64   `json:"avg_credits"`        // 平均每次运行花的积分
	PromptVersion    int       `json:"prompt_version"`     // 这一组的提示词版本（按 harness_key 分组时唯一；其他分组取最大值，仅供参考）
	BuildSHA         string    `json:"build_sha"`          // 这一组的构建号（同上）
	LastAt           time.Time `json:"last_at"`            // 这一组最近一次运行的创建时间，版本对比按它挑「最近几个版本」
}

// StatsQuery 是版本对比的条件。
type StatsQuery struct {
	GroupBy      string    // harness_key / prompt_version / model_key
	Since, Until time.Time // 运行创建时间的范围，Since 含、Until 不含
}

// AgentExportFacts 是导出一次运行（给 AI 读）需要的全部事实，工具参数和结果也在里面。
type AgentExportFacts struct {
	Digest      AgentRunDigest       // 运行摘要
	Run         AgentRun             // 运行本身
	Events      []AgentEvent         // 本轮的全部落库事件，按序号升序
	ToolCalls   []AgentToolCall      // 工具调用，含参数和结果
	ModelCalls  []AgentModelCall     // 模型调用
	Requests    []AgentModelCallBody // 模型请求的摘要（不含请求体），按调用 id 对应 ModelCalls；过了保留期的没有
	Mutations   []AgentMutation      // 画布改动
	Approvals   []AgentApproval      // 审批
	Feedback    *AgentFeedback       // 评价
	Context     []AgentEvent         // 同会话更早两轮的用户消息和 Agent 最终回复，按时间升序，给 AI 补上下文
	RuntimeTail string               // Node 进程 stderr 末尾
}
