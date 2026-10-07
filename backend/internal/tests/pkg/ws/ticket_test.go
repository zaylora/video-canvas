package ws_test

import (
	"context"
	"encoding/base64"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "video-canvas/internal/pkg/ws"

	"github.com/redis/go-redis/v9"
)

// ticketStoreCases 是两种实现共用的行为用例，保证 Redis 和内存实现语义一致。
func runTicketStoreCommonTests(t *testing.T, newStore func(t *testing.T) TicketStore) {
	ctx := context.Background()

	t.Run("签发后可消费且只能消费一次", func(t *testing.T) {
		s := newStore(t)
		tk, err := s.Issue(ctx, 42)
		if err != nil {
			t.Fatalf("签发失败：%v", err)
		}
		uid, ok := s.Consume(ctx, tk)
		if !ok || uid != 42 {
			t.Fatalf("首次消费应成功并返回 42，实际 uid=%d ok=%v", uid, ok)
		}
		if _, ok := s.Consume(ctx, tk); ok {
			t.Fatal("ticket 只能用一次，第二次消费应失败")
		}
	})

	t.Run("ticket 是 32 字节随机数的 base64url 且互不相同", func(t *testing.T) {
		s := newStore(t)
		seen := map[string]bool{}
		for i := 0; i < 20; i++ {
			tk, err := s.Issue(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := base64.RawURLEncoding.DecodeString(tk)
			if err != nil || len(raw) != 32 {
				t.Fatalf("ticket 应是 32 字节的 base64url，实际 %q（解码长度 %d，err=%v）", tk, len(raw), err)
			}
			if seen[tk] {
				t.Fatalf("ticket 重复：%s", tk)
			}
			seen[tk] = true
		}
	})

	t.Run("不存在或空 ticket 无效", func(t *testing.T) {
		s := newStore(t)
		for _, tk := range []string{"", "not-exist", "../../etc"} {
			if _, ok := s.Consume(ctx, tk); ok {
				t.Fatalf("ticket %q 不应有效", tk)
			}
		}
	})

	t.Run("并发消费同一个 ticket 只有一个成功", func(t *testing.T) {
		s := newStore(t)
		tk, err := s.Issue(ctx, 9)
		if err != nil {
			t.Fatal(err)
		}
		var wins int32
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, ok := s.Consume(ctx, tk); ok {
					atomic.AddInt32(&wins, 1)
				}
			}()
		}
		wg.Wait()
		if wins != 1 {
			t.Fatalf("并发消费应恰好 1 个成功，实际 %d", wins)
		}
	})
}

func TestMemoryTicketStore(t *testing.T) {
	runTicketStoreCommonTests(t, func(t *testing.T) TicketStore { return NewMemoryTicketStore() })
}

func TestMemoryTicketStore_过期(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryTicketStore()
	now := time.Now()
	s.SetNow(func() time.Time { return now })

	fresh, _ := s.Issue(ctx, 1)
	stale, _ := s.Issue(ctx, 2)

	// 29 秒时仍有效
	now = now.Add(29 * time.Second)
	if uid, ok := s.Consume(ctx, fresh); !ok || uid != 1 {
		t.Fatalf("29 秒内应有效，实际 uid=%d ok=%v", uid, ok)
	}
	// 30 秒时过期
	now = now.Add(time.Second)
	if _, ok := s.Consume(ctx, stale); ok {
		t.Fatal("满 30 秒的 ticket 应已过期")
	}
	if n := s.Size(); n != 0 {
		t.Fatalf("过期 ticket 消费后应被删除，剩余 %d", n)
	}
}

func TestMemoryTicketStore_过期项会被顺带清理(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryTicketStore()
	now := time.Now()
	s.SetNow(func() time.Time { return now })

	for i := 0; i < 10; i++ {
		_, _ = s.Issue(ctx, uint64(i+1))
	}
	if s.Size() != 10 {
		t.Fatalf("应有 10 个 ticket，实际 %d", s.Size())
	}
	// 时间前进超过 TTL 和清理间隔后，下一次 Issue 会清掉旧的
	now = now.Add(time.Minute)
	_, _ = s.Issue(ctx, 99)
	if n := s.Size(); n != 1 {
		t.Fatalf("过期项应被清理，只剩新签发的 1 个，实际 %d", n)
	}
}

func TestNewTicketStore_rdb为nil时降级为内存(t *testing.T) {
	if _, ok := NewTicketStore(nil).(*MemoryTicketStore); !ok {
		t.Fatal("rdb 为 nil 时应使用内存实现")
	}
}

// testRedis 连本机 Redis，连不上就跳过。
func testRedis(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DialTimeout: 500 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		t.Skipf("连不上 Redis（127.0.0.1:6379），跳过：%v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func TestRedisTicketStore(t *testing.T) {
	rdb := testRedis(t)
	runTicketStoreCommonTests(t, func(t *testing.T) TicketStore { return NewRedisTicketStore(rdb) })
}

func TestRedisTicketStore_TTL与过期(t *testing.T) {
	rdb := testRedis(t)
	ctx := context.Background()
	s := NewRedisTicketStore(rdb)

	tk, err := s.Issue(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	ttl, err := rdb.TTL(ctx, TicketKeyPrefix+tk).Result()
	if err != nil || ttl <= 0 || ttl > TicketTTL {
		t.Fatalf("key 应带不超过 %v 的 TTL，实际 ttl=%v err=%v", TicketTTL, ttl, err)
	}

	// 模拟过期：直接删 key（等价于 TTL 到期）
	rdb.Del(ctx, TicketKeyPrefix+tk)
	if _, ok := s.Consume(ctx, tk); ok {
		t.Fatal("过期（key 不存在）的 ticket 应无效")
	}
}

func TestRedisTicketStore_Redis不可用时按无效处理(t *testing.T) {
	// 指向一个不存在的端口：Consume 不能 panic，也不能放行
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: -1})
	defer rdb.Close()
	s := NewRedisTicketStore(rdb)
	if _, ok := s.Consume(context.Background(), "abc"); ok {
		t.Fatal("Redis 不可用时不应放行")
	}
	if _, err := s.Issue(context.Background(), 1); err == nil {
		t.Fatal("Redis 不可用时 Issue 应返回错误")
	}
}
