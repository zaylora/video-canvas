// 本文件：DSL 的数据结构定义：平台协议（ProviderConfig）、模型配置（ModelConfig）、输入 schema（InputSchema，保序编解码）
// 和 Duration 等基础类型。

// Package dsl 定义平台协议 / 模型配置的声明式结构（见 docs/design/平台协议配置化设计.md）。
//
// 本包只包含纯数据结构与纯函数（校验、表达式编译与渲染），不访问数据库和网络。
package dsl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Version 是当前支持的 DSL 版本号，配置里的 dsl 字段必须等于它。
const Version = 1

// ProviderConfig 平台协议：鉴权、提交、查询、上传、取消、状态映射、错误分类。
type ProviderConfig struct {
	DSL          int               `json:"dsl"`
	Key          string            `json:"key"`
	Name         string            `json:"name"`
	BaseURL      string            `json:"base_url"`
	AllowedHosts []string          `json:"allowed_hosts"` // 支持 *.example.com 通配；请求、重定向、结果下载都要校验
	Auth         AuthConfig        `json:"auth"`
	RateLimit    RateLimitConfig   `json:"rate_limit"`
	Poll         PollConfig        `json:"poll"`
	Operations   Operations        `json:"operations"`
	StatusMap    map[string]string `json:"status_map"` // 平台状态 -> queued/running/succeeded/failed，"_default" 兜底
	ErrorRules   []ErrorRule       `json:"error_rules"`
	Webhook      *WebhookConfig    `json:"webhook,omitempty"`
}

// AuthConfig 鉴权方式（代码里的有限枚举）：none / bearer / header / query / body_field。
type AuthConfig struct {
	Type   string `json:"type"`
	Secret string `json:"secret,omitempty"` // 引用 ai_secrets 的名字，永远不是明文
	Name   string `json:"name,omitempty"`   // header 的头名 / query 的参数名 / body_field 的字段名
}

// 鉴权类型。
const (
	AuthNone      = "none"
	AuthBearer    = "bearer"
	AuthHeader    = "header"
	AuthQuery     = "query"
	AuthBodyField = "body_field"
)

// RateLimitConfig 每个平台一个令牌桶 + 并发上限。
type RateLimitConfig struct {
	RPS            float64 `json:"rps"`
	MaxConcurrency int     `json:"max_concurrency"`
}

// PollConfig 轮询节奏。Duration 字段写成 "10s" 这样的字符串。
type PollConfig struct {
	FirstDelay  Duration `json:"first_delay"`
	Interval    Duration `json:"interval"`
	MaxInterval Duration `json:"max_interval"`
	Jitter      float64  `json:"jitter"` // 0–1，±比例
}

// Operations 平台的四个操作；Cancel 为 nil 表示平台不支持取消，走软取消。
type Operations struct {
	Upload *Operation `json:"upload,omitempty"`
	Submit *Operation `json:"submit,omitempty"`
	Query  *Operation `json:"query,omitempty"`
	Cancel *Operation `json:"cancel,omitempty"`
}

// Operation 一次 HTTP 调用的声明：请求模板 + 成功判定 + 字段提取。
// path / headers / query / body 里任意字符串值可以写 "${ expr }"，详见 Render。
type Operation struct {
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	Headers  map[string]string `json:"headers,omitempty"`
	Query    map[string]string `json:"query,omitempty"`
	Encoding EncodingConfig    `json:"encoding"`
	Body     any               `json:"body,omitempty"`
	Success  string            `json:"success"`           // expr 源码，返回 bool
	Extract  map[string]string `json:"extract,omitempty"` // 名字 -> expr 源码
	Timeout  Duration          `json:"timeout,omitempty"` // 默认 30s
}

// EncodingConfig 请求体编码：json / multipart（预留 form）。
type EncodingConfig struct {
	Type      string `json:"type"`
	FileField string `json:"file_field,omitempty"` // multipart 时文件所在的字段名
}

// 编码类型。
const (
	EncodingJSON      = "json"
	EncodingMultipart = "multipart"
)

// ErrorRule 错误分类规则，按顺序匹配第一条 when 为真的。
type ErrorRule struct {
	When  string `json:"when"`  // expr 源码，可用 status / resp
	Class string `json:"class"` // retryable / terminal / moderation / provider_balance
}

// WebhookConfig 回调：只用来触发立即轮询，不信任回调内容。
type WebhookConfig struct {
	Verify WebhookVerify `json:"verify"`
	TaskID string        `json:"task_id"` // expr 源码，从回调体 req 里取平台任务 id
}

// WebhookVerify 回调校验方式（MVP 只有 path_secret）。
type WebhookVerify struct {
	Type string `json:"type"`
}

// ModelConfig 模型 / 工作流：画布用户选的那一项。
type ModelConfig struct {
	Key         string         `json:"key"`
	Kind        string         `json:"kind"` // video / image / audio
	Provider    string         `json:"provider"`
	Label       string         `json:"label"`
	Hint        string         `json:"hint,omitempty"`
	Credits     int            `json:"credits"`
	Deadline    Duration       `json:"deadline"` // 默认 30m
	Enabled     bool           `json:"enabled"`
	Sort        int            `json:"sort"`
	Params      map[string]any `json:"params,omitempty"`  // 给 Provider 模板用，如 webappId
	InputSchema InputSchema    `json:"input_schema"`      // 前端渲染 + 后端校验
	Mapping     any            `json:"mapping,omitempty"` // 输入 -> 平台字段，渲染后作为 model.mapping
	Output      OutputConfig   `json:"output"`
}

// OutputConfig 从平台 outputs 中选出要转存的产物。
type OutputConfig struct {
	Select string `json:"select"` // expr 源码，输入 outputs，返回 []Output 形状的数组
	Media  string `json:"media"`  // video / image / audio
}

// Snapshot 是任务创建时冻结的 provider + model 发布版本正文，worker 只读快照，不读当前配置。
// 快照里只存 secret 的名字，执行时再解密，所以轮换 Key 能立即对进行中的任务生效。
type Snapshot struct {
	Provider ProviderConfig `json:"provider"`
	Model    ModelConfig    `json:"model"`
	// ModelRevisionID / ProviderRevisionID 记录快照来自哪个 revision，便于复现。
	ModelRevisionID    uint64 `json:"model_revision_id"`
	ProviderRevisionID uint64 `json:"provider_revision_id"`
}

// 输入字段类型：一个很小的集合。
const (
	FieldText    = "text"
	FieldNumber  = "number"
	FieldEnum    = "enum"
	FieldBoolean = "boolean"
	FieldImage   = "image"
	FieldVideo   = "video"
	FieldAudio   = "audio"
)

// InputField 是 input_schema 里的一个字段。
type InputField struct {
	Type      string       `json:"type"`
	Label     string       `json:"label"`
	Required  bool         `json:"required,omitempty"`
	Default   any          `json:"default,omitempty"`
	Min       *float64     `json:"min,omitempty"`
	Max       *float64     `json:"max,omitempty"`
	MaxLength int          `json:"max_length,omitempty"`
	Options   []EnumOption `json:"options,omitempty"`
	Port      string       `json:"port,omitempty"`     // 可由画布上游连线提供，取值 text/image/video/audio
	Advanced  bool         `json:"advanced,omitempty"` // 折叠到“高级”里
}

// EnumOption 枚举选项，Value 可以是数字或字符串。
type EnumOption struct {
	Value any    `json:"value"`
	Label string `json:"label"`
}

// InputFieldEntry 是带名字的字段，用来保留 input_schema 的书写顺序。
type InputFieldEntry struct {
	Name string
	InputField
}

// InputSchema 是有序的 name -> InputField。JSON 里写成对象，解码后保留键的书写顺序（前端按此顺序渲染）。
type InputSchema []InputFieldEntry

// Get 按名字取字段。
func (s InputSchema) Get(name string) (InputField, bool) {
	for _, e := range s {
		if e.Name == name {
			return e.InputField, true
		}
	}
	return InputField{}, false
}

// MarshalJSON 输出成 JSON 对象，键按切片顺序。
func (s InputSchema) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, e := range s {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := json.Marshal(e.Name)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(e.InputField)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON 按 token 流解码对象，保留键顺序。null 或空对象得到空 schema。
func (s *InputSchema) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*s = nil
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("input_schema 必须是对象")
	}
	out := InputSchema{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		name, ok := kt.(string)
		if !ok {
			return fmt.Errorf("input_schema 的键必须是字符串")
		}
		var f InputField
		if err := dec.Decode(&f); err != nil {
			return fmt.Errorf("input_schema.%s: %w", name, err)
		}
		out = append(out, InputFieldEntry{Name: name, InputField: f})
	}
	if _, err := dec.Token(); err != nil { // 消耗结尾的 '}'
		return err
	}
	*s = out
	return nil
}

// Duration 是 JSON 里写成 "10s" / "30m" 的时长。
type Duration time.Duration

// D 转成标准库 time.Duration。
func (d Duration) D() time.Duration { return time.Duration(d) }

// MarshalJSON 输出成 "10s" 形式的字符串。
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON 支持 "10s" 字符串；也接受数字（按秒）以方便手写。
func (d *Duration) UnmarshalJSON(data []byte) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch t := v.(type) {
	case nil:
		*d = 0
	case string:
		if t == "" {
			*d = 0
			return nil
		}
		parsed, err := time.ParseDuration(t)
		if err != nil {
			return fmt.Errorf("时长格式错误 %q，应类似 \"10s\" / \"30m\"", t)
		}
		*d = Duration(parsed)
	case float64:
		*d = Duration(time.Duration(t * float64(time.Second)))
	default:
		return fmt.Errorf("时长必须是字符串")
	}
	return nil
}
