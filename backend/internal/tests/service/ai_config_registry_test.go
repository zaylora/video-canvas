package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	. "video-canvas/internal/service"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/service/aiconfigfake"
)

// aicCountValidator 统计 ParseModel / ParseProvider 的调用次数，用来断言按 revision_id 的解析缓存生效。
type aicCountValidator struct {
	aiconfigfake.Validator
	modelParses    int
	providerParses int
}

func (v *aicCountValidator) ParseModel(body []byte, p *dsl.ProviderConfig) (*dsl.ModelConfig, []dsl.Issue) {
	v.modelParses++
	return v.Validator.ParseModel(body, p)
}

func (v *aicCountValidator) ParseProvider(body []byte) (*dsl.ProviderConfig, []dsl.Issue) {
	v.providerParses++
	return v.Validator.ParseProvider(body)
}

func TestAIConfigService_Registry_ListModels(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := aicNewSvc()
	aicPublishProvider(t, svc, "p1", "s1")
	aicPublishModel(t, svc, "m-b", "p1")
	aicPublishModel(t, svc, "m-a", "p1")
	// 一个 image 模型，排序更靠前
	if _, err := svc.SaveDraft(ctx, model.ConfigTargetModel, "", true, []byte(`{"key":"m-img","kind":"image","provider":"p1","label":"图","credits":2,"enabled":true,"sort":1,"input_schema":{"prompt":{"type":"text","label":"p"}}}`), "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, model.ConfigTargetModel, "m-img", 1); err != nil {
		t.Fatal(err)
	}
	// 只有草稿、未发布的模型不出现
	if _, err := svc.SaveDraft(ctx, model.ConfigTargetModel, "", true, aicModelBody("m-draft", "p1", ""), "", 1); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		kind string
		want []string
	}{
		{"全部按 sort 再按 key 排序", "", []string{"m-img", "m-a", "m-b"}},
		{"按 kind 过滤 video", "video", []string{"m-a", "m-b"}},
		{"按 kind 过滤 image", "image", []string{"m-img"}},
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
		if m.Label != "模型-m-b" || m.Hint != "提示" || m.Credits != 5 || m.Kind != "video" {
			t.Fatalf("公开字段不符合预期：%+v", m)
		}
		if _, ok := m.InputSchema.Get("prompt"); !ok {
			t.Fatal("input_schema 应带上")
		}
	})
}

func TestAIConfigService_Registry_Snapshot(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := aicNewSvc()
	aicPublishProvider(t, svc, "p1", "s1")
	aicPublishModel(t, svc, "m1", "p1")
	// 平台没发布的模型（直接造数据：模型已发布但平台指针为空）
	if _, err := svc.SaveDraft(ctx, model.ConfigTargetProvider, "", true, aicProviderBody("p2", "", ""), "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveDraft(ctx, model.ConfigTargetModel, "", true, aicModelBody("m-orphan", "p2", ""), "", 1); err != nil {
		t.Fatal(err)
	}
	orphan, _ := repo.GetDraft(ctx, model.ConfigTargetModel, "m-orphan")
	rid := orphan.ID
	repo.Models["m-orphan"].PublishedRevisionID = &rid
	repo.Revs[len(repo.Revs)-1].Status = model.RevisionPublished
	if err := svc.RefreshRegistry(ctx); err != nil {
		t.Fatal(err)
	}

	t.Run("冻结模型与平台的发布版本及 revision id", func(t *testing.T) {
		snap, err := svc.Snapshot(ctx, "m1")
		if err != nil {
			t.Fatal(err)
		}
		if snap.Model.Key != "m1" || snap.Provider.Key != "p1" || snap.Provider.Auth.Secret != "s1" {
			t.Fatalf("快照内容不符合预期：%+v", snap)
		}
		pub, _ := repo.GetPublishedRevision(ctx, model.ConfigTargetModel, "m1")
		ppub, _ := repo.GetPublishedRevision(ctx, model.ConfigTargetProvider, "p1")
		if snap.ModelRevisionID != pub.ID || snap.ProviderRevisionID != ppub.ID {
			t.Fatalf("revision id 不符合预期：%+v", snap)
		}
		if !snap.Model.Enabled || snap.Model.Sort != 10 {
			t.Fatalf("enabled / sort 应取指针行的运行时值：%+v", snap.Model)
		}
	})
	t.Run("模型不存在", func(t *testing.T) {
		if _, err := svc.Snapshot(ctx, "nope"); !errors.Is(err, provider.ErrModelUnavailable) {
			t.Fatalf("期望 ErrModelUnavailable，实际 %v", err)
		}
	})
	t.Run("已下线", func(t *testing.T) {
		repo.Models["m1"].Enabled = false
		_ = svc.RefreshRegistry(ctx)
		if _, err := svc.Snapshot(ctx, "m1"); !errors.Is(err, provider.ErrModelUnavailable) {
			t.Fatalf("期望 ErrModelUnavailable，实际 %v", err)
		}
		repo.Models["m1"].Enabled = true
		_ = svc.RefreshRegistry(ctx)
	})
	t.Run("平台没有发布", func(t *testing.T) {
		if _, err := svc.Snapshot(ctx, "m-orphan"); !errors.Is(err, provider.ErrModelUnavailable) {
			t.Fatalf("期望 ErrModelUnavailable，实际 %v", err)
		}
		list, _ := svc.ListModels(ctx, "")
		for _, m := range list {
			if m.Key == "m-orphan" {
				t.Fatal("平台没发布的模型不应出现在清单里")
			}
		}
	})
	t.Run("Provider 查询", func(t *testing.T) {
		p, err := svc.Provider(ctx, "p1")
		if err != nil || p.Key != "p1" {
			t.Fatalf("Provider 查询失败：%v %+v", err, p)
		}
		if _, err := svc.Provider(ctx, "p2"); !errors.Is(err, provider.ErrModelUnavailable) {
			t.Fatalf("未发布的平台期望 ErrModelUnavailable，实际 %v", err)
		}
	})
}

func TestAIConfigService_Registry_CacheAndHotRefresh(t *testing.T) {
	ctx := context.Background()
	repo := aiconfigfake.NewMemRepo()
	val := &aicCountValidator{}
	svc := NewAIConfigService(repo, val, "k")
	clock := time.Now()
	svc.SetNow(func() time.Time { return clock })
	aicPublishProvider(t, svc, "p1", "")
	aicPublishModel(t, svc, "m1", "p1")

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

	t.Run("发布后立即热生效且已解析的 revision 不重复解析", func(t *testing.T) {
		before, _ := svc.Snapshot(ctx, "m1")
		parsesBefore := val.modelParses
		if _, err := svc.SaveDraft(ctx, model.ConfigTargetModel, "m1", false, aicModelBody("m1", "p1", `"credits_note":"v2"`), "", 1); err != nil {
			t.Fatal(err)
		}
		parsesAfterSave := val.modelParses
		if _, err := svc.Publish(ctx, model.ConfigTargetModel, "m1", 1); err != nil {
			t.Fatal(err)
		}
		after, err := svc.Snapshot(ctx, "m1")
		if err != nil {
			t.Fatal(err)
		}
		if after.ModelRevisionID == before.ModelRevisionID {
			t.Fatal("发布后快照应立即指向新 revision")
		}
		// 发布时校验解析 1 次（checkPublishable 内的 collectIssues）+ 加载新 revision 1 次；旧 revision 已被清理，不会再解析
		if val.modelParses-parsesAfterSave > 2 || parsesAfterSave <= parsesBefore {
			t.Fatalf("解析次数异常：before=%d save=%d publish=%d", parsesBefore, parsesAfterSave, val.modelParses)
		}
	})

	t.Run("没有变化的 revision 在下次刷新时命中解析缓存", func(t *testing.T) {
		before := val.modelParses
		provBefore := val.providerParses
		_ = svc.RefreshRegistry(ctx)
		_ = svc.RefreshRegistry(ctx)
		if val.modelParses != before || val.providerParses != provBefore {
			t.Fatalf("同一 revision 不应重复解析：model %d->%d provider %d->%d", before, val.modelParses, provBefore, val.providerParses)
		}
	})

	t.Run("回滚后立即指回旧 revision", func(t *testing.T) {
		revs, _ := svc.ListRevisions(ctx, model.ConfigTargetModel, "m1")
		var oldID uint64
		for _, r := range revs {
			if r.RevisionNo == 1 {
				oldID = r.ID
			}
		}
		if _, err := svc.Rollback(ctx, model.ConfigTargetModel, "m1", oldID, 1); err != nil {
			t.Fatal(err)
		}
		snap, _ := svc.Snapshot(ctx, "m1")
		if snap.ModelRevisionID != oldID {
			t.Fatalf("回滚后快照应指向旧 revision %d，实际 %d", oldID, snap.ModelRevisionID)
		}
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
	svc := NewAIConfigService(repo, &aiconfigfake.Validator{}, "k")
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
	aicPublishProvider(t, svc, "p1", "")
	aicPublishModel(t, svc, "m1", "p1")
	// 手工把已发布的模型正文改成解析会失败的内容：不应带病运行，也不应拖垮其他模型
	aicPublishModel(t, svc, "m2", "p1")
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
