package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func storageModels() []any {
	return []any{&model.StorageConfig{}, &model.Asset{}, &model.AssetUploadIntent{}, &model.AISecret{}}
}

func newStorageRow(name string) *model.StorageConfig {
	return &model.StorageConfig{
		Name: name, Provider: "aliyun_oss", Region: "cn-hangzhou", Endpoint: "oss-cn-hangzhou.aliyuncs.com",
		Bucket: "vc-bucket", Addressing: "virtual", UseSSL: true, AccessKeyID: "LTAI123", SignedTTLSec: 3600, Version: 1,
	}
}

func TestStorageConfigRepository_CreateAndGet(t *testing.T) {
	db := isolatedDB(t, storageModels()...)
	r := NewStorageConfigRepository(db)
	ctx := context.Background()

	row := newStorageRow("OSS 杭州")
	row.DirectUpload = true
	row.PathPrefix = "assets"
	if err := r.Create(ctx, row); err != nil || row.ID == 0 {
		t.Fatalf("创建失败：id=%d err=%v", row.ID, err)
	}
	got, err := r.GetByID(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "OSS 杭州" || got.Bucket != "vc-bucket" || !got.DirectUpload || got.PathPrefix != "assets" || got.Version != 1 {
		t.Errorf("读回的字段不对：%+v", got)
	}

	t.Run("不存在返回 ErrNotFound", func(t *testing.T) {
		if _, err := r.GetByID(ctx, 9999); !errors.Is(err, ErrNotFound) {
			t.Fatalf("实际：%v", err)
		}
	})
	t.Run("名称重复返回 ErrDuplicate", func(t *testing.T) {
		if err := r.Create(ctx, newStorageRow("OSS 杭州")); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("实际：%v", err)
		}
	})
}

func TestStorageConfigRepository_Update(t *testing.T) {
	db := isolatedDB(t, storageModels()...)
	r := NewStorageConfigRepository(db)
	ctx := context.Background()
	row := newStorageRow("a")
	_ = r.Create(ctx, row)

	t.Run("带版本更新成功并把版本加 1", func(t *testing.T) {
		if err := r.Update(ctx, row.ID, 1, map[string]any{"name": "b", "updated_by": uint64(7)}); err != nil {
			t.Fatal(err)
		}
		got, _ := r.GetByID(ctx, row.ID)
		if got.Name != "b" || got.Version != 2 || got.UpdatedBy != 7 {
			t.Errorf("实际 %+v", got)
		}
	})
	t.Run("版本过期返回 ErrRevisionConflict", func(t *testing.T) {
		if err := r.Update(ctx, row.ID, 1, map[string]any{"name": "c"}); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("实际：%v", err)
		}
	})
	t.Run("记录不存在返回 ErrNotFound", func(t *testing.T) {
		if err := r.Update(ctx, 9999, 1, map[string]any{"name": "c"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("实际：%v", err)
		}
	})
	t.Run("名称与其他存储重复返回 ErrDuplicate", func(t *testing.T) {
		other := newStorageRow("other")
		_ = r.Create(ctx, other)
		if err := r.Update(ctx, other.ID, 1, map[string]any{"name": "b"}); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("实际：%v", err)
		}
	})
	t.Run("记录测试结果不改变版本", func(t *testing.T) {
		at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
		before, _ := r.GetByID(ctx, row.ID)
		if err := r.RecordCheck(ctx, row.ID, false, "AccessDenied", at); err != nil {
			t.Fatal(err)
		}
		got, _ := r.GetByID(ctx, row.ID)
		if got.Version != before.Version || got.LastCheckOK == nil || *got.LastCheckOK || got.LastCheckError != "AccessDenied" || got.LastCheckAt == nil {
			t.Errorf("实际 %+v", got)
		}
	})
}

func TestStorageConfigRepository_SetDefault(t *testing.T) {
	db := isolatedDB(t, storageModels()...)
	r := NewStorageConfigRepository(db)
	ctx := context.Background()
	a, b := newStorageRow("a"), newStorageRow("b")
	_ = r.Create(ctx, a)
	_ = r.Create(ctx, b)

	if _, err := r.GetDefault(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("没有默认存储时应返回 ErrNotFound，实际：%v", err)
	}
	if err := r.SetDefault(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.SetDefault(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	def, err := r.GetDefault(ctx)
	if err != nil || def.ID != b.ID {
		t.Fatalf("默认应是 b：%+v err=%v", def, err)
	}
	var n int64
	db.Model(&model.StorageConfig{}).Where("is_default").Count(&n)
	if n != 1 {
		t.Errorf("默认存储只能有一个，实际 %d", n)
	}
	if err := r.SetDefault(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在的存储：%v", err)
	}
	// 失败的切换不能把原来的默认清掉
	if def, _ := r.GetDefault(ctx); def == nil || def.ID != b.ID {
		t.Errorf("切换失败不应改变默认存储：%+v", def)
	}
}

func TestStorageConfigRepository_ListWithAssetCounts(t *testing.T) {
	db := isolatedDB(t, storageModels()...)
	r := NewStorageConfigRepository(db)
	ar := NewAssetRepository(db)
	ctx := context.Background()
	a, b := newStorageRow("a"), newStorageRow("b")
	_ = r.Create(ctx, a)
	_ = r.Create(ctx, b)
	for i, key := range []string{"u1/1.png", "u1/2.png", "u1/3.png"} {
		sid := a.ID
		if i == 2 {
			sid = b.ID
		}
		if err := ar.Create(ctx, &model.Asset{UserID: 1, Kind: "image", StorageKey: key, MimeType: "image/png", Source: "upload", StorageID: sid}); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := r.List(ctx)
	if err != nil || len(rows) != 2 || rows[0].ID != a.ID {
		t.Fatalf("应按 id 升序返回两条：%+v err=%v", rows, err)
	}
	counts, err := r.AssetCounts(ctx)
	if err != nil || counts[a.ID] != 2 || counts[b.ID] != 1 {
		t.Fatalf("素材数不对：%v err=%v", counts, err)
	}
}

func TestStorageConfigRepository_Delete(t *testing.T) {
	db := isolatedDB(t, storageModels()...)
	r := NewStorageConfigRepository(db)
	ar := NewAssetRepository(db)
	ir := NewUploadIntentRepository(db)
	ctx := context.Background()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

	used, pending, free := newStorageRow("used"), newStorageRow("pending"), newStorageRow("free")
	for _, s := range []*model.StorageConfig{used, pending, free} {
		_ = r.Create(ctx, s)
	}
	_ = ar.Create(ctx, &model.Asset{UserID: 1, Kind: "image", StorageKey: "u1/a.png", MimeType: "image/png", Source: "upload", StorageID: used.ID})
	_ = ir.Create(ctx, &model.AssetUploadIntent{UserID: 1, StorageID: pending.ID, StorageKey: "u1/p.png", ExpiresAt: now.Add(time.Minute)})

	if err := r.Delete(ctx, used.ID, now); !errors.Is(err, ErrInUse) {
		t.Errorf("有素材引用应返回 ErrInUse，实际：%v", err)
	}
	if err := r.Delete(ctx, pending.ID, now); !errors.Is(err, ErrInUse) {
		t.Errorf("有未完成的上传意图应返回 ErrInUse，实际：%v", err)
	}
	if err := r.Delete(ctx, pending.ID, now.Add(time.Hour)); err != nil {
		t.Errorf("意图已过期就不算引用：%v", err)
	}
	_ = db.Create(&model.AISecret{Name: model.StorageSecretName(free.ID), Ciphertext: []byte("x"), Nonce: []byte("y")}).Error
	if err := r.Delete(ctx, free.ID, now); err != nil {
		t.Errorf("无引用应删除成功：%v", err)
	}
	var secrets int64
	db.Model(&model.AISecret{}).Where("name = ?", model.StorageSecretName(free.ID)).Count(&secrets)
	if secrets != 0 {
		t.Errorf("删除存储应同时删掉它的密钥，实际还剩 %d 行", secrets)
	}
	if _, err := r.GetByID(ctx, free.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除后应查不到：%v", err)
	}
	if err := r.Delete(ctx, 9999, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在：%v", err)
	}
}

func TestAssetRepository_StorageFields(t *testing.T) {
	db := isolatedDB(t, storageModels()...)
	r := NewAssetRepository(db)
	ctx := context.Background()

	a := &model.Asset{UserID: 5, Kind: "image", StorageKey: "u5/202610/a.png", MimeType: "image/png", Source: "upload", StorageID: 3}
	if err := r.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	t.Run("按 storage_key 反查（不带用户条件，供 /files 路由使用）", func(t *testing.T) {
		got, err := r.GetByStorageKey(ctx, "u5/202610/a.png")
		if err != nil || got.ID != a.ID || got.StorageID != 3 {
			t.Fatalf("实际 %+v err=%v", got, err)
		}
		if _, err := r.GetByStorageKey(ctx, "u5/none.png"); !errors.Is(err, ErrNotFound) {
			t.Errorf("实际：%v", err)
		}
	})
	t.Run("storage_key 唯一", func(t *testing.T) {
		dup := &model.Asset{UserID: 6, Kind: "image", StorageKey: "u5/202610/a.png", MimeType: "image/png", Source: "upload", StorageID: 3}
		if err := r.Create(ctx, dup); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("实际：%v", err)
		}
	})
}

func TestUploadIntentRepository(t *testing.T) {
	db := isolatedDB(t, storageModels()...)
	r := NewUploadIntentRepository(db)
	ctx := context.Background()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

	in := &model.AssetUploadIntent{UserID: 1, StorageID: 2, StorageKey: "u1/202610/x.mp4", FileName: "x.mp4", MimeType: "video/mp4", Kind: "video", DeclaredSize: 100, ExpiresAt: now.Add(15 * time.Minute)}
	if err := r.Create(ctx, in); err != nil || in.ID == 0 {
		t.Fatalf("创建失败：%v", err)
	}

	t.Run("按用户读取，别人的意图查不到", func(t *testing.T) {
		got, err := r.GetByID(ctx, 1, in.ID)
		if err != nil || got.StorageID != 2 || got.StorageKey != "u1/202610/x.mp4" {
			t.Fatalf("实际 %+v err=%v", got, err)
		}
		if _, err := r.GetByID(ctx, 2, in.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("别人的意图应返回 ErrNotFound，实际：%v", err)
		}
	})
	t.Run("过期未完成的可被清理任务列出", func(t *testing.T) {
		if list, _ := r.ListExpired(ctx, now, 10); len(list) != 0 {
			t.Errorf("未过期不应列出：%+v", list)
		}
		list, err := r.ListExpired(ctx, now.Add(time.Hour), 10)
		if err != nil || len(list) != 1 || list[0].ID != in.ID {
			t.Errorf("过期应列出：%+v err=%v", list, err)
		}
	})
	t.Run("完成只能一次", func(t *testing.T) {
		if err := r.Complete(ctx, in.ID, now); err != nil {
			t.Fatal(err)
		}
		if err := r.Complete(ctx, in.ID, now); !errors.Is(err, ErrNotFound) {
			t.Errorf("重复完成应返回 ErrNotFound，实际：%v", err)
		}
		if list, _ := r.ListExpired(ctx, now.Add(time.Hour), 10); len(list) != 0 {
			t.Errorf("已完成的不应再被当成孤儿：%+v", list)
		}
	})
	t.Run("删除", func(t *testing.T) {
		if err := r.Delete(ctx, in.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetByID(ctx, 1, in.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("实际：%v", err)
		}
	})
}

func TestEnsureBuiltinStorage(t *testing.T) {
	db := isolatedDB(t, storageModels()...)
	ctx := context.Background()
	ar := NewAssetRepository(db)

	// 升级前留下的素材：storage_id 为 0
	_ = ar.Create(ctx, &model.Asset{UserID: 1, Kind: "image", StorageKey: "u1/old1.png", MimeType: "image/png", Source: "upload"})
	_ = ar.Create(ctx, &model.Asset{UserID: 1, Kind: "image", StorageKey: "u1/old2.png", MimeType: "image/png", Source: "upload"})

	t.Run("创建内置本地存储、设为默认并回填旧素材", func(t *testing.T) {
		id, err := EnsureBuiltinStorage(ctx, db)
		if err != nil || id == 0 {
			t.Fatalf("实际 id=%d err=%v", id, err)
		}
		r := NewStorageConfigRepository(db)
		def, err := r.GetDefault(ctx)
		if err != nil || def.ID != id || !def.Builtin || def.Provider != "local" {
			t.Fatalf("内置存储应是默认：%+v err=%v", def, err)
		}
		var zero int64
		db.Model(&model.Asset{}).Where("storage_id = 0").Count(&zero)
		if zero != 0 {
			t.Errorf("仍有 %d 个素材没回填", zero)
		}
		a, _ := ar.GetByStorageKey(ctx, "u1/old1.png")
		if a.StorageID != id {
			t.Errorf("旧素材应指向内置存储 %d，实际 %d", id, a.StorageID)
		}
	})
	t.Run("重复执行幂等", func(t *testing.T) {
		id1, _ := EnsureBuiltinStorage(ctx, db)
		id2, err := EnsureBuiltinStorage(ctx, db)
		if err != nil || id1 != id2 {
			t.Fatalf("两次应得到同一个 id：%d %d err=%v", id1, id2, err)
		}
		var n int64
		db.Model(&model.StorageConfig{}).Where("builtin").Count(&n)
		if n != 1 {
			t.Errorf("内置存储只能有一条，实际 %d", n)
		}
	})
	t.Run("已有别的默认存储时不抢默认", func(t *testing.T) {
		r := NewStorageConfigRepository(db)
		other := newStorageRow("other")
		_ = r.Create(ctx, other)
		_ = r.SetDefault(ctx, other.ID)
		_, _ = EnsureBuiltinStorage(ctx, db)
		def, _ := r.GetDefault(ctx)
		if def.ID != other.ID {
			t.Errorf("默认存储不应被改回内置：%+v", def)
		}
	})
}
