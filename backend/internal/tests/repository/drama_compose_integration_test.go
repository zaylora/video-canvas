package repository_test

import (
	"context"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

func TestDramaComposeRepository_SegmentVersions(t *testing.T) {
	db := isolatedDB(t, &model.DramaProject{}, &model.DramaEpisode{}, &model.DramaSegment{})
	p := model.DramaProject{UserID: 1, CanvasID: 9, Title: "剧", SourceText: "文", StyleKey: "real_city_film"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ep := model.DramaEpisode{UserID: 1, DramaID: p.ID, No: 1}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	r := repository.NewDramaRepository(db)
	ctx := context.Background()
	version := int64(0)
	assertVersion := func(change bool) {
		t.Helper()
		if change {
			version++
		}
		var got model.DramaEpisode
		if err := db.First(&got, ep.ID).Error; err != nil {
			t.Fatal(err)
		}
		if got.SegmentVersion != version {
			t.Fatalf("片段版本=%d 期望=%d", got.SegmentVersion, version)
		}
	}
	seg := model.DramaSegment{UserID: 1, EpisodeID: ep.ID, Seconds: 5, Revision: 1}
	if err := r.WithTx(ctx, func(tx repository.DramaTx) error { return tx.SaveSegment(ctx, &seg, 0) }); err != nil {
		t.Fatal(err)
	}
	assertVersion(true)
	seg.Prompt = "只改提示词"
	if err := r.SaveSegment(ctx, &seg, seg.Revision); err != nil {
		t.Fatal(err)
	}
	assertVersion(false)
	chosen := uint64(20)
	seg.ChosenTaskID = &chosen
	if err := r.SaveSegment(ctx, &seg, seg.Revision); err != nil {
		t.Fatal(err)
	}
	assertVersion(true)
	seg.Seq = 1
	if err := r.SaveSegment(ctx, &seg, seg.Revision); err != nil {
		t.Fatal(err)
	}
	assertVersion(true)
	seg.Seq = 0
	if err := r.SaveSegment(ctx, &seg, seg.Revision); err != nil {
		t.Fatal(err)
	}
	assertVersion(true)
	if err := r.DeleteSegment(ctx, 1, seg.ID); err != nil {
		t.Fatal(err)
	}
	assertVersion(true)
	if err := r.ReplaceSegments(ctx, 1, ep.ID, []model.DramaSegment{{Seconds: 5}}); err != nil {
		t.Fatal(err)
	}
	assertVersion(true)
	if err := r.ReplaceSegments(ctx, 1, ep.ID, nil); err != nil {
		t.Fatal(err)
	}
	assertVersion(true)
}
