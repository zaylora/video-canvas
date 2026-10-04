package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func processorModels() []any {
	return []any{&model.ImageProcessor{}, &model.ImageProcessorVersion{}, &model.Asset{}}
}

func newProcessor(name string, storageID uint64, cfg string) *model.ImageProcessor {
	return &model.ImageProcessor{Name: name, Vendor: "cloudflare", StorageID: storageID, Status: model.ProcessorDraft, Config: model.JSONText(cfg), Version: 1}
}

func TestImageProcessorRepository_CreateGetUpdate(t *testing.T) {
	db := isolatedDB(t, processorModels()...)
	r := NewImageProcessorRepository(db)
	ctx := context.Background()

	p := newProcessor("R2 处理", 2, `{"domain":"a.example.com"}`)
	if err := r.Create(ctx, p); err != nil || p.ID == 0 {
		t.Fatalf("新建失败：%v %+v", err, p)
	}
	if err := r.Create(ctx, newProcessor("R2 处理", 3, `{}`)); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("名称重复应返回 ErrDuplicate，实际 %v", err)
	}
	got, err := r.GetByID(ctx, p.ID)
	if err != nil || got.Name != "R2 处理" || string(got.Config) != `{"domain":"a.example.com"}` {
		t.Fatalf("查询结果不对：%v %+v", err, got)
	}
	if _, err := r.GetByID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在应返回 ErrNotFound，实际 %v", err)
	}

	t.Run("乐观锁更新", func(t *testing.T) {
		if err := r.Update(ctx, p.ID, 1, map[string]any{"name": "新名字"}); err != nil {
			t.Fatal(err)
		}
		got, _ := r.GetByID(ctx, p.ID)
		if got.Name != "新名字" || got.Version != 2 {
			t.Fatalf("更新后应为 version 2：%+v", got)
		}
		if err := r.Update(ctx, p.ID, 1, map[string]any{"name": "x"}); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("旧版本应返回 ErrRevisionConflict，实际 %v", err)
		}
		if err := r.Update(ctx, 999, 1, map[string]any{"name": "x"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("不存在应返回 ErrNotFound，实际 %v", err)
		}
	})

	t.Run("记录校验结果不改 version", func(t *testing.T) {
		if err := r.SetCheck(ctx, p.ID, model.JSONText(`{"ok":true}`)); err != nil {
			t.Fatal(err)
		}
		got, _ := r.GetByID(ctx, p.ID)
		if got.Version != 2 || string(got.CheckResult) != `{"ok":true}` {
			t.Fatalf("实际 %+v", got)
		}
	})
}

func TestImageProcessorRepository_PublishRollbackDisable(t *testing.T) {
	db := isolatedDB(t, processorModels()...)
	r := NewImageProcessorRepository(db)
	ctx := context.Background()
	now := time.Now()

	a := newProcessor("A", 5, `{"v":"a1"}`)
	b := newProcessor("B", 5, `{"v":"b1"}`)
	for _, p := range []*model.ImageProcessor{a, b} {
		if err := r.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("发布：版本号从 1 开始，线上配置是发布时的快照", func(t *testing.T) {
		n, err := r.Publish(ctx, PublishParams{ID: a.ID, Version: 1, TrialResult: model.JSONText(`{"ok":true}`), By: 7, At: now})
		if err != nil || n != 1 {
			t.Fatalf("实际 %d %v", n, err)
		}
		got, _ := r.GetByID(ctx, a.ID)
		if got.Status != model.ProcessorPublished || got.PublishedVersion != 1 || string(got.PublishedConfig) != `{"v":"a1"}` {
			t.Fatalf("实际 %+v", got)
		}
		pub, err := r.GetPublishedByStorage(ctx, 5)
		if err != nil || pub.ID != a.ID {
			t.Fatalf("存储 5 的已发布处理服务应是 A：%v %+v", err, pub)
		}
	})

	t.Run("发布时版本不一致返回 ErrRevisionConflict", func(t *testing.T) {
		if _, err := r.Publish(ctx, PublishParams{ID: b.ID, Version: 9, At: now}); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("实际 %v", err)
		}
	})

	t.Run("同存储再发布另一个：旧的自动停用", func(t *testing.T) {
		if _, err := r.Publish(ctx, PublishParams{ID: b.ID, Version: 1, By: 7, At: now}); err != nil {
			t.Fatal(err)
		}
		oldA, _ := r.GetByID(ctx, a.ID)
		if oldA.Status != model.ProcessorDisabled {
			t.Fatalf("A 应被停用，实际 %s", oldA.Status)
		}
		pub, _ := r.GetPublishedByStorage(ctx, 5)
		if pub.ID != b.ID {
			t.Fatalf("已发布的应是 B，实际 %d", pub.ID)
		}
	})

	t.Run("编辑后再发布得到 v2，回滚恢复 v1 的配置", func(t *testing.T) {
		if err := r.Update(ctx, b.ID, 1, map[string]any{"config": model.JSONText(`{"v":"b2"}`)}); err != nil {
			t.Fatal(err)
		}
		n, err := r.Publish(ctx, PublishParams{ID: b.ID, Version: 2, By: 7, At: now})
		if err != nil || n != 2 {
			t.Fatalf("实际 %d %v", n, err)
		}
		prev, err := r.PreviousVersions(ctx, b.ID)
		if err != nil || prev[b.ID] != 1 {
			t.Fatalf("B 的上一个版本应是 1：%v %v", prev, err)
		}
		to, err := r.Rollback(ctx, b.ID)
		if err != nil || to != 1 {
			t.Fatalf("回滚应到 v1：%d %v", to, err)
		}
		got, _ := r.GetByID(ctx, b.ID)
		if got.PublishedVersion != 1 || string(got.PublishedConfig) != `{"v":"b1"}` || got.Status != model.ProcessorPublished {
			t.Fatalf("实际 %+v", got)
		}
		if _, err := r.Rollback(ctx, b.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("没有更早版本应返回 ErrNotFound，实际 %v", err)
		}
		// 回滚后再发布，版本号继续递增而不是复用
		if n, err := r.Publish(ctx, PublishParams{ID: b.ID, Version: 2, At: now}); err != nil || n != 3 {
			t.Fatalf("回滚后再发布应是 v3：%d %v", n, err)
		}
	})

	t.Run("停用后该存储没有已发布的处理服务", func(t *testing.T) {
		if err := r.Disable(ctx, b.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetPublishedByStorage(ctx, 5); !errors.Is(err, ErrNotFound) {
			t.Fatalf("实际 %v", err)
		}
		if err := r.Disable(ctx, b.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("重复停用应返回 ErrNotFound，实际 %v", err)
		}
	})

	t.Run("删除连同发布历史一起删除", func(t *testing.T) {
		if err := r.Delete(ctx, b.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetByID(ctx, b.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("实际 %v", err)
		}
		var n int64
		db.Model(&model.ImageProcessorVersion{}).Where("processor_id = ?", b.ID).Count(&n)
		if n != 0 {
			t.Fatalf("发布历史应一并删除，剩 %d 行", n)
		}
		if err := r.Delete(ctx, b.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
		}
	})
}

func TestImageProcessorRepository_OnePublishedPerStorage(t *testing.T) {
	db := isolatedDB(t, processorModels()...)
	ctx := context.Background()
	// 部分唯一索引兜底：绕开 Publish 直接把两个处理服务都设为已发布，数据库必须拒绝
	p1 := newProcessor("P1", 8, `{}`)
	p2 := newProcessor("P2", 8, `{}`)
	p1.Status, p2.Status = model.ProcessorPublished, model.ProcessorPublished
	if err := db.Create(p1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(p2).Error; err == nil {
		t.Fatal("同一存储不应能有两个已发布的处理服务")
	}
	_ = ctx
}

func TestImageProcessorRepository_ListAndSampleAsset(t *testing.T) {
	db := isolatedDB(t, processorModels()...)
	r := NewImageProcessorRepository(db)
	ctx := context.Background()

	for _, n := range []string{"B", "A"} {
		if err := r.Create(ctx, newProcessor(n, 1, `{}`)); err != nil {
			t.Fatal(err)
		}
	}
	list, err := r.List(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "B" {
		t.Fatalf("应按 id 升序返回：%v %+v", err, list)
	}

	if _, err := r.SampleAsset(ctx, 1, "image"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("没有素材应返回 ErrNotFound，实际 %v", err)
	}
	for _, a := range []*model.Asset{
		{UserID: 1, Kind: "image", StorageID: 1, StorageKey: "u1/a.png", MimeType: "image/png", Source: "upload"},
		{UserID: 1, Kind: "image", StorageID: 1, StorageKey: "u1/b.png", MimeType: "image/png", Source: "upload"},
		{UserID: 1, Kind: "video", StorageID: 2, StorageKey: "u1/c.mp4", MimeType: "video/mp4", Source: "upload"},
	} {
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.SampleAsset(ctx, 1, "image")
	if err != nil || got.StorageKey != "u1/b.png" {
		t.Fatalf("应取该存储里最新的图片素材：%v %+v", err, got)
	}
	if _, err := r.SampleAsset(ctx, 1, "video"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("存储 1 没有视频，应返回 ErrNotFound，实际 %v", err)
	}
}
