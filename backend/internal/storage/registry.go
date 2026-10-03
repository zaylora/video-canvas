package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"video-canvas/internal/config"
)

// ErrStorageNotFound 存储配置不存在（已被删除，或 id 不对）。
var ErrStorageNotFound = errors.New("storage: 存储配置不存在")

// Entry 是一套存储的运行时配置（密钥已解密），由 Source 从数据库读出。
type Entry struct {
	ID           uint64
	Version      int // 配置每改一次加 1；Registry 靠它判断缓存的客户端是否过期
	Provider     string
	Local        config.LocalStorage // Provider 为 local 时使用
	Spec         Spec                // 对象存储使用
	SignedTTL    time.Duration       // 私有桶签名地址的有效期
	DirectUpload bool                // 管理员是否允许浏览器直传
}

// Source 是存储配置的来源，由 service 层基于 repository 实现。
type Source interface {
	// DefaultID 返回当前默认存储的 id。
	DefaultID(ctx context.Context) (uint64, error)
	// Entry 返回一套存储的运行时配置，不存在返回 ErrStorageNotFound。
	Entry(ctx context.Context, id uint64) (Entry, error)
}

// Handle 是一套存储的可用句柄：客户端 + 创建它时用到的策略。
type Handle struct {
	ID           uint64
	Version      int
	Provider     string
	Storage      Storage
	SignedTTL    time.Duration
	DirectUpload bool // 管理员允许直传，且这套存储实现了直传
}

// Registry 按 id 解析存储并缓存客户端。素材写入用 Default，读取、签名、删除用 Get（按素材记录的 storage_id）。
//
// 缓存策略：每项缓存 ttl；到期后重新读取配置，版本没变就复用客户端，变了才重建。
// 本实例改了配置会调用 Invalidate 立刻生效；多实例部署时，其他实例最多 ttl 后生效。
type Registry struct {
	src Source
	ttl time.Duration
	now func() time.Time

	mu         sync.Mutex
	items      map[uint64]cached
	defaultID  uint64
	defaultExp time.Time
}

type cached struct {
	h       *Handle
	expires time.Time
}

// NewRegistry 创建 Registry，ttl 为 0 时用 30 秒。
func NewRegistry(src Source, ttl time.Duration) *Registry {
	return NewRegistryWithClock(src, ttl, time.Now)
}

// NewRegistryWithClock 同 NewRegistry，可注入时钟（测试用）。
func NewRegistryWithClock(src Source, ttl time.Duration, now func() time.Time) *Registry {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &Registry{src: src, ttl: ttl, now: now, items: map[uint64]cached{}}
}

// Default 返回当前默认存储。
func (r *Registry) Default(ctx context.Context) (*Handle, error) {
	r.mu.Lock()
	id, exp := r.defaultID, r.defaultExp
	r.mu.Unlock()

	if id == 0 || !r.now().Before(exp) {
		var err error
		if id, err = r.src.DefaultID(ctx); err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.defaultID, r.defaultExp = id, r.now().Add(r.ttl)
		r.mu.Unlock()
	}
	return r.Get(ctx, id)
}

// Get 按 id 返回存储；不存在返回 ErrStorageNotFound。
func (r *Registry) Get(ctx context.Context, id uint64) (*Handle, error) {
	r.mu.Lock()
	c, ok := r.items[id]
	r.mu.Unlock()
	if ok && r.now().Before(c.expires) {
		return c.h, nil
	}

	e, err := r.src.Entry(ctx, id)
	if err != nil {
		return nil, err
	}
	// 版本没变就复用客户端，只续期
	if ok && c.h.Version == e.Version {
		r.store(id, c.h)
		return c.h, nil
	}
	h, err := build(e)
	if err != nil {
		return nil, err
	}
	r.store(id, h)
	return h, nil
}

func (r *Registry) store(id uint64, h *Handle) {
	r.mu.Lock()
	r.items[id] = cached{h: h, expires: r.now().Add(r.ttl)}
	r.mu.Unlock()
}

// Invalidate 让缓存失效，下一次读取会重新查配置；id 为 0 表示全部（含默认存储指针）。
func (r *Registry) Invalidate(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id == 0 {
		r.items = map[uint64]cached{}
	} else {
		delete(r.items, id)
	}
	r.defaultID, r.defaultExp = 0, time.Time{}
}

// build 按配置创建存储实现。
func build(e Entry) (*Handle, error) {
	h := &Handle{ID: e.ID, Version: e.Version, Provider: e.Provider, SignedTTL: e.SignedTTL}
	if e.Provider == ProviderLocal {
		st, err := NewLocal(e.Local)
		if err != nil {
			return nil, err
		}
		h.Storage = st
		return h, nil
	}
	st, err := NewFromSpec(e.Spec)
	if err != nil {
		return nil, fmt.Errorf("存储 %d 配置有误: %w", e.ID, err)
	}
	h.Storage = st
	h.DirectUpload = e.DirectUpload
	return h, nil
}
