package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	. "video-canvas/internal/service"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/service/aiconfigfake"
)

// aicKindBody 生成指定种类与排序值的模型正文（绑定渠道 channel）。
func aicKindBody(key, channel, kind string, sort int) json.RawMessage {
	body := aicWithKind(string(aicModelBody(key, channel, "")), kind)
	body = strings.Replace(body, `"sort":10`, fmt.Sprintf(`"sort":%d`, sort), 1)
	return json.RawMessage(body)
}

// aicPublishBody 保存并发布一份指定正文的新模型。
func aicPublishBody(t *testing.T, svc *AIConfigService, body json.RawMessage) {
	t.Helper()
	var head struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &head); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveDraft(context.Background(), ModelDraftInput{Create: true, Body: body, AdminID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(context.Background(), head.Key, 1); err != nil {
		t.Fatal(err)
	}
}

func TestAIConfigService_Registry_ListModels(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := aicNewSvc()
	aicSeedChannel(t, svc, repo, "c1", true)
	aicPublishModel(t, svc, "m-b", "c1")
	aicPublishModel(t, svc, "m-a", "c1")
	// 一个 text 模型，排序更靠前
	aicPublishBody(t, svc, aicKindBody("m-txt", "c1", "text", 1))
	// 只有草稿、未发布的模型不出现
	if _, err := aicSave(svc, "", true, aicModelBody("m-draft", "c1", "")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		kind string
		want []string
	}{
		{"全部按 sort 再按 key 排序", "", []string{"m-txt", "m-a", "m-b"}},
		{"按 kind 过滤 video", "video", []string{"m-a", "m-b"}},
		{"按 kind 过滤 text", "text", []string{"m-txt"}},
		{"没有该 kind 时返回空列表", "audio", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list, err := svc.ListModels(ctx, tt.kind)
			if err != nil {
				t.Fatal(err)
			}
			if list == nil || len(list) != len(tt.want) {
				t.Fatalf("期望 %v，实际 %+v", tt.want, list)
			}
			for i, m := range list {
				if m.Key != tt.want[i] {
					t.Fatalf("第 %d 项期望 %s，实际 %s", i, tt.want[i], m.Key)
				}
			}
		})
	}

	t.Run("下架的模型不在清单里", func(t *testing.T) {
		repo.Models["m-a"].Enabled = false
		if err := svc.RefreshRegistry(ctx); err != nil {
			t.Fatal(err)
		}
		list, _ := svc.ListModels(ctx, "video")
		if len(list) != 1 || list[0].Key != "m-b" {
			t.Fatalf("下架的模型不应出现：%+v", list)
		}
	})

	t.Run("公开字段完整", func(t *testing.T) {
		list, _ := svc.ListModels(ctx, "video")
		m := list[0]
		if m.Label != "模型-m-b" || m.Hint != "提示" || m.Pricing.Unit != 5 || m.Pricing.Cost != nil || m.Kind != "video" {
			t.Fatalf("公开字段不符合预期：%+v", m)
		}
		if m.Capabilities.Prompt.MaxLength != 2000 || len(m.Capabilities.Ops) != 1 {
			t.Fatalf("capabilities 应带上：%+v", m.Capabilities)
		}
		b, _ := json.Marshal(list)
		for _, bad := range []string{"channel", "kling", "instanceType", "sk-c1", "upstream"} {
			if strings.Contains(string(b), bad) {
				t.Fatalf("清单不应泄露 %q：%s", bad, b)
			}
		}
	})
}

func TestAIConfigService_Registry_Snapshot(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := aicNewSvc()
	aicSeedChannel(t, svc, repo, "c1", true)
	aicPublishModel(t, svc, "m1", "c1")

	t.Run("冻结模型、渠道、插件版本，不含 Key", func(t *testing.T) {
		snap, err := svc.Snapshot(ctx, "m1")
		if err != nil {
			t.Fatal(err)
		}
		if snap.Model.Key != "m1" || snap.Model.Kind != "video" || snap.Model.UpstreamModel != "kling-v2" || snap.Model.Pricing.Unit != 5 ||
			snap.Model.Params["instanceType"] != "default" {
			t.Fatalf("模型部分不符合预期：%+v", snap.Model)
		}
		ver, _ := repo.FindVersion(ctx, "kling", "1.0.0")
		if snap.Channel.Key != "c1" || snap.Channel.PluginKey != "kling" || snap.Channel.PluginVersionID != ver.ID ||
			snap.Channel.BaseURL != "https://gw.example.com" || snap.Channel.Settings["tenant"] != "t" {
			t.Fatalf("渠道部分不符合预期：%+v", snap.Channel)
		}
		if snap.Plugin.Key != "kling" || snap.Plugin.Version != "1.0.0" || snap.Plugin.SHA256 != "seed" || snap.Plugin.Meta.Key != "kling" {
			t.Fatalf("插件部分不符合预期：%+v", snap.Plugin)
		}
		pub, _ := repo.GetPublishedRevision(ctx, model.ConfigTargetModel, "m1")
		if snap.ModelRevisionID != pub.ID {
			t.Fatalf("revision id 不符合预期：%d vs %d", snap.ModelRevisionID, pub.ID)
		}
		b, _ := json.Marshal(snap)
		if strings.Contains(string(b), "sk-c1") {
			t.Fatalf("快照不能含渠道 Key：%s", b)
		}
	})

	unavailable := []struct {
		name   string
		mutate func(repo *aiconfigfake.MemRepo)
	}{
		{"模型已下线", func(r *aiconfigfake.MemRepo) { r.Models["m1"].Enabled = false }},
		{"渠道已停用", func(r *aiconfigfake.MemRepo) { r.Channels["c1"].Enabled = false }},
		{"渠道已被删除", func(r *aiconfigfake.MemRepo) { delete(r.Channels, "c1") }},
		{"渠道用的插件已停用", func(r *aiconfigfake.MemRepo) { r.Plugins["kling"].Enabled = false }},
		{"渠道用的插件行已被删除", func(r *aiconfigfake.MemRepo) { delete(r.Plugins, "kling") }},
		{"渠道固定的插件版本已不存在", func(r *aiconfigfake.MemRepo) {
			for id := range r.Versions {
				delete(r.Versions, id)
			}
		}},
		{"渠道固定的插件版本 meta 损坏", func(r *aiconfigfake.MemRepo) {
			for _, v := range r.Versions {
				v.MetaJSON = model.JSONText(`{oops`)
			}
		}},
		{"渠道后来切到了不支持该种类的插件版本", func(r *aiconfigfake.MemRepo) {
			for _, v := range r.Versions {
				v.MetaJSON = model.JSONText(`{"apiVersion":1,"key":"kling","name":"k","version":"1.0.0","auth":{"type":"none"},"endpoints":{"text":{"mode":"sync"}}}`)
			}
		}},
	}
	for _, tt := range unavailable {
		t.Run("不可用："+tt.name, func(t *testing.T) {
			s, r, _ := aicNewSvc()
			aicSeedChannel(t, s, r, "c1", true)
			aicPublishModel(t, s, "m1", "c1")
			tt.mutate(r)
			if err := s.RefreshRegistry(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Snapshot(ctx, "m1"); !errors.Is(err, provider.ErrModelUnavailable) {
				t.Fatalf("期望 ErrModelUnavailable，实际 %v", err)
			}
			if list, _ := s.ListModels(ctx, ""); len(list) != 0 {
				t.Fatalf("不可用的模型不应出现在清单里：%+v", list)
			}
		})
	}

	t.Run("模型不存在", func(t *testing.T) {
		if _, err := svc.Snapshot(ctx, "nope"); !errors.Is(err, provider.ErrModelUnavailable) {
			t.Fatalf("期望 ErrModelUnavailable，实际 %v", err)
		}
	})
	t.Run("渠道升级到新插件版本后，新快照用新版本，旧快照不受影响", func(t *testing.T) {
		old, _ := svc.Snapshot(ctx, "m1")
		v2 := adminSeedVersion(t, repo, adminMetaJSON("kling", "1.1.0"), model.PluginSourceUploaded, true)
		repo.Channels["c1"].PluginVersionID = v2
		if err := svc.RefreshRegistry(ctx); err != nil {
			t.Fatal(err)
		}
		fresh, err := svc.Snapshot(ctx, "m1")
		if err != nil || fresh.Plugin.Version != "1.1.0" || fresh.Channel.PluginVersionID != v2 {
			t.Fatalf("新快照应用 1.1.0：%+v %v", fresh, err)
		}
		if old.Plugin.Version != "1.0.0" {
			t.Fatalf("已取得的旧快照不应被改动：%+v", old.Plugin)
		}
	})
}

func TestAIConfigService_Registry_CacheAndHotRefresh(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := aicNewSvc()
	clock := time.Now()
	svc.SetNow(func() time.Time { return clock })
	aicSeedChannel(t, svc, repo, "c1", true)
	aicPublishModel(t, svc, "m1", "c1")

	t.Run("缓存命中不再查库", func(t *testing.T) {
		_ = svc.RefreshRegistry(ctx)
		base := repo.LoadPublishedCalls
		for i := 0; i < 5; i++ {
			if _, err := svc.ListModels(ctx, ""); err != nil {
				t.Fatal(err)
			}
		}
		if repo.LoadPublishedCalls != base {
			t.Fatalf("缓存未过期时不应查库：%d -> %d", base, repo.LoadPublishedCalls)
		}
	})

	t.Run("发布后立即热生效", func(t *testing.T) {
		before, _ := svc.Snapshot(ctx, "m1")
		if _, err := aicSave(svc, "m1", false, aicModelBody("m1", "c1", `"deadline":"45m"`)); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Publish(ctx, "m1", 1); err != nil {
			t.Fatal(err)
		}
		after, err := svc.Snapshot(ctx, "m1")
		if err != nil {
			t.Fatal(err)
		}
		if after.ModelRevisionID == before.ModelRevisionID || after.Model.Deadline.D() != 45*time.Minute {
			t.Fatalf("发布后快照应立即指向新 revision：%d -> %d，deadline=%v", before.ModelRevisionID, after.ModelRevisionID, after.Model.Deadline.D())
		}
	})

	t.Run("回滚后立即指回旧 revision", func(t *testing.T) {
		revs, _ := svc.ListRevisions(ctx, "m1")
		var oldID uint64
		for _, r := range revs {
			if r.RevisionNo == 1 {
				oldID = r.ID
			}
		}
		if _, err := svc.Rollback(ctx, "m1", oldID, 1); err != nil {
			t.Fatal(err)
		}
		snap, _ := svc.Snapshot(ctx, "m1")
		if snap.ModelRevisionID != oldID {
			t.Fatalf("回滚后快照应指向旧 revision %d，实际 %d", oldID, snap.ModelRevisionID)
		}
	})

	t.Run("渠道被改动后要刷新才生效（TTL 内沿用旧状态），刷新后立即生效", func(t *testing.T) {
		_ = svc.RefreshRegistry(ctx)
		repo.Channels["c1"].Enabled = false
		if _, err := svc.Snapshot(ctx, "m1"); err != nil {
			t.Fatalf("缓存未过期时仍应使用旧状态：%v", err)
		}
		svc.NotifyChanged(ctx, "test")
		if _, err := svc.Snapshot(ctx, "m1"); !errors.Is(err, provider.ErrModelUnavailable) {
			t.Fatalf("刷新后应立即不可用：%v", err)
		}
		repo.Channels["c1"].Enabled = true
		svc.NotifyChanged(ctx, "test")
	})

	t.Run("缓存过期后重新加载（多实例没有广播时的兜底）", func(t *testing.T) {
		_ = svc.RefreshRegistry(ctx)
		base := repo.LoadPublishedCalls
		clock = clock.Add(AIRegistryTTL + time.Second)
		if _, err := svc.ListModels(ctx, ""); err != nil {
			t.Fatal(err)
		}
		if repo.LoadPublishedCalls != base+1 {
			t.Fatalf("过期后应重新加载一次：%d -> %d", base, repo.LoadPublishedCalls)
		}
	})

	t.Run("重新加载失败时继续使用旧状态", func(t *testing.T) {
		_ = svc.RefreshRegistry(ctx)
		repo.FailLoad = errors.New("db down")
		clock = clock.Add(AIRegistryTTL + time.Second)
		list, err := svc.ListModels(ctx, "")
		if err != nil || len(list) != 1 {
			t.Fatalf("应回退到旧状态：%v %v", err, list)
		}
		repo.FailLoad = nil
	})
}

func TestAIConfigService_Registry_LoadFailureWithoutState(t *testing.T) {
	repo := aiconfigfake.NewMemRepo()
	repo.FailLoad = errors.New("db down")
	svc := NewAIConfigService(repo, repo, repo, "k")
	if _, err := svc.ListModels(context.Background(), ""); err == nil {
		t.Fatal("没有旧状态且加载失败时应返回错误")
	}
	if _, err := svc.Snapshot(context.Background(), "m1"); err == nil || errors.Is(err, provider.ErrModelUnavailable) {
		t.Fatalf("数据库故障不应伪装成模型不可用：%v", err)
	}
}

func TestAIConfigService_Registry_InvalidPublishedConfigIgnored(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := aicNewSvc()
	aicSeedChannel(t, svc, repo, "c1", true)
	aicPublishModel(t, svc, "m1", "c1")
	// 手工把已发布的模型正文改成解析会失败的内容：不应带病运行，也不应拖垮其他模型
	aicPublishModel(t, svc, "m2", "c1")
	for _, r := range repo.Revs {
		if r.TargetKey == "m1" {
			r.BodyJSON = []byte(`{"key":"m1","bad":true}`)
		}
	}
	svc.ResetModelParse() // 发布版本不可变，正常情况下缓存不会失效；这里模拟进程重启后的冷加载
	if err := svc.RefreshRegistry(ctx); err != nil {
		t.Fatal(err)
	}
	list, _ := svc.ListModels(ctx, "")
	if len(list) != 1 || list[0].Key != "m2" {
		t.Fatalf("解析失败的模型应被忽略：%+v", list)
	}
}
