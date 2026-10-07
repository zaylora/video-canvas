// 本文件：模型配置（ModelConfig）、Duration 等基础类型；模型能力（Capabilities，含保序的生成参数）在 capabilities.go。
// 协议插件设计把“平台协议”挪进了 JS 插件，模型配置里不再有映射表达式：只选渠道、上游模型名、固定参数和模型能力。

// Package modelcfg 定义模型配置的结构、校验与输入规范化（纯函数，不访问数据库和网络）。
package modelcfg

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Issue 是一条校验问题，Path 是精确到字段的 JSON 路径，例如 "channels[0].channel"。
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// FieldError 是用户输入的字段级错误，Field 是任务输入里的键（prompt / op / images / 生成参数名）。
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// JoinFieldErrors 把字段级错误拼成面向用户的一句话。
func JoinFieldErrors(errs []FieldError) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		if e.Field == "" {
			parts = append(parts, e.Message)
			continue
		}
		parts = append(parts, e.Field+"："+e.Message)
	}
	return strings.Join(parts, "；")
}

// 模型种类。
const (
	KindVideo = "video"
	KindImage = "image"
	KindAudio = "audio"
	KindText  = "text"
	// KindAgent 是画布 Agent 用的对话大模型：流式输出 + 工具调用。它不走插件（插件钩子是同步的、不能联网），
	// 由 Go 里的 LLM 网关直接请求渠道的 OpenAI 兼容接口，所以没有生成方式和生成参数，只按 Token 计费。
	KindAgent = "agent"
)

// Kinds 是合法的模型种类。
var Kinds = []string{KindVideo, KindImage, KindAudio, KindText, KindAgent}

// DefaultModelDeadline 是模型没配置 deadline 时的默认任务超时。
const DefaultModelDeadline = 30 * time.Minute

// ChannelRef 是模型绑定的一个渠道：渠道 key + 这个渠道上的上游模型名。首期 channels 只允许一个元素，数据结构预留多渠道。
type ChannelRef struct {
	Channel       string `json:"channel"`        // 渠道 key（ai_channels.key）
	UpstreamModel string `json:"upstream_model"` // 上游模型名 / 工作流 id，由插件解释
}

// ModelConfig 是模型配置（ai_config_revisions 里 target=model 的正文）：画布用户选的那一项。
type ModelConfig struct {
	Key          string         `json:"key"`
	Kind         string         `json:"kind"` // video / image / audio / text / agent
	Label        string         `json:"label"`
	Hint         string         `json:"hint,omitempty"`   // 模型描述，最多 maxHintLen 个字符
	Vendor       string         `json:"vendor,omitempty"` // 厂商 slug（小写字母/数字/连字符），前端据此显示 logo，可空
	Tags         []string       `json:"tags,omitempty"`   // 展示标签，最多 maxTags 个，每个最多 maxTagLen 个字符
	Deadline     Duration       `json:"deadline"`         // 默认 30m
	Enabled      bool           `json:"enabled"`
	Sort         int            `json:"sort"`
	Channels     []ChannelRef   `json:"channels"`         // 首期长度必须为 1
	Params       map[string]any `json:"params,omitempty"` // 固定参数，宿主只存不解释，原样交给插件
	Capabilities Capabilities   `json:"capabilities"`     // 模型能力：前端渲染 + 后端校验的唯一来源
	Pricing      Pricing        `json:"pricing"`          // 定价：计费方式、默认价、规格价格与成本
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
