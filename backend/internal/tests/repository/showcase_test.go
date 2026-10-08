package repository_test

import (
	"context"
	"errors"
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// 本文件是 showcase_items 与 AssetRepository.ListByIDs 的集成测试，连接专用测试库（TEST_DATABASE_DSN），未设置时跳过。

func newShowcaseItem(prompt string, sortVal int, enabled bool) *model.ShowcaseItem {
	return &model.ShowcaseItem{AssetID: 1, Prompt: prompt, Sort: sortVal, Enabled: enabled, CreatedBy: 9}
}

func showcasePrompts(items []model.ShowcaseItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Prompt)
	}
	return out
}

func TestShowcaseRepository_CreateAndList(t *testing.T) {
	db := isolatedDB(t, &model.ShowcaseItem{})
	r := NewShowcaseRepository(db)
	ctx := context.Background()

	if top, err := r.MaxSort(ctx); err != nil || top != -1 {
		t.Fatalf("空表 MaxSort 应为 -1：%d %v", top, err)
	}

	// 先建 sort=2、再建两个 sort=1（后建的 id 更大）、再建一个禁用的 sort=0
	for _, it := range []*model.ShowcaseItem{newShowcaseItem("c", 2, true), newShowcaseItem("a", 1, true), newShowcaseItem("b", 1, true), newShowcaseItem("off", 0, false)} {
		if err := r.Create(ctx, it); err != nil {
			t.Fatal(err)
		}
		if it.ID == 0 || it.CreatedAt.IsZero() {
			t.Fatalf("id 或创建时间未回填：%+v", it)
		}
	}

	t.Run("enabled=false 能原样写入（不被 gorm 零值规则改成默认值）", func(t *testing.T) {
		all, err := r.ListAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range all {
			if it.Prompt == "off" && it.Enabled {
				t.Fatalf("禁用的条目被存成了启用：%+v", it)
			}
		}
	})
	t.Run("ListAll 含禁用，按 sort 升序、sort 相同按 id 升序", func(t *testing.T) {
		all, err := r.ListAll(ctx)
		if err != nil || len(showcasePrompts(all)) != 4 {
			t.Fatalf("%v %v", all, err)
		}
		if got := showcasePrompts(all); got[0] != "off" || got[1] != "a" || got[2] != "b" || got[3] != "c" {
			t.Fatalf("顺序不对：%v", got)
		}
	})
	t.Run("ListEnabled 只返回启用的", func(t *testing.T) {
		en, err := r.ListEnabled(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got := showcasePrompts(en); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
			t.Fatalf("%v", got)
		}
	})
	t.Run("MaxSort 返回最大 sort", func(t *testing.T) {
		if top, err := r.MaxSort(ctx); err != nil || top != 2 {
			t.Fatalf("%d %v", top, err)
		}
	})
	t.Run("GetByID：存在返回，不存在返回 ErrNotFound", func(t *testing.T) {
		all, _ := r.ListAll(ctx)
		got, err := r.GetByID(ctx, all[1].ID)
		if err != nil || got.Prompt != "a" || got.CreatedBy != 9 {
			t.Fatalf("%+v %v", got, err)
		}
		if _, err := r.GetByID(ctx, 987654321); !errors.Is(err, ErrNotFound) {
			t.Fatalf("应返回 ErrNotFound：%v", err)
		}
	})
}

func TestShowcaseRepository_Update(t *testing.T) {
	db := isolatedDB(t, &model.ShowcaseItem{})
	r := NewShowcaseRepository(db)
	ctx := context.Background()
	poster := uint64(7)
	it := newShowcaseItem("旧", 0, true)
	it.PosterAssetID = &poster
	if err := r.Create(ctx, it); err != nil {
		t.Fatal(err)
	}

	t.Run("更新指定列：false 与 0 值也能写入，不碰其他列", func(t *testing.T) {
		if err := r.Update(ctx, it.ID, map[string]any{"enabled": false, "start_sec": 0.0, "prompt": "新", "model_label": ""}); err != nil {
			t.Fatal(err)
		}
		got, _ := r.GetByID(ctx, it.ID)
		if got.Enabled || got.Prompt != "新" || got.PosterAssetID == nil || *got.PosterAssetID != 7 || got.AssetID != 1 {
			t.Fatalf("%+v", got)
		}
		if !got.UpdatedAt.After(it.CreatedAt) {
			t.Fatalf("updated_at 应被刷新：%v vs %v", got.UpdatedAt, it.CreatedAt)
		}
	})
	t.Run("值为 nil 把列置 NULL（清空封面）", func(t *testing.T) {
		if err := r.Update(ctx, it.ID, map[string]any{"poster_asset_id": nil}); err != nil {
			t.Fatal(err)
		}
		if got, _ := r.GetByID(ctx, it.ID); got.PosterAssetID != nil {
			t.Fatalf("封面应为 NULL：%+v", got)
		}
	})
	t.Run("再设置封面", func(t *testing.T) {
		if err := r.Update(ctx, it.ID, map[string]any{"poster_asset_id": uint64(8)}); err != nil {
			t.Fatal(err)
		}
		if got, _ := r.GetByID(ctx, it.ID); got.PosterAssetID == nil || *got.PosterAssetID != 8 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("条目不存在返回 ErrNotFound", func(t *testing.T) {
		if err := r.Update(ctx, 987654321, map[string]any{"enabled": true}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%v", err)
		}
	})
}

func TestShowcaseRepository_Delete(t *testing.T) {
	db := isolatedDB(t, &model.ShowcaseItem{})
	r := NewShowcaseRepository(db)
	ctx := context.Background()
	it := newShowcaseItem("x", 0, true)
	if err := r.Create(ctx, it); err != nil {
		t.Fatal(err)
	}

	if err := r.Delete(ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	if all, _ := r.ListAll(ctx); len(all) != 0 {
		t.Fatalf("删除后不应再列出：%v", all)
	}
	if _, err := r.GetByID(ctx, it.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后 GetByID 应返回 ErrNotFound：%v", err)
	}
	if top, _ := r.MaxSort(ctx); top != -1 {
		t.Fatalf("已删除的条目不应参与 MaxSort：%d", top)
	}
	if err := r.Delete(ctx, it.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound：%v", err)
	}
}

func TestShowcaseRepository_WithTx(t *testing.T) {
	db := isolatedDB(t, &model.ShowcaseItem{})
	r := NewShowcaseRepository(db)
	ctx := context.Background()
	var ids []uint64
	for i, p := range []string{"a", "b", "c"} {
		it := newShowcaseItem(p, i, true)
		if err := r.Create(ctx, it); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
	}
	sortedPrompts := func() []string {
		all, _ := r.ListAll(ctx)
		return showcasePrompts(all)
	}

	t.Run("提交：事务里读到全部 id 并重写 sort", func(t *testing.T) {
		err := r.WithTx(ctx, func(tx ShowcaseTx) error {
			cur, err := tx.ListIDs(ctx)
			if err != nil || len(cur) != 3 || cur[0] != ids[0] {
				t.Fatalf("事务内 ListIDs 不对：%v %v", cur, err)
			}
			for i, id := range []uint64{ids[2], ids[0], ids[1]} {
				if err := tx.SetSort(ctx, id, i); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := sortedPrompts(); got[0] != "c" || got[1] != "a" || got[2] != "b" {
			t.Fatalf("%v", got)
		}
	})
	t.Run("回调返回错误整体回滚", func(t *testing.T) {
		boom := errors.New("boom")
		err := r.WithTx(ctx, func(tx ShowcaseTx) error {
			if err := tx.SetSort(ctx, ids[0], 100); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("%v", err)
		}
		if got := sortedPrompts(); got[0] != "c" || got[1] != "a" || got[2] != "b" {
			t.Fatalf("应回滚到事务前的顺序：%v", got)
		}
	})
	t.Run("SetSort 条目不存在返回 ErrNotFound", func(t *testing.T) {
		err := r.WithTx(ctx, func(tx ShowcaseTx) error { return tx.SetSort(ctx, 987654321, 1) })
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("%v", err)
		}
	})
}

func TestAssetRepository_ListByIDs(t *testing.T) {
	db := isolatedDB(t, &model.Asset{})
	r := NewAssetRepository(db)
	ctx := context.Background()
	var ids []uint64
	for i, owner := range []uint64{1, 2, 1} {
		a := &model.Asset{UserID: owner, Kind: "video", StorageKey: "k/" + string(rune('a'+i)), MimeType: "video/mp4", Source: model.AssetSourceUpload}
		if err := r.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}

	t.Run("按 id 批量取，不限用户，不存在的 id 缺席", func(t *testing.T) {
		got, err := r.ListByIDs(ctx, []uint64{ids[0], ids[1], 987654321})
		if err != nil || len(got) != 2 {
			t.Fatalf("%+v %v", got, err)
		}
		seen := map[uint64]bool{}
		for _, a := range got {
			seen[a.ID] = true
		}
		if !seen[ids[0]] || !seen[ids[1]] {
			t.Fatalf("%v", seen)
		}
	})
	t.Run("重复 id 不产生重复行", func(t *testing.T) {
		got, err := r.ListByIDs(ctx, []uint64{ids[0], ids[0]})
		if err != nil || len(got) != 1 {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("空列表返回空", func(t *testing.T) {
		got, err := r.ListByIDs(ctx, nil)
		if err != nil || len(got) != 0 {
			t.Fatalf("%+v %v", got, err)
		}
	})
}
