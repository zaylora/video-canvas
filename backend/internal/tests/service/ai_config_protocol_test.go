package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service"
	"video-canvas/internal/service/aiconfigfake"
)

// 渠道不绑协议、协议在模型里选之后，Registry、模型排序与批量升级的行为。

// aicEnv 准备：渠道 c1 就绪（带 Key，请求头 X-Biz），kling 的 1.0.0 / 1.1.0 两个版本。
func aicProtoEnv(t *testing.T) (*AIConfigService, *aiconfigfake.MemRepo) {
	t.Helper()
	svc, repo, _ := aicNewSvc()
	aicSeedChannel(t, svc, repo, "c1", true)
	repo.Channels["c1"].HeadersJSON = model.JSONText(`{"X-Biz":"canvas"}`)
	adminSeedVersion(t, repo, adminMetaJSON("kling", "1.1.0"), model.PluginSourceUploaded, true)
	return svc, repo
}

func TestRegistry_同一渠道下的模型各用各的协议(t *testing.T) {
	ctx := context.Background()
	svc, repo := aicProtoEnv(t)
	adminSeedVersion(t, repo, adminMetaLoose("loose", "1.0.0"), model.PluginSourceUploaded, true)
	loose := strings.Replace(string(aicModelBody("m-loose", "c1", "")),
		`"protocol":{"plugin":"kling","version":"1.0.0","settings":{"tenant":"t"}}`, `"protocol":{"plugin":"loose","version":"1.0.0"}`, 1)
	if _, err := aicSave(svc, "", true, json.RawMessage(loose)); err != nil {
		t.Fatal(err)
	}
	if _, err := aicSave(svc, "", true, aicModelBodyV("m-old", "c1", "1.0.0", "")); err != nil {
		t.Fatal(err)
	}
	if _, err := aicSave(svc, "", true, aicModelBodyV("m-new", "c1", "1.1.0", "")); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"m-loose", "m-old", "m-new"} {
		if err := svc.SetModelEnabled(ctx, k, true, 1); err != nil {
			t.Fatalf("%s 启用失败：%v", k, err)
		}
	}

	for key, want := range map[string]string{"m-loose": "loose@1.0.0", "m-old": "kling@1.0.0", "m-new": "kling@1.1.0"} {
		snap, err := svc.Snapshot(ctx, key)
		if err != nil {
			t.Fatalf("%s：%v", key, err)
		}
		if got := snap.Plugin.Key + "@" + snap.Plugin.Version; got != want {
			t.Errorf("%s 应用协议 %s，实际 %s", key, want, got)
		}
		if snap.Channel.Key != "c1" || snap.Channel.BaseURL != "https://gw.example.com" {
			t.Errorf("%s：渠道部分应都指向同一个渠道：%+v", key, snap.Channel)
		}
	}
}

func TestRegistry_快照带协议设置默认值与渠道请求头(t *testing.T) {
	ctx := context.Background()
	svc, repo := aicProtoEnv(t)
	aicEnableModel(t, svc, "m1", "c1")
	snap, err := svc.Snapshot(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}
	// 模型只填了 tenant，插件声明 region 默认 cn：快照里补全
	if snap.Channel.Settings["tenant"] != "t" || snap.Channel.Settings["region"] != "cn" {
		t.Fatalf("协议设置应补全默认值：%v", snap.Channel.Settings)
	}
	if snap.Channel.Headers["X-Biz"] != "canvas" {
		t.Fatalf("快照应带渠道自定义请求头：%v", snap.Channel.Headers)
	}
	ver, _ := repo.FindVersion(ctx, "kling", "1.0.0")
	if snap.Channel.PluginKey != "kling" || snap.Channel.PluginVersionID != ver.ID {
		t.Fatalf("快照里的插件应来自模型的协议：%+v", snap.Channel)
	}
	b, _ := json.Marshal(snap)
	if strings.Contains(string(b), "sk-c1") {
		t.Fatalf("快照不能含渠道 Key：%s", b)
	}
}

func TestRegistry_Agent模型不经过插件(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := aicNewSvc()
	aicSeedChannel(t, svc, repo, "c1", true) // 渠道没有任何协议插件的概念；这里甚至不登记插件
	delete(repo.Plugins, "kling")
	for id := range repo.Versions {
		delete(repo.Versions, id)
	}
	if _, err := aicSave(svc, "", true, aicAgentBody("agent1", "c1")); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetModelEnabled(ctx, "agent1", true, 1); err != nil {
		t.Fatalf("Agent 模型不需要协议插件，应能启用：%v", err)
	}
	snap, err := svc.Snapshot(ctx, "agent1")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Plugin.Key != "" || snap.Channel.Key != "c1" || snap.Channel.BaseURL != "https://gw.example.com" || snap.Model.UpstreamModel != "gpt-x" {
		t.Fatalf("Agent 快照只有渠道部分：%+v", snap)
	}
	list, _ := svc.ListModels(ctx, "agent")
	if len(list) != 1 {
		t.Fatalf("Agent 模型应出现在清单里：%+v", list)
	}
}

func TestRegistry_待配置的模型不可用且不进清单(t *testing.T) {
	ctx := context.Background()
	svc, repo := aicProtoEnv(t)
	_ = repo
	if _, err := svc.ImportChannelModels(ctx, 1, "c1", []string{"raw-model"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetModelEnabled(ctx, "raw-model", true, 1); err == nil {
		t.Fatal("待配置的模型不能启用")
	} else {
		adminWantCode(t, err, errcode.ErrConfigInvalid.Code)
	}
	if list, _ := svc.ListModels(ctx, ""); len(list) != 0 {
		t.Fatalf("待配置的模型不应进清单：%+v", list)
	}
}

func TestRegistry_清单顺序是渠道顺序加渠道内顺序(t *testing.T) {
	ctx := context.Background()
	svc, repo := aicProtoEnv(t)
	aicSeedChannel(t, svc, repo, "c2", true)
	repo.Channels["c1"].Sort, repo.Channels["c2"].Sort = 20, 10 // c2 在前
	body := func(key, channel string, sortNo int) json.RawMessage {
		return json.RawMessage(strings.Replace(string(aicModelBody(key, channel, "")), `"sort":10`, `"sort":`+string(rune('0'+sortNo)), 1))
	}
	// 排序值：a=3、b=1 在 c1；c=2、d=4 在 c2
	for key, spec := range map[string]struct {
		ch   string
		sort int
	}{"a": {"c1", 3}, "b": {"c1", 1}, "c": {"c2", 2}, "d": {"c2", 4}} {
		if _, err := aicSave(svc, "", true, body(key, spec.ch, spec.sort)); err != nil {
			t.Fatal(err)
		}
		if err := svc.SetModelEnabled(ctx, key, true, 1); err != nil {
			t.Fatal(err)
		}
	}
	list, err := svc.ListModels(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range list {
		got = append(got, m.Key)
	}
	if strings.Join(got, ",") != "c,d,b,a" {
		t.Fatalf("应先按渠道顺序（c2 在前），再按渠道内排序：%v", got)
	}
}

func TestAIConfigService_SetChannelModelOrder(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T) (*AIConfigService, *aiconfigfake.MemRepo, *aiconfigfake.Invalidator) {
		t.Helper()
		svc, repo, inv := aicNewSvc()
		aicSeedChannel(t, svc, repo, "c1", true)
		aicSeedChannel(t, svc, repo, "c2", true)
		for _, spec := range [][2]string{{"a", "c1"}, {"b", "c1"}, {"c", "c1"}, {"x", "c2"}} {
			if _, err := aicSave(svc, "", true, aicModelBody(spec[0], spec[1], "")); err != nil {
				t.Fatal(err)
			}
		}
		inv.Reasons = nil
		return svc, repo, inv
	}

	t.Run("按新顺序重写该渠道内模型的 sort，不影响其他渠道，并刷新 Registry", func(t *testing.T) {
		svc, repo, inv := seed(t)
		xBefore := repo.Models["x"].Sort
		if err := svc.SetChannelModelOrder(ctx, "c1", []string{"c", "a", "b"}); err != nil {
			t.Fatal(err)
		}
		if repo.Models["c"].Sort != 10 || repo.Models["a"].Sort != 20 || repo.Models["b"].Sort != 30 {
			t.Fatalf("sort 不对：c=%d a=%d b=%d", repo.Models["c"].Sort, repo.Models["a"].Sort, repo.Models["b"].Sort)
		}
		if repo.Models["x"].Sort != xBefore {
			t.Fatal("其他渠道的模型不应受影响")
		}
		if len(inv.Reasons) == 0 {
			t.Fatal("渠道内顺序决定用户看到的顺序，应刷新 Registry")
		}
	})

	t.Run("不是该渠道模型的全集（漏、重复、别的渠道的、不存在的）400，顺序不变", func(t *testing.T) {
		for name, keys := range map[string][]string{
			"漏掉": {"a", "b"}, "重复": {"a", "a", "b", "c"}, "别的渠道的": {"a", "b", "c", "x"}, "不存在的": {"a", "b", "ghost"}, "空": {},
		} {
			svc, repo, _ := seed(t)
			before := repo.Models["a"].Sort
			adminWantCode(t, svc.SetChannelModelOrder(ctx, "c1", keys), errcode.ErrInvalidParams.Code)
			if repo.Models["a"].Sort != before {
				t.Fatalf("%s：顺序不应改变", name)
			}
		}
	})
}

func TestAIChannelService_Delete连带删除SecretKey(t *testing.T) {
	ctx := context.Background()
	e := newAchEnv(t)
	_, _ = e.svc.Create(ctx, createInput())
	_ = e.svc.SetSecret(ctx, 1, "main", "api-key")
	_ = e.svc.SetSecretKey(ctx, 1, "main", "secret-key")
	if err := e.svc.Delete(ctx, 1, "main"); err != nil {
		t.Fatal(err)
	}
	if len(e.repo.Secrets) != 0 {
		t.Fatalf("渠道的 Key 与 Secret Key 都应删除：%v", e.repo.Secrets)
	}
	if _, err := e.cfg.Get(ctx, "channel:main:secret"); err == nil {
		t.Fatal("本实例缓存里的明文也应一并清掉")
	}
}

// ---------------------------------------------------------------------------
// 批量升级模型
// ---------------------------------------------------------------------------

// pluginUpgradeEnv 准备：kling@1.0.0（settings: region 默认 cn、tenant 必填、burst、debug）与 1.1.0（新版本：
// 去掉 burst / debug，新增必填的 zone），渠道 c1 就绪；用 AIPluginService 走完整的升级入口（含审计）。
type pluginUpgradeEnv struct {
	plugins *AIPluginService
	cfg     *AIConfigService
	repo    *aiconfigfake.MemRepo
	notif   *aiconfigfake.Notifier
}

func newPluginUpgradeEnv(t *testing.T, newMeta string) *pluginUpgradeEnv {
	t.Helper()
	svc, repo, _ := aicNewSvc()
	aicSeedChannel(t, svc, repo, "c1", true)
	if newMeta == "" {
		newMeta = `{"apiVersion":1,"key":"kling","name":"插件-kling","version":"1.1.0","auth":{"type":"bearer"},` +
			`"endpoints":{"video":{"mode":"async"},"text":{"mode":"sync"}},` +
			`"channelSettings":{"region":{"type":"enum","label":"区域","options":["cn","global"],"default":"cn"},` +
			`"tenant":{"type":"string","label":"租户","required":true},"zone":{"type":"string","label":"可用区","default":"z1"}}}`
	}
	adminSeedVersion(t, repo, json.RawMessage(newMeta), model.PluginSourceUploaded, true)
	notif := &aiconfigfake.Notifier{}
	return &pluginUpgradeEnv{plugins: NewAIPluginService(repo, svc, repo, nil, notif), cfg: svc, repo: repo, notif: notif}
}

func (e *pluginUpgradeEnv) protocolOf(t *testing.T, key string) map[string]any {
	t.Helper()
	rev, err := e.repo.GetModelConfig(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Channels []struct {
			Protocol map[string]any `json:"protocol"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(rev.BodyJSON, &body); err != nil {
		t.Fatal(err)
	}
	return body.Channels[0].Protocol
}

func TestAIPluginService_UpgradeModels(t *testing.T) {
	ctx := context.Background()

	t.Run("把旧版本的模型升到最新版本：丢弃新版本不再声明的设置项，补上新的默认值；已是新版本的、别的插件的不动", func(t *testing.T) {
		e := newPluginUpgradeEnv(t, "")
		withBurst := strings.Replace(string(aicModelBody("old", "c1", "")), `"settings":{"tenant":"t"}`, `"settings":{"tenant":"t","burst":5,"debug":true}`, 1)
		if _, err := aicSave(e.cfg, "", true, json.RawMessage(withBurst)); err != nil {
			t.Fatal(err)
		}
		if _, err := aicSave(e.cfg, "", true, aicModelBodyV("already", "c1", "1.1.0", "")); err != nil {
			t.Fatal(err)
		}
		adminSeedVersion(t, e.repo, adminMetaLoose("loose", "1.0.0"), model.PluginSourceUploaded, true)
		other := strings.Replace(string(aicModelBody("other", "c1", "")),
			`"protocol":{"plugin":"kling","version":"1.0.0","settings":{"tenant":"t"}}`, `"protocol":{"plugin":"loose","version":"1.0.0"}`, 1)
		if _, err := aicSave(e.cfg, "", true, json.RawMessage(other)); err != nil {
			t.Fatal(err)
		}
		e.repo.Audits = nil

		res, err := e.plugins.UpgradeModels(ctx, 9, "kling", "")
		if err != nil {
			t.Fatal(err)
		}
		if res.ToVersion != "1.1.0" || len(res.Upgraded) != 1 || res.Upgraded[0] != "old" || len(res.Failed) != 0 {
			t.Fatalf("结果不符合预期：%+v", res)
		}
		p := e.protocolOf(t, "old")
		settings := p["settings"].(map[string]any)
		if p["version"] != "1.1.0" || settings["tenant"] != "t" || settings["zone"] != "z1" || settings["region"] != "cn" {
			t.Fatalf("协议应升到 1.1.0 并补全默认值：%v", p)
		}
		if _, ok := settings["burst"]; ok {
			t.Fatalf("新版本不再声明的设置项应被丢弃：%v", settings)
		}
		if e.protocolOf(t, "other")["plugin"] != "loose" || e.protocolOf(t, "other")["version"] != "1.0.0" {
			t.Fatal("别的插件的模型不应被动")
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditPluginUpgrade {
			t.Fatalf("审计不符合预期：%v", got)
		}
	})

	t.Run("只改 channels：capabilities 里参数的书写顺序（前端的渲染顺序）原样保留", func(t *testing.T) {
		e := newPluginUpgradeEnv(t, "")
		body := strings.Replace(string(aicModelBody("m1", "c1", "")),
			`"capabilities":{"ops":["t2v"],"prompt":{"max_length":2000}}`,
			`"capabilities":{"ops":["t2v"],"prompt":{"max_length":2000},"params":{"zeta":{"type":"enum","label":"Z","options":["a"],"default":"a"},"alpha":{"type":"enum","label":"A","options":["b"],"default":"b"}}}`, 1)
		if _, err := aicSave(e.cfg, "", true, json.RawMessage(body)); err != nil {
			t.Fatal(err)
		}
		if _, err := e.plugins.UpgradeModels(ctx, 9, "kling", "1.1.0"); err != nil {
			t.Fatal(err)
		}
		rev, _ := e.repo.GetModelConfig(ctx, "m1")
		raw := string(rev.BodyJSON)
		zeta, alpha := strings.Index(raw, `"zeta"`), strings.Index(raw, `"alpha"`)
		if zeta < 0 || alpha < 0 || zeta > alpha {
			t.Fatalf("params 的键顺序被打乱了（zeta 应在 alpha 前）：%s", raw)
		}
	})

	t.Run("新版本新增了必填设置项：该模型升级失败，保持原样，其他模型照常升级", func(t *testing.T) {
		meta := `{"apiVersion":1,"key":"kling","name":"插件-kling","version":"1.1.0","auth":{"type":"bearer"},` +
			`"endpoints":{"video":{"mode":"async"}},"channelSettings":{"tenant":{"type":"string","label":"租户","required":true},` +
			`"must":{"type":"string","label":"必填新项","required":true}}}`
		e := newPluginUpgradeEnv(t, meta)
		if _, err := aicSave(e.cfg, "", true, aicModelBody("m1", "c1", "")); err != nil {
			t.Fatal(err)
		}
		res, err := e.plugins.UpgradeModels(ctx, 9, "kling", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Upgraded) != 0 || len(res.Failed) != 1 || res.Failed[0].Key != "m1" || !strings.Contains(res.Failed[0].Reason, "must") {
			t.Fatalf("应失败并说明缺哪个设置项：%+v", res)
		}
		if e.protocolOf(t, "m1")["version"] != "1.0.0" {
			t.Fatal("失败的模型应保持原版本")
		}
	})

	t.Run("已启用的模型升级后过不了启用检查：失败，保持原样（不让线上模型被改坏）", func(t *testing.T) {
		// 新版本只支持 text，已启用的是 video 模型
		meta := `{"apiVersion":1,"key":"kling","name":"插件-kling","version":"1.1.0","auth":{"type":"bearer"},` +
			`"endpoints":{"text":{"mode":"sync"}},"channelSettings":{"tenant":{"type":"string","label":"租户","required":true}}}`
		e := newPluginUpgradeEnv(t, meta)
		aicEnableModel(t, e.cfg, "m1", "c1")
		res, err := e.plugins.UpgradeModels(ctx, 9, "kling", "")
		if err != nil || len(res.Failed) != 1 || len(res.Upgraded) != 0 {
			t.Fatalf("应失败：%+v %v", res, err)
		}
		if e.protocolOf(t, "m1")["version"] != "1.0.0" || !e.repo.Models["m1"].Enabled {
			t.Fatal("线上模型应保持原版本并继续启用")
		}
	})

	t.Run("指定目标版本；没有要升级的模型返回空结果且不写审计", func(t *testing.T) {
		e := newPluginUpgradeEnv(t, "")
		res, err := e.plugins.UpgradeModels(ctx, 9, "kling", "1.1.0")
		if err != nil || len(res.Upgraded) != 0 || len(res.Failed) != 0 || res.Upgraded == nil || res.Failed == nil {
			t.Fatalf("应返回空数组：%+v %v", res, err)
		}
		if len(e.repo.Audits) != 0 {
			t.Fatalf("没有变化不该写审计：%v", e.repo.AuditActions())
		}
	})

	t.Run("错误：插件不存在 404、目标版本不存在 404、插件已停用 409", func(t *testing.T) {
		e := newPluginUpgradeEnv(t, "")
		_, err := e.plugins.UpgradeModels(ctx, 9, "ghost", "")
		adminWantCode(t, err, errcode.ErrPluginNotFound.Code)
		_, err = e.plugins.UpgradeModels(ctx, 9, "kling", "9.9.9")
		adminWantCode(t, err, errcode.ErrPluginNotFound.Code)
		e.repo.Plugins["kling"].Enabled = false
		_, err = e.plugins.UpgradeModels(ctx, 9, "kling", "")
		adminWantCode(t, err, errcode.ErrPluginDisabled.Code)
	})
}
