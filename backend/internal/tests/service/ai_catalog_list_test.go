package service_test

import (
	"context"
	"strings"
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/service"
)

// catEnv 在渠道服务的测试环境上搭一个目录服务：真实的渠道 / 插件 / 模型服务 + 内存仓储。
// 插件：kling 有 1.0.0 / 1.1.0 两个版本（bearer，video + text）；loose 只有 1.0.0（鉴权 none，只有 video）。
type catEnv struct {
	*achEnv
	cat *AICatalogService
}

func newCatEnv(t *testing.T) *catEnv {
	t.Helper()
	e := newAchEnv(t)
	adminSeedVersion(t, e.repo, adminMetaJSON("kling", "1.0.0"), model.PluginSourceUploaded, true)
	adminSeedVersion(t, e.repo, adminMetaJSON("kling", "1.1.0"), model.PluginSourceUploaded, true)
	adminSeedVersion(t, e.repo, adminMetaLoose("loose", "1.0.0"), model.PluginSourceUploaded, true)
	plugins := NewAIPluginService(e.repo, e.cfg, e.repo, nil, e.notifier)
	return &catEnv{achEnv: e, cat: NewAICatalogService(e.svc, plugins, e.cfg)}
}

// channel 建一个渠道；withKey 为 true 时设置 Key。
func (e *catEnv) channel(t *testing.T, key, name string, enabled, withKey bool) {
	t.Helper()
	in := createInput()
	in.Key, in.Name, in.Enabled = key, name, enabled
	if _, err := e.svc.Create(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if withKey {
		if err := e.svc.SetSecret(context.Background(), 1, key, "sk-"+key); err != nil {
			t.Fatal(err)
		}
	}
}

// model 建一个协议选了 kling@1.0.0 的视频模型并按需上架。
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

// pending 导入一个“待配置”模型（没选能力和协议）。
func (e *catEnv) pending(t *testing.T, key, channel string) {
	t.Helper()
	if _, err := e.cfg.ImportChannelModels(context.Background(), 1, channel, []string{key}); err != nil {
		t.Fatal(err)
	}
}

func chanReq(q, status string, page, size int) *model.ListChannelReq {
	return &model.ListChannelReq{AdminListReq: model.AdminListReq{Q: q, Page: page, PageSize: size}, Status: status}
}

func TestCatalog_ListChannels(t *testing.T) {
	ctx := context.Background()
	e := newCatEnv(t)
	e.channel(t, "a-main", "主线", true, true)      // 正常
	e.channel(t, "b-nokey", "没设Key", true, false) // 缺 Key
	e.channel(t, "c-off", "已停用", false, true)     // 停用
	e.model(t, "m1", "a-main", true)
	e.model(t, "m2", "a-main", false)
	e.model(t, "m3", "c-off", true)

	t.Run("每行带健康状态和模型数，顺序是渠道顺序", func(t *testing.T) {
		page, err := e.cat.ListChannels(ctx, chanReq("", "", 0, 0))
		if err != nil || page.Total != 3 || len(page.List) != 3 {
			t.Fatalf("实际 %+v err=%v", page, err)
		}
		a, b, c := page.List[0], page.List[1], page.List[2]
		if a.Key != "a-main" || a.Health.Tone != "ok" || a.ModelCount != 2 || a.OnlineModelCount != 1 {
			t.Errorf("a-main 不对：%+v", a)
		}
		if b.Health.Tone != "warn" || b.Health.Label != "未设 Key" || b.ModelCount != 0 {
			t.Errorf("b-nokey 不对：%+v", b)
		}
		if c.Health.Tone != "off" || c.ModelCount != 1 || c.OnlineModelCount != 1 {
			t.Errorf("c-off 不对：%+v", c)
		}
	})
	t.Run("搜索匹配名称 / key / 地址", func(t *testing.T) {
		page, _ := e.cat.ListChannels(ctx, chanReq("没设", "", 0, 0))
		if page.Total != 1 || page.List[0].Key != "b-nokey" {
			t.Fatalf("实际 %+v", page)
		}
		page, _ = e.cat.ListChannels(ctx, chanReq("gw.example.com", "", 0, 0))
		if page.Total != 3 {
			t.Fatalf("地址也能搜：%+v", page)
		}
	})
	t.Run("按状态筛选：ok / warn / off", func(t *testing.T) {
		for status, want := range map[string]string{"ok": "a-main", "warn": "b-nokey", "off": "c-off"} {
			page, _ := e.cat.ListChannels(ctx, chanReq("", status, 0, 0))
			if page.Total != 1 || page.List[0].Key != want {
				t.Errorf("status=%s 期望 %s，实际 %+v", status, want, page)
			}
		}
	})
	t.Run("分页", func(t *testing.T) {
		page, _ := e.cat.ListChannels(ctx, chanReq("", "", 2, 2))
		if page.Total != 3 || len(page.List) != 1 || page.Page != 2 || page.PageSize != 2 {
			t.Fatalf("实际 %+v", page)
		}
	})
	t.Run("没有命中返回空数组", func(t *testing.T) {
		page, _ := e.cat.ListChannels(ctx, chanReq("没有这个", "", 0, 0))
		if page.Total != 0 || page.List == nil {
			t.Fatalf("实际 %+v", page)
		}
	})
}

func TestCatalog_ChannelOptions(t *testing.T) {
	ctx := context.Background()
	e := newCatEnv(t)
	e.channel(t, "a-main", "主线", true, true)
	e.channel(t, "b-nokey", "没设Key", true, false)
	if err := e.svc.SetOrder(ctx, 1, []string{"b-nokey", "a-main"}); err != nil {
		t.Fatal(err)
	}

	opts, err := e.cat.ChannelOptions(ctx)
	if err != nil || len(opts) != 2 {
		t.Fatalf("应返回全部渠道，不分页：%+v err=%v", opts, err)
	}
	if opts[0].Key != "b-nokey" || opts[1].Key != "a-main" {
		t.Errorf("顺序应是渠道顺序（sort），而不是 key 顺序：%v %v", opts[0].Key, opts[1].Key)
	}
	if opts[1].Name != "主线" || !opts[1].Enabled || opts[1].Health.Tone != "ok" || opts[1].BaseURL == "" || opts[1].Headers == nil {
		t.Errorf("options 带完整渠道视图和健康状态：%+v", opts[1])
	}
	if opts[0].Health.Tone != "warn" || opts[0].SecretSet {
		t.Errorf("b-nokey 应是未设 Key：%+v", opts[0])
	}
}

func plugReq(q, kind, source, status string, page, size int) *model.ListPluginReq {
	return &model.ListPluginReq{AdminListReq: model.AdminListReq{Q: q, Page: page, PageSize: size}, Kind: kind, Source: source, Status: status}
}

func TestCatalog_ListPlugins(t *testing.T) {
	ctx := context.Background()
	e := newCatEnv(t)
	e.channel(t, "main", "主线", true, true)
	e.model(t, "m-old", "main", true)   // kling@1.0.0（旧版本），已上架
	e.model(t, "m-old2", "main", false) // kling@1.0.0（旧版本），未上架
	e.modelAt(t, "m-new", "main", "1.1.0", false)

	t.Run("每行带模型数、旧版模型数、在线模型数、最新版本", func(t *testing.T) {
		page, err := e.cat.ListPlugins(ctx, plugReq("", "", "", "", 0, 0))
		if err != nil || page.Total != 2 || len(page.List) != 2 {
			t.Fatalf("实际 %+v err=%v", page, err)
		}
		kling := page.List[0]
		if kling.Key != "kling" || kling.ModelCount != 3 || kling.OutdatedModelCount != 2 || kling.OnlineModelCount != 1 || kling.LatestVersion != "1.1.0" {
			t.Errorf("kling 不对：%+v", kling)
		}
		if len(kling.Versions) != 2 || kling.Versions[0].ModelCount+kling.Versions[1].ModelCount != 3 {
			t.Errorf("仍带 versions，且每个版本有被模型固定的数量：%+v", kling.Versions)
		}
		loose := page.List[1]
		if loose.Key != "loose" || loose.ModelCount != 0 || loose.OutdatedModelCount != 0 {
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
		if page, _ := e.cat.ListPlugins(ctx, plugReq("", "agent", "", "", 0, 0)); page.Total != 0 {
			t.Errorf("agent 不走插件，没有插件承接：%+v", page)
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

// modelAt 同 model，但协议固定在 kling 的指定版本上。
func (e *catEnv) modelAt(t *testing.T, key, channel, version string, online bool) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.cfg.SaveModel(ctx, ModelSaveInput{Create: true, Body: aicModelBodyV(key, channel, version, ""), AdminID: 1}); err != nil {
		t.Fatal(err)
	}
	if online {
		if err := e.repo.SetModelEnabled(ctx, key, true); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCatalog_PluginOptionsAndGet(t *testing.T) {
	ctx := context.Background()
	e := newCatEnv(t)

	opts, err := e.cat.PluginOptions(ctx)
	if err != nil || len(opts) != 2 {
		t.Fatalf("应返回全部插件，不分页：%+v err=%v", opts, err)
	}
	if opts[0].Key != "kling" || len(opts[0].Versions) != 2 || opts[0].Versions[0].Meta == nil {
		t.Errorf("options 带全部版本和 meta（模型弹窗的协议列表、协议设置项表单要用）：%+v", opts[0])
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
	e.channel(t, "a-main", "主线", true, true)
	e.channel(t, "b-nokey", "没设Key", true, false)
	e.model(t, "m-online", "a-main", true)  // 渠道正常、已上架 → 在线
	e.model(t, "m-draft", "a-main", false)  // 没上架 → 未上线
	e.model(t, "m-broken", "b-nokey", true) // 渠道缺 Key、已上架 → 不可用
	e.model(t, "m-ghost", "no-such", true)  // 渠道不存在 → 不可用
	e.pending(t, "m-pending", "a-main")     // 拉取得到、还没选能力和协议 → 待配置

	t.Run("每行带状态、原因、渠道名和协议名", func(t *testing.T) {
		page, err := e.cat.ListModels(ctx, modelReq("", "", "", "", 0, 0))
		if err != nil || page.Total != 5 {
			t.Fatalf("实际 %+v err=%v", page, err)
		}
		byKey := map[string]CatalogModelItem{}
		for _, it := range page.List {
			byKey[it.Key] = it
		}
		if m := byKey["m-online"]; m.Health.Status != "online" || m.ChannelName != "主线" || m.Plugin != "kling" || m.PluginVer != "1.0.0" || m.PluginName != "插件-kling" {
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
		if m := byKey["m-pending"]; m.Health.Status != "pending" || m.Kind != "" || m.Plugin != "" || m.Upstream != "m-pending" {
			t.Errorf("m-pending：%+v", m)
		}
	})
	t.Run("顺序是用户看到的顺序：渠道顺序，渠道内已启用在前，再按渠道内顺序；渠道不存在的排最后", func(t *testing.T) {
		if err := e.svc.SetOrder(ctx, 1, []string{"b-nokey", "a-main"}); err != nil {
			t.Fatal(err)
		}
		page, _ := e.cat.ListModels(ctx, modelReq("", "", "", "", 0, 0))
		var got []string
		for _, it := range page.List {
			got = append(got, it.Key)
		}
		// b-nokey 排在 a-main 前面；a-main 内已启用的 m-online 在前，未启用的再按 sort（m-draft 是 10，导入的 m-pending 排在最后）；no-such 的模型最后
		want := []string{"m-broken", "m-online", "m-draft", "m-pending", "m-ghost"}
		if len(got) != len(want) {
			t.Fatalf("顺序不对：%v", got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("顺序不对：%v，期望 %v", got, want)
			}
		}
	})
	t.Run("渠道内两组各自保持拖动顺序：组内按 sort，sort 相同按 key", func(t *testing.T) {
		e := newCatEnv(t)
		e.channel(t, "c", "渠道", true, true)
		e.model(t, "on-b", "c", true)
		e.model(t, "off-a", "c", false)
		e.model(t, "on-a", "c", true)
		e.model(t, "off-b", "c", false)
		e.model(t, "on-c", "c", true)
		// 拖动后的完整顺序（启用和未启用交错保存），组内相对顺序是 on-c、on-a、on-b 和 off-b、off-a
		if err := e.cfg.SetChannelModelOrder(ctx, "c", []string{"off-b", "on-c", "off-a", "on-a", "on-b"}); err != nil {
			t.Fatal(err)
		}
		// 新建的两个 sort 都是 10，排在组内最前，sort 相同按 key
		e.model(t, "on-z", "c", true)
		e.model(t, "on-0", "c", true)
		page, _ := e.cat.ListModels(ctx, modelReq("", "", "", "", 0, 0))
		var got []string
		for _, it := range page.List {
			got = append(got, it.Key)
		}
		want := []string{"on-0", "on-z", "on-c", "on-a", "on-b", "off-b", "off-a"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("顺序不对：%v，期望 %v", got, want)
		}
	})
	t.Run("按状态筛选：在线 / 未上线 / 不可用 / 待配置", func(t *testing.T) {
		for status, want := range map[string]int{"online": 1, "offline": 1, "broken": 2, "pending": 1} {
			if page, _ := e.cat.ListModels(ctx, modelReq("", "", "", status, 0, 0)); int(page.Total) != want {
				t.Errorf("status=%s 期望 %d，实际 %+v", status, want, page)
			}
		}
	})
	t.Run("按渠道和类型筛选", func(t *testing.T) {
		if page, _ := e.cat.ListModels(ctx, modelReq("", "", "a-main", "", 0, 0)); page.Total != 3 {
			t.Errorf("a-main 下 3 个（含待配置）：%+v", page)
		}
		if page, _ := e.cat.ListModels(ctx, modelReq("", "video", "", "", 0, 0)); page.Total != 4 {
			t.Errorf("4 个 video（待配置的没有类型）：%+v", page)
		}
		if page, _ := e.cat.ListModels(ctx, modelReq("", "image", "", "", 0, 0)); page.Total != 0 {
			t.Errorf("没有 image：%+v", page)
		}
	})
	t.Run("搜索匹配 key / 展示名 / 上游模型名 / 渠道名 / 协议名", func(t *testing.T) {
		for q, want := range map[string]int64{"m-broken": 1, "模型-m-draft": 1, "没设key": 1, "插件-kling": 4} {
			if page, _ := e.cat.ListModels(ctx, modelReq(q, "", "", "", 0, 0)); page.Total != want {
				t.Errorf("q=%q 期望 %d，实际 %+v", q, want, page)
			}
		}
	})
	t.Run("分页", func(t *testing.T) {
		page, _ := e.cat.ListModels(ctx, modelReq("", "", "", "", 2, 3))
		if page.Total != 5 || len(page.List) != 2 || page.Page != 2 || page.PageSize != 3 {
			t.Fatalf("实际 %+v", page)
		}
	})
}

func TestCatalog_ModelOptions(t *testing.T) {
	ctx := context.Background()
	e := newCatEnv(t)
	e.channel(t, "a-main", "主线", true, true)
	e.model(t, "m1", "a-main", true)
	e.model(t, "m2", "a-main", false)

	opts, err := e.cat.ModelOptions(ctx)
	if err != nil || len(opts) != 2 {
		t.Fatalf("应返回全部模型，不分页：%+v err=%v", opts, err)
	}
	if opts[0].Key == "" || opts[0].Label == "" || opts[0].Channel != "a-main" || opts[0].Kind != "video" || opts[0].Plugin != "kling" {
		t.Errorf("字段不全：%+v", opts[0])
	}
}
