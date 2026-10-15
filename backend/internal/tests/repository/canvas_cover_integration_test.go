package repository_test

import (
	"context"
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// 封面写回的条件更新与列表联查，在真实数据库上验证。
func TestCanvasProjectRepository_Cover(t *testing.T) {
	ctx := context.Background()
	db := isolatedDB(t, &model.CanvasProject{}, &model.Asset{})
	r := NewCanvasProjectRepository(db)

	mkCanvas := func(userID uint64, title string) *model.CanvasProject {
		c := &model.CanvasProject{UserID: userID, Title: title, Revision: 1, PayloadJSON: []byte(`{}`)}
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
		return c
	}
	mkAsset := func(userID uint64, key string) *model.Asset {
		a := &model.Asset{UserID: userID, Kind: "image", StorageKey: key, Source: "generated"}
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
		return a
	}
	reload := func(id uint64) model.CanvasProject {
		var c model.CanvasProject
		if err := db.First(&c, id).Error; err != nil {
			t.Fatal(err)
		}
		return c
	}

	t.Run("封面为空时写入，不动 revision 和更新时间", func(t *testing.T) {
		c, a := mkCanvas(1, "画布"), mkAsset(1, "u/1/first.png")
		before := reload(c.ID)
		if err := r.SetCoverIfEmpty(ctx, 1, c.ID, a.ID); err != nil {
			t.Fatal(err)
		}
		got := reload(c.ID)
		if got.CoverAssetID != a.ID {
			t.Fatalf("封面没写进去：%+v", got)
		}
		if got.Revision != before.Revision || !got.UpdatedAt.Equal(before.UpdatedAt) {
			t.Fatalf("写封面不能改 revision / 更新时间，否则会让用户正在编辑的画布保存冲突、列表顺序跳动：%+v → %+v", before, got)
		}
	})

	t.Run("已经有封面不覆盖：封面是第一张图", func(t *testing.T) {
		c, a1, a2 := mkCanvas(1, "画布"), mkAsset(1, "u/1/one.png"), mkAsset(1, "u/1/two.png")
		_ = r.SetCoverIfEmpty(ctx, 1, c.ID, a1.ID)
		if err := r.SetCoverIfEmpty(ctx, 1, c.ID, a2.ID); err != nil {
			t.Fatal(err)
		}
		if got := reload(c.ID); got.CoverAssetID != a1.ID {
			t.Fatalf("不应被后来的图覆盖：%+v", got)
		}
	})

	t.Run("别人的画布写不进去，也不报错", func(t *testing.T) {
		c, a := mkCanvas(1, "画布"), mkAsset(2, "u/2/x.png")
		if err := r.SetCoverIfEmpty(ctx, 2, c.ID, a.ID); err != nil {
			t.Fatal(err)
		}
		if got := reload(c.ID); got.CoverAssetID != 0 {
			t.Fatalf("不能给别人的画布设封面：%+v", got)
		}
	})

	t.Run("列表带出封面素材的 key；没封面或素材已删则为空", func(t *testing.T) {
		with, a := mkCanvas(9, "有封面"), mkAsset(9, "u/9/cover.png")
		mkCanvas(9, "没封面")
		gone, b := mkCanvas(9, "素材删了"), mkAsset(9, "u/9/gone.png")
		_ = r.SetCoverIfEmpty(ctx, 9, with.ID, a.ID)
		_ = r.SetCoverIfEmpty(ctx, 9, gone.ID, b.ID)
		if err := db.Delete(&model.Asset{}, b.ID).Error; err != nil {
			t.Fatal(err)
		}

		items, total, err := r.List(ctx, 9, "", 0, 10)
		if err != nil || total != 3 || len(items) != 3 {
			t.Fatalf("items=%d total=%d err=%v", len(items), total, err)
		}
		keys := map[string]string{}
		for _, it := range items {
			keys[it.Title] = it.CoverKey
		}
		if keys["有封面"] != "u/9/cover.png" || keys["没封面"] != "" || keys["素材删了"] != "" {
			t.Fatalf("封面 key 不对：%+v", keys)
		}
	})
}
