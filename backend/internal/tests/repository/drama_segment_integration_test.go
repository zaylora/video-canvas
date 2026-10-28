package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

func TestDramaSegmentRepository_IsolationTakesAndRevision(t *testing.T) {
	db := isolatedDB(t, &model.DramaProject{}, &model.DramaEpisode{}, &model.DramaSegment{}, &model.GenerationTask{})
	ctx := context.Background()
	p := model.DramaProject{UserID: 1, CanvasID: 9, Title: "剧", SourceText: "文", StyleKey: "real_city_film"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ep := model.DramaEpisode{UserID: 1, DramaID: p.ID, No: 1}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	r := repository.NewDramaRepository(db)
	seg := model.DramaSegment{UserID: 1, EpisodeID: ep.ID, Seconds: 5, Shots: []byte(`[]`), Revision: 1}
	if err := r.SaveSegment(ctx, &seg, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Segment(ctx, 2, seg.ID, false); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("他人片段：%v", err)
	}
	seg.Prompt = "更新"
	if err := r.SaveSegment(ctx, &seg, 1); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveSegment(ctx, &seg, 1); !errors.Is(err, repository.ErrStateConflict) {
		t.Fatalf("旧版本：%v", err)
	}
	canvas, other := uint64(9), uint64(10)
	node := "seg:" + idcodec.Encode(seg.ID)
	tasks := []model.GenerationTask{
		{ID: 100, UserID: 1, CanvasProjectID: &canvas, NodeID: node, Kind: "video", ModelKey: "v", Provider: "p", Status: "failed", InputJSON: []byte(`{}`), ConfigSnapshot: []byte(`{}`), NextPollAt: time.Now(), DeadlineAt: time.Now(), CreatedAt: time.Unix(2, 0)},
		{ID: 101, UserID: 1, CanvasProjectID: &canvas, NodeID: node, Kind: "video", ModelKey: "v", Provider: "p", Status: "succeeded", InputJSON: []byte(`{}`), ConfigSnapshot: []byte(`{}`), NextPollAt: time.Now(), DeadlineAt: time.Now(), CreatedAt: time.Unix(1, 0)},
	}
	for _, task := range tasks {
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	wrong := tasks[0]
	wrong.ID, wrong.CanvasProjectID = 102, &other
	if err := db.Create(&wrong).Error; err != nil {
		t.Fatal(err)
	}
	wrong.ID, wrong.UserID, wrong.CanvasProjectID = 103, 2, &canvas
	if err := db.Create(&wrong).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := r.SegmentTasks(ctx, 1, 9, node)
	if err != nil || len(rows) != 2 || rows[0].ID != 101 || rows[1].ID != 100 {
		t.Fatalf("任务归属与排序：%+v %v", rows, err)
	}
	if has, err := r.HasSegmentTakes(ctx, 1, ep.ID); err != nil || !has {
		t.Fatalf("本集已有任务应阻止重做分镜：%v %v", has, err)
	}
	if err := r.DeleteSegment(ctx, 1, seg.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.GenerationTask{}).Count(&count).Error; err != nil || count != 4 {
		t.Fatalf("删除片段保留任务：%d %v", count, err)
	}
	if err := db.Delete(&p).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := r.SegmentEpisode(ctx, 1, ep.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("已删除项目不得读取下属集：%v", err)
	}
}
