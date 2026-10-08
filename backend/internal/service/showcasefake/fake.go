// Package showcasefake 提供登录页展示模块的内存 fake，供 service 测试与 handler 测试共用。
// 它不依赖 service 包（否则会循环依赖），只按 service 里声明的接口结构实现，语义与真实 repository 保持一致。
package showcasefake

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

// Repo 是 showcase_items 的内存实现，同时实现 service.ShowcaseRepo 与 repository.ShowcaseTx。
type Repo struct {
	mu     sync.Mutex
	Items  []model.ShowcaseItem // 当前全部条目（未按 sort 排序，读取时才排序）
	NextID uint64               // 下一个自增 id，0 时从 1 开始

	Err           error // 非空时所有方法都返回它
	FailSetSortAt int   // 大于 0 时，事务里第 N 次 SetSort 返回 ErrInjected，用来验证整体回滚
	Calls         int   // WithTx 被调用的次数
}

func (r *Repo) sorted(only func(model.ShowcaseItem) bool) []model.ShowcaseItem {
	var out []model.ShowcaseItem
	for _, it := range r.Items {
		if only == nil || only(it) {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ListAll 返回全部条目，按 sort、id 升序。
func (r *Repo) ListAll(context.Context) ([]model.ShowcaseItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return nil, r.Err
	}
	return r.sorted(nil), nil
}

// ListEnabled 返回已启用条目，按 sort、id 升序。
func (r *Repo) ListEnabled(context.Context) ([]model.ShowcaseItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return nil, r.Err
	}
	return r.sorted(func(it model.ShowcaseItem) bool { return it.Enabled }), nil
}

func (r *Repo) find(id uint64) int {
	for i := range r.Items {
		if r.Items[i].ID == id {
			return i
		}
	}
	return -1
}

// GetByID 按 id 查询，不存在返回 repository.ErrNotFound。
func (r *Repo) GetByID(_ context.Context, id uint64) (*model.ShowcaseItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return nil, r.Err
	}
	i := r.find(id)
	if i < 0 {
		return nil, repository.ErrNotFound
	}
	cp := r.Items[i]
	return &cp, nil
}

// MaxSort 返回最大 sort，没有条目返回 -1。
func (r *Repo) MaxSort(context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return 0, r.Err
	}
	top := -1
	for _, it := range r.Items {
		top = max(top, it.Sort)
	}
	return top, nil
}

// Create 插入条目并回填 id、时间。
func (r *Repo) Create(_ context.Context, it *model.ShowcaseItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	if r.NextID == 0 {
		r.NextID = 1
	}
	it.ID = r.NextID
	r.NextID++
	if it.CreatedAt.IsZero() {
		it.CreatedAt = time.Now()
	}
	r.Items = append(r.Items, *it)
	return nil
}

// Update 按列名更新；值为 nil 表示置 NULL。条目不存在返回 repository.ErrNotFound。
func (r *Repo) Update(_ context.Context, id uint64, fields map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	i := r.find(id)
	if i < 0 {
		return repository.ErrNotFound
	}
	it := &r.Items[i]
	for k, v := range fields {
		switch k {
		case "prompt":
			it.Prompt = v.(string)
		case "model_label":
			it.ModelLabel = v.(string)
		case "start_sec":
			it.StartSec = v.(float64)
		case "enabled":
			it.Enabled = v.(bool)
		case "asset_id":
			it.AssetID = v.(uint64)
		case "poster_asset_id":
			if v == nil {
				it.PosterAssetID = nil
			} else {
				poster := v.(uint64)
				it.PosterAssetID = &poster
			}
		default:
			return errors.New("fake 不认识的列：" + k)
		}
	}
	return nil
}

// ReferencedAssetIDs 返回 ids 里已被某个条目作为视频引用的素材 id（去重，顺序不保证）。
func (r *Repo) ReferencedAssetIDs(_ context.Context, ids []uint64) ([]uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return nil, r.Err
	}
	want := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	seen := map[uint64]struct{}{}
	var out []uint64
	for _, it := range r.Items {
		if _, ok := want[it.AssetID]; !ok {
			continue
		}
		if _, dup := seen[it.AssetID]; dup {
			continue
		}
		seen[it.AssetID] = struct{}{}
		out = append(out, it.AssetID)
	}
	return out, nil
}

// Delete 删除条目，不存在返回 repository.ErrNotFound。
func (r *Repo) Delete(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	i := r.find(id)
	if i < 0 {
		return repository.ErrNotFound
	}
	r.Items = append(r.Items[:i], r.Items[i+1:]...)
	return nil
}

// WithTx 在“事务”里执行 fn：fn 返回错误时恢复进入前的数据，模拟回滚。
func (r *Repo) WithTx(_ context.Context, fn func(tx repository.ShowcaseTx) error) error {
	r.mu.Lock()
	r.Calls++
	if r.Err != nil {
		r.mu.Unlock()
		return r.Err
	}
	snapshot := append([]model.ShowcaseItem(nil), r.Items...)
	r.mu.Unlock()

	tx := &txRepo{r: r}
	if err := fn(tx); err != nil {
		r.mu.Lock()
		r.Items = snapshot
		r.mu.Unlock()
		return err
	}
	return nil
}

// txRepo 是事务内可用的原语，直接读写 Repo 的数据（锁由各方法自己取）。
type txRepo struct {
	r        *Repo
	setCalls int
}

func (t *txRepo) ListIDs(context.Context) ([]uint64, error) {
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	var ids []uint64
	for _, it := range t.r.sorted(nil) {
		ids = append(ids, it.ID)
	}
	return ids, nil
}

func (t *txRepo) SetSort(_ context.Context, id uint64, sortVal int) error {
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	t.setCalls++
	if t.r.FailSetSortAt > 0 && t.setCalls == t.r.FailSetSortAt {
		return ErrInjected
	}
	i := t.r.find(id)
	if i < 0 {
		return repository.ErrNotFound
	}
	t.r.Items[i].Sort = sortVal
	return nil
}

// Assets 是素材的内存实现：同时满足 service.ShowcaseAssets（GetByID / ListByIDs）与 service.ShowcaseAssetViewer（ViewOf）。
type Assets struct {
	Rows map[uint64]model.Asset // 素材 id → 素材
	Err  error                  // 非空时所有方法都返回它
}

// NewAssets 用给定素材建一个 fake。
func NewAssets(rows ...model.Asset) *Assets {
	a := &Assets{Rows: map[uint64]model.Asset{}}
	for _, row := range rows {
		a.Rows[row.ID] = row
	}
	return a
}

// GetByID 按 id 且 user_id 查询，不存在或不属于该用户返回 repository.ErrNotFound。
func (a *Assets) GetByID(_ context.Context, userID, id uint64) (*model.Asset, error) {
	if a.Err != nil {
		return nil, a.Err
	}
	row, ok := a.Rows[id]
	if !ok || row.UserID != userID {
		return nil, repository.ErrNotFound
	}
	return &row, nil
}

// ListByIDs 批量查询，不存在的 id 缺席。
func (a *Assets) ListByIDs(_ context.Context, ids []uint64) ([]model.Asset, error) {
	if a.Err != nil {
		return nil, a.Err
	}
	var out []model.Asset
	for _, id := range ids {
		if row, ok := a.Rows[id]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

// ListByKindSource 按 kind + source 分页查询素材（不限用户），created_at 倒序、id 倒序，同时返回总数。
func (a *Assets) ListByKindSource(_ context.Context, kind, source string, offset, limit int) ([]model.Asset, int64, error) {
	if a.Err != nil {
		return nil, 0, a.Err
	}
	var all []model.Asset
	for _, row := range a.Rows {
		if row.Kind == kind && row.Source == source {
			all = append(all, row)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.After(all[j].CreatedAt)
		}
		return all[i].ID > all[j].ID
	})
	total := int64(len(all))
	if offset >= len(all) {
		return nil, total, nil
	}
	end := min(offset+limit, len(all))
	return all[offset:end], total, nil
}

// ViewOf 生成与真实 AssetService 一致形状的视图：URL 是稳定地址 /files/<key>。
func (a *Assets) ViewOf(_ context.Context, row *model.Asset) (*model.AssetView, error) {
	return &model.AssetView{
		ID: row.ID, Kind: row.Kind, URL: "/files/" + row.StorageKey, MimeType: row.MimeType,
		ByteSize: row.ByteSize, Width: row.Width, Height: row.Height, DurationMs: row.DurationMs, FileName: row.FileName,
	}, nil
}

// Tasks 是生成任务的内存实现，满足 service.ShowcaseTasks。
type Tasks struct {
	Rows  map[uint64]model.GenerationTask // 任务 id → 任务
	Err   error                           // 非空时返回它
	Calls int                             // ListBriefByIDs 被调用的次数，用来断言“批量取、不是 N+1”
}

// NewTasks 用给定任务建一个 fake。
func NewTasks(rows ...model.GenerationTask) *Tasks {
	t := &Tasks{Rows: map[uint64]model.GenerationTask{}}
	for _, row := range rows {
		t.Rows[row.ID] = row
	}
	return t
}

// ListBriefByIDs 批量查询任务（不限用户），不存在的 id 缺席。
func (t *Tasks) ListBriefByIDs(_ context.Context, ids []uint64) ([]model.GenerationTask, error) {
	t.Calls++
	if t.Err != nil {
		return nil, t.Err
	}
	var out []model.GenerationTask
	for _, id := range ids {
		if row, ok := t.Rows[id]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

// Users 是用户名查询的内存实现，满足 service.ShowcaseUsers。
type Users struct {
	Names map[uint64]string // 用户 id → 用户名
	Err   error             // 非空时返回它
	Calls int               // ListUsernames 被调用的次数
}

// ListUsernames 批量取用户名，不存在的 id 缺席。
func (u *Users) ListUsernames(_ context.Context, ids []uint64) (map[uint64]string, error) {
	u.Calls++
	if u.Err != nil {
		return nil, u.Err
	}
	out := map[uint64]string{}
	for _, id := range ids {
		if name, ok := u.Names[id]; ok {
			out[id] = name
		}
	}
	return out, nil
}
