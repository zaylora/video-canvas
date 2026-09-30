package plugin

import (
	"context"
	"math"
	"sync"
	"time"
)

// 限流：每个渠道一个令牌桶（rps）+ 并发上限（max_concurrency），手写实现，不依赖 golang.org/x/time。

// tokenBucket 是令牌桶：每秒补充 rate 个令牌，最多存 burst 个。零值 rate 表示不限速。
type tokenBucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newTokenBucket(rps float64, now time.Time) *tokenBucket {
	burst := math.Max(1, math.Ceil(rps))
	return &tokenBucket{rate: rps, burst: burst, tokens: burst, last: now}
}

// take 尝试在 now 时刻取一个令牌。成功返回 0；失败返回需要等待多久之后再试。
// 把时间作为参数传入，是为了让单元测试不用真的 sleep。
func (b *tokenBucket) take(now time.Time) time.Duration {
	if b.rate <= 0 {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.tokens = math.Min(b.burst, b.tokens+elapsed.Seconds()*b.rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return 0
	}
	need := (1 - b.tokens) / b.rate
	return time.Duration(need * float64(time.Second))
}

// wait 阻塞直到取到令牌或 ctx 结束。
func (b *tokenBucket) wait(ctx context.Context) error {
	for {
		d := b.take(time.Now())
		if d <= 0 {
			return nil
		}
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// channelLimiter 是一个渠道的限流器：令牌桶 + 并发信号量。
type channelLimiter struct {
	rps     float64
	maxConc int
	bucket  *tokenBucket
	sem     chan struct{} // nil 表示不限并发
}

func newChannelLimiter(rps float64, maxConc int) *channelLimiter {
	l := &channelLimiter{rps: rps, maxConc: maxConc, bucket: newTokenBucket(rps, time.Now())}
	if maxConc > 0 {
		l.sem = make(chan struct{}, maxConc)
	}
	return l
}

// acquire 先取并发名额再取令牌（这样排队等名额时不会白白消耗令牌），返回释放函数。
func (l *channelLimiter) acquire(ctx context.Context) (release func(), err error) {
	if l.sem != nil {
		select {
		case l.sem <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	release = func() {
		if l.sem != nil {
			<-l.sem
		}
	}
	if err := l.bucket.wait(ctx); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// limiterSet 按渠道 key 缓存限流器。渠道的限流参数改变后，下一次调用会换成新的限流器
// （旧限流器上已占用的名额随请求结束自然释放，不会影响新限流器的计数）。
type limiterSet struct {
	mu sync.Mutex
	m  map[string]*channelLimiter
}

func newLimiterSet() *limiterSet { return &limiterSet{m: map[string]*channelLimiter{}} }

func (s *limiterSet) get(key string, rps float64, maxConc int) *channelLimiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.m[key]; ok && l.rps == rps && l.maxConc == maxConc {
		return l
	}
	l := newChannelLimiter(rps, maxConc)
	s.m[key] = l
	return l
}
