package service_test

import (
	"context"
	"testing"

	"video-canvas/internal/model"
)

func TestAIConfigTrialIdempotencyReusesTaskAndQueue(t *testing.T) {
	cfg, _, _, _ := aicRunSetup(t)
	env := newTaskSvcEnv(defaultTaskLimits())
	cfg.SetTestTaskCreator(env.svc)
	ctx := context.Background()
	first, err := cfg.TestRunIdempotent(ctx, 9, "m1", map[string]any{"prompt": "试跑"}, "admin-trial-1001")
	if err != nil {
		t.Fatal(err)
	}
	again, err := cfg.TestRunIdempotent(ctx, 9, "m1", map[string]any{"prompt": "试跑"}, "admin-trial-1001")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != again.ID || len(env.repo.tasks) != 1 || len(env.repo.enqueued) != 1 || len(env.repo.ledger) != 0 {
		t.Fatalf("重放重复任务、入队或扣分：%d %d %+v", first.ID, again.ID, env.repo.enqueued)
	}
	if !env.repo.tasks[first.ID].IsTest || env.repo.tasks[first.ID].Credits != 0 {
		t.Fatalf("试跑应不扣积分：%+v", env.repo.tasks[first.ID])
	}
	// 未经过助手的普通试跑每点一次创建一个独立任务。
	for i := 0; i < 2; i++ {
		if _, err := env.svc.SubmitTest(ctx, 9, fakeTaskSnapshot(model.KindVideo, 1), map[string]any{"prompt": "普通试跑"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(env.repo.tasks) != 3 {
		t.Fatalf("普通试跑被错误去重：%d", len(env.repo.tasks))
	}
}
