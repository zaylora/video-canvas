package service_test

import (
	"testing"

	. "video-canvas/internal/service"
)

// 这组用例与前端 web/src/tests/utils/admin/health.test.ts 逐条对应：状态规则从 TS 搬到 Go，行为必须一致。
// 渠道只管连接信息，协议在模型里选：渠道健康只看启停与 Key；模型状态要同时看渠道和它选的协议插件。

func catPlugin(extra func(*CatalogPlugin)) CatalogPlugin {
	p := CatalogPlugin{Key: "p", Name: "插件P", Enabled: true, Versions: []CatalogVersion{
		{ID: 1, Version: "1.0.0", AuthType: "bearer", Kinds: []string{"video"}},
	}}
	if extra != nil {
		extra(&p)
	}
	return p
}

func catChannel(extra func(*CatalogChannel)) CatalogChannel {
	c := CatalogChannel{Key: "c1", Name: "主线", Enabled: true, SecretSet: true}
	if extra != nil {
		extra(&c)
	}
	return c
}

// catModel 是一个选好协议（插件 p@1.0.0、视频）、已上架的模型。
func catModel(extra func(*CatalogModel)) CatalogModel {
	m := CatalogModel{Key: "m1", Kind: "video", Channel: "c1", Enabled: true, Plugin: "p", PluginVersion: "1.0.0"}
	if extra != nil {
		extra(&m)
	}
	return m
}

func TestChannelHealthRules(t *testing.T) {
	t.Run("渠道停用", func(t *testing.T) {
		h := ChannelHealthOf(catChannel(func(c *CatalogChannel) { c.Enabled = false }))
		if h.Tone != "off" || h.Label != "已停用" || h.Reason != "渠道已停用" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("没设 Key", func(t *testing.T) {
		h := ChannelHealthOf(catChannel(func(c *CatalogChannel) { c.SecretSet = false }))
		if h.Tone != "warn" || h.Label != "未设 Key" || h.Reason != "还没有设置 Key" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("停用优先于缺 Key", func(t *testing.T) {
		h := ChannelHealthOf(catChannel(func(c *CatalogChannel) { c.Enabled, c.SecretSet = false, false }))
		if h.Tone != "off" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("正常", func(t *testing.T) {
		h := ChannelHealthOf(catChannel(nil))
		if h.Tone != "ok" || h.Label != "可用" || h.Reason != "" {
			t.Fatalf("实际 %+v", h)
		}
	})
}

func TestModelHealthRules(t *testing.T) {
	ch := []CatalogChannel{catChannel(nil)}
	pl := []CatalogPlugin{catPlugin(nil)}

	t.Run("一切正常为在线", func(t *testing.T) {
		h := ModelHealthOf(catModel(nil), ch, pl)
		if h.Status != "online" || h.Reason != "" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("协议插件停用会传到已上架的模型上：不可用", func(t *testing.T) {
		off := catPlugin(func(p *CatalogPlugin) { p.Enabled = false })
		h := ModelHealthOf(catModel(nil), ch, []CatalogPlugin{off})
		if h.Status != "broken" || h.Reason != "协议插件「插件P」已停用" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("协议插件不存在", func(t *testing.T) {
		h := ModelHealthOf(catModel(nil), ch, nil)
		if h.Status != "broken" || h.Reason != "协议插件 p 不存在" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("固定的插件版本不存在", func(t *testing.T) {
		h := ModelHealthOf(catModel(func(m *CatalogModel) { m.PluginVersion = "9.9.9" }), ch, pl)
		if h.Status != "broken" || h.Reason != "固定的插件版本 p@9.9.9 不存在" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("协议不支持模型的能力", func(t *testing.T) {
		h := ModelHealthOf(catModel(func(m *CatalogModel) { m.Kind = "image" }), ch, pl)
		if h.Status != "broken" || h.Reason != "协议「插件P」不支持image类型的模型" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("渠道停用 / 不存在 / 没选渠道", func(t *testing.T) {
		off := catChannel(func(c *CatalogChannel) { c.Enabled = false })
		if h := ModelHealthOf(catModel(nil), []CatalogChannel{off}, pl); h.Status != "broken" || h.Reason != "渠道「主线」已停用" {
			t.Fatalf("渠道停用：%+v", h)
		}
		if h := ModelHealthOf(catModel(func(m *CatalogModel) { m.Channel = "x" }), nil, pl); h.Status != "broken" || h.Reason != "渠道 x 不存在" {
			t.Fatalf("渠道不存在：%+v", h)
		}
		if h := ModelHealthOf(catModel(func(m *CatalogModel) { m.Channel = "" }), nil, nil); h.Status != "broken" || h.Reason != "还没有选择渠道" {
			t.Fatalf("没选渠道：%+v", h)
		}
	})
	t.Run("缺 Key：协议要鉴权时才算问题，协议不需要鉴权时不算", func(t *testing.T) {
		noKey := []CatalogChannel{catChannel(func(c *CatalogChannel) { c.SecretSet = false })}
		if h := ModelHealthOf(catModel(nil), noKey, pl); h.Status != "broken" || h.Reason != "渠道「主线」还没有设置 Key" {
			t.Fatalf("要鉴权：%+v", h)
		}
		none := []CatalogPlugin{catPlugin(func(p *CatalogPlugin) { p.Versions[0].AuthType = "none" })}
		if h := ModelHealthOf(catModel(nil), noKey, none); h.Status != "online" {
			t.Fatalf("不需要鉴权：%+v", h)
		}
		unknown := []CatalogPlugin{catPlugin(func(p *CatalogPlugin) { p.Versions[0].AuthType = "" })}
		if h := ModelHealthOf(catModel(nil), noKey, unknown); h.Status != "online" {
			t.Fatalf("读不到 meta 时不断言缺 Key：%+v", h)
		}
	})
	t.Run("没启用的模型是未上线，但仍带上渠道 / 协议问题", func(t *testing.T) {
		noKey := []CatalogChannel{catChannel(func(c *CatalogChannel) { c.SecretSet = false })}
		h := ModelHealthOf(catModel(func(m *CatalogModel) { m.Enabled = false }), noKey, pl)
		if h.Status != "offline" || h.Reason != "渠道「主线」还没有设置 Key" {
			t.Fatalf("实际 %+v", h)
		}
		h = ModelHealthOf(catModel(func(m *CatalogModel) { m.Enabled = false }), ch, pl)
		if h.Status != "offline" || h.Reason != "" {
			t.Fatalf("实际 %+v", h)
		}
	})
}

func TestModelHealthPending(t *testing.T) {
	ch := []CatalogChannel{catChannel(nil)}
	pl := []CatalogPlugin{catPlugin(nil)}
	pending := func(extra func(*CatalogModel)) CatalogModel {
		return catModel(func(m *CatalogModel) {
			m.Enabled, m.Kind, m.Plugin, m.PluginVersion = false, "", "", ""
			if extra != nil {
				extra(m)
			}
		})
	}
	t.Run("拉取得到的模型：没选能力，待配置", func(t *testing.T) {
		h := ModelHealthOf(pending(nil), ch, pl)
		if h.Status != "pending" || h.Reason != "还没有选择能力" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("选了能力没选协议，仍是待配置", func(t *testing.T) {
		h := ModelHealthOf(pending(func(m *CatalogModel) { m.Kind = "video" }), ch, pl)
		if h.Status != "pending" || h.Reason != "还没有选择调用协议" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("Agent 模型不选协议，不算待配置", func(t *testing.T) {
		h := ModelHealthOf(pending(func(m *CatalogModel) { m.Kind = "agent" }), ch, pl)
		if h.Status != "offline" || h.Reason != "" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("Agent 模型：渠道没设 Key 才有问题（走网关，不看插件）", func(t *testing.T) {
		noKey := []CatalogChannel{catChannel(func(c *CatalogChannel) { c.SecretSet = false })}
		h := ModelHealthOf(catModel(func(m *CatalogModel) { m.Kind, m.Plugin, m.PluginVersion = "agent", "", "" }), noKey, nil)
		if h.Status != "broken" || h.Reason != "渠道「主线」还没有设置 Key" {
			t.Fatalf("实际 %+v", h)
		}
		h = ModelHealthOf(catModel(func(m *CatalogModel) { m.Kind, m.Plugin, m.PluginVersion = "agent", "", "" }), ch, nil)
		if h.Status != "online" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("渠道有问题时，待配置模型也带上渠道的原因", func(t *testing.T) {
		off := []CatalogChannel{catChannel(func(c *CatalogChannel) { c.Enabled = false })}
		h := ModelHealthOf(pending(nil), off, pl)
		if h.Status != "pending" || h.Reason != "渠道「主线」已停用" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("已启用却没选协议（数据不一致）按不可用处理，而不是待配置", func(t *testing.T) {
		h := ModelHealthOf(pending(func(m *CatalogModel) { m.Enabled, m.Kind = true, "video" }), ch, pl)
		if h.Status != "broken" || h.Reason != "还没有选择调用协议" {
			t.Fatalf("实际 %+v", h)
		}
	})
}

func TestAvailableUpgradeAndKinds(t *testing.T) {
	p := catPlugin(func(p *CatalogPlugin) {
		p.Versions = []CatalogVersion{
			{ID: 3, Version: "1.10.0", AuthType: "bearer"},
			{ID: 2, Version: "1.2.0", AuthType: "bearer"},
			{ID: 1, Version: "1.0.0", AuthType: "bearer"},
		}
	})
	t.Run("取比当前高的最高版本，按数值比较（1.10 > 1.2）", func(t *testing.T) {
		if got := AvailableUpgrade(p, "1.0.0"); got != "1.10.0" {
			t.Fatalf("实际 %q", got)
		}
	})
	t.Run("已是最新或更高没有更新", func(t *testing.T) {
		if got := AvailableUpgrade(p, "1.10.0"); got != "" {
			t.Fatalf("实际 %q", got)
		}
		if got := AvailableUpgrade(p, "2.0.0"); got != "" {
			t.Fatalf("实际 %q", got)
		}
	})
	t.Run("预发布后缀忽略", func(t *testing.T) {
		if got := AvailableUpgrade(p, "1.2.0-beta"); got != "1.10.0" {
			t.Fatalf("实际 %q", got)
		}
	})
	t.Run("看 endpoints；agent 不走插件，任何插件都不承接", func(t *testing.T) {
		v := CatalogVersion{AuthType: "bearer", Kinds: []string{"video"}}
		if !MetaSupportsKind(v, "video") || MetaSupportsKind(v, "image") {
			t.Fatalf("video 支持，image 不支持")
		}
		if MetaSupportsKind(v, "agent") || MetaSupportsKind(CatalogVersion{AuthType: "bearer", Kinds: []string{"agent"}}, "agent") {
			t.Fatalf("agent 不由插件承接")
		}
	})
}
