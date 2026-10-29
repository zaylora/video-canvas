package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// dramaQueueFailure 在真实业务事务入队时模拟队列故障，验证项目和画布不会被部分提交。
type dramaQueueFailure struct{}

func (dramaQueueFailure) EnqueueDramaTx(context.Context, *sql.Tx, uint64, uint64, uint64) error {
	return errors.New("queue unavailable")
}

func TestDramaRepository_CreateRollbackOnQueueFailure(t *testing.T) {
	db := isolatedDB(t, &model.CanvasProject{}, &model.DramaProject{}, &model.DramaEpisode{}, &model.DramaJob{})
	repo := repository.NewDramaRepository(db)
	repo.SetEnqueuer(dramaQueueFailure{})
	ctx := context.Background()
	p := &model.DramaProject{UserID: 1, Title: "短剧", SourceText: "原文", StyleKey: "real_city_film"}
	err := repo.WithTx(ctx, func(tx repository.DramaTx) error {
		if e := tx.InsertProject(ctx, p); e != nil {
			return e
		}
		return tx.InsertJob(ctx, &model.DramaJob{UserID: 1, DramaID: p.ID, Step: "analyze", Status: "queued"})
	})
	if err == nil {
		t.Fatal("队列故障应回滚")
	}
	for _, table := range []any{&model.DramaProject{}, &model.CanvasProject{}, &model.DramaJob{}} {
		var count int64
		if e := db.Model(table).Count(&count).Error; e != nil {
			t.Fatal(e)
		}
		if count != 0 {
			t.Fatalf("回滚留下数据 %T count=%d", table, count)
		}
	}
}

func TestDramaRepository_OwnershipRevisionAndCascade(t *testing.T) {
	db := isolatedDB(t, &model.CanvasProject{}, &model.DramaProject{}, &model.DramaEpisode{}, &model.DramaJob{})
	repo := repository.NewDramaRepository(db)
	ctx := context.Background()
	p := &model.DramaProject{UserID: 1, Title: "甲剧", SourceText: "原文", StyleKey: "real_city_film", Revision: 1}
	second := &model.DramaProject{UserID: 2, Title: "乙剧", SourceText: "原文", StyleKey: "real_city_film", Revision: 1}
	for _, project := range []*model.DramaProject{p, second} {
		if e := repo.WithTx(ctx, func(tx repository.DramaTx) error { return tx.InsertProject(ctx, project) }); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := repo.Project(ctx, 2, p.ID, false); !errors.Is(e, repository.ErrNotFound) {
		t.Fatalf("他人项目应不可见：%v", e)
	}
	if e := repo.WithTx(ctx, func(tx repository.DramaTx) error {
		return tx.UpdateProject(ctx, 1, p.ID, 1, map[string]any{"title": "更新"})
	}); e != nil {
		t.Fatal(e)
	}
	if e := repo.WithTx(ctx, func(tx repository.DramaTx) error {
		return tx.UpdateProject(ctx, 1, p.ID, 1, map[string]any{"title": "覆盖"})
	}); !errors.Is(e, repository.ErrStateConflict) {
		t.Fatalf("旧revision不得覆盖：%v", e)
	}
	got, e := repo.Project(ctx, 1, p.ID, false)
	if e != nil || got.Title != "更新" || got.Revision != 2 {
		t.Fatalf("更新状态错误：%+v %v", got, e)
	}
	if e := repo.WithTx(ctx, func(tx repository.DramaTx) error { return tx.DeleteProject(ctx, 1, p.ID) }); e != nil {
		t.Fatal(e)
	}
	if _, e := repo.Project(ctx, 1, p.ID, false); !errors.Is(e, repository.ErrNotFound) {
		t.Fatalf("短剧未软删：%v", e)
	}
	var canvas model.CanvasProject
	if e := db.Unscoped().First(&canvas, p.CanvasID).Error; e != nil || !canvas.DeletedAt.Valid {
		t.Fatalf("短剧画布应软删：%+v %v", canvas, e)
	}
	if _, e := repo.Project(ctx, 2, second.ID, false); e != nil {
		t.Fatalf("他人项目应保留：%v", e)
	}
}
