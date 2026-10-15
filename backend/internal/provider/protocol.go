package provider

import (
	"fmt"

	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
)

// ValidateSettings 按这个运行时的插件版本声明（meta.channelSettings）校验协议设置项并补全默认值。
// 协议设置项由模型保存（channels[0].protocol.settings），运行时装配和启用检查都用它，保证标准一致。
func (rt *ChannelRuntime) ValidateSettings(values map[string]any) (map[string]any, []modelcfg.Issue) {
	return pluginmeta.ValidateSettingValues(rt.Plugin.Meta.ChannelSettings, values)
}

// CarrySettings 把模型原有的协议设置项带到插件的另一个版本（metaJSON 是目标版本的 meta_json）：
// 目标版本不再声明的项丢弃，其余按声明校验并补默认值（必填项缺失返回问题列表）。meta 解析失败返回 error。
func CarrySettings(metaJSON []byte, old map[string]any) (map[string]any, []modelcfg.Issue, error) {
	meta, err := pluginmeta.Parse(metaJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("解析目标版本的 meta 失败：%w", err)
	}
	kept := make(map[string]any, len(old))
	for name, v := range old {
		if _, declared := meta.ChannelSettings.Get(name); declared {
			kept[name] = v
		}
	}
	settings, issues := pluginmeta.ValidateSettingValues(meta.ChannelSettings, kept)
	return settings, issues, nil
}
