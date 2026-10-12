package service_test

import (
	"context"
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/service"
)

// catEnv 在渠道服务的测试环境上搭一个目录服务：真实的渠道 / 插件 / 模型服务 + 内存仓储。
type catEnv struct {
	*achEnv
	cat *AICatalogService
}

func newCatEnv(t *testing.T) *catEnv {
	t.Helper()
	e := newAchEnv(t)
	plugins := NewAIPluginService(e.repo, e.repo, e.repo, nil, e.notifier)
	return &catEnv{achEnv: e, cat: NewAICatalogService(e.svc, plugins, e.cfg)}
}

// channel 建一个渠道；withKey 为 true 时设置 Key。
func (e *catEnv) channel(t *testing.T, key, name, version string, enabled, withKey bool) {
	t.Helper()
	in := createInput()
	in.Key, in.Name, in.PluginVersion, in.Enabled = key, name, version, enabled
	if _, err := e.svc.Create(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if withKey {
		if err := e.svc.SetSecret(context.Background(), 1, key, "sk-"+key); err != nil {
			t.Fatal(err)
		}
	}
}

// model 建一个模型并按需上架。
func (e *catEnv) model(t *testing.T, key, channel string, online bool) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.cfg.SaveModel(ctx, ModelSaveInput{Create: true, Body: aicModelBody(key, channel, ""), AdminID: 1}); err != nil {
		t.Fatal(err)
	}
	if online {
		if err := e.repo.SetModelEnabled(ctx, key, true); err != nil {
			t.Fatal(err)
		}
	}
}

func chanReq(q, kind, plugin, status string, page, size int) *model.ListChannelReq {
	return &model.ListChannelReq{AdminListReq: model.AdminListReq{Q: q, Page: page, PageSize: size}, Kind: kind, Plugin: plugin, Status: status}
}

func TestCatalog_ListChannels(t *testing.T) {
	ctx := context.Background()
	e := newCatEnv(t)
	e.channel(t, "a-main", "主线", "1.0.0", true, true)      // kling 旧版本（1.1.0 可升级），正常
	e.channel(t, "b-nokey", "没设Key", "1.1.0", true, false) // 缺 Key
	e.channel(t, "c-off", "已停用", "1.1.0", false, true)     // 停用
	e.model(t, "m1", "a-main", true)
	e.model(t, "m2", "a-main", false)
	e.model(t, "m3", "c-off", true)

	t.Run("每行带健康状态、可升级版本、能力和模型数", func(t *testing.T) {
		page, err := e.cat.ListChannels(ctx, chanReq("", "", "", "", 0, 0))
		if err != nil || page.Total != 3 || len(page.List) != 3 {
			t.Fatalf("实际 %+v err=%v", page, err)
		}
		a, b, c := page.List[0], page.List[1], page.List[2]
		if a.Key != "a-main" || a.Health.Tone != "ok" || a.UpgradeTo != "1.1.0" || a.ModelCount != 2 || a.OnlineModelCount != 1 {
			t.Errorf("a-main 不对：%+v", a)
		}
		if len(a.Kinds) != 3 || a.Kinds[0] != "text" || a.Kinds[1] != "video" || a.Kinds[2] != "agent" {
			t.Errorf("kling 声明了 text / video，且鉴权是 bearer 所以也能跑 agent：%v", a.Kinds)
		}
		if b.Health.Tone != "warn" || b.Health.Label != "未设 Key" || b.UpgradeTo != "" {
			t.Errorf("b-nokey 不对：%+v", b)
		}
		if c.Health.Tone != "off" || c.ModelCount != 1 || c.OnlineModelCount != 1 {
			t.Errorf("c-off 不对：%+v", c)
		}
	})
	t.Run("搜索匹配名称 / key / 地址", func(t *testing.T) {
		page, _ := e.cat.ListChannels(ctx, chanReq("没设", "", "", "", 0, 0))
		if page.Total != 1 || page.List[0].Key != "b-nokey" {
			t.Fatalf("实际 %+v", page)
		}
		page, _ = e.cat.ListChannels(ctx, chanReq("gw.example.com", "", "", "", 0, 0))
		if page.Total != 3 {
			t.Fatalf("地址也能搜：%+v", page)
		}
	})
	t.Run("按状态筛选：ok / warn / off / upgrade", func(t *testing.T) {
		for status, want := range map[string]string{"ok": "a-main", "warn": "b-nokey", "off": "c-off", "upgrade": "a-main"} {
			page, _ := e.cat.ListChannels(ctx, chanReq("", "", "", status, 0, 0))
			if page.Total != 1 || page.List[0].Key != want {
				t.Errorf("status=%s 期望 %s，实际 %+v", status, want, page)
			}
		}
	})
	t.Run("按能力和插件筛选", func(t *testing.T) {
		if page, _ := e.cat.ListChannels(ctx, chanReq("", "video", "kling", "", 0, 0)); page.Total != 3 {
			t.Errorf("kling 支持 video：%+v", page)
		}
		if page, _ := e.cat.ListChannels(ctx, chanReq("", "image", "", "", 0, 0)); page.Total != 0 {
			t.Errorf("没有渠道支持 image：%+v", page)
		}
		if page, _ := e.cat.ListChannels(ctx, chanReq("", "", "loose", "", 0, 0)); page.Total != 0 {
			t.Errorf("没有渠道用 loose 插件：%+v", page)
		}
	})
	t.Run("能力筛选：读不到插件版本的渠道不被排除，免得整张表被筛空", func(t *testing.T) {
		e.repo.Channels["z-orphan"] = &model.AIChannel{Key: "z-orphan", Name: "孤儿", PluginKey: "gone", PluginVersionID: 999, Enabled: true}
		defer delete(e.repo.Channels, "z-orphan")
		page, _ := e.cat.ListChannels(ctx, chanReq("", "image", "", "", 0, 0))
		if page.Total != 1 || page.List[0].Key != "z-orphan" {
			t.Fatalf("没有渠道声明 image，但读不到版本的孤儿渠道要保留：%+v", page)
		}
	})
	t.Run("分页", func(t *testing.T) {
		page, _ := e.cat.ListChannels(ctx, chanReq("", "", "", "", 2, 2))
		if page.Total != 3 || len(page.List) != 1 || page.Page != 2 || page.PageSize != 2 {
			t.Fatalf("实际 %+v", page)
		}
	})
	t.Run("没有命中返回空数组", func(t *testing.T) {
		page, _ := e.cat.ListChannels(ctx, chanReq("没有这个", "", "", "", 0, 0))
		if page.Total != 0 || page.List == nil {
			t.Fatalf("实际 %+v", page)
		}
	})
}

func TestCatalog_ChannelOptions(t *testing.T) {
	ctx := context.Background()
	e := newCatEnv(t)
	e.channel(t, "a-main", "主线", "1.0.0", true, true)
	e.channel(t, "b-nokey", "没设Key", "1.1.0", true, false)

	opts, err := e.cat.ChannelOptions(ctx)
	if err != nil || len(opts) != 2 {
		t.Fatalf("应返回全部渠道，不分页：%+v err=%v", opts, err)
	}
	if opts[0].Key != "a-main" || opts[0].Name != "主线" || opts[0].PluginKey != "kling" || !opts[0].Enabled || opts[0].Health.Tone != "ok" || len(opts[0].Kinds) != 3 {
		t.Errorf("字段不对：%+v", opts[0])
	}
	if opts[1].Health.Tone != "warn" || opts[1].SecretSet {
		t.Errorf("b-nokey 应是未设 Key：%+v", opts[1])
	}
	if opts[0].BaseURL == "" || opts[0].PluginVersionID == 0 || opts[0].Settings == nil {
		t.Errorf("options 带完整渠道视图（概览 / 弹窗要用 base_url、plugin_version_id、settings 等）：%+v", opts[0])
	}
}

func plugReq(q, kind, source, status string, page, size int) *model.ListPluginReq {
	return &model.ListPluginReq{AdminListReq: model.AdminListReq{Q: q, Page: page, PageSize: size}, Kind: kind, Source: source, Status: status}
}

func TestCatalog_ListPlugins(t *testing.T) {
	ctx := context.Background()
	e := newCatEnv(t)
	// kling 有 1.0.0 / 1.1.0 两个版本（bearer，video+text）；loose 只有 1.0.0（鉴权 none，video）
	e.channel(t, "a-old", "旧版渠道", "1.0.0", true, true) // 固定在旧版本 → kling 有旧版渠道
	e.channel(t, "b-new", "新版渠道", "1.1.0", true, true) // 已是最新
	e.model(t, "m1", "a-old", true)
	e.model(t, "m2", "b-new", false)

	t.Run("每行带渠道数、旧版渠道数、在线模型数、最新版本", func(t *testing.T) {
		page, err := e.cat.ListPlugins(ctx, plugReq("", "", "", "", 0, 0))
		if err != nil || page.Total != 2 || len(page.List) != 2 {
			t.Fatalf("实际 %+v err=%v", page, err)
		}
		kling := page.List[0]
		if kling.Key != "kling" || kling.ChannelCount != 2 || kling.OutdatedChannelCount != 1 || kling.OnlineModelCount != 1 || kling.LatestVersion != "1.1.0" {
			t.Errorf("kling 不对：%+v", kling)
		}
		if len(kling.Versions) != 2 {
			t.Errorf("仍带 versions：%+v", kling.Versions)
		}
		loose := page.List[1]
		if loose.Key != "loose" || loose.ChannelCount != 0 || loose.OutdatedChannelCount != 0 {
			t.Errorf("loose 不对：%+v", loose)
		}
	})
	t.Run("搜索匹配名称 / key", func(t *testing.T) {
		page, _ := e.cat.ListPlugins(ctx, plugReq("loo", "", "", "", 0, 0))
		if page.Total != 1 || page.List[0].Key != "loose" {
			t.Fatalf("实际 %+v", page)
		}
	})
	t.Run("按状态筛选：on / off / outdated", func(t *testing.T) {
		if page, _ := e.cat.ListPlugins(ctx, plugReq("", "", "", "outdated", 0, 0)); page.Total != 1 || page.List[0].Key != "kling" {
			t.Errorf("outdated：%+v", page)
		}
		if page, _ := e.cat.ListPlugins(ctx, plugReq("", "", "", "on", 0, 0)); page.Total != 2 {
			t.Errorf("on：%+v", page)
		}
		if page, _ := e.cat.ListPlugins(ctx, plugReq("", "", "", "off", 0, 0)); page.Total != 0 {
			t.Errorf("off：%+v", page)
		}
	})
	t.Run("按能力和来源筛选：能力按最新版本的声明", func(t *testing.T) {
		if page, _ := e.cat.ListPlugins(ctx, plugReq("", "text", "", "", 0, 0)); page.Total != 1 || page.List[0].Key != "kling" {
			t.Errorf("只有 kling 声明了 text：%+v", page)
		}
		if page, _ := e.cat.ListPlugins(ctx, plugReq("", "", "builtin", "", 0, 0)); page.Total != 0 {
			t.Errorf("没有内置插件：%+v", page)
		}
		if page, _ := e.cat.ListPlugins(ctx, plugReq("", "", "uploaded", "", 0, 0)); page.Total != 2 {
			t.Errorf("上传的 2 个：%+v", page)
		}
	})
	t.Run("分页", func(t *testing.T) {
		page, _ := e.cat.ListPlugins(ctx, plugReq("", "", "", "", 2, 1))
		if page.Total != 2 || len(page.List) != 1 || page.List[0].Key != "loose" {
			t.Fatalf("实际 %+v", page)
		}
	})
}

func TestCatalog_PluginOptionsAndGet(t *testing.T) {
	ctx := context.Background()
	e := newCatEnv(t)

	opts, err := e.cat.PluginOptions(ctx)
	if err != nil || len(opts) != 2 {
		t.Fatalf("应返回全部插件，不分页：%+v err=%v", opts, err)
	}
	if opts[0].Key != "kling" || len(opts[0].Versions) != 2 || opts[0].Versions[0].Meta == nil {
		t.Errorf("options 带全部版本和 meta（渠道弹窗选版本、渲染设置项要用）：%+v", opts[0])
	}

	one, err := e.cat.GetPlugin(ctx, "kling")
	if err != nil || one.Key != "kling" || len(one.Versions) != 2 {
		t.Fatalf("详情：%+v err=%v", one, err)
	}
	if _, err := e.cat.GetPlugin(ctx, "ghost"); err == nil {
		t.Fatal("不存在的插件应返回 404")
	}
}

func modelReq(q, kind, channel, status string, page, size int) *model.ListModelReq {
	return &model.ListModelReq{AdminListReq: model.AdminListReq{Q: q, Page: page, PageSize: size}, Kind: kind, Channel: channel, Status: status}
}

func TestCatalog_ListModels(t *testing.T) {
	ctx := context.Background()
	e := newCatEnv(t)
	e.channel(t, "a-main", "主线", "1.0.0", true, true)
	e.channel(t, "b-nokey", "没设Key", "1.1.0", true, false)
	e.model(t, "m-online", "a-main", true)  // 渠道正常、已上架 → 在线
	e.model(t, "m-draft", "a-main", false)  // 没上架 → 未上线
	e.model(t, "m-broken", "b-nokey", true) // 渠道缺 Key、已上架 → 不可用
	e.model(t, "m-ghost", "no-such", true)  // 渠道不存在 → 不可用

	t.Run("每行带状态、原因和渠道名", func(t *testing.T) {
		page, err := e.cat.ListModels(ctx, modelReq("", "", "", "", 0, 0))
		if err != nil || page.Total != 4 {
			t.Fatalf("实际 %+v err=%v", page, err)
		}
		byKey := map[string]CatalogModelItem{}
		for _, it := range page.List {
			byKey[it.Key] = it
		}
		if m := byKey["m-online"]; m.Health.Status != "online" || m.ChannelName != "主线" {
			t.Errorf("m-online：%+v", m)
		}
		if m := byKey["m-draft"]; m.Health.Status != "offline" || m.Health.Reason != "" {
			t.Errorf("m-draft：%+v", m)
		}
		if m := byKey["m-broken"]; m.Health.Status != "broken" || m.Health.Reason != "渠道「没设Key」还没有设置 Key" {
			t.Errorf("m-broken：%+v", m)
		}
		if m := byKey["m-ghost"]; m.Health.Status != "broken" || m.Health.Reason != "渠道 no-such 不存在" || m.ChannelName != "" {
			t.Errorf("m-ghost：%+v", m)
		}
	})
	t.Run("按状态筛选", func(t *testing.T) {
		for status, want := range map[string]int{"online": 1, "offline": 1, "broken": 2} {
			if page, _ := e.cat.ListModels(ctx, modelReq("", "", "", status, 0, 0)); int(page.Total) != want {
				t.Errorf("status=%s 期望 %d，实际 %+v", status, want, page)
			}
		}
	})
	t.Run("创作端预览用：只取某类型已上线的前几条", func(t *testing.T) {
		page, _ := e.cat.ListModels(ctx, modelReq("", "video", "", "online", 1, 2))
		if page.Total != 1 || len(page.List) != 1 || page.List[0].Key != "m-online" {
			t.Fatalf("实际 %+v", page)
		}
	})
	t.Run("按渠道和类型筛选", func(t *testing.T) {
		if page, _ := e.cat.ListModels(ctx, modelReq("", "", "a-main", "", 0, 0)); page.Total != 2 {
			t.Errorf("a-main 下 2 个：%+v", page)
		}
		if page, _ := e.cat.ListModels(ctx, modelReq("", "video", "", "", 0, 0)); page.Total != 4 {
			t.Errorf("都是 video：%+v", page)
		}
		if page, _ := e.cat.ListModels(ctx, modelReq("", "image", "", "", 0, 0)); page.Total != 0 {
			t.Errorf("没有 image：%+v", page)
		}
	})
	t.Run("搜索匹配 key / 展示名 / 渠道名", func(t *testing.T) {
		if page, _ := e.cat.ListModels(ctx, modelReq("m-broken", "", "", "", 0, 0)); page.Total != 1 {
			t.Errorf("按 key：%+v", page)
		}
		if page, _ := e.cat.ListModels(ctx, modelReq("模型-m-draft", "", "", "", 0, 0)); page.Total != 1 {
			t.Errorf("按展示名：%+v", page)
		}
		if page, _ := e.cat.ListModels(ctx, modelReq("没设key", "", "", "", 0, 0)); page.Total != 1 {
			t.Errorf("按渠道名：%+v", page)
		}
	})
	t.Run("分页", func(t *testing.T) {
		page, _ := e.cat.ListModels(ctx, modelReq("", "", "", "", 2, 3))
		if page.Total != 4 || len(page.List) != 1 || page.Page != 2 || page.PageSize != 3 {
			t.Fatalf("实际 %+v", page)
		}
	})
}

func TestCatalog_ModelOptions(t *testing.T) {
	ctx := context.Background()
	e := newCatEnv(t)
	e.channel(t, "a-main", "主线", "1.0.0", true, true)
	e.model(t, "m1", "a-main", true)
	e.model(t, "m2", "a-main", false)

	opts, err := e.cat.ModelOptions(ctx)
	if err != nil || len(opts) != 2 {
		t.Fatalf("应返回全部模型，不分页：%+v err=%v", opts, err)
	}
	if opts[0].Key == "" || opts[0].Label == "" || opts[0].Channel != "a-main" || opts[0].Kind != "video" {
		t.Errorf("字段不全：%+v", opts[0])
	}
}
