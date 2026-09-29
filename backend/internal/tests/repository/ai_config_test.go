package repository_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
	. "video-canvas/internal/repository"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"video-canvas/internal/model"
)

// aicRepoTestDB 连接专用测试库并迁移 AI 配置相关的表；未设置 TEST_DATABASE_DSN 时跳过。
func aicRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("未设置 TEST_DATABASE_DSN，跳过仓储集成测试")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatalf("连接测试库失败：%v", err)
	}
	if err := db.AutoMigrate(&model.AIProvider{}, &model.AIModel{}, &model.AIConfigRevision{}, &model.AISecret{}); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	return db
}

// aicRepoUnique 生成本用例独有的 key，避免用例之间互相污染。
func aicRepoUnique(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func aicRepoSaveModel(t *testing.T, r *AIConfigRepository, key, body string) *model.AIConfigRevision {
	t.Helper()
	rev, err := r.SaveDraft(context.Background(), SaveDraftInput{
		Pointer: ConfigPointer{Target: model.ConfigTargetModel, Key: key, Kind: "video", ProviderKey: "p1"},
		Body:    []byte(body), CreatedBy: 7, Note: "n", InitialEnabled: false, InitialSort: 50,
	})
	if err != nil {
		t.Fatalf("保存草稿失败：%v", err)
	}
	return rev
}

func TestAIConfigRepository_SaveDraft(t *testing.T) {
	db := aicRepoTestDB(t)
	r := NewAIConfigRepository(db)
	ctx := context.Background()
	key := aicRepoUnique("m")

	t.Run("首次保存创建指针行且版本号从 1 开始", func(t *testing.T) {
		rev := aicRepoSaveModel(t, r, key, `{"a":1}`)
		if rev.RevisionNo != 1 || rev.Status != model.RevisionDraft || rev.CreatedBy != 7 {
			t.Fatalf("revision 不符合预期：%+v", rev)
		}
		p, err := r.GetModelPointer(ctx, key)
		if err != nil || p.Sort != 50 || p.Enabled || p.PublishedRevisionID != nil || p.Kind != "video" {
			t.Fatalf("指针行不符合预期：%+v err=%v", p, err)
		}
	})

	t.Run("再次保存版本号递增且旧草稿归档，只留一个 draft", func(t *testing.T) {
		rev2 := aicRepoSaveModel(t, r, key, `{"a":2}`)
		if rev2.RevisionNo != 2 {
			t.Fatalf("期望 revision_no=2，实际 %d", rev2.RevisionNo)
		}
		list, err := r.ListRevisions(ctx, model.ConfigTargetModel, key, 10)
		if err != nil || len(list) != 2 {
			t.Fatalf("历史列表异常：%v %d", err, len(list))
		}
		if list[0].RevisionNo != 2 || list[0].Status != model.RevisionDraft || list[1].Status != model.RevisionArchived {
			t.Fatalf("状态不符合预期：%+v", list)
		}
		if len(list[0].BodyJSON) != 0 {
			t.Fatalf("历史列表不应带正文")
		}
		d, err := r.GetDraft(ctx, model.ConfigTargetModel, key)
		if err != nil || d.ID != rev2.ID || string(d.BodyJSON) != `{"a": 2}` && string(d.BodyJSON) != `{"a":2}` {
			t.Fatalf("GetDraft 异常：%+v %v", d, err)
		}
	})

	t.Run("已有指针行时不覆盖 enabled 与 sort，但同步 kind", func(t *testing.T) {
		if err := r.SetModelEnabled(ctx, key, true); err != nil {
			t.Fatal(err)
		}
		if err := r.SetModelSort(ctx, key, 7); err != nil {
			t.Fatal(err)
		}
		_, err := r.SaveDraft(ctx, SaveDraftInput{
			Pointer: ConfigPointer{Target: model.ConfigTargetModel, Key: key, Kind: "image", ProviderKey: "p2"},
			Body:    []byte(`{"a":3}`), InitialEnabled: false, InitialSort: 999,
		})
		if err != nil {
			t.Fatal(err)
		}
		p, _ := r.GetModelPointer(ctx, key)
		if !p.Enabled || p.Sort != 7 || p.Kind != "image" || p.ProviderKey != "p2" {
			t.Fatalf("指针行不符合预期：%+v", p)
		}
	})

	t.Run("平台草稿同步 name", func(t *testing.T) {
		pk := aicRepoUnique("prov")
		_, err := r.SaveDraft(ctx, SaveDraftInput{Pointer: ConfigPointer{Target: model.ConfigTargetProvider, Key: pk, Name: "平台A"}, Body: []byte(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		p, err := r.GetProviderPointer(ctx, pk)
		if err != nil || p.Name != "平台A" {
			t.Fatalf("%+v %v", p, err)
		}
	})
}

func TestAIConfigRepository_PublishAndRollback(t *testing.T) {
	db := aicRepoTestDB(t)
	r := NewAIConfigRepository(db)
	ctx := context.Background()
	key := aicRepoUnique("m")
	ptr := ConfigPointer{Target: model.ConfigTargetModel, Key: key, Kind: "video", ProviderKey: "p1"}

	v1 := aicRepoSaveModel(t, r, key, `{"v":1}`)

	t.Run("尚未发布时没有已发布版本", func(t *testing.T) {
		if _, err := r.GetPublishedRevision(ctx, model.ConfigTargetModel, key); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})

	t.Run("发布草稿后指针指向它", func(t *testing.T) {
		pub, err := r.PublishDraft(ctx, ptr, v1.ID)
		if err != nil || pub.Status != model.RevisionPublished {
			t.Fatalf("发布失败：%+v %v", pub, err)
		}
		got, err := r.GetPublishedRevision(ctx, model.ConfigTargetModel, key)
		if err != nil || got.ID != v1.ID {
			t.Fatalf("已发布版本不符合预期：%+v %v", got, err)
		}
		p, _ := r.GetModelPointer(ctx, key)
		if p.PublishedRevisionID == nil || *p.PublishedRevisionID != v1.ID {
			t.Fatalf("指针未更新：%+v", p)
		}
	})

	t.Run("非 draft 的 revision 不能再次发布，返回冲突", func(t *testing.T) {
		if _, err := r.PublishDraft(ctx, ptr, v1.ID); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("期望 ErrRevisionConflict，实际 %v", err)
		}
	})

	v2 := aicRepoSaveModel(t, r, key, `{"v":2}`)

	t.Run("发布新版本后旧发布版归档", func(t *testing.T) {
		if _, err := r.PublishDraft(ctx, ptr, v2.ID); err != nil {
			t.Fatal(err)
		}
		old, _ := r.GetRevision(ctx, v1.ID)
		if old.Status != model.RevisionArchived {
			t.Fatalf("旧版本应为 archived，实际 %s", old.Status)
		}
	})

	t.Run("被新草稿顶替的旧草稿发布时返回冲突", func(t *testing.T) {
		d1 := aicRepoSaveModel(t, r, key, `{"v":3}`)
		_ = aicRepoSaveModel(t, r, key, `{"v":4}`)
		if _, err := r.PublishDraft(ctx, ptr, d1.ID); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("期望 ErrRevisionConflict，实际 %v", err)
		}
	})

	t.Run("不属于该目标的 revision 返回 ErrNotFound", func(t *testing.T) {
		other := aicRepoUnique("other")
		o := aicRepoSaveModel(t, r, other, `{}`)
		if _, err := r.PublishDraft(ctx, ptr, o.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})

	t.Run("回滚到旧版本：旧版改回 published，当前版归档", func(t *testing.T) {
		if _, err := r.Rollback(ctx, ptr, v1.ID); err != nil {
			t.Fatal(err)
		}
		got, _ := r.GetPublishedRevision(ctx, model.ConfigTargetModel, key)
		if got.ID != v1.ID {
			t.Fatalf("回滚后应发布 v1，实际 %d", got.ID)
		}
		cur, _ := r.GetRevision(ctx, v2.ID)
		if cur.Status != model.RevisionArchived {
			t.Fatalf("v2 应为 archived，实际 %s", cur.Status)
		}
		// 回滚不动当前 draft
		if d, err := r.GetDraft(ctx, model.ConfigTargetModel, key); err != nil || string(d.BodyJSON) == "" {
			t.Fatalf("回滚不应影响草稿：%v", err)
		}
	})

	t.Run("回滚到 published 或 draft 的 revision 返回冲突", func(t *testing.T) {
		if _, err := r.Rollback(ctx, ptr, v1.ID); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("期望 ErrRevisionConflict，实际 %v", err)
		}
	})

	t.Run("指针行不存在时发布返回 ErrNotFound", func(t *testing.T) {
		if _, err := r.PublishDraft(ctx, ConfigPointer{Target: model.ConfigTargetModel, Key: aicRepoUnique("none")}, 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})
}

func TestAIConfigRepository_LoadPublished(t *testing.T) {
	db := aicRepoTestDB(t)
	r := NewAIConfigRepository(db)
	ctx := context.Background()

	pk := aicRepoUnique("prov")
	pptr := ConfigPointer{Target: model.ConfigTargetProvider, Key: pk, Name: "P"}
	pd, err := r.SaveDraft(ctx, SaveDraftInput{Pointer: pptr, Body: []byte(`{"key":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	mk := aicRepoUnique("m")
	mptr := ConfigPointer{Target: model.ConfigTargetModel, Key: mk, Kind: "image", ProviderKey: pk}
	md := aicRepoSaveModel(t, r, mk, `{"key":"y"}`)
	unpub := aicRepoUnique("unpub")
	aicRepoSaveModel(t, r, unpub, `{}`)

	if _, err := r.PublishDraft(ctx, pptr, pd.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PublishDraft(ctx, mptr, md.ID); err != nil {
		t.Fatal(err)
	}

	provs, err := r.LoadPublishedProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundP := false
	for _, p := range provs {
		if p.Key == pk {
			foundP = true
			if p.RevisionID != pd.ID || p.Name != "P" || len(p.Body) == 0 {
				t.Fatalf("已发布平台不符合预期：%+v", p)
			}
		}
	}
	if !foundP {
		t.Fatal("已发布平台未出现在加载结果里")
	}

	models, err := r.LoadPublishedModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundM, foundUnpub := false, false
	for _, m := range models {
		if m.Key == mk {
			foundM = true
			if m.RevisionID != md.ID || m.Kind != "image" || m.ProviderKey != pk || len(m.Body) == 0 || m.Sort != 50 {
				t.Fatalf("已发布模型不符合预期：%+v", m)
			}
		}
		if m.Key == unpub {
			foundUnpub = true
		}
	}
	if !foundM || foundUnpub {
		t.Fatalf("加载结果不符合预期：found=%v unpublishedFound=%v", foundM, foundUnpub)
	}

	heads, err := r.ListRevisionHeads(ctx, model.ConfigTargetModel, false)
	if err != nil || len(heads) == 0 {
		t.Fatalf("ListRevisionHeads 异常：%v", err)
	}
}

func TestAIConfigRepository_ModelPointer(t *testing.T) {
	db := aicRepoTestDB(t)
	r := NewAIConfigRepository(db)
	ctx := context.Background()
	key := aicRepoUnique("m")
	aicRepoSaveModel(t, r, key, `{}`)

	t.Run("上下架与排序", func(t *testing.T) {
		if err := r.SetModelEnabled(ctx, key, true); err != nil {
			t.Fatal(err)
		}
		if err := r.SetModelSort(ctx, key, 3); err != nil {
			t.Fatal(err)
		}
		p, _ := r.GetModelPointer(ctx, key)
		if !p.Enabled || p.Sort != 3 {
			t.Fatalf("%+v", p)
		}
	})
	t.Run("不存在的模型返回 ErrNotFound", func(t *testing.T) {
		if err := r.SetModelEnabled(ctx, aicRepoUnique("none"), true); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
		if err := r.SetModelSort(ctx, aicRepoUnique("none"), 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})
	t.Run("列表包含该模型", func(t *testing.T) {
		list, err := r.ListModelPointers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, m := range list {
			found = found || m.Key == key
		}
		if !found {
			t.Fatal("指针列表缺少模型")
		}
	})
}

func TestAIConfigRepository_Secrets(t *testing.T) {
	db := aicRepoTestDB(t)
	r := NewAIConfigRepository(db)
	ctx := context.Background()
	name := aicRepoUnique("secret")

	t.Run("不存在返回 ErrNotFound", func(t *testing.T) {
		if _, err := r.GetSecret(ctx, name); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})
	t.Run("写入后可读，再次写入覆盖", func(t *testing.T) {
		if err := r.UpsertSecret(ctx, &model.AISecret{Name: name, Ciphertext: []byte("c1"), Nonce: []byte("n1"), KeyVersion: 1, UpdatedBy: 3}); err != nil {
			t.Fatal(err)
		}
		if err := r.UpsertSecret(ctx, &model.AISecret{Name: name, Ciphertext: []byte("c2"), Nonce: []byte("n2"), KeyVersion: 1, UpdatedBy: 4}); err != nil {
			t.Fatal(err)
		}
		s, err := r.GetSecret(ctx, name)
		if err != nil || string(s.Ciphertext) != "c2" || string(s.Nonce) != "n2" || s.UpdatedBy != 4 {
			t.Fatalf("凭证不符合预期：%+v %v", s, err)
		}
	})
	t.Run("列表不含密文与 nonce", func(t *testing.T) {
		list, err := r.ListSecrets(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, s := range list {
			if s.Name == name {
				found = true
				if len(s.Ciphertext) != 0 || len(s.Nonce) != 0 || s.UpdatedBy != 4 || s.UpdatedAt.IsZero() {
					t.Fatalf("列表项不符合预期：%+v", s)
				}
			}
		}
		if !found {
			t.Fatal("列表缺少凭证")
		}
	})
}
