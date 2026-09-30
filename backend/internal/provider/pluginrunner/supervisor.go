package pluginrunner

import (
	"context"
	"time"
)

// Process 是被监督的 runner 进程。抽成接口是为了让重启逻辑不依赖真实子进程，单元测试用假进程即可。
type Process interface {
	// Wait 阻塞到进程退出，返回退出原因。
	Wait() error
	// Kill 强制结束进程；进程已退出时调用也是安全的。
	Kill() error
}

// SupervisorOptions 是重启监督的参数。
type SupervisorOptions struct {
	// Start 重新拉起一个 runner 进程。
	Start func() (Process, error)
	// MinBackoff 是第一次重启前的等待时间，默认 1 秒；MaxBackoff 是退避上限，默认 30 秒。
	MinBackoff, MaxBackoff time.Duration
	// StableAfter 是“稳定运行”的门槛：进程活过这段时间再退出，退避重新从 MinBackoff 算起，默认 30 秒。
	StableAfter time.Duration
	// OnExit 在进程退出、准备等待 delay 后重启时回调（用来记日志）。
	OnExit func(err error, lived, delay time.Duration)
	// OnStartError 在重新拉起失败、准备等待 delay 后再试时回调。
	OnStartError func(err error, delay time.Duration)
}

func (o *SupervisorOptions) applyDefaults() {
	if o.MinBackoff <= 0 {
		o.MinBackoff = time.Second
	}
	if o.MaxBackoff < o.MinBackoff {
		o.MaxBackoff = 30 * time.Second
		if o.MaxBackoff < o.MinBackoff {
			o.MaxBackoff = o.MinBackoff
		}
	}
	if o.StableAfter <= 0 {
		o.StableAfter = 30 * time.Second
	}
	if o.OnExit == nil {
		o.OnExit = func(error, time.Duration, time.Duration) {}
	}
	if o.OnStartError == nil {
		o.OnStartError = func(error, time.Duration) {}
	}
}

// Supervise 监督 first 进程：它退出后按指数退避（MinBackoff 起步，翻倍，封顶 MaxBackoff）自动重启，
// 直到 ctx 结束，此时会杀掉当前进程并返回。
//
// 退避的目的是避免崩溃循环把 CPU 占满：runner 因插件耗尽内存被杀时，重启后往往还会被同一个坏任务再次打挂，
// 立刻重启只会空转；而活过 StableAfter 之后再退出，则视为一次新的偶发故障，重新从最短间隔开始。
// first 为 nil 表示由监督循环自己完成第一次拉起。
func Supervise(ctx context.Context, first Process, opts SupervisorOptions) {
	opts.applyDefaults()
	proc := first
	backoff := opts.MinBackoff
	for {
		if proc != nil {
			started := time.Now()
			exited := make(chan error, 1)
			go func(p Process) { exited <- p.Wait() }(proc)
			select {
			case <-ctx.Done():
				_ = proc.Kill()
				<-exited
				return
			case err := <-exited:
				lived := time.Since(started)
				delay := backoff
				if lived >= opts.StableAfter {
					backoff, delay = opts.MinBackoff, opts.MinBackoff
				}
				backoff = nextBackoff(backoff, opts.MaxBackoff)
				opts.OnExit(err, lived, delay)
				if !sleepCtx(ctx, delay) {
					return
				}
			}
			proc = nil
		}
		p, err := opts.Start()
		if err != nil {
			delay := backoff
			backoff = nextBackoff(backoff, opts.MaxBackoff)
			opts.OnStartError(err, delay)
			if !sleepCtx(ctx, delay) {
				return
			}
			continue
		}
		proc = p
	}
}

func nextBackoff(cur, limit time.Duration) time.Duration {
	if cur *= 2; cur > limit {
		return limit
	}
	return cur
}

// sleepCtx 等待 d，ctx 先结束返回 false。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
