// 本文件：模型能力（Capabilities）：生成方式、参考素材上限、提示词上限、生成参数（有序）、文本上下文与系统提示。
// 它是画布渲染、下单校验与任务快照的唯一来源，由运营在后台手填；插件只负责对接平台，不声明能力。

package modelcfg

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// 生成方式。
const (
	OpT2V  = "t2v"  // 文生视频
	OpI2V  = "i2v"  // 图生视频
	OpOmni = "omni" // 全能参考：不限素材类型，已开启的类型都能引用
	OpT2I  = "t2i"  // 文生图
	OpI2I  = "i2i"  // 图生图
)

// opsOfKind 是各种类允许的生成方式；text / audio 没有生成方式。
var opsOfKind = map[string][]string{
	KindVideo: {OpT2V, OpI2V, OpOmni},
	KindImage: {OpT2I, OpI2I},
}

// 参考素材在任务输入里的键（值是素材 id 数组），以及它们对应的素材种类。
const (
	MediaKeyImages = "images"
	MediaKeyVideos = "videos"
	MediaKeyAudios = "audios"
)

// MediaKinds 是任务输入里素材键 -> 素材种类，书写顺序固定。
var MediaKinds = []struct{ Key, Kind string }{
	{MediaKeyImages, "image"},
	{MediaKeyVideos, "video"},
	{MediaKeyAudios, "audio"},
}

// 生成参数的取值类型。
const (
	ParamEnum    = "enum"
	ParamNumber  = "number"
	ParamBoolean = "boolean"
)

// reservedInputKeys 是任务输入里宿主占用的键，生成参数不能重名。
var reservedInputKeys = []string{"prompt", "op", "system", "max_tokens", MediaKeyImages, MediaKeyVideos, MediaKeyAudios}

// Capabilities 是模型能力。
type Capabilities struct {
	Ops     []string     `json:"ops,omitempty"`     // 生成方式，仅 video / image，至少一种
	Refs    Refs         `json:"refs"`              // 参考素材：按媒体类型配开关与上限，仅 video / image
	Prompt  PromptSpec   `json:"prompt"`            // 提示词上限
	Params  ParamSet     `json:"params,omitempty"`  // 生成参数：有序，书写顺序 = 画布参数面板的显示顺序
	Context *ContextSpec `json:"context,omitempty"` // 上下文能力，仅 text
	System  string       `json:"system,omitempty"`  // 固定系统提示，仅 text，用户看不到
}

// Refs 是三种参考素材的配置。
type Refs struct {
	Image RefSpec `json:"image"`
	Audio RefSpec `json:"audio"`
	Video RefSpec `json:"video"`
}

// RefSpec 是一种参考素材：是否接收、最多几个、单个最大多少 MB。
type RefSpec struct {
	On    bool `json:"on"`
	Max   int  `json:"max"`
	MaxMB int  `json:"max_mb"`
}

// Of 按素材种类（image / video / audio）取配置。
func (r Refs) Of(kind string) RefSpec {
	switch kind {
	case "image":
		return r.Image
	case "video":
		return r.Video
	case "audio":
		return r.Audio
	}
	return RefSpec{}
}

// PromptSpec 是提示词配置。
type PromptSpec struct {
	MaxLength int `json:"max_length"`
}

// ContextSpec 是文本模型的上下文能力（Token）。
type ContextSpec struct {
	Window int `json:"window"`
	Output int `json:"output"`
}

// ParamField 是一个生成参数。
type ParamField struct {
	Type    string `json:"type"` // enum / number / boolean
	Label   string `json:"label"`
	Open    bool   `json:"open"`              // true：出现在画布参数面板；false：不出现，按 default 发送
	Options []any  `json:"options,omitempty"` // enum：可选值（字符串或数字）
	Default any    `json:"default,omitempty"`
	Min     *int   `json:"min,omitempty"`    // number：画布上拖动滑块的范围
	Max     *int   `json:"max,omitempty"`    // number
	Step    *int   `json:"step,omitempty"`   // number：步长，省略按 1
	Unit    string `json:"unit,omitempty"`   // 展示用的单位
	Spec    bool   `json:"spec,omitempty"`   // 可作为规格价格的条件维度（enum / boolean）
	Fanout  bool   `json:"fanout,omitempty"` // 生成数量：一次提交拆成 N 个任务（enum，取值为正整数）
}

// ParamEntry 是带名字的参数，用来保留书写顺序。
type ParamEntry struct {
	Name string
	ParamField
}

// ParamSet 是有序的 name -> ParamField。JSON 里写成对象，解码后保留键的书写顺序（画布按此顺序渲染）。
type ParamSet []ParamEntry

// Get 按名字取参数。
func (s ParamSet) Get(name string) (ParamField, bool) {
	for _, e := range s {
		if e.Name == name {
			return e.ParamField, true
		}
	}
	return ParamField{}, false
}

// MarshalJSON 输出成 JSON 对象，键按切片顺序。
func (s ParamSet) MarshalJSON() ([]byte, error) {
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
		v, err := json.Marshal(e.ParamField)
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

// UnmarshalJSON 按 token 流解码对象，保留键顺序。null 或空对象得到空集合。
func (s *ParamSet) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*s = nil
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("params 必须是对象")
	}
	out := ParamSet{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		name, ok := kt.(string)
		if !ok {
			return fmt.Errorf("params 的键必须是字符串")
		}
		var f ParamField
		if err := dec.Decode(&f); err != nil {
			return fmt.Errorf("params.%s: %w", name, err)
		}
		out = append(out, ParamEntry{Name: name, ParamField: f})
	}
	if _, err := dec.Token(); err != nil { // 消耗结尾的 '}'
		return err
	}
	*s = out
	return nil
}

// Public 返回面向画布的能力：去掉用户不该看到的固定系统提示。
func (c Capabilities) Public() Capabilities {
	c.System = ""
	return c
}
