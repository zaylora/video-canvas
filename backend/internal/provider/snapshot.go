package provider

import (
	"encoding/json"
	"fmt"

	"video-canvas/internal/model"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
)

// Snapshot 是任务创建时冻结的配置快照（generation_tasks.config_snapshot），worker 只读快照，不读当前配置。
// 冻结：模型 revision、渠道配置（不含 Key）、插件版本哈希与 meta。Key 只在执行时按渠道名解密，所以换 Key 立即对进行中的任务生效；
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
	Credits       int                   `json:"credits"`
	Deadline      modelcfg.Duration     `json:"deadline"`
	UpstreamModel string                `json:"upstream_model"`
	Params        map[string]any        `json:"params,omitempty"`
	Capabilities  modelcfg.Capabilities `json:"capabilities"`
}

// RateLimit 是渠道限流：每个渠道一个令牌桶 + 并发上限，零值表示不限。
type RateLimit struct {
	RPS            float64 `json:"rps"`
	MaxConcurrency int     `json:"max_concurrency"`
}

// ChannelSnapshot 是渠道配置（不含 Key）。
type ChannelSnapshot struct {
	Key              string         `json:"key"`
	PluginKey        string         `json:"plugin_key"`
	PluginVersionID  uint64         `json:"plugin_version_id"`
	BaseURL          string         `json:"base_url"`
	TrustedInternal  bool           `json:"trusted_internal"`
	AllowCredentials bool           `json:"allow_credentials"`
	Settings         map[string]any `json:"settings,omitempty"`
	RateLimit        RateLimit      `json:"rate_limit"`
}

// PluginSnapshot 是插件版本的标识与 meta。
type PluginSnapshot struct {
	Key     string          `json:"key"`
	Version string          `json:"version"`
	SHA256  string          `json:"sha256"`
	Meta    pluginmeta.Meta `json:"meta"`
}

// ChannelRuntime 是“一个渠道 + 它固定的插件版本”，管理端连通性检查与导入用，也是 Snapshot 的后半部分。
type ChannelRuntime struct {
	Channel ChannelSnapshot
	Plugin  PluginSnapshot
}

// RuntimeOf 由渠道行与它固定的插件版本行（需含 meta_json 与 sha256，可不含 code）组装 ChannelRuntime。
// 渠道的 settings / rate_limit 正文解析失败、插件 meta 解析失败都返回错误。
func RuntimeOf(ch *model.AIChannel, ver *model.AIPluginVersion) (*ChannelRuntime, error) {
	meta, err := pluginmeta.Parse(ver.MetaJSON)
	if err != nil {
		return nil, fmt.Errorf("插件 %s@%s：%w", ver.PluginKey, ver.Version, err)
	}
	cs := ChannelSnapshot{
		Key: ch.Key, PluginKey: ch.PluginKey, PluginVersionID: ch.PluginVersionID, BaseURL: ch.BaseURL,
		TrustedInternal: ch.TrustedInternal, AllowCredentials: ch.AllowCredentials,
	}
	if len(ch.SettingsJSON) > 0 {
		if err := json.Unmarshal(ch.SettingsJSON, &cs.Settings); err != nil {
			return nil, fmt.Errorf("渠道 %s 的 settings 不是合法的 JSON 对象：%w", ch.Key, err)
		}
	}
	if len(ch.RateLimitJSON) > 0 {
		if err := json.Unmarshal(ch.RateLimitJSON, &cs.RateLimit); err != nil {
			return nil, fmt.Errorf("渠道 %s 的 rate_limit 不是合法的 JSON 对象：%w", ch.Key, err)
		}
	}
	return &ChannelRuntime{
		Channel: cs,
		Plugin:  PluginSnapshot{Key: ver.PluginKey, Version: ver.Version, SHA256: ver.SHA256, Meta: *meta},
	}, nil
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
			Key: cfg.Key, Kind: cfg.Kind, Label: cfg.Label, Credits: cfg.Credits, Deadline: cfg.Deadline,
			UpstreamModel: upstream, Params: cfg.Params, Capabilities: cfg.Capabilities,
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
