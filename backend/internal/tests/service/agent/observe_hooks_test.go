package agent_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"video-canvas/internal/model"
	. "video-canvas/internal/service/agent"
)

// fakeRefresher 记下被要求刷新摘要的运行。
type fakeRefresher struct {
	mu   sync.Mutex
	ids  []uint64
	fail error
}

func (f *fakeRefresher) RefreshDigest(_ context.Context, runID uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, runID)
	return f.fail
}

func (f *fakeRefresher) got() []uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uint64(nil), f.ids...)
}

func TestRunStatusEvent_RefreshesDigest(t *testing.T) {
	ctx := context.Background()

	t.Run("排队和运行中不刷新；停止后刷新一次", func(t *testing.T) {
		e := newAgentEnv(t)
		obs := &fakeRefresher{}
		e.svc.SetObserver(obs)
		v := e.start(t, "拆分镜")
		if got := obs.got(); len(got) != 0 {
			t.Fatalf("queued 不刷新：%v", got)
		}
		e.setStatus(e.rt.started[0], model.RunRunning)
		runID := e.rt.started[0]
		if _, err := e.svc.Cancel(ctx, 1, runID); err != nil {
			t.Fatal(err)
		}
		if got := obs.got(); len(got) != 1 || got[0] != runID {
			t.Errorf("停止后应刷新该运行：%v（v=%v）", got, v.ID)
		}
	})

	t.Run("启动失败（运行时不可用）也刷新：失败的运行最该留下摘要", func(t *testing.T) {
		e := newAgentEnv(t)
		obs := &fakeRefresher{}
		e.svc.SetObserver(obs)
		e.rt.startErr = errors.New("没有 node")
		_, _ = e.svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "x"})
		if got := obs.got(); len(got) != 1 {
			t.Errorf("got=%v", got)
		}
	})

	t.Run("刷新失败不影响运行本身", func(t *testing.T) {
		e := newAgentEnv(t)
		e.svc.SetObserver(&fakeRefresher{fail: errors.New("库不可用")})
		e.start(t, "x")
		if _, err := e.svc.Cancel(ctx, 1, e.rt.started[0]); err != nil {
			t.Errorf("观测失败不能让用户的操作失败：%v", err)
		}
	})

	t.Run("没有配置观察者：照常工作", func(t *testing.T) {
		e := newAgentEnv(t)
		e.start(t, "x")
		if _, err := e.svc.Cancel(ctx, 1, e.rt.started[0]); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAgentService_Emit(t *testing.T) {
	e := newAgentEnv(t)
	e.svc.Emit(context.Background(), 1, e.sess, 0, 7, "feedback.set", map[string]any{"rating": 1})
	if got := e.repo.eventTypes(); len(got) != 1 || got[0] != "feedback.set" {
		t.Errorf("Emit 应落库并推送一条事件：%v", got)
	}
	if len(e.bc.msgs) != 1 {
		t.Errorf("应推送：%d", len(e.bc.msgs))
	}
}

func TestAgentService_UndoEmitsEvent(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*agentEnv, *fakeRefresher, *model.AgentRun) {
		e := newAgentEnv(t)
		obs := &fakeRefresher{}
		e.svc.SetObserver(obs)
		run := e.runningRun(t)
		if _, err := NewAgentCanvasService(e.repo, e.bc).ApplyOps(ctx, run, "tc1", []byte(buildOps)); err != nil {
			t.Fatal(err)
		}
		e.setStatus(run.ID, model.RunSucceeded)
		return e, obs, run
	}

	t.Run("撤销成功：发 run.undone（带还原条数），并刷新摘要", func(t *testing.T) {
		e, obs, run := setup(t)
		res, err := e.svc.Undo(ctx, 1, run.ID)
		if err != nil || res.Reverted == 0 {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		ev := e.repo.lastEvent("run.undone")
		if ev == nil || ev["reverted"] != float64(res.Reverted) {
			t.Errorf("run.undone=%v", ev)
		}
		if got := obs.got(); len(got) == 0 || got[len(got)-1] != run.ID {
			t.Errorf("撤销后摘要的 undone 要更新：%v", got)
		}
	})

	t.Run("撤销失败（别人的运行）：不发事件", func(t *testing.T) {
		e, _, run := setup(t)
		if _, err := e.svc.Undo(ctx, 2, run.ID); err == nil {
			t.Fatal("应失败")
		}
		if ev := e.repo.lastEvent("run.undone"); ev != nil {
			t.Errorf("不应有事件：%v", ev)
		}
	})

	t.Run("运行没改过画布：没有写入就不算用户撤销，不发事件", func(t *testing.T) {
		e := newAgentEnv(t)
		run := e.runningRun(t)
		e.setStatus(run.ID, model.RunSucceeded)
		res, err := e.svc.Undo(ctx, 1, run.ID)
		if err != nil || res.Reverted != 0 {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		if ev := e.repo.lastEvent("run.undone"); ev != nil {
			t.Errorf("不应有事件：%v", ev)
		}
	})
}

// ---- 进程 stderr 末尾落库 ----

type fakeTailSink struct {
	mu   sync.Mutex
	got  map[uint64]string
	fail error
}

func (f *fakeTailSink) RecordRuntimeTail(_ context.Context, runID uint64, tail string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.got == nil {
		f.got = map[uint64]string{}
	}
	f.got[runID] = tail
	return f.fail
}

func (f *fakeTailSink) tail(runID uint64) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.got[runID]
	return v, ok
}

func TestProcessRuntime_RecordsStderrTail(t *testing.T) {
	t.Run("异常退出：stderr 末尾落库，运行照常标为中断", func(t *testing.T) {
		e := newRuntimeEnv(t)
		sink := &fakeTailSink{}
		e.rt.SetTailSink(sink)
		run, _ := e.startRun(t, "all")
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 1, StderrTail: "boom: 桥断了"}
		waitFor(t, "运行变为 interrupted", func() bool { return e.run(run.ID).Status == model.RunInterrupted })
		if got, ok := sink.tail(run.ID); !ok || got != "boom: 桥断了" {
			t.Errorf("tail=%q ok=%v", got, ok)
		}
	})

	t.Run("正常退出：诊断信息照样留下（压缩失败、保存历史失败这类都只写 stderr）", func(t *testing.T) {
		e := newRuntimeEnv(t)
		sink := &fakeTailSink{}
		e.rt.SetTailSink(sink)
		run, _ := e.startRun(t, "all")
		e.setStatus(run.ID, model.RunSucceeded)
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 0, StderrTail: `{"msg":"摘要压缩失败，只做裁剪"}`}
		waitFor(t, "令牌被收回", func() bool { return e.br.TokenCount() == 0 })
		waitFor(t, "stderr 落库", func() bool { _, ok := sink.tail(run.ID); return ok })
	})

	t.Run("stderr 为空：不写", func(t *testing.T) {
		e := newRuntimeEnv(t)
		sink := &fakeTailSink{}
		e.rt.SetTailSink(sink)
		run, _ := e.startRun(t, "all")
		e.setStatus(run.ID, model.RunSucceeded)
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 0}
		waitFor(t, "令牌被收回", func() bool { return e.br.TokenCount() == 0 })
		time.Sleep(50 * time.Millisecond)
		if _, ok := sink.tail(run.ID); ok {
			t.Error("没有 stderr 不该写")
		}
	})

	t.Run("落库失败不影响退出后的状态修正", func(t *testing.T) {
		e := newRuntimeEnv(t)
		e.rt.SetTailSink(&fakeTailSink{fail: errors.New("库不可用")})
		run, _ := e.startRun(t, "all")
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 1, StderrTail: "boom"}
		waitFor(t, "运行变为 interrupted", func() bool { return e.run(run.ID).Status == model.RunInterrupted })
	})
}

func TestObserve_RecordRuntimeTail(t *testing.T) {
	ctx := context.Background()
	e := newObserveEnv(t)
	if err := e.svc.RecordRuntimeTail(ctx, 5, "stderr 内容"); err != nil {
		t.Fatal(err)
	}
	d := e.repo.digests[5]
	if d == nil || d.RuntimeTail == nil || *d.RuntimeTail != "stderr 内容" {
		t.Fatalf("摘要应带上 stderr：%+v", d)
	}
	// 空串不写，也不创建摘要
	e2 := newObserveEnv(t)
	if err := e2.svc.RecordRuntimeTail(ctx, 5, ""); err != nil || len(e2.repo.digests) != 0 {
		t.Errorf("空 stderr 不写：err=%v digests=%d", err, len(e2.repo.digests))
	}
	// 最多 4KB，不切坏字符
	e3 := newObserveEnv(t)
	_ = e3.svc.RecordRuntimeTail(ctx, 5, strings.Repeat("错", 3000))
	got := *e3.repo.digests[5].RuntimeTail
	if len(got) > 4<<10 || !utf8.ValidString(got) {
		t.Errorf("len=%d valid=%v", len(got), utf8.ValidString(got))
	}
	if err := e.svc.RecordRuntimeTail(ctx, 404, "x"); err == nil {
		t.Error("运行不存在应返回错误")
	}
}

func TestAgentBridge_RunIDForToken(t *testing.T) {
	b := newBridgeEnv(t)
	tok, run := b.token(t, "all")
	if got := b.br.RunIDForToken(tok); got != run.ID {
		t.Errorf("got=%d want %d", got, run.ID)
	}
	if got := b.br.RunIDForToken("nope"); got != 0 {
		t.Errorf("未知令牌应返回 0：%d", got)
	}
	b.br.RevokeToken(tok)
	if got := b.br.RunIDForToken(tok); got != 0 {
		t.Errorf("已收回的令牌应返回 0：%d", got)
	}
}
