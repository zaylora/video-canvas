package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	. "video-canvas/internal/service"

	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/model"
	"video-canvas/internal/service/aiconfigfake"
)

func TestAIConfigService_SeedDefaults(t *testing.T) {
	ctx := context.Background()

	t.Run("不存在时创建并直接发布，且跳过凭证检查", func(t *testing.T) {
		svc, repo, inv := aicNewSvc()
		if err := svc.SeedDefaults(ctx); err != nil {
			t.Fatal(err)
		}
		pub, err := repo.GetPublishedRevision(ctx, model.ConfigTargetProvider, "runninghub")
		if err != nil {
			t.Fatalf("应已发布：%v", err)
		}
		if pub.Status != model.RevisionPublished || pub.RevisionNo != 1 || pub.Note != "系统种子" || pub.CreatedBy != 0 {
			t.Fatalf("种子 revision 不符合预期：%+v", pub)
		}
		if repo.Providers["runninghub"].Name != "RunningHub" {
			t.Fatalf("指针行不符合预期：%+v", repo.Providers["runninghub"])
		}
		if len(repo.Secrets) != 0 {
			t.Fatal("种子不应创建凭证")
		}
		if len(inv.Reasons) != 1 {
			t.Fatalf("应广播一次失效：%v", inv.Reasons)
		}
		// 已加载进 Registry
		p, err := svc.Provider(ctx, "runninghub")
		if err != nil || p.BaseURL != "https://www.runninghub.cn" || p.Auth.Secret != "runninghub_api_key" {
			t.Fatalf("Registry 应能取到种子平台：%v %+v", err, p)
		}
	})

	t.Run("幂等：重复调用不产生新 revision", func(t *testing.T) {
		svc, repo, _ := aicNewSvc()
		for i := 0; i < 3; i++ {
			if err := svc.SeedDefaults(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if len(repo.Revs) != 1 {
			t.Fatalf("revision 数量应为 1，实际 %d", len(repo.Revs))
		}
	})

	t.Run("已存在（运营改过）时不覆盖", func(t *testing.T) {
		svc, repo, _ := aicNewSvc()
		aicPublishProvider(t, svc, "runninghub", "")
		before := len(repo.Revs)
		body := repo.Revs[0].BodyJSON
		if err := svc.SeedDefaults(ctx); err != nil {
			t.Fatal(err)
		}
		if len(repo.Revs) != before || string(repo.Revs[0].BodyJSON) != string(body) {
			t.Fatal("已存在的平台不应被种子改动")
		}
	})

	t.Run("仓储错误透传", func(t *testing.T) {
		boom := errors.New("db down")
		svc := NewAIConfigService(aicSeedFailRepo{MemRepo: aiconfigfake.NewMemRepo(), err: boom}, &aiconfigfake.Validator{}, "k")
		if err := svc.SeedDefaults(ctx); !errors.Is(err, boom) {
			t.Fatalf("应透传仓储错误：%v", err)
		}
	})
}

// aicSeedFailRepo 让 GetProviderPointer 返回非 NotFound 的错误。
type aicSeedFailRepo struct {
	*aiconfigfake.MemRepo
	err error
}

func (r aicSeedFailRepo) GetProviderPointer(ctx context.Context, key string) (*model.AIProvider, error) {
	return nil, r.err
}

func TestAIRunningHubSeed_IsValid(t *testing.T) {
	// 1. 必须是合法 JSON，且带设计文档里的关键字段
	var probe map[string]any
	if err := json.Unmarshal([]byte(AIRunningHubSeed), &probe); err != nil {
		t.Fatalf("种子不是合法 JSON：%v", err)
	}
	if probe["key"] != "runninghub" {
		t.Fatalf("种子 key 不对：%v", probe["key"])
	}
	ops := probe["operations"].(map[string]any)
	for _, op := range []string{"upload", "submit", "query"} {
		if ops[op] == nil {
			t.Fatalf("缺少操作 %s", op)
		}
	}
	if v, ok := ops["cancel"]; !ok || v != nil {
		t.Fatalf("cancel 应显式为 null：%v", v)
	}
	// 2. 能用真实 dsl 校验通过（dsl 实现还是占位时跳过）
	cfg, issues := dsl.ParseProvider([]byte(AIRunningHubSeed))
	if len(issues) == 1 && issues[0].Message == dsl.ErrNotImplemented.Error() {
		t.Skip("dsl.ParseProvider 尚未实现，跳过真实校验")
	}
	if len(issues) > 0 || cfg == nil {
		t.Fatalf("种子应能通过 dsl 校验：%+v", issues)
	}
}
