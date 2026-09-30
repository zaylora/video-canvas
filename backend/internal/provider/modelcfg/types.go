// 本文件：模型配置（ModelConfig）与输入 schema（InputSchema，保序编解码）、Duration 等基础类型。
// 协议插件设计把“平台协议”挪进了 JS 插件，模型配置里不再有映射表达式：只选渠道、上游模型名、固定参数和给用户看的输入参数。

// Package modelcfg 定义模型配置的结构、校验与输入规范化（纯函数，不访问数据库和网络）。
package modelcfg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Issue 是一条校验问题，Path 是精确到字段的 JSON 路径，例如 "channels[0].channel"。
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// FieldError 是用户输入的字段级错误，Field 是 input_schema 里的字段名。
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// 模型种类。
const (
	KindVideo = "video"
	KindImage = "image"
	KindAudio = "audio"
	KindText  = "text"
)

// Kinds 是合法的模型种类。
var Kinds = []string{KindVideo, KindImage, KindAudio, KindText}

// DefaultModelDeadline 是模型没配置 deadline 时的默认任务超时。
const DefaultModelDeadline = 30 * time.Minute

// ChannelRef 是模型绑定的一个渠道：渠道 key + 这个渠道上的上游模型名。首期 channels 只允许一个元素，数据结构预留多渠道。
type ChannelRef struct {
	Channel       string `json:"channel"`        // 渠道 key（ai_channels.key）
	UpstreamModel string `json:"upstream_model"` // 上游模型名 / 工作流 id，由插件解释
}

// ModelConfig 是模型配置（ai_config_revisions 里 target=model 的正文）：画布用户选的那一项。
type ModelConfig struct {
	Key         string         `json:"key"`
	Kind        string         `json:"kind"` // video / image / audio / text
	Label       string         `json:"label"`
	Hint        string         `json:"hint,omitempty"`
	Credits     int            `json:"credits"`
	Deadline    Duration       `json:"deadline"` // 默认 30m
	Enabled     bool           `json:"enabled"`
	Sort        int            `json:"sort"`
	Channels    []ChannelRef   `json:"channels"`         // 首期长度必须为 1
	Params      map[string]any `json:"params,omitempty"` // 固定参数，宿主只存不解释，原样交给插件
	InputSchema InputSchema    `json:"input_schema"`     // 前端渲染 + 后端校验
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
