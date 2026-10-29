package service_test

import (
	"context"
	"errors"
	"testing"

	"video-canvas/internal/model"
)

// Agent 发起的任务带着发起它的运行 id；任务进入任何终态时，在结算的同一个事务里通知 Agent。

func TestGenerationTaskService_Create_RecordsAgentRunID(t *testing.T) {
	env := newTaskSvcEnv(defaultTaskLimits())
	env.repo.addCredit(1, 50, 0)
	req := validCreateReq()
	req.AgentRunID = 9001

	view, err := createSingle(context.Background(), env.svc, 1, "", req)
	assertTaskCode(t, err, 0)
	if got := env.repo.tasks[view.ID].AgentRunID; got != 9001 {
		t.Fatalf("任务应记下发起它的 Agent 运行 id 9001，实际 %d", got)
	}

	plain, err := createSingle(context.Background(), env.svc, 1, "k2", validCreateReq())
	assertTaskCode(t, err, 0)
	if got := env.repo.tasks[plain.ID].AgentRunID; got != 0 {
		t.Fatalf("用户自己发起的任务没有运行 id，实际 %d", got)
	}
}

func TestGenerationTaskService_TerminalTransitions_NotifyAgent(t *testing.T) {
	ctx := context.Background()
	// 每种终态迁移都要通知：成功、失败、超时、取消
	finishers := map[string]struct {
		from   string
		finish func(env *taskSvcEnv, task *model.GenerationTask) (bool, error)
	}{
		"成功": {model.TaskFinalizing, func(env *taskSvcEnv, task *model.GenerationTask) (bool, error) {
			return env.svc.Complete(ctx, task, nil, nil)
		}},
		"失败": {model.TaskRunning, func(env *taskSvcEnv, task *model.GenerationTask) (bool, error) {
			return env.svc.Fail(ctx, task, "x", "boom")
		}},
		"超时": {model.TaskRunning, func(env *taskSvcEnv, task *model.GenerationTask) (bool, error) {
			return env.svc.Expire(ctx, task)
		}},
		"取消": {model.TaskPending, func(env *taskSvcEnv, task *model.GenerationTask) (bool, error) {
			_, err := env.svc.Cancel(ctx, task.UserID, task.ID)
			return err == nil, err
		}},
	}
	for name, f := range finishers {
		t.Run(name+"：带运行 id 的任务入队一次门铃", func(t *testing.T) {
			env := newTaskSvcEnv(defaultTaskLimits())
			env.repo.addCredit(1, 50, 10)
			task := env.repo.addTask(model.GenerationTask{UserID: 1, Status: f.from, Credits: 10, Provider: "p1", AgentRunID: 9001})
			applied, err := f.finish(env, task)
			if err != nil || !applied {
				t.Fatalf("applied=%v err=%v", applied, err)
			}
			if len(env.repo.agentNotified) != 1 || env.repo.agentNotified[0] != task.ID {
				t.Fatalf("应为任务 %d 入队一次门铃，实际 %v", task.ID, env.repo.agentNotified)
			}
		})
		t.Run(name+"：用户自己的任务不通知", func(t *testing.T) {
			env := newTaskSvcEnv(defaultTaskLimits())
			env.repo.addCredit(1, 50, 10)
			task := env.repo.addTask(model.GenerationTask{UserID: 1, Status: f.from, Credits: 10, Provider: "p1"})
			if _, err := f.finish(env, task); err != nil {
				t.Fatal(err)
			}
			if len(env.repo.agentNotified) != 0 {
				t.Fatalf("没有运行 id 的任务不应入队门铃：%v", env.repo.agentNotified)
			}
		})
	}

	t.Run("试跑任务不通知", func(t *testing.T) {
		env := newTaskSvcEnv(defaultTaskLimits())
		task := env.repo.addTask(model.GenerationTask{UserID: 1, Status: model.TaskFinalizing, Provider: "p1", AgentRunID: 9001, IsTest: true})
		if _, err := env.svc.Complete(ctx, task, nil, nil); err != nil {
			t.Fatal(err)
		}
		if len(env.repo.agentNotified) != 0 {
			t.Fatalf("试跑任务不应入队门铃：%v", env.repo.agentNotified)
		}
	})

	t.Run("状态迁移没有命中（已被别的流程处理）：不重复通知", func(t *testing.T) {
		env := newTaskSvcEnv(defaultTaskLimits())
		env.repo.addCredit(1, 50, 10)
		task := env.repo.addTask(model.GenerationTask{UserID: 1, Status: model.TaskSucceeded, Credits: 10, Provider: "p1", AgentRunID: 9001})
		if applied, _ := env.svc.Fail(ctx, task, "x", "boom"); applied {
			t.Fatal("终态任务不应再被置为失败")
		}
		if len(env.repo.agentNotified) != 0 {
			t.Fatalf("没有发生迁移就不应通知：%v", env.repo.agentNotified)
		}
	})

	t.Run("入队失败：整个结算事务回滚，任务还是原状态", func(t *testing.T) {
		env := newTaskSvcEnv(defaultTaskLimits())
		env.repo.addCredit(1, 50, 10)
		task := env.repo.addTask(model.GenerationTask{UserID: 1, Status: model.TaskRunning, Credits: 10, Provider: "p1", AgentRunID: 9001})
		env.repo.errs["EnqueueAgentNotify"] = errors.New("boom")
		if _, err := env.svc.Fail(ctx, task, "x", "boom"); err == nil {
			t.Fatal("入队失败应返回错误，让 worker 稍后重试")
		}
		if env.repo.tasks[task.ID].Status != model.TaskRunning || env.repo.credits[1].Frozen != 10 {
			t.Fatalf("事务应整体回滚：%+v %+v", env.repo.tasks[task.ID], env.repo.credits[1])
		}
	})
}
