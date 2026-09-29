package ws

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"video-canvas/internal/pkg/logger"
)

// TicketTTL 是 ticket 的有效期。ticket 只用来换一次 WebSocket 连接，所以要短。
const TicketTTL = 30 * time.Second

const (
	ticketBytes     = 32               // 随机字节数，base64url 编码后 43 个字符
	ticketKeyPrefix = "ws:ticket:"     // Redis key 前缀
	memSweepEvery   = 10 * time.Second // 内存实现里，最多每隔这么久顺带清理一次过期项
)

// TicketStore 管理 WebSocket 一次性连接凭证：
// 浏览器的 WebSocket 不能带 Authorization 头，又不想把长期 JWT 放进 URL（会进访问日志），
// 所以先用 JWT 换一个 30 秒有效、只能用一次的 ticket，再用 ticket 建连。
type TicketStore interface {
	// Issue 为用户签发一个 ticket。
	Issue(ctx context.Context, userID uint64) (ticket string, err error)
	// Consume 校验并消费 ticket：一次性，取出即删；不存在、已用过、已过期都返回 ok=false。
	Consume(ctx context.Context, ticket string) (userID uint64, ok bool)
}

// NewTicketStore 按 Redis 是否启用选择实现：rdb 为 nil（Redis 关闭）时降级为进程内存。
// 内存实现只在单实例下正确；多实例部署必须开启 Redis。
func NewTicketStore(rdb *redis.Client) TicketStore {
	if rdb == nil {
		return NewMemoryTicketStore()
	}
	return NewRedisTicketStore(rdb)
}

// newTicket 生成 32 字节随机数的 base64url（无填充）字符串。
func newTicket() (string, error) {
	b := make([]byte, ticketBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ---------------------------------------------------------------------------
// Redis 实现
// ---------------------------------------------------------------------------

// RedisTicketStore 把 ticket 存在 Redis，key 带 TTL，天然过期。
type RedisTicketStore struct {
	rdb *redis.Client
}

// NewRedisTicketStore 创建 Redis 版 TicketStore。
func NewRedisTicketStore(rdb *redis.Client) *RedisTicketStore {
	return &RedisTicketStore{rdb: rdb}
}

// consumeScript 原子地“读取并删除”：用 Lua 而不是 GETDEL，是为了兼容 Redis 6.2 以下的版本。
// 两个并发请求拿同一个 ticket，只有一个能读到值。
var consumeScript = redis.NewScript(`
local v = redis.call('GET', KEYS[1])
if v then redis.call('DEL', KEYS[1]) end
return v
`)

func (s *RedisTicketStore) Issue(ctx context.Context, userID uint64) (string, error) {
	t, err := newTicket()
	if err != nil {
		return "", err
	}
	if err := s.rdb.Set(ctx, ticketKeyPrefix+t, strconv.FormatUint(userID, 10), TicketTTL).Err(); err != nil {
		return "", err
	}
	return t, nil
}

func (s *RedisTicketStore) Consume(ctx context.Context, ticket string) (uint64, bool) {
	if ticket == "" {
		return 0, false
	}
	v, err := consumeScript.Run(ctx, s.rdb, []string{ticketKeyPrefix + ticket}).Text()
	if err != nil {
		// redis.Nil 表示不存在/已过期/已被消费，属于正常的“无效 ticket”；其他错误记日志，同样按无效处理（不放行）
		if !errors.Is(err, redis.Nil) {
			logger.Warn("Redis 消费 WebSocket ticket 失败", zap.Error(err))
		}
		return 0, false
	}
	uid, err := strconv.ParseUint(v, 10, 64)
	if err != nil || uid == 0 {
		return 0, false
	}
	return uid, true
}

// ---------------------------------------------------------------------------
// 内存实现
// ---------------------------------------------------------------------------

type memTicket struct {
	userID  uint64
	expires time.Time
}

// MemoryTicketStore 是进程内存实现，Redis 关闭时的降级方案。
// 不起后台 goroutine：过期项在 Issue/Consume 时顺带清理，避免额外的生命周期管理。
type MemoryTicketStore struct {
	mu        sync.Mutex
	items     map[string]memTicket
	lastSweep time.Time
	now       func() time.Time // 可注入，便于测试过期
}

// NewMemoryTicketStore 创建内存版 TicketStore。
func NewMemoryTicketStore() *MemoryTicketStore {
	return &MemoryTicketStore{items: make(map[string]memTicket), now: time.Now}
}

func (s *MemoryTicketStore) Issue(_ context.Context, userID uint64) (string, error) {
	t, err := newTicket()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)
	s.items[t] = memTicket{userID: userID, expires: now.Add(TicketTTL)}
	return t, nil
}

func (s *MemoryTicketStore) Consume(_ context.Context, ticket string) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)
	it, ok := s.items[ticket]
	if !ok {
		return 0, false
	}
	delete(s.items, ticket) // 取出即删：无论是否过期都不能再用
	if !now.Before(it.expires) {
		return 0, false
	}
	return it.userID, true
}

// sweepLocked 清理过期项；限频，避免每次调用都全表扫描。
func (s *MemoryTicketStore) sweepLocked(now time.Time) {
	if now.Sub(s.lastSweep) < memSweepEvery {
		return
	}
	s.lastSweep = now
	for k, it := range s.items {
		if !now.Before(it.expires) {
			delete(s.items, k)
		}
	}
}

// size 返回当前存的 ticket 数（测试用）。
func (s *MemoryTicketStore) size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}
