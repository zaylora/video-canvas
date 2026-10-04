package cache_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	. "video-canvas/internal/cache"
)

var keySeq atomic.Int64

// storeCase 用同一组用例验证内存与 Redis 两个实现；advance 让“时间”前进（内存实现拨动注入的时钟，Redis 用真实 sleep）。
type storeCase struct {
	name    string
	store   RegisterCodeStore
	advance func(d time.Duration)
	// ttl 是测试用的短有效期：内存实现靠时钟前进，Redis 实现要真实等待，所以取得很短
	ttl time.Duration
}

func storeCases(t *testing.T) []storeCase {
	t.Helper()
	now := time.Now()
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	cases := []storeCase{{
		name:    "内存",
		store:   NewMemoryRegisterCodeStore(clock),
		advance: func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() },
		ttl:     time.Minute,
	}}
	if addr := os.Getenv("TEST_REDIS_ADDR"); addr != "" {
		rdb := redis.NewClient(&redis.Options{Addr: addr})
		t.Cleanup(func() { _ = rdb.Close() })
		cases = append(cases, storeCase{
			name:    "Redis",
			store:   NewRegisterCodeStore(rdb),
			advance: func(d time.Duration) { time.Sleep(d) },
			ttl:     300 * time.Millisecond,
		})
	}
	return cases
}

// uniq 给 key 加唯一前缀，避免 Redis 里的残留数据干扰重复运行。
func uniq(s string) string { return fmt.Sprintf("%s-%d-%d", s, time.Now().UnixNano(), keySeq.Add(1)) }

func TestRegisterCodeStore_CheckCode(t *testing.T) {
	ctx := context.Background()
	for _, c := range storeCases(t) {
		t.Run(c.name+"：正确码通过且一次性", func(t *testing.T) {
			email := uniq("a") + "@x.com"
			_ = c.store.Save(ctx, email, "123456", c.ttl)
			if ok, _ := c.store.Check(ctx, email, "123456", 5); !ok {
				t.Fatal("正确验证码应通过")
			}
			if ok, _ := c.store.Check(ctx, email, "123456", 5); ok {
				t.Fatal("验证码用过一次就作废")
			}
		})
		t.Run(c.name+"：错误 5 次后作废，之后正确码也不行", func(t *testing.T) {
			email := uniq("b") + "@x.com"
			_ = c.store.Save(ctx, email, "123456", c.ttl)
			for i := range 4 {
				if ok, _ := c.store.Check(ctx, email, "000000", 5); ok {
					t.Fatalf("第 %d 次错误码不应通过", i+1)
				}
			}
			// 第 5 次错误：作废
			if ok, _ := c.store.Check(ctx, email, "000001", 5); ok {
				t.Fatal("错误码不应通过")
			}
			if ok, _ := c.store.Check(ctx, email, "123456", 5); ok {
				t.Fatal("错误次数用尽后验证码应已作废")
			}
		})
		t.Run(c.name+"：第 4 次错误后正确码仍可用", func(t *testing.T) {
			email := uniq("c") + "@x.com"
			_ = c.store.Save(ctx, email, "123456", c.ttl)
			for range 4 {
				_, _ = c.store.Check(ctx, email, "000000", 5)
			}
			if ok, _ := c.store.Check(ctx, email, "123456", 5); !ok {
				t.Fatal("未用尽次数时正确码应通过")
			}
		})
		t.Run(c.name+"：过期后失效", func(t *testing.T) {
			email := uniq("d") + "@x.com"
			_ = c.store.Save(ctx, email, "123456", c.ttl)
			c.advance(c.ttl + 100*time.Millisecond)
			if ok, _ := c.store.Check(ctx, email, "123456", 5); ok {
				t.Fatal("过期验证码不应通过")
			}
		})
		t.Run(c.name+"：重新发送会覆盖旧码并重置错误次数", func(t *testing.T) {
			email := uniq("e") + "@x.com"
			_ = c.store.Save(ctx, email, "111111", c.ttl)
			for range 4 {
				_, _ = c.store.Check(ctx, email, "000000", 5)
			}
			_ = c.store.Save(ctx, email, "222222", c.ttl)
			if ok, _ := c.store.Check(ctx, email, "111111", 5); ok {
				t.Fatal("旧码应失效")
			}
			if ok, _ := c.store.Check(ctx, email, "222222", 5); !ok {
				t.Fatal("新码应通过（错误次数已重置；旧码那次错误计 1 次）")
			}
		})
		t.Run(c.name+"：没有发过码返回 false", func(t *testing.T) {
			if ok, err := c.store.Check(ctx, uniq("none")+"@x.com", "123456", 5); ok || err != nil {
				t.Fatalf("ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestRegisterCodeStore_TryLockAndAllow(t *testing.T) {
	ctx := context.Background()
	for _, c := range storeCases(t) {
		t.Run(c.name+"：TryLock 冷却期内只有第一次成功，过期后可再次获取", func(t *testing.T) {
			k := uniq("cool")
			if ok, _ := c.store.TryLock(ctx, k, c.ttl); !ok {
				t.Fatal("第一次应成功")
			}
			if ok, _ := c.store.TryLock(ctx, k, c.ttl); ok {
				t.Fatal("冷却期内第二次应失败")
			}
			c.advance(c.ttl + 100*time.Millisecond)
			if ok, _ := c.store.TryLock(ctx, k, c.ttl); !ok {
				t.Fatal("冷却结束后应可再次获取")
			}
		})
		t.Run(c.name+"：Allow 固定窗口限频", func(t *testing.T) {
			k := uniq("ip")
			for i := range 3 {
				if ok, _ := c.store.Allow(ctx, k, 3, c.ttl); !ok {
					t.Fatalf("第 %d 次应放行", i+1)
				}
			}
			if ok, _ := c.store.Allow(ctx, k, 3, c.ttl); ok {
				t.Fatal("超过上限应拒绝")
			}
			c.advance(c.ttl + 100*time.Millisecond)
			if ok, _ := c.store.Allow(ctx, k, 3, c.ttl); !ok {
				t.Fatal("窗口过后应重新放行")
			}
		})
		t.Run(c.name+"：并发 TryLock 只有一个成功", func(t *testing.T) {
			k := uniq("race")
			var wg sync.WaitGroup
			var okN atomic.Int32
			for range 20 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if ok, _ := c.store.TryLock(ctx, k, c.ttl); ok {
						okN.Add(1)
					}
				}()
			}
			wg.Wait()
			if okN.Load() != 1 {
				t.Fatalf("应只有 1 个成功，实际 %d", okN.Load())
			}
		})
	}
}
