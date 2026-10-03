package storage_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	. "video-canvas/internal/storage"

	"video-canvas/internal/config"
)

// fakeSource 实现 Source，记录被查询的次数。
type fakeSource struct {
	mu        sync.Mutex
	defaultID uint64
	entries   map[uint64]Entry
	entryHits map[uint64]int
	defHits   int
}

func newFakeSource(entries ...Entry) *fakeSource {
	s := &fakeSource{entries: map[uint64]Entry{}, entryHits: map[uint64]int{}}
	for _, e := range entries {
		s.entries[e.ID] = e
	}
	return s
}

func (f *fakeSource) DefaultID(ctx context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defHits++
	return f.defaultID, nil
}

func (f *fakeSource) Entry(ctx context.Context, id uint64) (Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entryHits[id]++
	e, ok := f.entries[id]
	if !ok {
		return Entry{}, ErrStorageNotFound
	}
	return e, nil
}

func (f *fakeSource) set(e Entry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[e.ID] = e
}

func ossEntry(id uint64, version int) Entry {
	sp := specOSS()
	return Entry{ID: id, Version: version, Provider: ProviderAliyunOSS, Spec: sp, SignedTTL: 2 * time.Hour, DirectUpload: true}
}

func TestRegistry(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	t.Run("按 id 解析并在有效期内复用，不重复查库", func(t *testing.T) {
		src := newFakeSource(ossEntry(2, 1))
		r := NewRegistryWithClock(src, 30*time.Second, clock)
		h1, err := r.Get(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		h2, _ := r.Get(ctx, 2)
		if h1.Storage != h2.Storage || src.entryHits[2] != 1 {
			t.Errorf("应复用同一个客户端且只查一次，实际查询 %d 次", src.entryHits[2])
		}
		if h1.SignedTTL != 2*time.Hour || !h1.DirectUpload || h1.Provider != ProviderAliyunOSS {
			t.Errorf("句柄信息不对：%+v", h1)
		}
	})

	t.Run("过期后重新读取；版本没变就复用客户端，版本变了就重建", func(t *testing.T) {
		src := newFakeSource(ossEntry(2, 1))
		r := NewRegistryWithClock(src, 30*time.Second, clock)
		h1, _ := r.Get(ctx, 2)

		now = now.Add(31 * time.Second)
		h2, _ := r.Get(ctx, 2)
		if src.entryHits[2] != 2 || h1.Storage != h2.Storage {
			t.Errorf("版本相同应复用客户端：查询 %d 次，复用=%v", src.entryHits[2], h1.Storage == h2.Storage)
		}

		now = now.Add(31 * time.Second)
		src.set(ossEntry(2, 2))
		h3, _ := r.Get(ctx, 2)
		if h3.Storage == h2.Storage || h3.Version != 2 {
			t.Errorf("版本变化应重建客户端：%+v", h3)
		}
	})

	t.Run("Invalidate 让下一次立刻重新读取", func(t *testing.T) {
		src := newFakeSource(ossEntry(2, 1))
		r := NewRegistryWithClock(src, time.Hour, clock)
		_, _ = r.Get(ctx, 2)
		src.set(ossEntry(2, 2))
		r.Invalidate(2)
		h, _ := r.Get(ctx, 2)
		if h.Version != 2 {
			t.Errorf("应读到新版本，实际 %d", h.Version)
		}
	})

	t.Run("Default 返回默认存储，设置变更后失效即生效", func(t *testing.T) {
		src := newFakeSource(ossEntry(2, 1), ossEntry(3, 1))
		src.defaultID = 2
		r := NewRegistryWithClock(src, time.Hour, clock)
		h, err := r.Default(ctx)
		if err != nil || h.ID != 2 {
			t.Fatalf("实际 %+v err=%v", h, err)
		}
		src.defaultID = 3
		if h, _ := r.Default(ctx); h.ID != 2 {
			t.Errorf("有效期内默认存储应保持缓存值，实际 %d", h.ID)
		}
		r.Invalidate(0)
		if h, _ := r.Default(ctx); h.ID != 3 {
			t.Errorf("失效后应读到新的默认存储，实际 %d", h.ID)
		}
	})

	t.Run("存储不存在", func(t *testing.T) {
		r := NewRegistryWithClock(newFakeSource(), time.Hour, clock)
		if _, err := r.Get(ctx, 9); !errors.Is(err, ErrStorageNotFound) {
			t.Fatalf("实际：%v", err)
		}
	})

	t.Run("配置不合法时返回错误且不缓存", func(t *testing.T) {
		bad := ossEntry(2, 1)
		bad.Spec.Bucket = ""
		src := newFakeSource(bad)
		r := NewRegistryWithClock(src, time.Hour, clock)
		if _, err := r.Get(ctx, 2); !errors.Is(err, ErrInvalidSpec) {
			t.Fatalf("实际：%v", err)
		}
		src.set(ossEntry(2, 2))
		if _, err := r.Get(ctx, 2); err != nil {
			t.Fatalf("修好后应能解析：%v", err)
		}
	})

	t.Run("本地磁盘存储", func(t *testing.T) {
		src := newFakeSource(Entry{ID: 1, Version: 1, Provider: ProviderLocal, Local: config.LocalStorage{Dir: t.TempDir()}})
		r := NewRegistryWithClock(src, time.Hour, clock)
		h, err := r.Get(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := h.Storage.(*LocalStorage); !ok {
			t.Errorf("本地存储应是 *LocalStorage，实际 %T", h.Storage)
		}
		if h.DirectUpload {
			t.Error("本地存储不支持直传")
		}
	})
}
