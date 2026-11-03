package agent_test

import (
	"context"
	"testing"
	"time"

	"video-canvas/internal/model"
)

func TestObserveRemote_SyncIdleDoesNotRewriteFinishedRuns(t *testing.T) {
	for _, mode := range []string{model.AgentModeAdmin, "all"} {
		for _, age := range []time.Duration{time.Second, 10 * time.Second} {
			t.Run(mode+"/"+age.String(), func(t *testing.T) {
				e := newRemoteEnv(t)
				e.now = obsT0.Add(100 * time.Second)
				e.addRemote(921, model.RunSucceeded, e.now.Add(-age).UnixMilli())
				run := &e.remote.facts[921].Run
				run.Mode = mode
				if mode == model.AgentModeAdmin {
					run.CanvasID = "0"
				}
				ctx := context.Background()
				if n, err := e.svc.SyncRemote(ctx); err != nil || n != 1 {
					t.Fatalf("首次同步应写入一次: n=%d err=%v", n, err)
				}
				digest := e.repo.digests[921]
				for range 3 {
					previousScan := e.now
					e.now = e.now.Add(10 * time.Second)
					if n, err := e.svc.SyncRemote(ctx); err != nil || n != 0 {
						t.Fatalf("已结束且未变化的运行不应重复写入: n=%d err=%v", n, err)
					}
					if e.repo.digests[921] != digest {
						t.Fatal("未变化的摘要被覆盖")
					}
					if got, want := e.remote.listArgs[len(e.remote.listArgs)-1], previousScan.Add(-5*time.Second).UnixMilli(); got != want {
						t.Fatalf("空闲时游标也应随成功扫描推进: after=%d want=%d", got, want)
					}
				}
			})
		}
	}
}

func TestObserveRemote_SyncOverlapStillFindsLateRunsAndChanges(t *testing.T) {
	e := newRemoteEnv(t)
	e.now = obsT0.Add(100 * time.Second)
	e.addRemote(931, model.RunSucceeded, ms(99_000))
	ctx := context.Background()
	if n, err := e.svc.SyncRemote(ctx); err != nil || n != 1 {
		t.Fatalf("首次同步: n=%d err=%v", n, err)
	}
	unchanged := e.repo.digests[931]

	// 同一毫秒的另一条运行晚提交：重叠窗口必须补到它，已处理的版本不应重写。
	e.addRemote(932, model.RunSucceeded, ms(99_000))
	e.now = e.now.Add(10 * time.Second)
	if n, err := e.svc.SyncRemote(ctx); err != nil || n != 1 {
		t.Fatalf("只应补拉迟到的运行: n=%d err=%v", n, err)
	}
	if e.repo.digests[932] == nil || e.repo.digests[931] != unchanged {
		t.Fatal("迟到运行未同步，或已处理的版本被重写")
	}

	// 同一运行后来发生评价等真实变化，仍需重新汇总。
	e.remote.refs[0].UpdatedAt = ms(111_000)
	e.remote.facts[931].Feedback.Rating = 1
	e.now = e.now.Add(10 * time.Second)
	if n, err := e.svc.SyncRemote(ctx); err != nil || n != 1 {
		t.Fatalf("真实更新应重新汇总: n=%d err=%v", n, err)
	}
	if e.repo.digests[931].Feedback != 1 {
		t.Fatal("评价变化未同步")
	}
}

func TestObserveRemote_SyncEmptyScanAdvancesCursor(t *testing.T) {
	e := newRemoteEnv(t)
	e.now = obsT0.Add(100 * time.Second)
	ctx := context.Background()
	if n, err := e.svc.SyncRemote(ctx); err != nil || n != 0 {
		t.Fatalf("空扫描: n=%d err=%v", n, err)
	}
	e.addRemote(941, model.RunSucceeded, ms(101_000))
	e.now = e.now.Add(10 * time.Second)
	if n, err := e.svc.SyncRemote(ctx); err != nil || n != 1 {
		t.Fatalf("空扫描之后的新运行应同步: n=%d err=%v", n, err)
	}
	if got := e.remote.listArgs[1]; got != ms(95_000) {
		t.Fatalf("空扫描也应推进游标: after=%d want=%d", got, ms(95_000))
	}
}
