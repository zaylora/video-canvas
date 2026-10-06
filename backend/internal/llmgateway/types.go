// Package llmgateway 是画布 Agent 的大模型网关：向渠道的 OpenAI 兼容接口发一次流式对话请求，
// 把响应拆成事件、拼出完整结果和 Token 用量。
//
// 它不走插件：插件钩子是同步的、不能联网，没法流式；网关复用渠道配置和 netguard 的 SSRF 防护，
// 但自己不碰数据库、不管计费，只做「一次请求」。密钥由调用方传入，不会出现在任何错误信息里。
package llmgateway

import (
	"encoding/json"
	"errors"
	"fmt"
)

// 消息角色。
const (
	RoleUser      = "user"      // 用户
	RoleAssistant = "assistant" // 模型
	RoleTool      = "tool"      // 工具调用的结果
)

// 网关返回的错误。
var (
	// ErrInvalidTarget 表示渠道配置不能用：地址不合法、没有 Key、没有上游模型名。
	ErrInvalidTarget = errors.New("渠道配置不合法")
	// ErrInvalidRequest 表示请求本身不合法：没有消息。
	ErrInvalidRequest = errors.New("请求不合法")
	// ErrStreamTruncated 表示上游连接中途断了，没有收到结束标记。已收到的部分仍随结果返回。
	ErrStreamTruncated = errors.New("上游响应中断")
	// ErrIdleTimeout 表示上游太久没有发数据。已收到的部分仍随结果返回。
	ErrIdleTimeout = errors.New("上游长时间没有响应")
)

// UpstreamError 是上游返回的错误。Message 已脱敏（不含 Key）并截断，可以直接给用户看。
type UpstreamError struct {
	Status    int    // HTTP 状态码
	Message   string // 给用户看的说明
	Retryable bool   // 稍后重试可能成功：限流和上游故障
}

// Error 实现 error。
func (e *UpstreamError) Error() string {
	return fmt.Sprintf("上游错误 %d: %s", e.Status, e.Message)
}

// Part 是多模态消息的一段：文字或图片（二选一）。
type Part struct {
	Text     string // 文字
	ImageURL string // 图片地址，可以是 data URL
}

// ToolCall 是模型发起的一次工具调用。
type ToolCall struct {
	ID        string // 调用 id，工具结果要带回它
	Name      string // 工具名
	Arguments string // 参数，JSON 文本
}

// Message 是对话里的一条消息。
type Message struct {
	Role       string     // user / assistant / tool
	Text       string     // 文字内容；有 Parts 时忽略
	Parts      []Part     // 多模态内容（用户看图时用）
	ToolCalls  []ToolCall // assistant 发起的工具调用
	ToolCallID string     // tool 消息对应的调用 id
}

// ToolDef 是提供给模型的一个工具。
type ToolDef struct {
	Name        string          // 工具名
	Description string          // 说明
	Parameters  json.RawMessage // 参数的 JSON Schema
}

// Request 是一次对话请求。
type Request struct {
	System      string    // 系统提示，放在最前面
	Messages    []Message // 对话历史
	Tools       []ToolDef // 可用工具，可为空
	MaxTokens   int       // 最大输出 Token，0 表示不限制
	Temperature *float64  // 采样温度，nil 表示用上游默认
}

// Target 是请求发往的渠道。
type Target struct {
	ChannelKey      string  // 渠道 key，限流按它分组
	BaseURL         string  // 渠道地址，请求发往 BaseURL + /v1/chat/completions
	APIKey          string  // 渠道 Key，用 Bearer 鉴权
	UpstreamModel   string  // 上游模型名
	TrustedInternal bool    // 允许地址解析到内网（自建网关）；只有超级管理员能给渠道开启
	RPS             float64 // 每秒最多发起几次请求，0 不限
	MaxConcurrency  int     // 同时在途的请求数上限，0 不限
}

// Usage 是上游报告的 Token 用量。
type Usage struct {
	InputTokens  int // 输入
	OutputTokens int // 输出
	CachedTokens int // 输入里命中缓存的部分
}

// Result 是一次对话的完整结果。
type Result struct {
	Text         string     // 正文
	Thinking     string     // 思考内容，不混进正文
	ToolCalls    []ToolCall // 模型发起的工具调用，按序号排好
	FinishReason string     // stop / tool_calls / length 等
	Usage        Usage      // 用量
	UsageMissing bool       // 上游没给用量：调用方要按估算计费
}

// EventType 是流式事件的种类。
type EventType string

// 流式事件种类。
const (
	EventText      EventType = "text"       // 正文增量
	EventThinking  EventType = "thinking"   // 思考增量
	EventToolDelta EventType = "tool_delta" // 工具调用的一段增量
	EventUsage     EventType = "usage"      // 用量
)

// Event 是流式过程中的一个增量，调用方（反向桥）原样转给 Node 端。
type Event struct {
	Type      EventType // 事件种类
	Text      string    // text / thinking：增量文字
	ToolIndex int       // tool_delta：工具调用序号
	ToolID    string    // tool_delta：调用 id（首段才有）
	ToolName  string    // tool_delta：工具名（首段才有）
	ArgsDelta string    // tool_delta：参数增量
	Usage     *Usage    // usage：用量
}
