package service_test

import (
	"context"
	"strings"
	"testing"

	"video-canvas/internal/model"
)

// 失败 / 超时 / 取消迁移时，要把运营排查需要的原因交给仓储写进任务事件：
// 上游错误原文（≤16KB，只为保护存储）、超时与取消的原因。
func TestGenerationTaskService_EventDetail(t *testing.T) {
	ctx := context.Background()
	const user = uint64(7)
	newEnv := func(status string) (*taskSvcEnv, *model.GenerationTask) {
		env := newTaskSvcEnv(defaultTaskLimits())
		env.repo.addCredit(user, 50, 10)
		task := env.repo.addTask(model.GenerationTask{UserID: user, Status: status, Credits: 10, Provider: "p1"})
		env.repo.ledger = append(env.repo.ledger, model.CreditLedger{UserID: user, TaskID: model.TaskIDPtr(task.ID), Type: model.LedgerFreeze, Amount: 10})
		return env, task
	}

	t.Run("FailWithUpstream：上游原文写进事件详情，用户文案不变", func(t *testing.T) {
		env, task := newEnv(model.TaskRunning)
		applied, err := env.svc.FailWithUpstream(ctx, task, "provider_error", "平台繁忙，请稍后重试", `HTTP 503 {"error":"overloaded"}`)
		if err != nil || !applied {
			t.Fatalf("applied=%v err=%v", applied, err)
		}
		if d := env.repo.eventDetails[task.ID]; d["upstream"] != `HTTP 503 {"error":"overloaded"}` {
			t.Fatalf("事件详情应带上游原文：%+v", d)
		}
		if got := env.repo.tasks[task.ID]; got.ErrorMessage != "平台繁忙，请稍后重试" || got.Status != model.TaskFailed {
			t.Fatalf("用户可见字段不应含上游原文：%+v", got)
		}
	})

	t.Run("FailWithUpstream：超过 16KB 截断且不切坏 UTF-8", func(t *testing.T) {
		env, task := newEnv(model.TaskRunning)
		raw := strings.Repeat("错", 6000) // 18000 字节
		if _, err := env.svc.FailWithUpstream(ctx, task, "provider_error", "x", raw); err != nil {
			t.Fatal(err)
		}
		got, _ := env.repo.eventDetails[task.ID]["upstream"].(string)
		if len(got) > 16*1024 || len(got) < 16*1024-3 || strings.ContainsRune(got, '�') {
			t.Fatalf("应截到 16KB 以内且保持合法 UTF-8：len=%d", len(got))
		}
	})

	t.Run("Fail（没有上游原文）：事件详情里没有 upstream", func(t *testing.T) {
		env, task := newEnv(model.TaskRunning)
		if _, err := env.svc.Fail(ctx, task, "moderation", "内容未通过审核"); err != nil {
			t.Fatal(err)
		}
		if _, ok := env.repo.eventDetails[task.ID]["upstream"]; ok {
			t.Fatalf("没有上游原文时不应写 upstream：%+v", env.repo.eventDetails[task.ID])
		}
	})

	t.Run("Expire：事件原因是 deadline", func(t *testing.T) {
		env, task := newEnv(model.TaskPending)
		if _, err := env.svc.Expire(ctx, task); err != nil {
			t.Fatal(err)
		}
		if env.repo.eventDetails[task.ID]["reason"] != "deadline" {
			t.Fatalf("超时事件原因应为 deadline：%+v", env.repo.eventDetails[task.ID])
		}
	})

	t.Run("Cancel：用户取消排队中的任务，事件原因是 user_cancel", func(t *testing.T) {
		env, task := newEnv(model.TaskPending)
		if _, err := env.svc.Cancel(ctx, user, task.ID); err != nil {
			t.Fatal(err)
		}
		if env.repo.eventDetails[task.ID]["reason"] != "user_cancel" {
			t.Fatalf("取消事件原因应为 user_cancel：%+v", env.repo.eventDetails[task.ID])
		}
	})

	t.Run("CancelForced：后台封禁时强制取消，事件原因是 admin_cancel，不会被当成用户取消", func(t *testing.T) {
		env, task := newEnv(model.TaskRunning)
		if _, err := env.svc.CancelForced(ctx, user, task.ID); err != nil {
			t.Fatal(err)
		}
		if env.repo.eventDetails[task.ID]["reason"] != "admin_cancel" {
			t.Fatalf("强制取消事件原因应为 admin_cancel：%+v", env.repo.eventDetails[task.ID])
		}
	})

	t.Run("Cancel：已提交给平台的任务用户取消被拒绝，不留取消事件详情", func(t *testing.T) {
		env, task := newEnv(model.TaskQueued)
		if _, err := env.svc.Cancel(ctx, user, task.ID); err == nil {
			t.Fatal("已受理的任务用户不能取消")
		}
		if len(env.repo.eventDetails[task.ID]) != 0 {
			t.Fatalf("被拒绝的取消不应写事件详情：%+v", env.repo.eventDetails[task.ID])
		}
	})

	t.Run("Complete：成功事件没有额外详情", func(t *testing.T) {
		env, task := newEnv(model.TaskFinalizing)
		if _, err := env.svc.Complete(ctx, task, nil, nil); err != nil {
			t.Fatal(err)
		}
		if len(env.repo.eventDetails[task.ID]) != 0 {
			t.Fatalf("成功事件不应带详情：%+v", env.repo.eventDetails[task.ID])
		}
	})
}
