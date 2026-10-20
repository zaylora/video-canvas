package model_test

import (
	"testing"

	. "video-canvas/internal/model"
)

// 任务状态决定它由哪个阶段的队列负责：pending 提交、queued / running 查询、finalizing 转存，终态没有阶段。
func TestQueueStageOfStatus(t *testing.T) {
	cases := map[string]string{
		TaskPending: QueueStageSubmit, TaskQueued: QueueStagePoll, TaskRunning: QueueStagePoll, TaskFinalizing: QueueStageFinalize,
	}
	for status, want := range cases {
		if got, ok := QueueStageOfStatus(status); !ok || got != want {
			t.Fatalf("%s 应属于 %s，实际 %s %v", status, want, got, ok)
		}
	}
	for _, status := range []string{TaskSucceeded, TaskFailed, TaskCanceled, TaskExpired, "", "nope"} {
		if got, ok := QueueStageOfStatus(status); ok || got != "" {
			t.Fatalf("%q 不是进行中的状态，不应有阶段：%s %v", status, got, ok)
		}
	}
	// 所有非终态都必须有阶段：新增状态时漏改映射，任务会没有队列负责
	for _, status := range ActiveTaskStatuses {
		if _, ok := QueueStageOfStatus(status); !ok {
			t.Fatalf("非终态 %s 没有对应的队列阶段", status)
		}
	}
}

// 后台只显示中文名；每个阶段都有默认并发，未知阶段没有名字。
func TestQueueStageLabelsAndDefaults(t *testing.T) {
	want := map[string]string{QueueStageSubmit: "提交上游", QueueStagePoll: "查询进度", QueueStageFinalize: "转存产物"}
	for _, stage := range QueueStages {
		if QueueStageLabel(stage) != want[stage] {
			t.Fatalf("%s 的中文名应为 %q，实际 %q", stage, want[stage], QueueStageLabel(stage))
		}
		if DefaultQueueConcurrency[stage] < 1 {
			t.Fatalf("%s 缺少默认并发", stage)
		}
	}
	if len(QueueStages) != 3 || QueueStageLabel("nope") != "" {
		t.Fatalf("阶段列表或未知阶段不对：%v", QueueStages)
	}
	if DefaultQueueConcurrency[QueueStageSubmit] != 8 || DefaultQueueConcurrency[QueueStagePoll] != 16 || DefaultQueueConcurrency[QueueStageFinalize] != 4 {
		t.Fatalf("默认并发应为 提交 8 / 查询 16 / 转存 4：%v", DefaultQueueConcurrency)
	}
}
