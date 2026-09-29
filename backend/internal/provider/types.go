// Package provider 是外部供应商调用与任务调度模块：跨包契约（Executor / Registry / 素材接口 / 错误分类）都定义在这里。
//
// 分工：
//   - provider/dsl     声明式配置的结构、校验与表达式渲染（纯函数）
//   - provider/engine  Executor 的唯一实现：解析 DSL → 渲染请求 → 发 HTTP → 提取字段（含 SSRF 防护）
//   - service/ai_config  配置的草稿 / 发布 / 回滚 / 凭证，实现 Registry 与 SecretResolver
//   - service/generation_task + provider/worker  任务提交、状态迁移、积分事务、轮询调度、转存
package provider

import (
	"context"
	"errors"
	"io"

	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/model"
)

// ErrorClass 是统一的错误分类，worker 据此决定重试还是失败。
type ErrorClass string

const (
	ClassRetryable       ErrorClass = "retryable"        // 网络 / 5xx / 429：退避重试
	ClassTerminal        ErrorClass = "terminal"         // 参数 / 平台拒绝：直接失败并退积分
	ClassModeration      ErrorClass = "moderation"       // 内容审核未通过：失败，文案“内容未通过审核”
	ClassProviderBalance ErrorClass = "provider_balance" // 平台账户余额不足：失败，对用户显示“服务繁忙”，告警运维
	ClassSubmitUnknown   ErrorClass = "submit_unknown"   // 提交读超时，结果未知：失败并退积分，记录告警供人工核对
)

// Error 是 Executor 返回的分类错误。Message 只用于日志和管理端，不直接给终端用户看。
type Error struct {
	Class   ErrorClass
	Code    string // 平台错误码或统一错误码，可空
	Message string
	Cause   error
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

// ErrCancelUnsupported 平台没有取消操作，调用方走软取消。
var ErrCancelUnsupported = errors.New("平台不支持取消")

// ErrModelUnavailable 模型不存在、未发布、已下线，或它引用的平台没有发布。
var ErrModelUnavailable = errors.New("模型不可用")

// TaskRef 是 Executor 认识的任务最小信息。
type TaskRef struct {
	ID             uint64
	UserID         uint64
	ProviderTaskID string // 提交前为空
}

// SubmitInput 提交请求。Input 是已按 input_schema 校验过的规范化输入，媒体字段的值是 asset id（uint64）。
type SubmitInput struct {
	Task       TaskRef
	Input      map[string]any
	WebhookURL string // 为空表示不注册回调
}

// Output 是平台返回的一个产物，Type 是平台给的类型（如 mp4）。已经过 model.output.select 筛选。
type Output struct {
	URL  string `json:"url"`
	Type string `json:"type"`
	Node string `json:"node,omitempty"`
	Text string `json:"text,omitempty"`
}

// 统一的平台任务状态（Query 结果里的 Status）。
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

// QueryResult 是一次查询的统一结果。
type QueryResult struct {
	Status       string     // StatusQueued / StatusRunning / StatusSucceeded / StatusFailed
	Progress     *int       // 0–100，平台不提供时为 nil
	Outputs      []Output   // 仅 succeeded 时有值
	ErrorClass   ErrorClass // 仅 failed 时有值，由 error_rules 分类
	ErrorCode    string
	ErrorMessage string
	ProviderCost *float64 // 平台侧成本，只用于对账日志
}

// Download 是一次受 allowed_hosts 保护的结果下载。调用方负责 Close。
type Download struct {
	Body        io.ReadCloser
	ContentType string
	Size        int64 // 未知为 -1
	FileName    string
}

// Executor 是通用声明式引擎的接口，worker / 任务服务只依赖它，不碰任何平台细节。
// 每次调用都传快照：引擎无状态，只按快照里冻结的配置执行。
type Executor interface {
	// Submit 提交任务，返回平台任务 id。读超时等“结果未知”的失败返回 ClassSubmitUnknown。
	Submit(ctx context.Context, snap *dsl.Snapshot, in SubmitInput) (providerTaskID string, err error)
	// Query 查询一次，映射成统一状态；succeeded 时已按 model.output.select 筛出产物。
	Query(ctx context.Context, snap *dsl.Snapshot, task TaskRef) (*QueryResult, error)
	// Cancel 尽力取消；平台没有 cancel 操作时返回 ErrCancelUnsupported。
	Cancel(ctx context.Context, snap *dsl.Snapshot, task TaskRef) error
	// Download 下载平台产物，会校验 allowed_hosts、内网 IP、重定向和 DNS rebinding。
	Download(ctx context.Context, snap *dsl.Snapshot, url string) (*Download, error)
}

// ModelInfo 是面向画布的模型信息（GET /models），不含 params / mapping / provider 细节。
type ModelInfo struct {
	Key         string          `json:"key"`
	Kind        string          `json:"kind"`
	Label       string          `json:"label"`
	Hint        string          `json:"hint"`
	Credits     int             `json:"credits"`
	InputSchema dsl.InputSchema `json:"input_schema"`
}

// Registry 只读访问“已发布”的配置。实现方缓存编译结果，发布 / 回滚后热生效。
type Registry interface {
	// ListModels 返回已发布且启用的模型，kind 为空表示全部，按 sort 升序。
	ListModels(ctx context.Context, kind string) ([]ModelInfo, error)
	// Snapshot 冻结一个模型当前发布版本 + 它所属平台当前发布版本；不可用时返回 ErrModelUnavailable。
	Snapshot(ctx context.Context, modelKey string) (*dsl.Snapshot, error)
	// Provider 返回平台当前发布版本（webhook 用），不存在返回 ErrModelUnavailable。
	Provider(ctx context.Context, providerKey string) (*dsl.ProviderConfig, error)
}

// SecretResolver 解密并返回凭证明文。只有 engine 的 auth 块能调用，明文不进表达式上下文，也不进日志。
type SecretResolver interface {
	Get(ctx context.Context, name string) (string, error)
}

// AssetFile 是打开的一份素材，调用方负责关闭 Body。
type AssetFile struct {
	Asset *model.Asset
	Body  io.ReadCloser
	URL   string // 可被平台访问的地址（公开或签名），用于“生成签名 URL”方式的媒体入参
}

// AssetStore 是引擎与任务服务读取素材的接口，所有方法都校验素材归属当前用户。
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
