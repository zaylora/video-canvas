// Package tracereport 把一轮 Agent 运行的轨迹整理成给人和 AI 编码助手读的 Markdown 报告（report.md）。
// 它是纯函数：输入轨迹数据，输出文本，不碰数据库和存储；轨迹包里的 trace.jsonl 行也定义在这里，两边共用一份字段。
// 契约见 docs/design/Agent调优Harness/Agent调优Harness.md 第 8.4 节。
package tracereport

import (
	"encoding/json"
	"time"
)

// span 的种类与状态，取值与契约一致。
const (
	KindSystem      = "system"       // 第一行：提示词版本、工具声明全文
	KindSegment     = "segment"      // 一个 Node 进程片段
	KindChat        = "chat"         // 一次模型调用
	KindExecuteTool = "execute_tool" // 一次工具调用
	KindApproval    = "approval"     // 一次审批（生成、删除、提问）

	StatusOK      = "ok"
	StatusError   = "error"
	StatusDenied  = "denied"
	StatusAborted = "aborted"
	StatusPending = "pending"
)

// Usage 是一次模型调用的 Token 用量。
type Usage struct {
	Input  int `json:"input"`  // 输入
	Output int `json:"output"` // 输出
	Cached int `json:"cached"` // 输入里命中缓存的部分
}

// Span 是 trace.jsonl 里的一行（system 行除外）。所有行共有前几个字段，其余按 kind 出现。
type Span struct {
	Seq          int       `json:"seq"`            // 在运行内的顺序，从 1 开始，导出时按开始时间重新编号
	SpanID       string    `json:"span_id"`        // span 标识
	ParentSpanID *string   `json:"parent_span_id"` // codemode 嵌套调用指向外层调用，其余为 null
	Kind         string    `json:"kind"`           // segment | chat | execute_tool | approval
	Name         string    `json:"name"`           // 模型 key、工具名、启动方式或审批种类
	Status       string    `json:"status"`         // ok | error | denied | aborted | pending
	Summary      string    `json:"summary"`        // 一行摘要
	StartedAt    time.Time `json:"started_at"`     // 开始时间
	DurationMS   int       `json:"duration_ms"`    // 耗时
	ErrorCode    string    `json:"error_code"`     // 错误分类，没有为空串

	// chat
	Purpose       string          `json:"purpose,omitempty"`        // chat | summary
	UpstreamModel string          `json:"upstream_model,omitempty"` // 实际走的上游模型
	ChannelID     string          `json:"channel_id,omitempty"`     // 实际走的渠道 key
	FirstTokenMS  *int            `json:"first_token_ms,omitempty"` // 首字延迟
	FinishReason  string          `json:"finish_reason,omitempty"`  // 上游给的结束原因
	Usage         *Usage          `json:"usage,omitempty"`          // Token 用量
	Credits       *int            `json:"credits,omitempty"`        // 这次调用扣的积分
	Request       json.RawMessage `json:"request,omitempty"`        // 发给模型的请求体原文（去掉请求头，图片换成引用）
	Response      json.RawMessage `json:"response,omitempty"`       // {text, thinking, tool_calls:[{id,name,arguments}]}

	// execute_tool
	ToolCallID string          `json:"tool_call_id,omitempty"` // 工具调用 id
	Args       json.RawMessage `json:"args,omitempty"`         // 解析后的参数对象
	Result     json.RawMessage `json:"result,omitempty"`       // 工具：返回给模型的文本（JSON 字符串）；审批：执行结果对象
	IsError    *bool           `json:"is_error,omitempty"`     // 工具有没有报错
	MutationID string          `json:"mutation_id,omitempty"`  // 关联的画布改动（编码后的 id），没有改画布则无

	// segment
	Launch     string `json:"launch,omitempty"`      // start | continue | resume | approval
	ExitCode   *int   `json:"exit_code,omitempty"`   // 进程退出码
	StderrTail string `json:"stderr_tail,omitempty"` // 标准错误的末尾 4KB

	// approval
	ApprovalKind string          `json:"approval_kind,omitempty"` // generate | delete | ask
	AskKind      string          `json:"ask_kind,omitempty"`      // ask 的子类（choice | model），没有为空
	Question     string          `json:"question,omitempty"`      // ask 的提问原文
	Quote        *int            `json:"quote,omitempty"`         // 预估积分
	Decision     json.RawMessage `json:"decision,omitempty"`      // 用户的决定

	// Truncated 在 chat 和 execute_tool 上总会出现：原文被截断或因超出整轮上限没有记原文时为 true。
	Truncated *bool `json:"truncated,omitempty"`
}

// SystemRow 是 trace.jsonl 的第一行，工具声明全文只在这里出现一次，chat 的 request 里用 tools_hash 引用。
type SystemRow struct {
	Seq           int             `json:"seq"`            // 固定为 0
	Kind          string          `json:"kind"`           // 固定为 system
	PromptVersion int             `json:"prompt_version"` // 开跑时的系统提示词版本
	ToolsHash     string          `json:"tools_hash"`     // 工具声明 JSON 的 sha256 前 16 位
	SystemPrompt  string          `json:"system_prompt"`  // 系统提示词全文
	Tools         json.RawMessage `json:"tools"`          // 工具声明数组
}

// Run 是报告用到的运行概况。
type Run struct {
	ID            string        // 编码后的运行 id
	Status        string        // 运行状态，取值见 model.Run* 常量
	Active        bool          // 运行是否还没结束（仍占用画布）：此时轨迹必然不完整
	PromptVersion int           // 系统提示词版本
	Model         string        // Agent 模型 key
	Steps         int           // 已执行的步数
	SpentCredits  int           // 已花积分
	Duration      time.Duration // 起止时间之差；未结束为 0
	UserText      string        // 用户原话
	TraceComplete bool          // 轨迹是否完整
}

// Input 是 Build 的输入。
type Input struct {
	Run         Run
	Spans       []Span          // 按 seq 升序，含 approval
	CanvasAfter json.RawMessage // 运行结束时的画布 payload（canvas.after.json 的内容）；为空表示没有快照
}
