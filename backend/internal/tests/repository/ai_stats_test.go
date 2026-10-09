package repository_test

import (
	"context"
	"testing"
	"time"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// statsTask 插入一个指定状态 / 模型 / 类型 / 创建时间的任务。
func statsTask(t *testing.T, repo *GenerationTaskRepository, status, modelKey, kind string, created time.Time, isTest bool) {
	t.Helper()
	gtChannelTask(t, repo, "p1", status, created, func(tk *model.GenerationTask) {
		tk.ModelKey, tk.Kind, tk.IsTest = modelKey, kind, isTest
	})
}

// 按上海时区的自然日分桶；三个桶：成功 / 失败（含过期）/ 其他（取消与进行中）；试跑和区间外的不算。
func TestAIStatsRepo_DailyBuckets(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	tasks := NewGenerationTaskRepository(db)
	stats := NewAIStatsRepository(db)
	sh, _ := time.LoadLocation("Asia/Shanghai")

	day := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, sh) }
	statsTask(t, tasks, model.TaskSucceeded, "m1", "video", day(9, 10), false)
	statsTask(t, tasks, model.TaskFailed, "m1", "video", day(9, 11), false)
	statsTask(t, tasks, model.TaskExpired, "m1", "video", day(9, 12), false)  // 过期并入失败
	statsTask(t, tasks, model.TaskCanceled, "m1", "video", day(9, 13), false) // 取消是“其他”
	statsTask(t, tasks, model.TaskRunning, "m1", "video", day(10, 0), false)  // 上海 10-10 00:00 刚过日界，属于 10-10
	statsTask(t, tasks, model.TaskPending, "m1", "video", day(10, 9), false)
	statsTask(t, tasks, model.TaskSucceeded, "m1", "video", day(10, 9), true)  // 试跑不算
	statsTask(t, tasks, model.TaskSucceeded, "m1", "video", day(8, 23), false) // 区间之前
	statsTask(t, tasks, model.TaskSucceeded, "m1", "video", day(11, 0), false) // 区间终点（不含）

	got, err := stats.DailyBuckets(ctx, "Asia/Shanghai", day(9, 0), day(11, 0))
	if err != nil {
		t.Fatalf("DailyBuckets 失败：%v", err)
	}
	want := []model.AIStatsDay{
		{Date: "2026-10-09", Succeeded: 1, Failed: 2, Other: 1},
		{Date: "2026-10-10", Succeeded: 0, Failed: 0, Other: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("只应返回有任务的 2 天：%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 天不对：got=%+v want=%+v", i, got[i], want[i])
		}
	}
}

// 没有任务时返回空，不报错。
func TestAIStatsRepo_DailyBuckets_Empty(t *testing.T) {
	db := gtTestDB(t)
	now := time.Now()
	got, err := NewAIStatsRepository(db).DailyBuckets(context.Background(), "Asia/Shanghai", now.Add(-24*time.Hour), now)
	if err != nil || len(got) != 0 {
		t.Fatalf("应为空：%+v %v", got, err)
	}
}

// 按（模型, 类型）计数：状态不过滤，试跑与区间外不算；所以合计与每日三个桶之和一致。
func TestAIStatsRepo_ModelKindCounts(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	tasks := NewGenerationTaskRepository(db)
	stats := NewAIStatsRepository(db)
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 7)
	in := from.Add(time.Hour)

	statsTask(t, tasks, model.TaskSucceeded, "kling", "video", in, false)
	statsTask(t, tasks, model.TaskFailed, "kling", "video", in, false)
	statsTask(t, tasks, model.TaskRunning, "kling", "video", in, false)
	statsTask(t, tasks, model.TaskSucceeded, "img", "image", in, false)
	statsTask(t, tasks, model.TaskSucceeded, "img", "image", in, true)                 // 试跑
	statsTask(t, tasks, model.TaskSucceeded, "img", "image", to.Add(time.Hour), false) // 区间外

	got, err := stats.ModelKindCounts(ctx, from, to)
	if err != nil {
		t.Fatalf("ModelKindCounts 失败：%v", err)
	}
	counts := map[string]int{}
	for _, r := range got {
		counts[r.Model+"/"+r.Kind] = r.Count
	}
	if len(counts) != 2 || counts["kling/video"] != 3 || counts["img/image"] != 1 {
		t.Fatalf("计数不对：%+v", got)
	}
}

// 全站按时间范围聚合要走 created_at 单列索引，不能只有 (user_id, created_at)。
func TestGenerationTask_HasCreatedAtIndex(t *testing.T) {
	db := gtTestDB(t)
	if !db.Migrator().HasIndex(&model.GenerationTask{}, "idx_task_created") {
		t.Fatal("generation_tasks 缺少 idx_task_created（created_at）索引，总览统计会全表扫描")
	}
}
