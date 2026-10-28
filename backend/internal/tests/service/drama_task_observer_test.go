package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/ws"
)

// TestGenerationTaskService_ObserverCannotUndoSettlement 验证片段回写失败不会把已结算任务改成失败，原有任务事件继续送达。
func TestGenerationTaskService_ObserverCannotUndoSettlement(t *testing.T) {
	for _, node := range []string{"seg:abc", "canvas-node", "rec:12:0"} {
		t.Run(node, func(t *testing.T) {
			env := newTaskSvcEnv(defaultTaskLimits())
			env.repo.addCredit(1, 100, 10)
			task := env.repo.addTask(model.GenerationTask{UserID: 1, NodeID: node, Kind: model.KindVideo, Status: model.TaskFinalizing, Credits: 10})
			observer := &failingTaskObserver{}
			env.svc.SetTaskObserver(observer)
			applied, err := env.svc.Complete(context.Background(), task, []model.TaskOutput{{AssetID: 9, MediaType: "video"}}, nil)
			if err != nil || !applied {
				t.Fatalf("结算失败：applied=%v err=%v", applied, err)
			}
			if env.repo.tasks[task.ID].Status != model.TaskSucceeded || env.repo.credits[1].Balance != 90 || env.repo.credits[1].Frozen != 0 {
				t.Fatalf("观察者故障破坏任务或积分：task=%+v credit=%+v", env.repo.tasks[task.ID], env.repo.credits[1])
			}
			if observer.status != model.TaskSucceeded || observer.assetID != 9 {
				t.Fatalf("观察者未见到已提交产物：%+v", observer)
			}
			if len(env.bc.msgs) != 1 || env.bc.msgs[0].Type != ws.TypeTaskUpdated {
				t.Fatalf("原有任务通知丢失：%+v", env.bc.msgs)
			}
		})
	}
}

type failingTaskObserver struct {
	status  string
	assetID uint64
}

func (o *failingTaskObserver) OnTaskChanged(_ context.Context, task *model.GenerationTask) error {
	o.status = task.Status
	var outputs []model.TaskOutput
	if err := json.Unmarshal(task.OutputJSON, &outputs); err != nil {
		return err
	}
	if len(outputs) > 0 {
		o.assetID = outputs[0].AssetID
	}
	return errors.New("片段写入暂不可用")
}
