package service_test

import (
	"context"
	"testing"

	. "video-canvas/internal/service"
)

// 待处理清单的规则与前端 web/src/tests/utils/admin/health.test.ts 的 adminTodos 用例逐条对应。

func todoIDs(todos []OverviewTodo) []string {
	out := make([]string, 0, len(todos))
	for _, t := range todos {
		out = append(out, t.ID)
	}
	return out
}

func TestOverviewTodos_OrderAndText(t *testing.T) {
	plugins := []CatalogPlugin{
		catPlugin(func(p *CatalogPlugin) {
			p.Versions = []CatalogVersion{
				{ID: 2, Version: "1.1.0", AuthType: "bearer", Kinds: []string{"video"}},
				{ID: 1, Version: "1.0.0", AuthType: "bearer", Kinds: []string{"video"}},
			}
		}),
		catPlugin(func(p *CatalogPlugin) { p.Key, p.Name, p.Enabled = "q", "插件Q", false }),
	}
	channels := []CatalogChannel{
		catChannel(func(c *CatalogChannel) { c.SecretSet = false }),
		catChannel(func(c *CatalogChannel) { c.Key, c.Name, c.PluginKey = "c2", "Q线", "q" }),
	}
	models := []CatalogModel{catModel(nil), catModel(func(m *CatalogModel) { m.Key, m.Channel = "m2", "c2" })}

	todos := OverviewTodos(models, channels, plugins)
	if got := todoIDs(todos); len(got) != 3 || got[0] != "plugin-off:q" || got[1] != "key:c1" || got[2] != "upgrade:p" {
		t.Fatalf("应按严重度排：插件停用 > 缺 Key > 可升级，实际 %v", got)
	}
	if todos[0].Text != "插件「插件Q」已停用，1 个渠道、1 个上架模型实际不可用" || todos[0].Tone != "bad" || todos[0].Action.Kind != "open-plugin" || todos[0].Action.PluginKey != "q" {
		t.Errorf("plugin-off：%+v", todos[0])
	}
	if todos[1].Text != "渠道「主线」还没设置 Key，1 个模型因此无法上线" || todos[1].Tone != "warn" || todos[1].Action.Kind != "set-key" || todos[1].Action.ChannelKey != "c1" || todos[1].Action.ChannelName != "主线" {
		t.Errorf("key：%+v", todos[1])
	}
	if todos[2].Text != "插件P v1.1.0 可用，1 个渠道还在旧版本" || todos[2].Tone != "info" || todos[2].Action.Kind != "upgrade-plugin" || todos[2].Action.PluginKey != "p" {
		t.Errorf("upgrade：%+v", todos[2])
	}
}

func TestOverviewTodos_ChannelOffAndNothing(t *testing.T) {
	p := []CatalogPlugin{catPlugin(nil)}
	off := catChannel(func(c *CatalogChannel) { c.Enabled = false })

	if got := todoIDs(OverviewTodos([]CatalogModel{catModel(nil)}, []CatalogChannel{off}, p)); len(got) != 1 || got[0] != "channel-off:c1" {
		t.Errorf("停用的渠道下还有上架模型才提示：%v", got)
	}
	todos := OverviewTodos([]CatalogModel{catModel(nil)}, []CatalogChannel{off}, p)
	if todos[0].Text != "渠道「主线」已停用，1 个上架模型实际不可用" || todos[0].Action.Kind != "open-channel" {
		t.Errorf("channel-off：%+v", todos[0])
	}
	down := catModel(func(m *CatalogModel) { m.Enabled = false })
	if got := OverviewTodos([]CatalogModel{down}, []CatalogChannel{off}, p); len(got) != 0 {
		t.Errorf("模型没上架就不提示：%v", got)
	}
	if got := OverviewTodos([]CatalogModel{catModel(nil)}, []CatalogChannel{catChannel(nil)}, p); len(got) != 0 {
		t.Errorf("全部正常应为空：%v", got)
	}
}

func TestOverviewTodos_NoPinnedChannelsNoTodo(t *testing.T) {
	off := catPlugin(func(p *CatalogPlugin) { p.Enabled = false })
	if got := OverviewTodos(nil, nil, []CatalogPlugin{off}); len(got) != 0 {
		t.Errorf("停用的插件没有渠道在用就不提示：%v", got)
	}
}

func TestCatalog_Overview(t *testing.T) {
	ctx := context.Background()
	e := newCatEnv(t)
	e.channel(t, "a-main", "主线", "1.0.0", true, true)      // 正常，kling 可升级
	e.channel(t, "b-nokey", "没设Key", "1.1.0", true, false) // 缺 Key
	e.channel(t, "c-off", "已停用", "1.1.0", false, true)     // 停用
	e.model(t, "m1", "a-main", true)                       // 在线
	e.model(t, "m2", "b-nokey", true)                      // 不可用（缺 Key）
	e.model(t, "m3", "a-main", false)                      // 未上线

	ov, err := e.cat.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Plugins.Total != 2 || ov.Plugins.Usable != 2 {
		t.Errorf("插件：%+v", ov.Plugins)
	}
	if ov.Channels.Total != 3 || ov.Channels.Usable != 1 || ov.Channels.NoKey != 1 {
		t.Errorf("渠道：总数 3、可用 1、未设 Key 1，实际 %+v", ov.Channels)
	}
	if ov.Models.Total != 3 || ov.Models.Online != 1 || ov.Models.Broken != 1 {
		t.Errorf("模型：总数 3、在线 1、不可用 1，实际 %+v", ov.Models)
	}
	if got := todoIDs(ov.Todos); len(got) != 2 || got[0] != "key:b-nokey" || got[1] != "upgrade:kling" {
		t.Errorf("没有插件停用，最严重的是缺 Key，其次是可升级：%v", got)
	}
	if len(ov.ChannelBriefs) != 3 || ov.ChannelBriefs[0].Key != "a-main" || ov.ChannelBriefs[0].Name != "主线" || ov.ChannelBriefs[0].Health.Tone != "ok" {
		t.Errorf("渠道负载卡片要的精简渠道：%+v", ov.ChannelBriefs)
	}
	if ov.Todos == nil || ov.ChannelBriefs == nil {
		t.Errorf("切片不能是 nil，JSON 要是 []")
	}
}

func TestCatalog_OverviewEmpty(t *testing.T) {
	ov, err := newCatEnv(t).cat.Overview(context.Background())
	if err != nil || ov.Todos == nil || ov.ChannelBriefs == nil || ov.Models.Total != 0 {
		t.Fatalf("没有渠道和模型时也应返回空数组：%+v err=%v", ov, err)
	}
}
