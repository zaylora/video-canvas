package repository_test

import (
	"context"
	"errors"
	"strings"
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
	seg := model.DramaSegment{UserID: 1, EpisodeID: ep.ID, Seconds: 5, Revision: 1}
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

// 转写稿存在 adaptations 列；保存片段时漏写这一列，转写会返回成功却什么都没存下。
func TestDramaSegmentRepository_SavesAdaptations(t *testing.T) {
	db := isolatedDB(t, &model.DramaProject{}, &model.DramaEpisode{}, &model.DramaSegment{})
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
	seg := model.DramaSegment{UserID: 1, EpisodeID: ep.ID, Seconds: 5, Prompt: "原稿", Revision: 1}
	if err := r.SaveSegment(ctx, &seg, 0); err != nil {
		t.Fatal(err)
	}
	seg.Adaptations = []byte(`{"h3":{"prompt":"转写稿","source_hash":"abc","edited":false,"updated_at":"2026-11-10T00:00:00Z"}}`)
	if err := r.SaveSegment(ctx, &seg, 1); err != nil {
		t.Fatal(err)
	}
	got, err := r.Segment(ctx, 1, seg.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got.Adaptations), "转写稿") || got.Revision != 2 {
		t.Fatalf("转写稿没有存下：revision=%d adaptations=%s", got.Revision, got.Adaptations)
	}
}

// 片场转写的扣费流水：关联片段、不唯一（同一片段可以多次转写），备注出现在用户积分明细里。
func TestDramaSegmentRepository_AdaptLedger(t *testing.T) {
	db := isolatedDB(t, &model.CreditLedger{}, &model.UserCredit{})
	ctx := context.Background()
	r := repository.NewDramaRepository(db)
	sid := uint64(42)
	for range 2 {
		for _, typ := range []string{model.LedgerFreeze, model.LedgerSettle} {
			ok, err := r.InsertLedger(ctx, &model.CreditLedger{UserID: 1, DramaSegmentID: &sid, Type: typ, Amount: 3, Note: "片场转写 · 第 1 集片段 01 · H3"})
			if err != nil || !ok {
				t.Fatalf("写流水：ok=%v err=%v", ok, err)
			}
		}
	}
	rows, err := repository.NewUserRepository(db).ListLedgerPage(ctx, repository.LedgerPageFilter{UserID: 1, Limit: 10})
	if err != nil || len(rows) != 4 || rows[0].Note != "片场转写 · 第 1 集片段 01 · H3" {
		t.Fatalf("积分明细：%+v %v", rows, err)
	}
}
