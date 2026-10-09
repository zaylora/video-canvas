package repository_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// aicRepoTestDB 为本用例创建独立 schema 并迁移 AI 配置相关的表；未设置 TEST_DATABASE_DSN 时跳过。
func aicRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return isolatedDB(t, &model.AIModel{}, &model.AIConfigRevision{}, &model.AISecret{})
}

// aicRepoUnique 生成本用例独有的 key，避免用例之间互相污染。
func aicRepoUnique(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func aicRepoSaveModel(t *testing.T, r *AIConfigRepository, key, body string) *model.AIConfigRevision {
	t.Helper()
	rev, err := r.SaveModel(context.Background(), SaveModelInput{
		Pointer: ConfigPointer{Target: model.ConfigTargetModel, Key: key, Kind: "video"},
		Body:    []byte(body), CreatedBy: 7, Note: "n", InitialEnabled: false, InitialSort: 50,
	})
	if err != nil {
		t.Fatalf("保存模型失败：%v", err)
	}
	return rev
}

func TestAIConfigRepository_SaveModel(t *testing.T) {
	db := aicRepoTestDB(t)
	r := NewAIConfigRepository(db)
	ctx := context.Background()
	key := aicRepoUnique("m")

	t.Run("首次保存创建指针行，配置立即成为当前配置", func(t *testing.T) {
		rev := aicRepoSaveModel(t, r, key, `{"a":1}`)
		if rev.CreatedBy != 7 {
			t.Fatalf("revision 不符合预期：%+v", rev)
		}
		p, err := r.GetModelPointer(ctx, key)
		if err != nil || p.Sort != 50 || p.Enabled || p.Kind != "video" {
			t.Fatalf("指针行不符合预期：%+v err=%v", p, err)
		}
		if p.PublishedRevisionID == nil || *p.PublishedRevisionID != rev.ID {
			t.Fatalf("指针应指向刚保存的配置：%+v", p)
		}
	})

	t.Run("再次保存原地覆盖正文，不产生新版本", func(t *testing.T) {
		first, err := r.GetModelConfig(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		rev2 := aicRepoSaveModel(t, r, key, `{"a":2}`)
		if rev2.ID != first.ID {
			t.Fatalf("应覆盖同一行：%d != %d", rev2.ID, first.ID)
		}
		var n int64
		if err := db.Model(&model.AIConfigRevision{}).Where("target_key = ?", key).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("每个模型只应有一行配置：n=%d err=%v", n, err)
		}
		got, err := r.GetModelConfig(ctx, key)
		if err != nil || string(got.BodyJSON) != `{"a": 2}` && string(got.BodyJSON) != `{"a":2}` {
			t.Fatalf("GetModelConfig 异常：%+v %v", got, err)
		}
	})

	t.Run("已有指针行时不覆盖 enabled 与 sort，但同步 kind", func(t *testing.T) {
		if err := r.SetModelEnabled(ctx, key, true); err != nil {
			t.Fatal(err)
		}
		if err := r.SetModelSort(ctx, key, 7); err != nil {
			t.Fatal(err)
		}
		_, err := r.SaveModel(ctx, SaveModelInput{
			Pointer: ConfigPointer{Target: model.ConfigTargetModel, Key: key, Kind: "image"},
			Body:    []byte(`{"a":3}`), InitialEnabled: false, InitialSort: 999,
		})
		if err != nil {
			t.Fatal(err)
		}
		p, _ := r.GetModelPointer(ctx, key)
		if !p.Enabled || p.Sort != 7 || p.Kind != "image" {
			t.Fatalf("指针行不符合预期：%+v", p)
		}
	})

	t.Run("不存在的模型读配置返回 ErrNotFound", func(t *testing.T) {
		if _, err := r.GetModelConfig(ctx, aicRepoUnique("none")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})
}

func TestAIConfigRepository_LoadModels(t *testing.T) {
	db := aicRepoTestDB(t)
	r := NewAIConfigRepository(db)
	ctx := context.Background()

	k1, k2 := aicRepoUnique("m1"), aicRepoUnique("m2")
	md := aicRepoSaveModel(t, r, k1, `{"key":"y"}`)
	aicRepoSaveModel(t, r, k2, `{}`)

	models, err := r.LoadModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("应加载全部模型（含未启用），实际 %d 个：%+v", len(models), models)
	}
	var got *PublishedModel
	for i := range models {
		if models[i].Key == k1 {
			got = &models[i]
		}
	}
	if got == nil || got.RevisionID != md.ID || got.Kind != "video" || len(got.Body) == 0 || got.Sort != 50 || got.Enabled {
		t.Fatalf("模型不符合预期：%+v", got)
	}

	list, err := r.ListModelConfigs(ctx)
	if err != nil || len(list) != 2 || len(list[0].BodyJSON) == 0 {
		t.Fatalf("ListModelConfigs 应带正文：%v %+v", err, list)
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
