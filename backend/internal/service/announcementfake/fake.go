// Package announcementfake 提供首页公告模块的内存 fake，供 service 测试与 handler 测试共用。
// 它不依赖 service 包（否则会循环依赖），只按 service 里声明的接口结构实现，语义与真实 repository 保持一致：
// Update 刷新 UpdatedAt，SetEnabled 与 SetSort 不刷新。
package announcementfake

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// ErrInjected 是测试注入的数据库故障。
var ErrInjected = errors.New("注入的数据库故障")

// Repo 是 announcements 的内存实现，同时实现 service.AnnouncementRepo 与 repository.AnnouncementTx。
type Repo struct {
	mu     sync.Mutex
	Items  []model.Announcement // 当前全部未删除公告（未按 sort 排序，读取时才排序）
	NextID uint64               // 下一个自增 id，0 时从 1 开始

	// Clock 给 CreatedAt / UpdatedAt 打时间；nil 时用 time.Now。测试里用它制造不同的 updated_at。
	Clock func() time.Time
	Err   error // 非空时所有方法都返回它

	FailSetSortAt int // 大于 0 时，事务里第 N 次 SetSort 返回 ErrInjected，用来验证整体回滚
}

func (r *Repo) now() time.Time {
	if r.Clock != nil {
		return r.Clock()
	}
	return time.Now()
}

func (r *Repo) sorted() []model.Announcement {
	out := append([]model.Announcement(nil), r.Items...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (r *Repo) index(id uint64) int {
	for i := range r.Items {
		if r.Items[i].ID == id {
			return i
		}
	}
	return -1
}

// ListAll 返回全部公告，按 sort、id 升序。
func (r *Repo) ListAll(context.Context) ([]model.Announcement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return nil, r.Err
	}
	return r.sorted(), nil
}

// GetByID 按 id 查询，不存在返回 repository.ErrNotFound。
func (r *Repo) GetByID(_ context.Context, id uint64) (*model.Announcement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return nil, r.Err
	}
	i := r.index(id)
	if i < 0 {
		return nil, repository.ErrNotFound
	}
	a := r.Items[i]
	return &a, nil
}

// MaxSort 返回最大 sort，没有公告时返回 -1。
func (r *Repo) MaxSort(context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return 0, r.Err
	}
	top := -1
	for _, a := range r.Items {
		if a.Sort > top {
			top = a.Sort
		}
	}
	return top, nil
}

// Create 插入公告并回填 ID、CreatedAt、UpdatedAt。
func (r *Repo) Create(_ context.Context, a *model.Announcement) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	if r.NextID == 0 {
		r.NextID = 1
	}
	a.ID = r.NextID
	r.NextID++
	t := r.now()
	a.CreatedAt, a.UpdatedAt = t, t
	r.Items = append(r.Items, *a)
	return nil
}

// Update 按列名改字段并刷新 UpdatedAt，不存在返回 repository.ErrNotFound。
func (r *Repo) Update(_ context.Context, id uint64, fields map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	i := r.index(id)
	if i < 0 {
		return repository.ErrNotFound
	}
	a := &r.Items[i]
	for k, v := range fields {
		switch k {
		case "title":
			a.Title = v.(string)
		case "enabled":
			a.Enabled = v.(bool)
		case "starts_at":
			a.StartsAt = timePtr(v)
		case "ends_at":
			a.EndsAt = timePtr(v)
		case "updated_by":
			a.UpdatedBy = v.(uint64)
		default:
			panic("announcementfake: 未支持的列 " + k)
		}
	}
	a.UpdatedAt = r.now()
	return nil
}

func timePtr(v any) *time.Time {
	switch t := v.(type) {
	case nil:
		return nil
	case *time.Time:
		return t
	case time.Time:
		return &t
	default:
		panic("announcementfake: 时间列只接受 nil / time.Time / *time.Time")
	}
}

// SetEnabled 只改启用开关与修改人，不刷新 UpdatedAt；不存在返回 repository.ErrNotFound。
func (r *Repo) SetEnabled(_ context.Context, id uint64, enabled bool, actorID uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	i := r.index(id)
	if i < 0 {
		return repository.ErrNotFound
	}
	r.Items[i].Enabled = enabled
	r.Items[i].UpdatedBy = actorID
	return nil
}

// Delete 删除公告，不存在返回 repository.ErrNotFound。
func (r *Repo) Delete(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	i := r.index(id)
	if i < 0 {
		return repository.ErrNotFound
	}
	r.Items = append(r.Items[:i], r.Items[i+1:]...)
	return nil
}

// WithTx 在“事务”里执行 fn：先给全部数据拍快照，fn 返回错误时恢复快照，模拟回滚。
func (r *Repo) WithTx(ctx context.Context, fn func(tx repository.AnnouncementTx) error) error {
	r.mu.Lock()
	if r.Err != nil {
		r.mu.Unlock()
		return r.Err
	}
	snapshot := append([]model.Announcement(nil), r.Items...)
	r.mu.Unlock()
	tx := &txView{repo: r}
	if err := fn(tx); err != nil {
		r.mu.Lock()
		r.Items = snapshot
		r.mu.Unlock()
		return err
	}
	return nil
}

// txView 是事务回调里拿到的 AnnouncementTx。
type txView struct {
	repo     *Repo
	setSorts int
}

// ListIDs 返回全部 id，按 sort、id 升序。
func (t *txView) ListIDs(context.Context) ([]uint64, error) {
	t.repo.mu.Lock()
	defer t.repo.mu.Unlock()
	var ids []uint64
	for _, a := range t.repo.sorted() {
		ids = append(ids, a.ID)
	}
	return ids, nil
}

// SetSort 改排序，不刷新 UpdatedAt；第 FailSetSortAt 次调用返回 ErrInjected。
func (t *txView) SetSort(_ context.Context, id uint64, sort int) error {
	t.setSorts++
	if t.repo.FailSetSortAt > 0 && t.setSorts == t.repo.FailSetSortAt {
		return ErrInjected
	}
	t.repo.mu.Lock()
	defer t.repo.mu.Unlock()
	i := t.repo.index(id)
	if i < 0 {
		return repository.ErrNotFound
	}
	t.repo.Items[i].Sort = sort
	return nil
}
