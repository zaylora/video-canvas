package repository_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

func cacheReservation(now time.Time, key, identity string, size int64, taskID uint64) model.ReferenceCacheReservation {
	return model.ReferenceCacheReservation{
		Object: model.ReferenceCacheObject{
			Key: key, Namespace: "test", Identity: identity, Size: size, Digest: "digest",
			CreatedAt: now, LastUsedAt: now, BusyUntil: now.Add(10 * time.Minute),
		},
		TaskID: taskID, MaxBytes: 10, ReuseAfter: now.Add(-5 * 24 * time.Hour),
	}
}

func TestReferenceCacheRepositorySerializesReservations(t *testing.T) {
	db := isolatedDB(t, &model.ReferenceCacheObject{}, &model.ReferenceCacheUse{})
	repo := repository.NewReferenceCacheRepository(db)
	const workers = 12
	type result struct {
		key string
		err error
	}
	now := time.Now()
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := cacheReservation(now, fmt.Sprintf("ref-cache/generation-%d", i), "same-source", 7, uint64(i+1))
			row, err := repo.Reserve(t.Context(), req)
			results <- result{key: row.Key, err: err}
		}()
	}
	wg.Wait()
	close(results)
	var first string
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if first == "" {
			first = r.key
		}
		if r.key != first {
			t.Fatal("并发任务必须复用同一个对象代际")
		}
	}

	req := cacheReservation(now, "ref-cache/different", "other-source", 4, 100)
	if _, err := repo.Reserve(t.Context(), req); !errors.Is(err, repository.ErrReferenceCacheCapacity) {
		t.Fatalf("待确认对象也必须占用容量：%v", err)
	}
	var uses int64
	if err := db.Model(&model.ReferenceCacheUse{}).Count(&uses).Error; err != nil || uses != workers {
		t.Fatalf("每个任务应有自己的使用记录：uses=%d err=%v", uses, err)
	}
}

func cacheSweep(now time.Time) model.ReferenceCacheSweep {
	return model.ReferenceCacheSweep{
		Namespace: "test", Now: now, UnusedSince: now.Add(-72 * time.Hour),
		UncertainSince: now.Add(-24 * time.Hour), ClaimUntil: now.Add(10 * time.Minute),
	}
}

func TestReferenceCacheRepositoryProtectsActiveAndUncertainTasks(t *testing.T) {
	db := isolatedDB(t, &model.ReferenceCacheObject{}, &model.ReferenceCacheUse{}, &model.GenerationTask{})
	repo := repository.NewReferenceCacheRepository(db)
	now := time.Now()
	active := gtTask(1, model.TaskRunning, 0, now)
	unknown := gtTask(1, model.TaskFailed, 0, now)
	unknown.ErrorCode = "submit_unknown"
	canceled := gtTask(1, model.TaskCanceled, 0, now)
	referenceTasks(t, repo, now, db.Create([]*model.GenerationTask{active, unknown, canceled}).Error, active, unknown, canceled)
	claim, err := repo.ClaimCleanup(t.Context(), cacheSweep(now))
	if err != nil || claim == nil || claim.Key != "ref-cache/unused" {
		t.Fatalf("应只认领没有活跃任务的对象：got=%v err=%v", claim, err)
	}
	assertCleanupReservation(t, repo, claim.Key, now)
	later := now.Add(25 * time.Hour)
	for range 2 {
		row, err := repo.ClaimCleanup(t.Context(), cacheSweep(later))
		if err != nil || row == nil || row.Key == "ref-cache/active" {
			t.Fatalf("未知或取消的任务宽限结束才可回收：got=%v err=%v", row, err)
		}
		if err := repo.FinishCleanup(t.Context(), row.Key); err != nil {
			t.Fatal(err)
		}
	}
	if row, err := repo.ClaimCleanup(t.Context(), cacheSweep(later)); err != nil || row != nil {
		t.Fatalf("活跃任务仍应受保护：got=%v err=%v", row, err)
	}
}

func referenceTasks(t *testing.T, repo *repository.ReferenceCacheRepository, now time.Time, taskErr error, tasks ...*model.GenerationTask) {
	t.Helper()
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	keys := []string{"active", "unknown", "canceled", "unused"}
	ids := []uint64{tasks[0].ID, tasks[1].ID, tasks[2].ID, 999}
	for i, key := range keys {
		old := now.Add(-4 * 24 * time.Hour)
		row, err := repo.Reserve(t.Context(), cacheReservation(old, "ref-cache/"+key, key, 2, ids[i]))
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.MarkReady(t.Context(), row.Key, ids[i], old.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
}

func assertCleanupReservation(t *testing.T, repo *repository.ReferenceCacheRepository, key string, now time.Time) {
	t.Helper()
	req := cacheReservation(now, "ref-cache/new", "unused", 2, 1000)
	row, err := repo.Reserve(t.Context(), req)
	if err != nil || row.Key == key {
		t.Fatalf("正在清理的代际不能重新租用：got=%v err=%v", row, err)
	}
	if err := repo.MarkReady(t.Context(), row.Key, req.TaskID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Bytes(t.Context(), "test"); got != 10 || err != nil {
		t.Fatalf("未确认删除之前不能释放容量：bytes=%d err=%v", got, err)
	}
	if err := repo.FinishCleanup(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Bytes(t.Context(), "test"); got != 8 || err != nil {
		t.Fatalf("确认删除才释放容量：bytes=%d err=%v", got, err)
	}
}

func TestReferenceCacheRepositoryRenewsAgingObject(t *testing.T) {
	db := isolatedDB(t, &model.ReferenceCacheObject{}, &model.ReferenceCacheUse{})
	repo := repository.NewReferenceCacheRepository(db)
	now := time.Now()
	old := cacheReservation(now.Add(-6*24*time.Hour), "ref-cache/old", "source", 4, 1)
	if _, err := repo.Reserve(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	fresh := cacheReservation(now, "ref-cache/fresh", "source", 4, 2)
	row, err := repo.Reserve(t.Context(), fresh)
	if err != nil || row.Key != fresh.Object.Key {
		t.Fatalf("剩余生命周期不足时需要新代际：got=%v err=%v", row, err)
	}
	if got, err := repo.Bytes(t.Context(), "test"); got != 8 || err != nil {
		t.Fatalf("替换时两代对象分别计入占用：bytes=%d err=%v", got, err)
	}
}

func TestReferenceCacheRepositoryTracksOnlyUnrecordedObjects(t *testing.T) {
	db := isolatedDB(t, &model.ReferenceCacheObject{}, &model.ReferenceCacheUse{}, &model.GenerationTask{})
	repo := repository.NewReferenceCacheRepository(db)
	now := time.Now()
	req := cacheReservation(now, "ref-cache/known", "known", 3, 1)
	if _, err := repo.Reserve(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	discovered := req.Object
	discovered.Deleting = true
	discovered.BusyUntil = now
	if err := repo.TrackOrphan(t.Context(), discovered); err != nil {
		t.Fatal(err)
	}
	discovered.Key = "ref-cache/orphan"
	if err := repo.TrackOrphan(t.Context(), discovered); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Bytes(t.Context(), "test"); got != 6 || err != nil {
		t.Fatalf("应只补记未记录对象：bytes=%d err=%v", got, err)
	}
	row, err := repo.ClaimCleanup(t.Context(), cacheSweep(now.Add(time.Second)))
	if err != nil || row == nil || row.Key != discovered.Key {
		t.Fatalf("只应认领孤儿，不能改变已租用对象：got=%v err=%v", row, err)
	}
}

func TestReferenceCacheRepositoryReservesCapacityPerNamespace(t *testing.T) {
	db := isolatedDB(t, &model.ReferenceCacheObject{}, &model.ReferenceCacheUse{})
	repo := repository.NewReferenceCacheRepository(db)
	now := time.Now()
	old := cacheReservation(now, "ref-cache/old-bucket", "source", 10, 1)
	if _, err := repo.Reserve(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	fresh := cacheReservation(now, "ref-cache/new-bucket", "source", 6, 2)
	fresh.Object.Namespace = "new-bucket"
	if _, err := repo.Reserve(t.Context(), fresh); err != nil {
		t.Fatalf("原桶占满容量时，新桶仍应可预留：%v", err)
	}
	over := cacheReservation(now, "ref-cache/over-limit", "another-source", 5, 3)
	over.Object.Namespace = fresh.Object.Namespace
	if _, err := repo.Reserve(t.Context(), over); !errors.Is(err, repository.ErrReferenceCacheCapacity) {
		t.Fatalf("新桶仍应执行自己的限额：%v", err)
	}
	if got, err := repo.Bytes(t.Context(), fresh.Object.Namespace); got != 6 || err != nil {
		t.Fatalf("新桶用量应只计入本桶预留：bytes=%d err=%v", got, err)
	}
	if got, err := repo.Bytes(t.Context(), old.Object.Namespace); got != 10 || err != nil {
		t.Fatalf("原桶的容量记录应保留：bytes=%d err=%v", got, err)
	}
}
