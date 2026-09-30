package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func TestGenerationTaskRepo_ProviderColumns(t *testing.T) {
	ctx := context.Background()
	repo := NewGenerationTaskRepository(gtTestDB(t))

	t.Run("新任务的三个新列默认为空", func(t *testing.T) {
		tk := gtTask(gtNewUserID(), model.TaskPending, 1, time.Now())
		if ok, err := repo.InsertTask(ctx, tk); err != nil || !ok {
			t.Fatalf("插入失败：ok=%v err=%v", ok, err)
		}
		got, err := repo.GetByIDAny(ctx, tk.ID)
		if err != nil || len(got.ProviderState) != 0 || len(got.ProviderResult) != 0 || len(got.TraceJSON) != 0 {
			t.Fatalf("新列应为空：%+v %v", got, err)
		}
	})

	t.Run("UpdateIf 可以写入与覆盖 provider_state / provider_result，RETURNING 带回新值", func(t *testing.T) {
		tk := gtTask(gtNewUserID(), model.TaskPending, 1, time.Now())
		if _, err := repo.InsertTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
		got, err := repo.UpdateIf(ctx, tk.ID, []string{model.TaskPending}, map[string]any{
			"status":          model.TaskRunning,
			"provider_state":  datatypes.JSON(`{"step":1}`),
			"provider_result": datatypes.JSON(`[{"url":"https://up.example.com/a.mp4"}]`),
		}, true)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != model.TaskRunning || got.Version != 2 ||
			!jsonEqual(got.ProviderState, `{"step": 1}`) || !jsonEqual(got.ProviderResult, `[{"url": "https://up.example.com/a.mp4"}]`) {
			t.Fatalf("RETURNING 结果不符合预期：%+v", got)
		}

		// 再次更新只改 provider_state，provider_result 保持不变
		got, err = repo.UpdateIf(ctx, tk.ID, []string{model.TaskRunning}, map[string]any{
			"provider_state": datatypes.JSON(`{"step":2}`),
		}, false)
		if err != nil || !jsonEqual(got.ProviderState, `{"step": 2}`) || len(got.ProviderResult) == 0 {
			t.Fatalf("覆盖 state 后 result 应保留：%+v %v", got, err)
		}

		// 转存完成后清空 provider_result（写 nil）
		got, err = repo.UpdateIf(ctx, tk.ID, []string{model.TaskRunning}, map[string]any{
			"provider_result": datatypes.JSON(nil),
		}, false)
		if err != nil || len(got.ProviderResult) != 0 {
			t.Fatalf("应能清空 provider_result：%+v %v", got, err)
		}
		reload, _ := repo.GetByIDAny(ctx, tk.ID)
		if !jsonEqual(reload.ProviderState, `{"step": 2}`) || len(reload.ProviderResult) != 0 {
			t.Fatalf("落库值不符合预期：%+v", reload)
		}
	})

	t.Run("文本产物 output_json 含 text 字段", func(t *testing.T) {
		tk := gtTask(gtNewUserID(), model.TaskRunning, 1, time.Now())
		if _, err := repo.InsertTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
		got, err := repo.UpdateIf(ctx, tk.ID, []string{model.TaskRunning}, map[string]any{
			"status":      model.TaskSucceeded,
			"output_json": datatypes.JSON(`[{"media_type":"text","text":"你好"}]`),
		}, true)
		if err != nil || !jsonEqual(got.OutputJSON, `[{"media_type": "text", "text": "你好"}]`) {
			t.Fatalf("%+v %v", got, err)
		}
	})
}

func TestGenerationTaskRepo_SaveTrace(t *testing.T) {
	ctx := context.Background()
	repo := NewGenerationTaskRepository(gtTestDB(t))
	trial := gtTask(gtNewUserID(), model.TaskRunning, 0, time.Now())
	trial.IsTest = true
	formal := gtTask(gtNewUserID(), model.TaskRunning, 1, time.Now())
	for _, tk := range []*model.GenerationTask{trial, formal} {
		if ok, err := repo.InsertTask(ctx, tk); err != nil || !ok {
			t.Fatalf("插入失败：ok=%v err=%v", ok, err)
		}
	}

	t.Run("试跑任务写入 trace，不改状态与 version，可覆盖", func(t *testing.T) {
		if err := repo.SaveTrace(ctx, trial.ID, datatypes.JSON(`{"hooks":[{"name":"buildSubmitRequest"}]}`)); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetByIDAny(ctx, trial.ID)
		if err != nil || !jsonEqual(got.TraceJSON, `{"hooks": [{"name": "buildSubmitRequest"}]}`) {
			t.Fatalf("trace 未写入：%+v %v", got, err)
		}
		if got.Status != model.TaskRunning || got.Version != trial.Version {
			t.Fatalf("不应改动状态与 version：%+v", got)
		}
		if err := repo.SaveTrace(ctx, trial.ID, datatypes.JSON(`{"hooks":[]}`)); err != nil {
			t.Fatal(err)
		}
		if got, _ = repo.GetByIDAny(ctx, trial.ID); !jsonEqual(got.TraceJSON, `{"hooks": []}`) {
			t.Fatalf("trace 未被覆盖：%s", got.TraceJSON)
		}
	})

	t.Run("终态的试跑任务也可以补写 trace", func(t *testing.T) {
		done := gtTask(gtNewUserID(), model.TaskSucceeded, 0, time.Now())
		done.IsTest = true
		if _, err := repo.InsertTask(ctx, done); err != nil {
			t.Fatal(err)
		}
		if err := repo.SaveTrace(ctx, done.ID, datatypes.JSON(`{"ok":true}`)); err != nil {
			t.Fatalf("终态试跑任务应允许写 trace：%v", err)
		}
	})

	t.Run("正式任务不写入，返回 ErrNotFound", func(t *testing.T) {
		if err := repo.SaveTrace(ctx, formal.ID, datatypes.JSON(`{"x":1}`)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
		if got, _ := repo.GetByIDAny(ctx, formal.ID); len(got.TraceJSON) != 0 {
			t.Fatalf("正式任务不应被写入 trace：%s", got.TraceJSON)
		}
	})

	t.Run("任务不存在返回 ErrNotFound", func(t *testing.T) {
		if err := repo.SaveTrace(ctx, 999999999, datatypes.JSON(`{}`)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})
}
