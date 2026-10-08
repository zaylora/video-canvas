package cache_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	. "video-canvas/internal/cache"
)

// 本文件用同一组用例验证改密码失败计数器的内存与 Redis 两个实现。

type limiterCase struct {
	name    string
	lim     PasswordFailLimiter
	advance func(d time.Duration)
	window  time.Duration
}

func limiterCases(t *testing.T) []limiterCase {
	t.Helper()
	now := time.Now()
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	cases := []limiterCase{{
		name:    "内存",
		lim:     NewMemoryPasswordFailLimiter(clock),
		advance: func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() },
		window:  15 * time.Minute,
	}}
	if addr := os.Getenv("TEST_REDIS_ADDR"); addr != "" {
		rdb := redis.NewClient(&redis.Options{Addr: addr})
		t.Cleanup(func() { _ = rdb.Close() })
		cases = append(cases, limiterCase{
			name:    "Redis",
			lim:     NewPasswordFailLimiter(rdb),
			advance: func(d time.Duration) { time.Sleep(d) }, // Redis 只能真实等待过期
			window:  400 * time.Millisecond,
		})
	}
	return cases
}

// uid 生成不会和其他用例 / 残留数据冲突的用户 id。
func uid() uint64 { return uint64(time.Now().UnixNano()) + uint64(keySeq.Add(1)) }

func TestPasswordFailLimiter(t *testing.T) {
	ctx := context.Background()
	for _, c := range limiterCases(t) {
		t.Run(c.name+"：计数递增，达到上限后锁定一个窗口，过期后解锁", func(t *testing.T) {
			id := uid()
			for i := 1; i <= 5; i++ {
				if left, _ := c.lim.Locked(ctx, id, 5); left != 0 {
					t.Fatalf("第 %d 次前不应锁定", i)
				}
				n, err := c.lim.Fail(ctx, id, 5, c.window)
				if err != nil || n != i {
					t.Fatalf("第 %d 次计数 = %d，%v", i, n, err)
				}
			}
			left, err := c.lim.Locked(ctx, id, 5)
			if err != nil || left <= 0 || left > c.window {
				t.Fatalf("应锁定且剩余时间在窗口内：%v %v", left, err)
			}
			c.advance(c.window + 100*time.Millisecond)
			if left, _ := c.lim.Locked(ctx, id, 5); left != 0 {
				t.Fatalf("过期后应解锁：%v", left)
			}
			if n, _ := c.lim.Fail(ctx, id, 5, c.window); n != 1 {
				t.Fatalf("过期后重新计数：%d", n)
			}
		})
		t.Run(c.name+"：Reset 清零", func(t *testing.T) {
			id := uid()
			_, _ = c.lim.Fail(ctx, id, 5, c.window)
			_, _ = c.lim.Fail(ctx, id, 5, c.window)
			if err := c.lim.Reset(ctx, id); err != nil {
				t.Fatal(err)
			}
			if n, _ := c.lim.Fail(ctx, id, 5, c.window); n != 1 {
				t.Fatalf("Reset 后应从 1 开始：%d", n)
			}
		})
		t.Run(c.name+"：不同用户互不影响", func(t *testing.T) {
			a, b := uid(), uid()
			for range 5 {
				_, _ = c.lim.Fail(ctx, a, 5, c.window)
			}
			if left, _ := c.lim.Locked(ctx, b, 5); left != 0 {
				t.Fatal("别人的失败不应锁住我")
			}
		})
	}
}
