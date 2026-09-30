// Package pluginmeta 定义协议插件 `meta` 的结构与预检规则（见 docs/design/协议插件设计.md 6.2）。
//
// 纯数据与纯函数：runner 预检时用 Validate 检查插件导出的 meta 和钩子；宿主与服务层用 Parse 读取已登记版本的 meta_json。
package pluginmeta

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// APIVersion 是宿主当前支持的插件契约版本；插件 meta.apiVersion 必须等于它。
const APIVersion = 1

// 鉴权类型：bearer / header / query 由宿主在请求描述返回后注入（插件拿不到 Key）；
// custom 由插件自己签名，需要渠道显式开启 allow_credentials 才会把 Key 放进 ctx.credentials；none 表示不需要鉴权。
const (
	AuthNone   = "none"
	AuthBearer = "bearer"
	AuthHeader = "header"
	AuthQuery  = "query"
	AuthCustom = "custom"
)

// AuthTypes 是合法的鉴权类型。
var AuthTypes = []string{AuthNone, AuthBearer, AuthHeader, AuthQuery, AuthCustom}

// endpoint 的执行方式：sync 一次请求出结果（parseSubmitResponse 必须返回 immediate）；async 提交后轮询（必须实现 query 钩子）。
const (
	ModeSync  = "sync"
	ModeAsync = "async"
)

// 限制（预检用）。
const (
	MaxPluginBytes = 512 << 10 // 插件文件大小上限 512KB
	MaxKeyLen      = 30        // meta.key 最大长度
)

// Meta 是插件导出的 meta。字段名与 JS 里的写法一致（驼峰）。
type Meta struct {
	APIVersion      int                 `json:"apiVersion"`
	Key             string              `json:"key"`     // 小写字母、数字、连字符，≤30 字符
	Name            string              `json:"name"`    // 显示名
	Version         string              `json:"version"` // semver，同一 key 下唯一
	Description     string              `json:"description,omitempty"`
	Auth            Auth                `json:"auth"`
	AllowedHosts    []string            `json:"allowedHosts,omitempty"` // 结果下载可能访问的域名（支持 *.example.com）；请求本身只能去渠道 base_url
	Endpoints       map[string]Endpoint `json:"endpoints"`              // 键是模型 kind：text / video / image / audio
	ChannelSettings SettingSchema       `json:"channelSettings,omitempty"`
	Import          *ImportMeta         `json:"import,omitempty"`
	Poll            *PollMeta           `json:"poll,omitempty"` // 异步任务的轮询节奏，缺省由宿主给默认值
}

// Auth 是鉴权声明。Name 用于 header（头名）与 query（参数名）。
type Auth struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// Endpoint 是一种生成方式的声明。
type Endpoint struct {
	Mode string `json:"mode"` // sync / async
}

// PollMeta 是轮询节奏，单位秒。缺省（0）时宿主用 首次 10 秒、间隔 5 秒、封顶 15 秒、抖动 20%。
type PollMeta struct {
	FirstDelay  float64 `json:"firstDelay,omitempty"`
	Interval    float64 `json:"interval,omitempty"`
	MaxInterval float64 `json:"maxInterval,omitempty"`
	Jitter      float64 `json:"jitter,omitempty"` // 0–1
}

// ImportMeta 声明“从渠道导入模型”的参数表单；没有参数就是列出全部。
type ImportMeta struct {
	Args SettingSchema `json:"args,omitempty"`
}

// 设置项类型。
const (
	SettingString  = "string"
	SettingNumber  = "number"
	SettingBoolean = "boolean"
	SettingEnum    = "enum"
)

// Setting 是一个渠道设置项 / 导入参数的声明，管理端据此渲染表单。
type Setting struct {
	Type        string   `json:"type"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"` // enum 的可选值
}

// SettingEntry 是带名字的设置项，用来保留书写顺序。
type SettingEntry struct {
	Name string
	Setting
}

// SettingSchema 是有序的 name -> Setting。JSON 里写成对象，解码后保留键的书写顺序（管理端按此顺序渲染表单）。
type SettingSchema []SettingEntry

// Get 按名字取设置项。
func (s SettingSchema) Get(name string) (Setting, bool) {
	for _, e := range s {
		if e.Name == name {
			return e.Setting, true
		}
	}
	return Setting{}, false
}

// MarshalJSON 输出成 JSON 对象，键按切片顺序。
func (s SettingSchema) MarshalJSON() ([]byte, error) {
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
		v, err := json.Marshal(e.Setting)
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
func (s *SettingSchema) UnmarshalJSON(data []byte) error {
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
		return fmt.Errorf("设置项声明必须是对象")
	}
	out := SettingSchema{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		name, ok := kt.(string)
		if !ok {
			return fmt.Errorf("设置项的键必须是字符串")
		}
		var f Setting
		if err := dec.Decode(&f); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, SettingEntry{Name: name, Setting: f})
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	*s = out
	return nil
}

// Endpoint 返回 kind 对应的 endpoint 声明；插件不支持这种 kind 时 ok 为 false。
func (m *Meta) Endpoint(kind string) (Endpoint, bool) {
	e, ok := m.Endpoints[kind]
	return e, ok
}

// Parse 读取已登记版本的 meta_json（宽松解码，不做预检规则校验）。
func Parse(raw []byte) (*Meta, error) {
	var m Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("解析插件 meta 失败：%w", err)
	}
	return &m, nil
}
