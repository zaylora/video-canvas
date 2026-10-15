package provider

import (
	"encoding/json"
	"fmt"

	"video-canvas/internal/model"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
)

// Snapshot 是任务创建时冻结的配置快照（generation_tasks.config_snapshot），worker 只读快照，不读当前配置。
// 冻结：模型 revision、渠道配置（不含 Key）、模型选的插件版本哈希与 meta、协议设置项。Key 只在执行时按渠道名解密，所以换 Key 立即对进行中的任务生效；
// 插件代码不在快照里，按 Plugin.SHA256 / Channel.PluginVersionID 从版本表取。
type Snapshot struct {
	Model           ModelSnapshot   `json:"model"`
	Channel         ChannelSnapshot `json:"channel"`
	Plugin          PluginSnapshot  `json:"plugin"`
	ModelRevisionID uint64          `json:"model_revision_id"`
}

// ModelSnapshot 是模型配置里执行要用的部分。
type ModelSnapshot struct {
	Key           string                `json:"key"`
	Kind          string                `json:"kind"`
	Label         string                `json:"label"`
	Deadline      modelcfg.Duration     `json:"deadline"`
	UpstreamModel string                `json:"upstream_model"`
	Capabilities  modelcfg.Capabilities `json:"capabilities"`
	Pricing       modelcfg.Pricing      `json:"pricing"` // 定价规则：运营中途调价不影响进行中的任务
}

// RateLimit 是渠道限流，零值表示不限：
//   - RPS / MaxConcurrency 约束单次 HTTP 请求（令牌桶 + 同时请求数），由插件宿主在进程内执行；
//   - MaxRunning 约束“同时在上游生成的任务数”，由 worker 领取任务时在数据库里执行（跨实例生效），
//     超出的任务留在 pending（用户看到“排队中”），不调用上游。
type RateLimit struct {
	RPS            float64 `json:"rps"`
	MaxConcurrency int     `json:"max_concurrency"`
	MaxRunning     int     `json:"max_running"`
}

// ChannelSnapshot 是渠道配置（不含 Key）加上这个模型选的协议：渠道本身只有地址、请求头与限流，
// PluginKey / PluginVersionID / Settings 来自模型的 channels[0].protocol（插件钩子里仍叫 ctx.channel.settings）。
type ChannelSnapshot struct {
	Key             string            `json:"key"`
	PluginKey       string            `json:"plugin_key"`        // 模型选的插件
	PluginVersionID uint64            `json:"plugin_version_id"` // 模型固定的插件版本（取插件代码用）
	BaseURL         string            `json:"base_url"`
	Headers         map[string]string `json:"headers,omitempty"`  // 渠道自定义请求头，只加到发往 base_url 同源的请求
	Settings        map[string]any    `json:"settings,omitempty"` // 模型的协议设置项取值
	RateLimit       RateLimit         `json:"rate_limit"`
}

// PluginSnapshot 是插件版本的标识与 meta。
type PluginSnapshot struct {
	Key     string          `json:"key"`
	Version string          `json:"version"`
	SHA256  string          `json:"sha256"`
	Meta    pluginmeta.Meta `json:"meta"`
}

// ChannelRuntime 是“一个渠道 + 模型固定的插件版本 + 协议设置项”，也是 Snapshot 的后半部分。
type ChannelRuntime struct {
	Channel ChannelSnapshot
	Plugin  PluginSnapshot
}

// RuntimeOf 由渠道行、模型固定的插件版本行（需含 meta_json 与 sha256，可不含 code）与模型的协议设置项组装 ChannelRuntime。
// 渠道的 headers / rate_limit 正文解析失败、插件 meta 解析失败都返回错误。
func RuntimeOf(ch *model.AIChannel, ver *model.AIPluginVersion, settings map[string]any) (*ChannelRuntime, error) {
	meta, err := pluginmeta.Parse(ver.MetaJSON)
	if err != nil {
		return nil, fmt.Errorf("插件 %s@%s：%w", ver.PluginKey, ver.Version, err)
	}
	cs, err := channelSnapshotOf(ch)
	if err != nil {
		return nil, err
	}
	cs.PluginKey, cs.PluginVersionID, cs.Settings = ver.PluginKey, ver.ID, settings
	return &ChannelRuntime{
		Channel: *cs,
		Plugin:  PluginSnapshot{Key: ver.PluginKey, Version: ver.Version, SHA256: ver.SHA256, Meta: *meta},
	}, nil
}

// AgentRuntimeOf 组装 Agent 模型的运行时：Agent 不走插件，由 LLM 网关直接请求渠道，所以只有渠道部分，Plugin 为零值。
func AgentRuntimeOf(ch *model.AIChannel) (*ChannelRuntime, error) {
	cs, err := channelSnapshotOf(ch)
	if err != nil {
		return nil, err
	}
	return &ChannelRuntime{Channel: *cs}, nil
}

// channelSnapshotOf 由渠道行取出快照里的渠道部分（地址、请求头、限流）；协议相关的字段由调用方补。
// 渠道的 headers / rate_limit 正文解析失败返回错误。
func channelSnapshotOf(ch *model.AIChannel) (*ChannelSnapshot, error) {
	cs := ChannelSnapshot{Key: ch.Key, BaseURL: ch.BaseURL}
	if len(ch.HeadersJSON) > 0 {
		if err := json.Unmarshal(ch.HeadersJSON, &cs.Headers); err != nil {
			return nil, fmt.Errorf("渠道 %s 的 headers 不是合法的 JSON 对象：%w", ch.Key, err)
		}
	}
	if len(ch.RateLimitJSON) > 0 {
		if err := json.Unmarshal(ch.RateLimitJSON, &cs.RateLimit); err != nil {
			return nil, fmt.Errorf("渠道 %s 的 rate_limit 不是合法的 JSON 对象：%w", ch.Key, err)
		}
	}
	return &cs, nil
}

// NewSnapshot 由模型配置（取 channels[0] 的上游模型名）、渠道运行时与模型 revision 组装快照。
// cfg.Channels 为空时上游模型名为空，由调用方（已校验过配置）保证不会发生。
func NewSnapshot(cfg *modelcfg.ModelConfig, rt *ChannelRuntime, modelRevisionID uint64) *Snapshot {
	upstream := ""
	if len(cfg.Channels) > 0 {
		upstream = cfg.Channels[0].UpstreamModel
	}
	return &Snapshot{
		Model: ModelSnapshot{
			Key: cfg.Key, Kind: cfg.Kind, Label: cfg.Label, Deadline: cfg.Deadline,
			UpstreamModel: upstream, Capabilities: cfg.Capabilities, Pricing: cfg.Pricing,
		},
		Channel:         rt.Channel,
		Plugin:          rt.Plugin,
		ModelRevisionID: modelRevisionID,
	}
}

// Runtime 取快照里的渠道运行时。
func (s *Snapshot) Runtime() *ChannelRuntime {
	return &ChannelRuntime{Channel: s.Channel, Plugin: s.Plugin}
}
