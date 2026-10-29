package repository_test

import (
	"context"
	"errors"
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// 导入事务：任何一步失败都整体回滚，成功时素材、画布两类行都落库。
func TestCanvasTransferRepository_WithTx_RollsBackEverything(t *testing.T) {
	ctx := context.Background()
	db := isolatedDB(t, &model.Asset{}, &model.CanvasProject{})
	r := NewCanvasTransferRepository(db)
	const user = uint64(7)
	count := func(m any) int64 {
		var n int64
		if err := db.Model(m).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("回调出错时已插入的素材回滚", func(t *testing.T) {
		boom := errors.New("boom")
		err := r.WithTx(ctx, func(tx CanvasTransferTx) error {
			if err := tx.CreateAsset(ctx, &model.Asset{UserID: user, Kind: "image", StorageKey: "u7/rollback.png", Source: "upload"}); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("应当原样返回回调错误，实际 %v", err)
		}
		if n := count(&model.Asset{}); n != 0 {
			t.Fatalf("回滚后素材表应为空，实际 %d 行", n)
		}
	})

	t.Run("成功时素材和画布都落库", func(t *testing.T) {
		var asset model.Asset
		var canvas model.CanvasProject
		err := r.WithTx(ctx, func(tx CanvasTransferTx) error {
			asset = model.Asset{UserID: user, Kind: "image", StorageKey: "u7/ok.png", Source: "upload"}
			if err := tx.CreateAsset(ctx, &asset); err != nil {
				return err
			}
			if asset.ID == 0 {
				t.Error("CreateAsset 应回填 ID")
			}
			canvas = model.CanvasProject{UserID: user, Title: "导入的画布", PayloadJSON: []byte(`{"nodes":[]}`), Revision: 1}
			return tx.CreateCanvas(ctx, &canvas)
		})
		if err != nil {
			t.Fatalf("事务应成功：%v", err)
		}
		if canvas.ID == 0 {
			t.Error("CreateCanvas 应回填 ID")
		}
		if count(&model.Asset{}) != 1 || count(&model.CanvasProject{}) != 1 {
			t.Fatalf("落库行数不对：素材 %d、画布 %d", count(&model.Asset{}), count(&model.CanvasProject{}))
		}
	})
}
