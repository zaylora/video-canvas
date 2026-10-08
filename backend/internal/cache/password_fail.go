package cache

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// PasswordFailLimiter 记录“改密码时当前密码输错”的次数，用于防爆破：window 内失败达到 limit 次后锁定 window 时长。
// Redis 启用时存 Redis（多实例共享）；未启用时降级为进程内存（只在单实例下正确）。
type PasswordFailLimiter interface {
	// Locked 返回剩余锁定时长；没有锁定返回 0。
	Locked(ctx context.Context, userID uint64, limit int) (time.Duration, error)
	// Fail 记一次失败并返回当前窗口内的失败次数：第一次失败开始计窗口，达到 limit 次时从这一刻起再锁 window。
	Fail(ctx context.Context, userID uint64, limit int, window time.Duration) (int, error)
	// Reset 清零失败次数（改密码成功后调用）。
	Reset(ctx context.Context, userID uint64) error
}

// NewPasswordFailLimiter 按 Redis 是否启用选择实现：rdb 为 nil 时降级为进程内存。
func NewPasswordFailLimiter(rdb *redis.Client) PasswordFailLimiter {
	if rdb == nil {
		return NewMemoryPasswordFailLimiter(time.Now)
	}
	return &redisPasswordFailLimiter{rdb: rdb}
}

// pwFailKeyPrefix 是失败计数的 key 前缀：pwchg:fail:<uid>。计数与锁定共用一个 key，TTL 就是剩余的窗口 / 锁定时长。
const pwFailKeyPrefix = "pwchg:fail:" //nolint:gosec // G101 误报：这是 Redis key 前缀，不是凭证

func pwFailKey(userID uint64) string { return pwFailKeyPrefix + strconv.FormatUint(userID, 10) }

// ---------------------------------------------------------------------------
// Redis 实现
// ---------------------------------------------------------------------------

type redisPasswordFailLimiter struct {
	rdb *redis.Client
}

// failScript 原子地计数：第一次失败设窗口过期；达到上限时把过期重设为一个完整的锁定时长。
// KEYS[1]=计数 key；ARGV[1]=窗口毫秒；ARGV[2]=上限。返回当前计数。
var failScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 or n == tonumber(ARGV[2]) then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return n
`)

func (l *redisPasswordFailLimiter) Locked(ctx context.Context, userID uint64, limit int) (time.Duration, error) {
	key := pwFailKey(userID)
	n, err := l.rdb.Get(ctx, key).Int()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if n < limit {
		return 0, nil
	}
	ttl, err := l.rdb.PTTL(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if ttl <= 0 { // 刚好过期，或 key 意外没有过期时间：按未锁定处理，下一次失败会重新设置
		return 0, nil
	}
	return ttl, nil
}

func (l *redisPasswordFailLimiter) Fail(ctx context.Context, userID uint64, limit int, window time.Duration) (int, error) {
	return failScript.Run(ctx, l.rdb, []string{pwFailKey(userID)}, window.Milliseconds(), limit).Int()
}

func (l *redisPasswordFailLimiter) Reset(ctx context.Context, userID uint64) error {
	return l.rdb.Del(ctx, pwFailKey(userID)).Err()
}

// ---------------------------------------------------------------------------
// 内存实现
// ---------------------------------------------------------------------------

// memoryPasswordFailLimiter 是进程内存实现，Redis 关闭时的降级方案；过期项在访问时判断并删除。
type memoryPasswordFailLimiter struct {
	mu      sync.Mutex
	entries map[uint64]*memCounter
	now     func() time.Time
}

// NewMemoryPasswordFailLimiter 创建内存版 PasswordFailLimiter；now 可注入以便测试过期。
func NewMemoryPasswordFailLimiter(now func() time.Time) PasswordFailLimiter {
	return &memoryPasswordFailLimiter{entries: map[uint64]*memCounter{}, now: now}
}

// liveLocked 返回未过期的计数项；已过期的顺手删掉。
func (l *memoryPasswordFailLimiter) liveLocked(userID uint64, now time.Time) *memCounter {
	c, ok := l.entries[userID]
	if !ok {
		return nil
	}
	if !now.Before(c.expires) {
		delete(l.entries, userID)
		return nil
	}
	return c
}

func (l *memoryPasswordFailLimiter) Locked(_ context.Context, userID uint64, limit int) (time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	c := l.liveLocked(userID, now)
	if c == nil || c.n < limit {
		return 0, nil
	}
	return c.expires.Sub(now), nil
}

func (l *memoryPasswordFailLimiter) Fail(_ context.Context, userID uint64, limit int, window time.Duration) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	c := l.liveLocked(userID, now)
	if c == nil {
		c = &memCounter{expires: now.Add(window)}
		l.entries[userID] = c
	}
	c.n++
	if c.n == limit {
		c.expires = now.Add(window) // 达到上限：从这一刻起锁一个完整窗口
	}
	return c.n, nil
}

func (l *memoryPasswordFailLimiter) Reset(_ context.Context, userID uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, userID)
	return nil
}
