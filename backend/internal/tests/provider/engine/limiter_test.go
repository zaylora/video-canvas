package engine_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	. "video-canvas/internal/provider/engine"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
)

func TestTokenBucket(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("突发用完后按速率补充", func(t *testing.T) {
		b := NewTokenBucket(2, t0) // 每秒 2 个，突发 2 个
		if d := b.Take(t0); d != 0 {
			t.Fatalf("第 1 个应立即通过：%v", d)
		}
		if d := b.Take(t0); d != 0 {
			t.Fatalf("第 2 个应立即通过：%v", d)
		}
		d := b.Take(t0)
		if d < 400*time.Millisecond || d > 600*time.Millisecond {
			t.Fatalf("第 3 个应等约 500ms：%v", d)
		}
		// 等了 500ms 后可以通过
		if d := b.Take(t0.Add(500 * time.Millisecond)); d != 0 {
			t.Fatalf("500ms 后应通过：%v", d)
		}
		if d := b.Take(t0.Add(500 * time.Millisecond)); d == 0 {
			t.Fatal("紧接着的下一个应被限流")
		}
	})

	t.Run("令牌不会超过突发上限", func(t *testing.T) {
		b := NewTokenBucket(5, t0)
		for i := 0; i < 5; i++ {
			b.Take(t0)
		}
		later := t0.Add(time.Hour) // 很久之后也只能突发 5 个
		for i := 0; i < 5; i++ {
			if d := b.Take(later); d != 0 {
				t.Fatalf("第 %d 个应通过：%v", i+1, d)
			}
		}
		if d := b.Take(later); d == 0 {
			t.Fatal("第 6 个应被限流")
		}
	})

	t.Run("rps 为 0 不限速", func(t *testing.T) {
		b := NewTokenBucket(0, t0)
		for i := 0; i < 1000; i++ {
			if d := b.Take(t0); d != 0 {
				t.Fatal("不应限速")
			}
		}
	})

	t.Run("小于 1 的速率也至少允许 1 个突发", func(t *testing.T) {
		b := NewTokenBucket(0.5, t0)
		if d := b.Take(t0); d != 0 {
			t.Fatal("第 1 个应通过")
		}
		if d := b.Take(t0); d < time.Second {
			t.Fatalf("第 2 个应等约 2 秒：%v", d)
		}
	})
}

func TestTokenBucket_wait真实等待(t *testing.T) {
	b := NewTokenBucket(20, time.Now()) // 突发 20，之后每 50ms 一个
	for i := 0; i < 20; i++ {
		if err := b.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	if err := b.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el < 20*time.Millisecond || el > time.Second {
		t.Fatalf("等待时间异常：%v", el)
	}
}

func TestProviderLimiter_ctx取消(t *testing.T) {
	t.Run("等待并发名额时取消", func(t *testing.T) {
		l := NewProviderLimiter(0, 1)
		rel, err := l.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer rel()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := l.Acquire(ctx); err == nil {
			t.Fatal("名额被占满时应等待到 ctx 超时")
		}
	})
	t.Run("等待令牌时取消且归还并发名额", func(t *testing.T) {
		l := NewProviderLimiter(0.1, 2) // 突发 1，之后每 10 秒 1 个
		rel, err := l.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		rel()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := l.Acquire(ctx); err == nil {
			t.Fatal("令牌不足应等到 ctx 超时")
		}
		if l.InFlight() != 0 {
			t.Fatalf("取消后应归还并发名额，当前占用 %d", l.InFlight())
		}
	})
}

func TestLimiterSet_按平台缓存且参数变化时重建(t *testing.T) {
	s := NewLimiterSet()
	a := s.Get("p1", 5, 2)
	if s.Get("p1", 5, 2) != a {
		t.Fatal("相同平台相同参数应复用限流器")
	}
	if s.Get("p2", 5, 2) == a {
		t.Fatal("不同平台应各自限流")
	}
	if s.Get("p1", 10, 2) == a {
		t.Fatal("rps 变化后应重建")
	}
	b := s.Get("p1", 10, 2)
	if s.Get("p1", 10, 4) == b {
		t.Fatal("并发上限变化后应重建")
	}
}

func TestEngine_并发上限(t *testing.T) {
	rh := engNewRH(t)
	var inflight, maxSeen atomic.Int32
	rh.onQuery = func(w http.ResponseWriter, r *http.Request) {
		n := inflight.Add(1)
		for {
			m := maxSeen.Load()
			if n <= m || maxSeen.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(60 * time.Millisecond)
		inflight.Add(-1)
		engJSON(w, 200, map[string]any{"status": "RUNNING"})
	}
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.RateLimit = dsl.RateLimitConfig{RPS: 0, MaxConcurrency: 2}
	ex := engNew(nil, engDefaultSecrets(), nil)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ex.Query(context.Background(), snap, provider.TaskRef{ProviderTaskID: "T"}); err != nil {
				t.Errorf("查询失败：%v", err)
			}
		}()
	}
	wg.Wait()
	if got := maxSeen.Load(); got > 2 || got < 1 {
		t.Fatalf("最大并发应不超过 2，实际 %d", got)
	}
}

func TestEngine_按平台隔离限流(t *testing.T) {
	rh := engNewRH(t)
	var inflight, maxSeen atomic.Int32
	rh.onQuery = func(w http.ResponseWriter, r *http.Request) {
		n := inflight.Add(1)
		for {
			m := maxSeen.Load()
			if n <= m || maxSeen.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(80 * time.Millisecond)
		inflight.Add(-1)
		engJSON(w, 200, map[string]any{"status": "RUNNING"})
	}
	snapA := engSnapshot(t, rh.srv.URL)
	snapA.Provider.Key = "a"
	snapA.Provider.RateLimit = dsl.RateLimitConfig{MaxConcurrency: 1}
	snapB := engSnapshot(t, rh.srv.URL)
	snapB.Provider.Key = "b"
	snapB.Provider.RateLimit = dsl.RateLimitConfig{MaxConcurrency: 1}
	ex := engNew(nil, engDefaultSecrets(), nil)

	var wg sync.WaitGroup
	for _, s := range []*dsl.Snapshot{snapA, snapB} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = ex.Query(context.Background(), s, provider.TaskRef{ProviderTaskID: "T"})
		}()
	}
	wg.Wait()
	if maxSeen.Load() != 2 {
		t.Fatalf("两个平台各有 1 个名额，应能同时进行 2 个请求，实际最大并发 %d", maxSeen.Load())
	}
}

func TestEngine_限流等待受ctx约束(t *testing.T) {
	rh := engNewRH(t)
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.RateLimit = dsl.RateLimitConfig{RPS: 0.1} // 突发 1，之后每 10 秒 1 个
	ex := engNew(nil, engDefaultSecrets(), nil)
	if _, err := ex.Query(context.Background(), snap, provider.TaskRef{ProviderTaskID: "T"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ex.Query(ctx, snap, provider.TaskRef{ProviderTaskID: "T"})
	if engClass(t, err) != provider.ClassRetryable {
		t.Fatalf("限流等待被取消应为 retryable：%v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("应在 ctx 到期后立即返回")
	}
}
