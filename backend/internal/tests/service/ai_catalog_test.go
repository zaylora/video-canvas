package service_test

import (
	"testing"

	. "video-canvas/internal/service"
)

// 这组用例与前端 web/src/tests/utils/admin/health.test.ts 逐条对应：状态规则从 TS 搬到 Go，行为必须一致。

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
	c := CatalogChannel{Key: "c1", Name: "主线", PluginKey: "p", PluginVersionID: 1, PluginVersion: "1.0.0", Enabled: true, SecretSet: true}
	if extra != nil {
		extra(&c)
	}
	return c
}

func catModel(extra func(*CatalogModel)) CatalogModel {
	m := CatalogModel{Key: "m1", Channel: "c1", Enabled: true}
	if extra != nil {
		extra(&m)
	}
	return m
}

func TestChannelHealthRules(t *testing.T) {
	p := catPlugin(nil)
	t.Run("渠道停用", func(t *testing.T) {
		h := ChannelHealthOf(catChannel(func(c *CatalogChannel) { c.Enabled = false }), []CatalogPlugin{p})
		if h.Tone != "off" || h.Label != "已停用" || h.Reason != "渠道已停用" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("插件不存在", func(t *testing.T) {
		h := ChannelHealthOf(catChannel(nil), nil)
		if h.Tone != "bad" || h.Label != "不可用" || h.Reason != "插件 p 不存在" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("插件停用", func(t *testing.T) {
		h := ChannelHealthOf(catChannel(nil), []CatalogPlugin{catPlugin(func(p *CatalogPlugin) { p.Enabled = false })})
		if h.Tone != "bad" || h.Reason != "插件「插件P」已停用" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("需要 Key 但没设", func(t *testing.T) {
		h := ChannelHealthOf(catChannel(func(c *CatalogChannel) { c.SecretSet = false }), []CatalogPlugin{p})
		if h.Tone != "warn" || h.Label != "未设 Key" || h.Reason != "还没有设置 Key" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("正常", func(t *testing.T) {
		h := ChannelHealthOf(catChannel(nil), []CatalogPlugin{p})
		if h.Tone != "ok" || h.Label != "可用" || h.Reason != "" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("插件不需要鉴权时不断言缺 Key", func(t *testing.T) {
		none := catPlugin(func(p *CatalogPlugin) { p.Versions[0].AuthType = "none" })
		if h := ChannelHealthOf(catChannel(func(c *CatalogChannel) { c.SecretSet = false }), []CatalogPlugin{none}); h.Tone != "ok" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("读不到插件版本时不断言缺 Key", func(t *testing.T) {
		unknown := catPlugin(func(p *CatalogPlugin) { p.Versions = nil })
		if h := ChannelHealthOf(catChannel(func(c *CatalogChannel) { c.SecretSet = false }), []CatalogPlugin{unknown}); h.Tone != "ok" {
			t.Fatalf("实际 %+v", h)
		}
	})
}

func TestModelHealthRules(t *testing.T) {
	t.Run("插件停用会传到已上架的模型上：不可用", func(t *testing.T) {
		off := catPlugin(func(p *CatalogPlugin) { p.Enabled = false })
		h := ModelHealthOf(catModel(nil), []CatalogChannel{catChannel(nil)}, []CatalogPlugin{off})
		if h.Status != "broken" || h.Reason != "渠道「主线」插件「插件P」已停用" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("没启用的模型是未上线，但仍带上渠道问题", func(t *testing.T) {
		noKey := catChannel(func(c *CatalogChannel) { c.SecretSet = false })
		h := ModelHealthOf(catModel(func(m *CatalogModel) { m.Enabled = false }), []CatalogChannel{noKey}, []CatalogPlugin{catPlugin(nil)})
		if h.Status != "offline" || h.Reason != "渠道「主线」还没有设置 Key" {
			t.Fatalf("实际 %+v", h)
		}
		h = ModelHealthOf(catModel(func(m *CatalogModel) { m.Enabled = false }), []CatalogChannel{catChannel(nil)}, []CatalogPlugin{catPlugin(nil)})
		if h.Status != "offline" || h.Reason != "" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("渠道不存在", func(t *testing.T) {
		h := ModelHealthOf(catModel(func(m *CatalogModel) { m.Channel = "x" }), nil, []CatalogPlugin{catPlugin(nil)})
		if h.Status != "broken" || h.Reason != "渠道 x 不存在" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("没选渠道", func(t *testing.T) {
		h := ModelHealthOf(catModel(func(m *CatalogModel) { m.Channel = "" }), nil, nil)
		if h.Status != "broken" || h.Reason != "还没有选择渠道" {
			t.Fatalf("实际 %+v", h)
		}
	})
	t.Run("一切正常为在线", func(t *testing.T) {
		h := ModelHealthOf(catModel(nil), []CatalogChannel{catChannel(nil)}, []CatalogPlugin{catPlugin(nil)})
		if h.Status != "online" || h.Reason != "" {
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
	t.Run("agent 要求 bearer，其余看 endpoints", func(t *testing.T) {
		v := CatalogVersion{AuthType: "bearer", Kinds: []string{"video"}}
		if !MetaSupportsKind(v, "agent") || !MetaSupportsKind(v, "video") || MetaSupportsKind(v, "image") {
			t.Fatalf("bearer 版本：agent、video 支持，image 不支持")
		}
		if MetaSupportsKind(CatalogVersion{AuthType: "header", Kinds: []string{"video"}}, "agent") {
			t.Fatalf("非 bearer 不支持 agent")
		}
	})
}
