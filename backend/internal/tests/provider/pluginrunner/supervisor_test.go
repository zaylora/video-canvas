package pluginrunner_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"video-canvas/internal/provider/pluginrunner"
)

// fakeProc 是可以由测试控制退出时机的假进程。
type fakeProc struct {
	exit   chan error
	killed atomic.Bool
	once   sync.Once
}

func newFakeProc() *fakeProc { return &fakeProc{exit: make(chan error, 1)} }

func (p *fakeProc) Wait() error { return <-p.exit }

func (p *fakeProc) Kill() error {
	p.killed.Store(true)
	p.once.Do(func() { p.exit <- errors.New("killed") })
	return nil
}

func (p *fakeProc) die(err error) { p.once.Do(func() { p.exit <- err }) }

// 进程崩溃后被自动重启，且重启间隔按指数退避增长。
func TestSuperviseRestartsWithBackoff(t *testing.T) {
	started := make(chan *fakeProc, 8)
	var mu sync.Mutex
	var delays []time.Duration
	opts := pluginrunner.SupervisorOptions{
		MinBackoff: 20 * time.Millisecond, MaxBackoff: 80 * time.Millisecond, StableAfter: time.Hour,
		Start: func() (pluginrunner.Process, error) {
			p := newFakeProc()
			started <- p
			return p, nil
		},
		OnExit: func(_ error, _, delay time.Duration) {
			mu.Lock()
			delays = append(delays, delay)
			mu.Unlock()
		},
	}
	first := newFakeProc()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); pluginrunner.Supervise(ctx, first, opts) }()

	first.die(errors.New("oom"))
	cur := waitProc(t, started)
	for i := 0; i < 3; i++ {
		cur.die(errors.New("oom again"))
		cur = waitProc(t, started)
	}

	cancel()
	<-done
	if !cur.killed.Load() {
		t.Fatal("监督结束时应杀掉当前进程")
	}
	mu.Lock()
	defer mu.Unlock()
	want := []time.Duration{20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond, 80 * time.Millisecond}
	if len(delays) != len(want) {
		t.Fatalf("退避记录 %v，期望 %v", delays, want)
	}
	for i := range want {
		if delays[i] != want[i] {
			t.Fatalf("第 %d 次退避 %v，期望 %v（全部 %v）", i+1, delays[i], want[i], delays)
		}
	}
}

// 进程活过稳定门槛之后再退出，退避从最短间隔重新算起。
func TestSuperviseBackoffResetsAfterStableRun(t *testing.T) {
	started := make(chan *fakeProc, 8)
	var mu sync.Mutex
	var delays []time.Duration
	opts := pluginrunner.SupervisorOptions{
		MinBackoff: 10 * time.Millisecond, MaxBackoff: time.Second, StableAfter: 60 * time.Millisecond,
		Start: func() (pluginrunner.Process, error) {
			p := newFakeProc()
			started <- p
			return p, nil
		},
		OnExit: func(_ error, _, delay time.Duration) {
			mu.Lock()
			delays = append(delays, delay)
			mu.Unlock()
		},
	}
	first := newFakeProc()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); pluginrunner.Supervise(ctx, first, opts) }()

	first.die(errors.New("crash 1")) // 立刻崩：退避 10ms，下次 20ms
	second := waitProc(t, started)
	time.Sleep(100 * time.Millisecond) // 稳定运行超过门槛
	second.die(errors.New("crash 2"))
	third := waitProc(t, started)
	cancel()
	<-done
	_ = third

	mu.Lock()
	defer mu.Unlock()
	if len(delays) != 2 || delays[0] != 10*time.Millisecond || delays[1] != 10*time.Millisecond {
		t.Fatalf("稳定运行后退避应重置为最短间隔：%v", delays)
	}
}

// 重启失败也按退避重试，直到成功。
func TestSuperviseRetriesFailedStart(t *testing.T) {
	var attempts atomic.Int32
	started := make(chan *fakeProc, 1)
	opts := pluginrunner.SupervisorOptions{
		MinBackoff: 5 * time.Millisecond, MaxBackoff: 20 * time.Millisecond,
		Start: func() (pluginrunner.Process, error) {
			if attempts.Add(1) < 3 {
				return nil, errors.New("exec 失败")
			}
			p := newFakeProc()
			started <- p
			return p, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); pluginrunner.Supervise(ctx, nil, opts) }() // nil：由监督循环自己完成第一次拉起

	waitProc(t, started)
	cancel()
	<-done
	if attempts.Load() != 3 {
		t.Fatalf("应尝试 3 次拉起：%d", attempts.Load())
	}
}

// 退避等待期间 ctx 结束，立即返回，不再重启。
func TestSuperviseStopsDuringBackoff(t *testing.T) {
	var starts atomic.Int32
	opts := pluginrunner.SupervisorOptions{
		MinBackoff: time.Hour, MaxBackoff: time.Hour,
		Start: func() (pluginrunner.Process, error) {
			starts.Add(1)
			return newFakeProc(), nil
		},
	}
	first := newFakeProc()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); pluginrunner.Supervise(ctx, first, opts) }()
	first.die(errors.New("crash"))
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("退避期间 ctx 结束应立即返回")
	}
	if starts.Load() != 0 {
		t.Fatal("停止后不应再拉起进程")
	}
}

func waitProc(t *testing.T, ch chan *fakeProc) *fakeProc {
	t.Helper()
	select {
	case p := <-ch:
		return p
	case <-time.After(3 * time.Second):
		t.Fatal("等待重启超时")
		return nil
	}
}
