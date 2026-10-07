package llmgateway

import (
	"context"
	"sync"
	"time"
)

// limiter 限制同一个渠道的请求：同时在途数（信号量）和每秒请求数（按间隔排队）。
type limiter struct {
	sem      chan struct{} // 为 nil 表示不限并发
	cap      int
	interval time.Duration // 为 0 表示不限速率
	mu       sync.Mutex
	next     time.Time
}

// acquire 取得一个名额，排队期间 ctx 取消立即返回。返回的 release 必须调用。
func (l *limiter) acquire(ctx context.Context) (func(), error) {
	if l.sem != nil {
		select {
		case l.sem <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	release := func() {
		if l.sem != nil {
			<-l.sem
		}
	}
	if err := l.waitRate(ctx); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// waitRate 按最小间隔把请求错开：每个请求预约下一个可用时刻，再睡到那个时刻。
func (l *limiter) waitRate(ctx context.Context) error {
	if l.interval <= 0 {
		return nil
	}
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	wait := l.next.Sub(now)
	l.next = l.next.Add(l.interval)
	l.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// limiterSet 按渠道 key 保存限流器；渠道的限流配置变了就换一个新的。
type limiterSet struct {
	mu sync.Mutex
	m  map[string]*limiter
}

func (s *limiterSet) get(key string, rps float64, maxConc int) *limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	var interval time.Duration
	if rps > 0 {
		interval = time.Duration(float64(time.Second) / rps)
	}
	if l, ok := s.m[key]; ok && l.cap == maxConc && l.interval == interval {
		return l
	}
	l := &limiter{cap: maxConc, interval: interval}
	if maxConc > 0 {
		l.sem = make(chan struct{}, maxConc)
	}
	if s.m == nil {
		s.m = map[string]*limiter{}
	}
	s.m[key] = l
	return l
}
