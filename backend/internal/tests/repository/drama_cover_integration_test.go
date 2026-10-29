package repository_test

import (
	"context"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

func TestDramaRepository_CoverKeys(t *testing.T) {
	db := isolatedDB(t, &model.CanvasProject{}, &model.Asset{})
	repo := repository.NewDramaRepository(db)
	ctx := context.Background()
	img := &model.Asset{UserID: 1, Kind: "image", StorageKey: "u1/first.png", MimeType: "image/png"}
	if err := db.Create(img).Error; err != nil {
		t.Fatal(err)
	}
	withCover := &model.CanvasProject{UserID: 1, Title: "甲剧资产", CoverAssetID: img.ID, PayloadJSON: []byte(`{}`)}
	noCover := &model.CanvasProject{UserID: 1, Title: "乙剧资产", PayloadJSON: []byte(`{}`)}
	others := &model.CanvasProject{UserID: 2, Title: "他人", CoverAssetID: img.ID, PayloadJSON: []byte(`{}`)}
	for _, c := range []*model.CanvasProject{withCover, noCover, others} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.CoverKeys(ctx, 1, []uint64{withCover.ID, noCover.ID, others.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[withCover.ID] != "u1/first.png" {
		t.Fatalf("只返回本人、有封面的画布：%v", got)
	}
	if empty, err := repo.CoverKeys(ctx, 1, nil); err != nil || len(empty) != 0 {
		t.Fatalf("空列表不查库：%v %v", empty, err)
	}
}
