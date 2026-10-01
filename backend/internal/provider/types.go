// Package provider 是外部上游调用与任务调度模块：跨包契约（Executor / Registry / 素材接口 / 错误分类 / 配置快照）都定义在这里。
//
// 分工（见 docs/design/协议插件设计.md）：
//   - provider/modelcfg      模型配置与输入 schema 的结构、校验（纯函数）
//   - provider/pluginmeta    插件 meta 的结构与预检规则（纯函数）
//   - provider/pluginproto   宿主与 plugin-runner 之间的线协议
//   - provider/pluginrunner  runner 进程：goja 沙箱里执行插件钩子，不连数据库、不发 HTTP
//   - provider/plugin        宿主：Executor 的唯一实现，调钩子 → 校验请求描述 → 注入鉴权 → 发 HTTP → 解析结果
//   - provider/netguard      出网的 SSRF 防护传输层
//   - service                模型配置的草稿 / 发布 / 回滚，插件与渠道管理，实现 Registry 与 SecretResolver
//   - service/generation_task + provider/worker  任务提交、状态迁移、积分事务、轮询调度、转存
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"video-canvas/internal/model"
	"video-canvas/internal/provider/modelcfg"
)

// ErrorClass 是统一的错误分类，worker 据此决定重试还是失败。
type ErrorClass string

const (
	ClassRetryable       ErrorClass = "retryable"        // 网络 / 5xx / 429：退避重试
	ClassTerminal        ErrorClass = "terminal"         // 参数 / 平台拒绝 / 插件出错：直接失败并退积分
	ClassModeration      ErrorClass = "moderation"       // 内容审核未通过：失败，文案“内容未通过审核”
	ClassProviderBalance ErrorClass = "provider_balance" // 上游账户余额不足：失败，对用户显示“服务繁忙”，告警运维
	ClassSubmitUnknown   ErrorClass = "submit_unknown"   // 提交读超时，结果未知：失败并退积分，记录告警供人工核对
)

// 宿主自己产生的错误码（Error.Code），worker 对其中几个有专门的处理。
const (
	// CodeRunnerUnavailable 表示 plugin-runner 连不上：ClassRetryable，但任务应保持 pending 直到恢复（不消耗重试次数，
	// 由任务 deadline 兜底），恢复后继续。
	CodeRunnerUnavailable = "plugin_runner_unavailable"
	// CodeRunnerCrashed 表示调用进行中 runner 进程崩溃（如内存超限）：ClassRetryable，但只重试一次，再次崩溃就失败退积分。
	CodeRunnerCrashed = "plugin_runner_crashed"
	// CodePluginError 表示插件抛异常、超时被中断、返回值结构不合规、请求描述非法：ClassTerminal，告警。
	CodePluginError = "plugin_error"
	// CodeSSRFBlocked 表示请求被 SSRF 防护拒绝（域名不在白名单、内网地址、重定向过多）。
	CodeSSRFBlocked = "ssrf_blocked"
)

// Error 是 Executor 返回的分类错误。Message 只用于日志和管理端，不直接给终端用户看。
type Error struct {
	Class   ErrorClass
	Code    string // 上游错误码或统一错误码，可空
	Message string
	Cause   error
	// PluginFault 为 true 表示这是“插件级失败”（异常 / 超时 / 结构不合规 / runner 崩溃），
	// 同一渠道连续多次插件级失败会自动停用渠道（阶段 2）。
	PluginFault bool
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return string(e.Class) + ": " + e.Message + ": " + e.Cause.Error()
	}
	return string(e.Class) + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

// ClassOf 取错误的分类；不是 *Error 的错误一律按 terminal 处理（未知错误不盲目重试）。
func ClassOf(err error) ErrorClass {
	var e *Error
	if errors.As(err, &e) {
		return e.Class
	}
	return ClassTerminal
}

// CodeOf 取分类错误携带的错误码；不是 *Error 返回空串。
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// ErrCancelUnsupported 上游没有取消操作，调用方走软取消。
var ErrCancelUnsupported = errors.New("平台不支持取消")

// ErrModelUnavailable 模型不存在、未发布、已下线，或它的渠道 / 插件被停用。
var ErrModelUnavailable = errors.New("模型不可用")

// ProviderState 是 generation_tasks.provider_state 列里的 JSON：宿主与插件需要跨调用保留的数据。
type ProviderState struct {
	// Prepared 是准备阶段（如先上传文件）的结果（parsePrepareResponses 的返回值）。持久化之后，提交阶段重试不会重复上传。
	Prepared json.RawMessage `json:"prepared,omitempty"`
	// Plugin 是插件在 parseSubmitResponse / parseQueryResponse 里返回的私有 state（≤64KB），下次调用原样传回。
	Plugin json.RawMessage `json:"plugin,omitempty"`
}

// DecodeProviderState 解码 provider_state 列；空值或损坏得到零值（损坏时 ok=false，调用方记警告后继续）。
func DecodeProviderState(raw []byte) (st ProviderState, ok bool) {
	if len(raw) == 0 {
		return ProviderState{}, true
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return ProviderState{}, false
	}
	return st, true
}

// TaskRef 是 Executor 认识的任务最小信息。
type TaskRef struct {
	ID             uint64
	UserID         uint64
	ProviderTaskID string          // 提交前为空
	State          json.RawMessage // 插件上次返回的私有状态（ProviderState.Plugin），没有为空
	Prepared       json.RawMessage // 准备阶段已经完成的结果（ProviderState.Prepared），没有为空
}

// SubmitInput 提交请求。Input 是已按模型能力（capabilities）校验过的规范化输入：prompt、op、各生成参数，以及 images / videos / audios 素材 id（uint64）数组。
type SubmitInput struct {
	Task  TaskRef
	Input map[string]any
	// OnPrepared 在准备阶段完成（parsePrepareResponses 返回）后、发提交请求前被调用，worker 借此把 prepared
	// 写进 provider_state：之后提交阶段失败重试时，Task.Prepared 非空，宿主跳过准备阶段，不会重复上传。
	// 为 nil 表示不持久化；返回错误时宿主按 retryable 失败（此时 prepared 没有落库，重试会重新准备）。
	OnPrepared func(ctx context.Context, prepared json.RawMessage) error
}

// SubmitResult 是提交的结果。异步任务返回 ProviderTaskID；同步接口（或上游立刻出结果）带 Immediate，
// 此时 ProviderTaskID 可以为空（宿主会补一个稳定的占位 id）。
type SubmitResult struct {
	ProviderTaskID string
	State          json.RawMessage // 插件返回的私有状态，≤64KB，由 worker 持久化
	Immediate      *QueryResult    // 非空表示提交即出结果（succeeded / failed），worker 据此跳过轮询
}

// 产物类型。
const (
	OutputURL   = "url"   // 上游给了一个下载地址，worker 转存到自有存储
	OutputText  = "text"  // 文本正文，不转存
	OutputAsset = "asset" // 二进制响应已由宿主落库（阶段 2）
)

// Output 是上游返回的一个产物。
type Output struct {
	Type      string `json:"type"`                 // url / text / asset
	URL       string `json:"url,omitempty"`        // type=url
	Text      string `json:"text,omitempty"`       // type=text
	MediaType string `json:"media_type,omitempty"` // video / image / audio / text；为空取模型 kind
	Mime      string `json:"mime,omitempty"`       // 插件声明的 MIME，下载响应头优先
	// Usage 是文本产物的 Token 用量（插件从上游响应里取，如 OpenAI 风格的 usage.prompt_tokens / completion_tokens），
	// 按 Token 计费的模型据此结算；没有可以不填，宿主按冻结额扣费
	Usage *modelcfg.Usage `json:"usage,omitempty"`

	// 以下只在 type=asset 时由宿主填写：二进制响应已经写入素材存储，worker 不必再下载转存，直接据此生成 TaskOutput。
	AssetID    uint64 `json:"asset_id,omitempty"`    // 宿主已写入的素材
	AssetURL   string `json:"asset_url,omitempty"`   // 素材的访问地址
	DurationMs int64  `json:"duration_ms,omitempty"` // 时长（毫秒）
	Width      int    `json:"width,omitempty"`       // 宽度（像素）
	Height     int    `json:"height,omitempty"`      // 高度（像素）
}

// 统一的上游任务状态（QueryResult.Status）。
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

// QueryResult 是一次查询（或 Submit 的 Immediate）的统一结果。
type QueryResult struct {
	Status       string          // StatusQueued / StatusRunning / StatusSucceeded / StatusFailed
	Progress     *int            // 0–100，上游不提供时为 nil
	Outputs      []Output        // 仅 succeeded 时有值
	ErrorClass   ErrorClass      // 仅 failed 时有值
	ErrorCode    string          // 上游错误码
	ErrorMessage string          // 上游错误文案，只进日志
	ProviderCost *float64        // 上游成本，只用于对账日志
	State        json.RawMessage // 插件返回的新私有状态；为 nil 表示不变
}

// Download 是一次受白名单保护的结果下载。调用方负责 Close。
type Download struct {
	Body        io.ReadCloser
	ContentType string
	Size        int64 // 未知为 -1
	FileName    string
}

// Executor 是插件宿主的接口，worker / 任务服务只依赖它，不碰任何协议细节。
// 每次调用都传快照：宿主无状态，只按快照里冻结的渠道配置与插件版本执行。
type Executor interface {
	// Submit 提交任务。读超时等“结果未知”的失败返回 ClassSubmitUnknown。
	Submit(ctx context.Context, snap *Snapshot, in SubmitInput) (*SubmitResult, error)
	// Query 查询一次，映射成统一状态。
	Query(ctx context.Context, snap *Snapshot, task TaskRef) (*QueryResult, error)
	// Cancel 尽力取消；插件没有 buildCancelRequest 时返回 ErrCancelUnsupported。
	Cancel(ctx context.Context, snap *Snapshot, task TaskRef) error
	// Download 下载上游产物，校验域名（插件 allowedHosts + 渠道 base_url 主机）、内网 IP、重定向和 DNS rebinding。
	Download(ctx context.Context, snap *Snapshot, url string) (*Download, error)
}

// CheckResult 是渠道连通性检查的结果。
type CheckResult struct {
	OK         bool   `json:"ok"`
	Message    string `json:"message"` // 成功或失败的说明（已脱敏）
	DurationMs int64  `json:"duration_ms"`
}

// ModelDraft 是“从渠道导入模型”得到的一份模型草稿建议，只预填编辑器，运营确认、试跑后才发布。
type ModelDraft struct {
	UpstreamModel string         `json:"upstream_model"`
	Kind          string         `json:"kind"`
	Label         string         `json:"label"`
	Params        map[string]any `json:"params,omitempty"`
	// ParamHints 是插件对生成参数的预填建议（参数名 -> 建议），只在导入时预填编辑器，见 modelcfg.ParamHint
	ParamHints map[string]modelcfg.ParamHint `json:"param_hints,omitempty"`
}

// PluginOps 是管理端经插件钩子做的两件事：连通性检查与导入模型。由宿主（provider/plugin）实现。
type PluginOps interface {
	// Check 调插件的 buildCheckRequest 发一次请求，确认地址和 Key 可用；插件没实现该钩子返回 ErrCheckUnsupported。
	Check(ctx context.Context, rt *ChannelRuntime) (*CheckResult, error)
	// Import 按插件 meta.import.args 的取值调 buildImportRequest / parseImportResponse，返回模型草稿；
	// 插件没实现这两个钩子返回 ErrImportUnsupported。
	Import(ctx context.Context, rt *ChannelRuntime, args map[string]any) ([]ModelDraft, error)
}

// PrecheckResult 是插件上传预检的结果：通过时 Meta 是插件导出的 meta（JSON）、Hooks 是导出的钩子、SHA256 是代码哈希；
// 不通过时 Issues 列出全部问题（精确到字段）。
type PrecheckResult struct {
	OK     bool
	Meta   json.RawMessage
	Hooks  []string
	SHA256 string
	Issues []modelcfg.Issue
}

// Prechecker 在沙箱里编译并执行插件代码，读出 meta 与导出的钩子并按契约检查（不登记）。由 plugin 包包装 runner 客户端实现；
// service 只依赖这个接口，不认识 runner 的线协议。runner 连不上时返回 *Error（Code 为 CodeRunnerUnavailable）。
type Prechecker interface {
	Precheck(ctx context.Context, code string) (*PrecheckResult, error)
}

// 管理端操作对应插件没有实现可选钩子。
var (
	ErrCheckUnsupported  = errors.New("插件不支持连通性检查")
	ErrImportUnsupported = errors.New("插件不支持导入模型")
)

// ModelInfo 是面向画布的模型信息（GET /models），不含 params / 渠道 / 插件细节。
type ModelInfo struct {
	Key          string                `json:"key"`
	Kind         string                `json:"kind"`
	Label        string                `json:"label"`
	Hint         string                `json:"hint"`
	Vendor       string                `json:"vendor"`
	Tags         []string              `json:"tags"`
	Pricing      modelcfg.Pricing      `json:"pricing"` // 不含积分成本
	Capabilities modelcfg.Capabilities `json:"capabilities"`
}

// Registry 只读访问“已发布”的模型配置。实现方缓存编译结果，发布 / 回滚 / 渠道变更后热生效。
type Registry interface {
	// ListModels 返回已发布、已上架且渠道与插件可用的模型，kind 为空表示全部，按 sort 升序。
	ListModels(ctx context.Context, kind string) ([]ModelInfo, error)
	// Snapshot 冻结一个模型当前发布版本 + 它的渠道配置 + 插件版本；不可用时返回 ErrModelUnavailable。
	Snapshot(ctx context.Context, modelKey string) (*Snapshot, error)
}

// SecretResolver 解密并返回凭证明文。只有宿主的鉴权注入环节能调用，明文不进插件（auth: custom 且渠道开启除外）、也不进日志。
type SecretResolver interface {
	Get(ctx context.Context, name string) (string, error)
}

// AssetFile 是打开的一份素材，调用方负责关闭 Body。
type AssetFile struct {
	Asset *model.Asset
	Body  io.ReadCloser
	URL   string // 可被上游访问的地址（公开或签名），用于文件引用 as=url
}

// AssetStore 是宿主与任务服务读取素材的接口，所有方法都校验素材归属当前用户。
type AssetStore interface {
	// Get 查询素材元数据，不存在或不属于该用户返回 ErrAssetNotFound。
	Get(ctx context.Context, userID, assetID uint64) (*model.Asset, error)
	// Open 打开素材内容并生成可访问 URL。
	Open(ctx context.Context, userID, assetID uint64) (*AssetFile, error)
}

// ErrAssetNotFound 素材不存在或不属于该用户。
var ErrAssetNotFound = errors.New("素材不存在")

// SaveGeneratedInput 是转存一个生成产物所需的信息。
type SaveGeneratedInput struct {
	UserID   uint64
	TaskID   uint64
	Kind     string // image / video / audio
	MimeType string
	FileName string
	Body     io.Reader
	// MaxBytes 是下载大小上限，超出返回错误；0 表示使用存储层默认上限。
	MaxBytes int64
}

// AssetSaver 是 worker 转存产物的接口：写入自有存储 + 插入 assets 行，返回素材与可访问 URL。
type AssetSaver interface {
	SaveGenerated(ctx context.Context, in SaveGeneratedInput) (*model.Asset, string, error)
}
