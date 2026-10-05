package cache

import (
	"context"
	"crypto/subtle"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RegisterCodeStore 保存注册邮箱验证码，并提供发码限频所需的两个原语。
// Redis 启用时存 Redis（多实例共享）；未启用时降级为进程内存（只在单实例下正确）。
type RegisterCodeStore interface {
	// Save 保存（覆盖）邮箱的验证码，ttl 后过期，并把错误次数清零。email 由调用方统一成小写。
	Save(ctx context.Context, email, code string, ttl time.Duration) error
	// Check 原子地校验验证码：正确返回 true 并立即作废（一次性）；错误累计次数，达到 maxAttempts 次后作废；
	// 不存在 / 已过期 / 已作废返回 false。
	Check(ctx context.Context, email, code string, maxAttempts int) (bool, error)
	// TryLock 尝试占住 key 一段时间（SET NX）：成功返回 true；ttl 内再次尝试返回 false。用于“同邮箱 60 秒冷却”。
	TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	// Allow 固定窗口限频：window 内第 1..limit 次返回 true，之后返回 false。用于“同 IP 每小时 20 次”。
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

// NewRegisterCodeStore 按 Redis 是否启用选择实现：rdb 为 nil 时降级为进程内存。
func NewRegisterCodeStore(rdb *redis.Client) RegisterCodeStore {
	if rdb == nil {
		return NewMemoryRegisterCodeStore(time.Now)
	}
	return &redisRegisterCodeStore{rdb: rdb}
}

const (
	regCodeKeyPrefix = "reg:code:" // 验证码（hash：code / attempts）
	regLockKeyPrefix = "reg:lock:" // 冷却锁
	regRateKeyPrefix = "reg:rate:" // 限频计数
)

// ---------------------------------------------------------------------------
// Redis 实现
// ---------------------------------------------------------------------------

type redisRegisterCodeStore struct {
	rdb *redis.Client
}

// checkCodeScript 原子地“比对 + 计数 + 作废”：并发的多次尝试不会绕过次数上限。
// KEYS[1]=验证码 key；ARGV[1]=用户输入；ARGV[2]=最大错误次数。返回 1 表示通过。
var checkCodeScript = redis.NewScript(`
local c = redis.call('HGET', KEYS[1], 'code')
if not c then return 0 end
if c == ARGV[1] then
  redis.call('DEL', KEYS[1])
  return 1
end
local n = redis.call('HINCRBY', KEYS[1], 'attempts', 1)
if n >= tonumber(ARGV[2]) then redis.call('DEL', KEYS[1]) end
return 0
`)

// allowScript 固定窗口计数：第一次 INCR 时设置过期，之后只递增；返回当前计数。
var allowScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return n
`)

func (s *redisRegisterCodeStore) Save(ctx context.Context, email, code string, ttl time.Duration) error {
	key := regCodeKeyPrefix + email
	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, key)
	pipe.HSet(ctx, key, "code", code, "attempts", 0)
	pipe.PExpire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *redisRegisterCodeStore) Check(ctx context.Context, email, code string, maxAttempts int) (bool, error) {
	n, err := checkCodeScript.Run(ctx, s.rdb, []string{regCodeKeyPrefix + email}, code, maxAttempts).Int()
	if err != nil && !errors.Is(err, redis.Nil) {
		return false, err
	}
	return n == 1, nil
}

func (s *redisRegisterCodeStore) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return s.rdb.SetNX(ctx, regLockKeyPrefix+key, 1, ttl).Result()
}

func (s *redisRegisterCodeStore) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	n, err := allowScript.Run(ctx, s.rdb, []string{regRateKeyPrefix + key}, window.Milliseconds()).Int()
	if err != nil {
		return false, err
	}
	return n <= limit, nil
}

// ---------------------------------------------------------------------------
// 内存实现
// ---------------------------------------------------------------------------

type memCode struct {
	code     string
	attempts int
	expires  time.Time
}

type memCounter struct {
	n       int
	expires time.Time
}

// memoryRegisterCodeStore 是进程内存实现，Redis 关闭时的降级方案。
// 不起后台 goroutine：过期项在访问时判断，并限频地顺带清理，避免额外的生命周期管理。
type memoryRegisterCodeStore struct {
	mu        sync.Mutex
	codes     map[string]*memCode
	locks     map[string]time.Time // key -> 过期时间
	counters  map[string]*memCounter
	now       func() time.Time
	lastSweep time.Time
}

// NewMemoryRegisterCodeStore 创建内存版 RegisterCodeStore；now 可注入以便测试过期。
func NewMemoryRegisterCodeStore(now func() time.Time) RegisterCodeStore {
	return &memoryRegisterCodeStore{
		codes: map[string]*memCode{}, locks: map[string]time.Time{}, counters: map[string]*memCounter{}, now: now,
	}
}

// sweepLocked 清理过期项，最多每分钟一次。
func (s *memoryRegisterCodeStore) sweepLocked(now time.Time) {
	if now.Sub(s.lastSweep) < time.Minute {
		return
	}
	s.lastSweep = now
	for k, c := range s.codes {
		if !now.Before(c.expires) {
			delete(s.codes, k)
		}
	}
	for k, exp := range s.locks {
		if !now.Before(exp) {
			delete(s.locks, k)
		}
	}
	for k, c := range s.counters {
		if !now.Before(c.expires) {
			delete(s.counters, k)
		}
	}
}

func (s *memoryRegisterCodeStore) Save(_ context.Context, email, code string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)
	s.codes[email] = &memCode{code: code, expires: now.Add(ttl)}
	return nil
}

func (s *memoryRegisterCodeStore) Check(_ context.Context, email, code string, maxAttempts int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)
	c, ok := s.codes[email]
	if !ok || !now.Before(c.expires) {
		delete(s.codes, email)
		return false, nil
	}
	if subtle.ConstantTimeCompare([]byte(c.code), []byte(code)) == 1 {
		delete(s.codes, email)
		return true, nil
	}
	c.attempts++
	if c.attempts >= maxAttempts {
		delete(s.codes, email)
	}
	return false, nil
}

func (s *memoryRegisterCodeStore) TryLock(_ context.Context, key string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)
	if exp, ok := s.locks[key]; ok && now.Before(exp) {
		return false, nil
	}
	s.locks[key] = now.Add(ttl)
	return true, nil
}

func (s *memoryRegisterCodeStore) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)
	c, ok := s.counters[key]
	if !ok || !now.Before(c.expires) {
		c = &memCounter{expires: now.Add(window)}
		s.counters[key] = c
	}
	c.n++
	return c.n <= limit, nil
}
