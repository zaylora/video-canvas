// Package pluginproto 是宿主（主服务）与 plugin-runner 进程之间的线协议：HTTP + JSON，跑在本地 Unix socket（生产）
// 或本机回环 TCP（开发）上。runner 不连数据库、不发 HTTP、不解密凭证，只执行插件钩子。
package pluginproto

import "encoding/json"

// 接口路径。
const (
	PathHealth   = "/healthz"  // GET：存活检查，返回 200
	PathLoad     = "/load"     // POST LoadRequest → LoadResponse：把插件代码装进 runner（按 sha256 缓存）
	PathCall     = "/call"     // POST CallRequest → CallResponse：执行一个钩子
	PathPrecheck = "/precheck" // POST PrecheckRequest → PrecheckResponse：上传预检，不登记
)

// 钩子名（插件导出的函数名）。
const (
	HookBuildPrepareRequests = "buildPrepareRequests"
	HookParsePrepareResponse = "parsePrepareResponses"
	HookBuildSubmitRequest   = "buildSubmitRequest"
	HookParseSubmitResponse  = "parseSubmitResponse"
	HookBuildQueryRequest    = "buildQueryRequest"
	HookParseQueryResponse   = "parseQueryResponse"
	HookBuildCancelRequest   = "buildCancelRequest"
	HookClassifyError        = "classifyError"
	HookBuildCheckRequest    = "buildCheckRequest"
	HookBuildImportRequest   = "buildImportRequest"
	HookParseImportResponse  = "parseImportResponse"
)

// AllHooks 是契约认识的全部钩子，预检时用它判断插件导出了哪些。
var AllHooks = []string{
	HookBuildPrepareRequests, HookParsePrepareResponse, HookBuildSubmitRequest, HookParseSubmitResponse,
	HookBuildQueryRequest, HookParseQueryResponse, HookBuildCancelRequest, HookClassifyError,
	HookBuildCheckRequest, HookBuildImportRequest, HookParseImportResponse,
}

// 默认与上限（提案值，压测后定；runner 与宿主都读它们）。
const (
	DefaultHookTimeoutMs = 200     // 单次钩子调用时限
	MaxPayloadBytes      = 1 << 20 // ctx 与返回值编码后各 ≤1MB
	MaxStateBytes        = 64 << 10
	MaxPluginBytes       = 512 << 10
	DefaultPoolSize      = 8 // 每个插件版本最多的 goja Runtime 数
	MaxLogsPerCall       = 50
)

// LoadRequest 把插件代码装进 runner。SHA256 是 Code 的十六进制 sha256，runner 会重新计算并核对，不一致拒绝。
type LoadRequest struct {
	SHA256 string `json:"sha256"`
	Code   string `json:"code"`
}

// LoadResponse 是装载结果。装载会编译并执行到导出，失败时 Error 非空（已登记的版本不应失败，预检保证过）。
type LoadResponse struct {
	OK    bool       `json:"ok"`
	Hooks []string   `json:"hooks,omitempty"` // 插件导出的钩子
	Error *CallError `json:"error,omitempty"`
}

// CallRequest 执行一个钩子。Args 是按顺序传给钩子的参数（每个都是 JSON），第一个通常是 ctx。
// TimeoutMs 为 0 用默认值。runner 不认识 SHA256 时返回 CodeUnknownPlugin，宿主随即 Load 后重试一次。
type CallRequest struct {
	SHA256    string            `json:"sha256"`
	Hook      string            `json:"hook"`
	Args      []json.RawMessage `json:"args"`
	TimeoutMs int               `json:"timeout_ms,omitempty"`
}

// CallResponse 是一次钩子调用的结果。无论成功失败都是 HTTP 200；Error 非空表示钩子没有产出结果。
type CallResponse struct {
	Result     json.RawMessage `json:"result,omitempty"` // 钩子返回值的 JSON；钩子返回 undefined 时为 null
	Logs       []string        `json:"logs,omitempty"`   // utils.log 的输出（宿主只写进试跑追踪）
	Error      *CallError      `json:"error,omitempty"`
	DurationMs int64           `json:"duration_ms"`
}

// 调用错误码。
const (
	CodeUnknownPlugin = "unknown_plugin" // runner 没有装载这个 sha256（重启过）
	CodeHookMissing   = "hook_missing"   // 插件没有导出这个钩子
	CodeException     = "exception"      // 钩子抛出异常
	CodeTimeout       = "timeout"        // 超时被中断
	CodeInvalidResult = "invalid_result" // 返回值不能编码成 JSON（含循环引用、函数）
	CodeTooLarge      = "too_large"      // ctx 或返回值超过大小上限
	CodeBadRequest    = "bad_request"    // 请求本身不合法
	CodeInternal      = "internal"       // runner 内部错误
)

// CallError 是钩子调用的失败。这些错误都是确定性的：宿主一律按 terminal 处理，不自动重试。
type CallError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PrecheckRequest 是上传预检：runner 编译并执行插件，读出 meta 与导出的钩子，再调用 pluginmeta.Validate。
type PrecheckRequest struct {
	Code string `json:"code"`
}

// PrecheckResponse 是预检结果。OK 为 false 时 Issues 列出全部问题（精确到字段）；
// 通过时 Meta 是插件导出的 meta（JSON），SHA256 是代码哈希，Hooks 是导出的钩子。
type PrecheckResponse struct {
	OK     bool            `json:"ok"`
	Meta   json.RawMessage `json:"meta,omitempty"`
	Hooks  []string        `json:"hooks,omitempty"`
	SHA256 string          `json:"sha256,omitempty"`
	Issues []Issue         `json:"issues,omitempty"`
}

// Issue 是一条预检问题。
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}
