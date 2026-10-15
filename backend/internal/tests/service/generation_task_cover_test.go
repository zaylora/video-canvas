package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	. "video-canvas/internal/service"

	"video-canvas/internal/model"
)

// fakeCoverSetter 记录「把哪张素材设成哪张画布的封面」，可以让它返回错误。
type fakeCoverSetter struct {
	calls []coverCall
	err   error
}

type coverCall struct{ userID, canvasID, assetID uint64 }

func (f *fakeCoverSetter) SetCoverIfEmpty(_ context.Context, userID, canvasID, assetID uint64) error {
	f.calls = append(f.calls, coverCall{userID, canvasID, assetID})
	return f.err
}

// newCoverEnv 组装带封面写回依赖的任务服务，并放一个 finalizing 状态的任务。
func newCoverEnv(mut func(t *model.GenerationTask)) (*GenerationTaskService, *fakeCoverSetter, *model.GenerationTask) {
	repo := newFakeTaskRepo()
	repo.addCredit(7, 50, 10)
	cover := &fakeCoverSetter{}
	svc := NewGenerationTaskService(GenerationTaskDeps{
		Repo: repo, Registry: &fakeTaskRegistry{snap: fakeTaskSnapshot(model.KindImage, 10)},
		Executor: &fakeTaskExecutor{}, Assets: &fakeTaskAssets{items: map[uint64]*model.Asset{}},
		Broadcaster: &fakeTaskBroadcaster{}, Limits: defaultTaskLimits(), Covers: cover,
	}, WithTaskClock(func() time.Time { return time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC) }))
	canvas := uint64(42)
	tk := model.GenerationTask{UserID: 7, Status: model.TaskFinalizing, Credits: 10, Provider: "p1", CanvasProjectID: &canvas}
	if mut != nil {
		mut(&tk)
	}
	task := repo.addTask(tk)
	repo.ledger = append(repo.ledger, model.CreditLedger{UserID: 7, TaskID: model.TaskIDPtr(task.ID), Type: model.LedgerFreeze, Amount: 10})
	return svc, cover, task
}

// 任务成功时把画布里第一张生成的图写回画布封面；只有图片算，且只在任务真正完成时写。
func TestGenerationTaskComplete_WritesCanvasCover(t *testing.T) {
	ctx := context.Background()
	img := func(id uint64) model.TaskOutput { return model.TaskOutput{AssetID: id, MediaType: "image"} }

	t.Run("图片产出：用第一张图设封面", func(t *testing.T) {
		svc, cover, task := newCoverEnv(nil)
		if applied, err := svc.Complete(ctx, task, []model.TaskOutput{img(11), img(12)}, nil); err != nil || !applied {
			t.Fatalf("applied=%v err=%v", applied, err)
		}
		if len(cover.calls) != 1 || cover.calls[0] != (coverCall{7, 42, 11}) {
			t.Fatalf("应只用第一张图设封面：%+v", cover.calls)
		}
	})

	t.Run("产出里夹着文本和视频：跳过它们取第一张图片", func(t *testing.T) {
		svc, cover, task := newCoverEnv(nil)
		outs := []model.TaskOutput{{MediaType: "text", Text: "说明"}, {AssetID: 9, MediaType: "video"}, img(13)}
		_, _ = svc.Complete(ctx, task, outs, nil)
		if len(cover.calls) != 1 || cover.calls[0].assetID != 13 {
			t.Fatalf("应取图片 13：%+v", cover.calls)
		}
	})

	skip := []struct {
		name string
		mut  func(t *model.GenerationTask)
		outs []model.TaskOutput
	}{
		{"任务不属于任何画布", func(t *model.GenerationTask) { t.CanvasProjectID = nil }, []model.TaskOutput{img(11)}},
		{"试跑任务", func(t *model.GenerationTask) { t.IsTest = true }, []model.TaskOutput{img(11)}},
		{"只有视频", nil, []model.TaskOutput{{AssetID: 9, MediaType: "video"}}},
		{"只有文本", nil, []model.TaskOutput{{MediaType: "text", Text: "正文"}}},
		{"图片没有素材 ID", nil, []model.TaskOutput{{MediaType: "image"}}},
		{"没有任何产出", nil, nil},
	}
	for _, c := range skip {
		t.Run(c.name+"：不写封面", func(t *testing.T) {
			svc, cover, task := newCoverEnv(c.mut)
			if applied, err := svc.Complete(ctx, task, c.outs, nil); err != nil || !applied {
				t.Fatalf("applied=%v err=%v", applied, err)
			}
			if len(cover.calls) != 0 {
				t.Fatalf("不应写封面：%+v", cover.calls)
			}
		})
	}

	t.Run("写封面失败不影响任务完成", func(t *testing.T) {
		svc, cover, task := newCoverEnv(nil)
		cover.err = errors.New("数据库繁忙")
		applied, err := svc.Complete(ctx, task, []model.TaskOutput{img(11)}, nil)
		if err != nil || !applied {
			t.Fatalf("封面只是锦上添花，失败不该让任务失败：applied=%v err=%v", applied, err)
		}
	})

	t.Run("重复完成不重复写封面", func(t *testing.T) {
		svc, cover, task := newCoverEnv(nil)
		_, _ = svc.Complete(ctx, task, []model.TaskOutput{img(11)}, nil)
		if applied, _ := svc.Complete(ctx, task, []model.TaskOutput{img(11)}, nil); applied {
			t.Fatal("第二次应 applied=false")
		}
		if len(cover.calls) != 1 {
			t.Fatalf("只应写一次：%+v", cover.calls)
		}
	})
}
