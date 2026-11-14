package service_test

import (
	"context"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
)

// 生成任务：创建打 run.create，终态（失败 / 成功 / 取消）打 run.finish，run_id 都是界面上的任务编号。
func TestGenerationTaskLog_CreateAndFail(t *testing.T) {
	read := serviceLog(t)
	env := newTaskSvcEnv(defaultTaskLimits())
	env.repo.addCredit(1, 50, 0)
	view, err := createSingle(context.Background(), env.svc, 1, "", validCreateReq())
	assertTaskCode(t, err, 0)
	task := env.repo.tasks[view.ID]
	if _, err := env.svc.FailWithUpstream(context.Background(), task, "upstream_error", "参数不合法", `{"error":"bad size"}`); err != nil {
		t.Fatal(err)
	}

	lines := read()
	creates := logEvents(lines, logger.EventRunCreate)
	if len(creates) != 1 {
		t.Fatalf("应有 1 行 run.create：%v", lines)
	}
	wantFields(t, "run.create", creates[0], map[string]any{"run_kind": "gen", "run_id": view.TaskRef, "user_id": float64(1),
		"kind": model.KindVideo, "model": "m1", "frozen_credits": float64(task.Credits)})

	fins := logEvents(lines, logger.EventRunFinish)
	if len(fins) != 1 {
		t.Fatalf("应有 1 行 run.finish：%v", lines)
	}
	wantFields(t, "run.finish", fins[0], map[string]any{"run_kind": "gen", "run_id": view.TaskRef, "status": "failed", "level": "warn",
		"error_code": "upstream_error", "error_type": "upstream_error", "error_message": "参数不合法", "upstream": `{"error":"bad size"}`, "charged_credits": float64(0)})
	if _, ok := fins[0]["total_ms"]; !ok {
		t.Errorf("收尾应带创建到结束的总耗时：%v", fins[0])
	}
}

func TestGenerationTaskLog_SucceededAndCanceled(t *testing.T) {
	read := serviceLog(t)
	env := newTaskSvcEnv(defaultTaskLimits())
	env.repo.addCredit(1, 50, 20)
	done := env.repo.addTask(model.GenerationTask{UserID: 1, Status: model.TaskFinalizing, Credits: 10, Kind: model.KindVideo})
	if _, err := env.svc.Complete(context.Background(), done, nil, nil); err != nil {
		t.Fatal(err)
	}
	pending := env.repo.addTask(model.GenerationTask{UserID: 1, Status: model.TaskPending, Credits: 10})
	if _, err := env.svc.Cancel(context.Background(), 1, pending.ID); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	for _, m := range logEvents(read(), logger.EventRunFinish) {
		byID[m["run_id"].(string)] = m
	}
	wantFields(t, "成功", byID[idcodec.Encode(done.ID)], map[string]any{"status": "succeeded", "level": "info", "charged_credits": float64(10)})
	wantFields(t, "取消", byID[idcodec.Encode(pending.ID)], map[string]any{"status": "canceled", "level": "warn", "user_id": float64(1)})
}

// Agent 发起的任务指向那次 Agent 运行，按 Agent 运行编号能把它生成的任务一起找出来。
func TestGenerationTaskLog_AgentParent(t *testing.T) {
	read := serviceLog(t)
	env := newTaskSvcEnv(defaultTaskLimits())
	env.repo.addCredit(1, 50, 0)
	req := validCreateReq()
	req.AgentRunID = 88213
	if _, err := createSingle(context.Background(), env.svc, 1, "", req); err != nil {
		t.Fatal(err)
	}
	c := logEvents(read(), logger.EventRunCreate)[0]
	wantFields(t, "run.create", c, map[string]any{"parent_kind": "agent", "parent_id": "88213"})
}
