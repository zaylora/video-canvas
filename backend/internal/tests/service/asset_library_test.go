package service_test

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// 本文件测试资产库服务（AssetLibraryService）：文件夹、库归属与生成历史的业务规则。
// 过滤条件本身（source / canvas_id / created_from / order / q）是 SQL，由 tests/repository 的集成测试覆盖，
// 这里的假仓储只按游标与上限工作，服务层测的是组装、归属校验、错误翻译与分页游标。

type libFolders struct {
	rows    map[uint64]*model.AssetFolder
	items   *libItems
	next    uint64
	failTx  error // 非空时 WithTx 的回调返回它之后整体回滚
	dupOnce bool  // 下一次 Create 返回 ErrDuplicate（模拟并发撞唯一索引）
}

func (f *libFolders) ListWithCount(_ context.Context, userID uint64) ([]model.AssetFolderCount, error) {
	var out []model.AssetFolderCount
	for _, r := range f.rows {
		if r.UserID == userID {
			n, _ := f.CountItems(context.Background(), userID, r.ID)
			out = append(out, model.AssetFolderCount{AssetFolder: *r, ItemCount: int(n)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *libFolders) Count(ctx context.Context, userID uint64) (int64, error) {
	l, _ := f.ListWithCount(ctx, userID)
	return int64(len(l)), nil
}

func (f *libFolders) NameExists(_ context.Context, userID uint64, name string, excludeID uint64) (bool, error) {
	for _, r := range f.rows {
		if r.UserID == userID && r.ID != excludeID && strings.EqualFold(r.Name, name) {
			return true, nil
		}
	}
	return false, nil
}

func (f *libFolders) Create(_ context.Context, m *model.AssetFolder) error {
	if f.dupOnce {
		f.dupOnce = false
		return repository.ErrDuplicate
	}
	f.next++
	m.ID = f.next
	cp := *m
	f.rows[m.ID] = &cp
	return nil
}

func (f *libFolders) GetByID(_ context.Context, userID, id uint64) (*model.AssetFolder, error) {
	r, ok := f.rows[id]
	if !ok || r.UserID != userID {
		return nil, repository.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (f *libFolders) Rename(_ context.Context, userID, id uint64, name string) error {
	r, ok := f.rows[id]
	if !ok || r.UserID != userID {
		return repository.ErrNotFound
	}
	r.Name = name
	return nil
}

func (f *libFolders) CountItems(_ context.Context, userID, folderID uint64) (int64, error) {
	var n int64
	for _, it := range f.items.rows {
		if it.UserID == userID && it.FolderID != nil && *it.FolderID == folderID {
			n++
		}
	}
	return n, nil
}

// WithTx 用快照模拟事务：回调返回错误时恢复两张表。
func (f *libFolders) WithTx(ctx context.Context, fn func(tx repository.AssetFolderTx) error) error {
	snapF, snapI := map[uint64]*model.AssetFolder{}, map[uint64]*model.AssetLibraryItem{}
	for k, v := range f.rows {
		cp := *v
		snapF[k] = &cp
	}
	for k, v := range f.items.rows {
		cp := *v
		snapI[k] = &cp
	}
	err := fn(libFolderTx{f})
	if err == nil {
		err = f.failTx
	}
	if err != nil {
		f.rows, f.items.rows = snapF, snapI
	}
	return err
}

type libFolderTx struct{ f *libFolders }

func (t libFolderTx) GetByID(ctx context.Context, userID, id uint64) (*model.AssetFolder, error) {
	return t.f.GetByID(ctx, userID, id)
}

func (t libFolderTx) DeleteItems(_ context.Context, userID, folderID uint64) (int64, error) {
	var n int64
	for id, it := range t.f.items.rows {
		if it.UserID == userID && it.FolderID != nil && *it.FolderID == folderID {
			delete(t.f.items.rows, id)
			n++
		}
	}
	return n, nil
}

func (t libFolderTx) Delete(_ context.Context, userID, id uint64) error {
	r, ok := t.f.rows[id]
	if !ok || r.UserID != userID {
		return repository.ErrNotFound
	}
	delete(t.f.rows, id)
	return nil
}

type libItems struct {
	rows       map[uint64]*model.AssetLibraryItem
	next       uint64
	dupOnce    bool // 下一次 Create 先偷偷插入同素材的归属再返回 ErrDuplicate
	listCalled int
}

func (r *libItems) Create(_ context.Context, it *model.AssetLibraryItem) error {
	if r.dupOnce {
		r.dupOnce = false
		r.next++
		r.rows[r.next] = &model.AssetLibraryItem{ID: r.next, UserID: it.UserID, AssetID: it.AssetID, CreatedAt: time.Now()}
		return repository.ErrDuplicate
	}
	r.next++
	it.ID = r.next
	it.CreatedAt = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(r.next) * time.Minute)
	cp := *it
	r.rows[it.ID] = &cp
	return nil
}

func (r *libItems) GetByID(_ context.Context, userID, id uint64) (*model.AssetLibraryItem, error) {
	it, ok := r.rows[id]
	if !ok || it.UserID != userID {
		return nil, repository.ErrNotFound
	}
	cp := *it
	return &cp, nil
}

func (r *libItems) GetByAssetID(_ context.Context, userID, assetID uint64) (*model.AssetLibraryItem, error) {
	for _, it := range r.rows {
		if it.UserID == userID && it.AssetID == assetID {
			cp := *it
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *libItems) ListByAssetIDs(_ context.Context, userID uint64, ids []uint64) ([]model.AssetLibraryItem, error) {
	var out []model.AssetLibraryItem
	for _, it := range r.rows {
		if it.UserID == userID && slices.Contains(ids, it.AssetID) {
			out = append(out, *it)
		}
	}
	return out, nil
}

func (r *libItems) SetFolder(_ context.Context, userID, id uint64, folderID *uint64) error {
	it, ok := r.rows[id]
	if !ok || it.UserID != userID {
		return repository.ErrNotFound
	}
	it.FolderID = folderID
	return nil
}

func (r *libItems) Delete(_ context.Context, userID, id uint64) error {
	it, ok := r.rows[id]
	if !ok || it.UserID != userID {
		return repository.ErrNotFound
	}
	delete(r.rows, id)
	return nil
}

// List 只实现范围、游标与上限（类型 / 文件名过滤由仓储集成测试覆盖）。
func (r *libItems) List(_ context.Context, userID uint64, f repository.LibraryItemFilter) ([]model.AssetLibraryItem, error) {
	r.listCalled++
	var all []model.AssetLibraryItem
	for _, it := range r.rows {
		if it.UserID != userID {
			continue
		}
		switch f.Scope {
		case repository.LibraryScopeRoot:
			if it.FolderID != nil {
				continue
			}
		case repository.LibraryScopeFolder:
			if it.FolderID == nil || *it.FolderID != f.FolderID {
				continue
			}
		case repository.LibraryScopeAll:
		}
		all = append(all, *it)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })
	if f.After != nil {
		var rest []model.AssetLibraryItem
		for _, it := range all {
			if it.ID < f.After.ID {
				rest = append(rest, it)
			}
		}
		all = rest
	}
	if len(all) > f.Limit {
		all = all[:f.Limit]
	}
	return all, nil
}

type libAssets struct {
	rows      map[uint64]*model.Asset
	generated []model.GeneratedAssetRow // 已按「最新优先」排好，游标按 id 截断
	lastF     repository.GeneratedAssetFilter
}

func (a *libAssets) GetByID(_ context.Context, userID, id uint64) (*model.Asset, error) {
	r, ok := a.rows[id]
	if !ok || r.UserID != userID {
		return nil, repository.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (a *libAssets) ListByIDs(_ context.Context, ids []uint64) ([]model.Asset, error) {
	var out []model.Asset
	for _, id := range ids {
		if r, ok := a.rows[id]; ok {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (a *libAssets) ListGeneratedByUser(_ context.Context, userID uint64, f repository.GeneratedAssetFilter) ([]model.GeneratedAssetRow, error) {
	a.lastF = f
	var out []model.GeneratedAssetRow
	for _, r := range a.generated {
		if r.UserID != userID {
			continue
		}
		if f.After != nil && r.ID >= f.After.ID {
			continue
		}
		out = append(out, r)
	}
	if len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

type libCanvases struct {
	briefs map[uint64]model.CanvasBrief
	calls  int
}

func (c *libCanvases) BriefsByIDs(_ context.Context, _ uint64, ids []uint64) ([]model.CanvasBrief, error) {
	c.calls++
	var out []model.CanvasBrief
	for _, id := range ids {
		if b, ok := c.briefs[id]; ok {
			out = append(out, b)
		}
	}
	return out, nil
}

type libViewer struct{}

func (libViewer) ViewOf(_ context.Context, a *model.Asset) (*model.AssetView, error) {
	return &model.AssetView{ID: a.ID, Kind: a.Kind, URL: "/files/" + a.StorageKey, FileName: a.FileName}, nil
}

type libEnvT struct {
	svc      *AssetLibraryService
	folders  *libFolders
	items    *libItems
	assets   *libAssets
	canvases *libCanvases
}

func newLibEnv() *libEnvT {
	items := &libItems{rows: map[uint64]*model.AssetLibraryItem{}}
	e := &libEnvT{
		folders:  &libFolders{rows: map[uint64]*model.AssetFolder{}, items: items},
		items:    items,
		assets:   &libAssets{rows: map[uint64]*model.Asset{}},
		canvases: &libCanvases{briefs: map[uint64]model.CanvasBrief{}},
	}
	e.svc = NewAssetLibraryService(AssetLibraryDeps{Folders: e.folders, Items: e.items, Assets: e.assets, Canvases: e.canvases, Views: libViewer{}})
	return e
}

func (e *libEnvT) addAsset(id, user uint64, kind string) {
	e.assets.rows[id] = &model.Asset{ID: id, UserID: user, Kind: kind, StorageKey: "k" + string(rune('0'+id%10)), FileName: "f.png"}
}

func libIsCode(err error, want *errcode.Error) bool {
	var ec *errcode.Error
	return errors.As(err, &ec) && ec.Code == want.Code
}

func TestAssetLibraryService_Folders(t *testing.T) {
	ctx := context.Background()
	const me, other = uint64(1), uint64(2)

	t.Run("名称去首尾空白，空白 / 超过 30 字返回 64001", func(t *testing.T) {
		e := newLibEnv()
		v, err := e.svc.CreateFolder(ctx, me, "  角色  ")
		if err != nil || v.Name != "角色" || v.ItemCount != 0 {
			t.Fatalf("%+v %v", v, err)
		}
		for _, bad := range []string{"", "   ", strings.Repeat("长", 31)} {
			if _, err := e.svc.CreateFolder(ctx, me, bad); !libIsCode(err, errcode.ErrAssetFolderInvalid) {
				t.Fatalf("%q 期望 64001，实际 %v", bad, err)
			}
		}
		if _, err := e.svc.CreateFolder(ctx, me, strings.Repeat("长", 30)); err != nil {
			t.Fatalf("30 字应可：%v", err)
		}
	})
	t.Run("同名忽略大小写返回 64002，别的用户可以同名；并发撞唯一索引同样 64002", func(t *testing.T) {
		e := newLibEnv()
		if _, err := e.svc.CreateFolder(ctx, me, "Hero"); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.CreateFolder(ctx, me, "hERO"); !libIsCode(err, errcode.ErrAssetFolderDup) {
			t.Fatalf("期望 64002，实际 %v", err)
		}
		if _, err := e.svc.CreateFolder(ctx, other, "Hero"); err != nil {
			t.Fatalf("别的用户应可同名：%v", err)
		}
		e.folders.dupOnce = true
		if _, err := e.svc.CreateFolder(ctx, me, "Other"); !libIsCode(err, errcode.ErrAssetFolderDup) {
			t.Fatalf("期望 64002，实际 %v", err)
		}
	})
	t.Run("每用户最多 100 个，超出返回 64003", func(t *testing.T) {
		e := newLibEnv()
		for i := 0; i < model.MaxAssetFolders; i++ {
			if _, err := e.svc.CreateFolder(ctx, me, "f"+string(rune('A'+i/26))+string(rune('a'+i%26))); err != nil {
				t.Fatalf("第 %d 个应可建：%v", i, err)
			}
		}
		if _, err := e.svc.CreateFolder(ctx, me, "超出"); !libIsCode(err, errcode.ErrAssetFolderLimit) {
			t.Fatalf("期望 64003，实际 %v", err)
		}
		if _, err := e.svc.CreateFolder(ctx, other, "别人的额度独立"); err != nil {
			t.Fatalf("别的用户额度独立：%v", err)
		}
	})
	t.Run("重命名：只改大小写允许；与其他文件夹重名 64002；别人的 / 不存在 64004", func(t *testing.T) {
		e := newLibEnv()
		a, _ := e.svc.CreateFolder(ctx, me, "Hero")
		b, _ := e.svc.CreateFolder(ctx, me, "场景")
		if v, err := e.svc.RenameFolder(ctx, me, uint64(a.ID), "hero"); err != nil || v.Name != "hero" {
			t.Fatalf("改大小写应可：%+v %v", v, err)
		}
		if _, err := e.svc.RenameFolder(ctx, me, uint64(b.ID), "HERO"); !libIsCode(err, errcode.ErrAssetFolderDup) {
			t.Fatalf("期望 64002，实际 %v", err)
		}
		if _, err := e.svc.RenameFolder(ctx, other, uint64(a.ID), "x"); !libIsCode(err, errcode.ErrAssetFolderNotFound) {
			t.Fatalf("别人的文件夹期望 64004，实际 %v", err)
		}
		if _, err := e.svc.RenameFolder(ctx, me, 999, "x"); !libIsCode(err, errcode.ErrAssetFolderNotFound) {
			t.Fatalf("不存在期望 64004，实际 %v", err)
		}
		if _, err := e.svc.RenameFolder(ctx, me, uint64(a.ID), " "); !libIsCode(err, errcode.ErrAssetFolderInvalid) {
			t.Fatalf("空名期望 64001，实际 %v", err)
		}
	})
	t.Run("列表带素材数，只含自己的", func(t *testing.T) {
		e := newLibEnv()
		a, _ := e.svc.CreateFolder(ctx, me, "A")
		_, _ = e.svc.CreateFolder(ctx, other, "B")
		e.addAsset(1, me, "image")
		if _, _, err := e.svc.AddItem(ctx, me, 1, uint64(a.ID)); err != nil {
			t.Fatal(err)
		}
		l, err := e.svc.ListFolders(ctx, me)
		if err != nil || len(l.Items) != 1 || l.Items[0].ItemCount != 1 || l.Items[0].Name != "A" {
			t.Fatalf("%+v %v", l, err)
		}
	})
	t.Run("删除：同一事务里移出归属并删文件夹，返回 removed，不动素材与别的归属", func(t *testing.T) {
		e := newLibEnv()
		a, _ := e.svc.CreateFolder(ctx, me, "A")
		for id := uint64(1); id <= 3; id++ {
			e.addAsset(id, me, "image")
		}
		_, _, _ = e.svc.AddItem(ctx, me, 1, uint64(a.ID))
		_, _, _ = e.svc.AddItem(ctx, me, 2, uint64(a.ID))
		_, _, _ = e.svc.AddItem(ctx, me, 3, 0) // 根目录

		v, err := e.svc.DeleteFolder(ctx, me, uint64(a.ID))
		if err != nil || v.Removed != 2 {
			t.Fatalf("%+v %v", v, err)
		}
		if len(e.folders.rows) != 0 || len(e.items.rows) != 1 || len(e.assets.rows) != 3 {
			t.Fatalf("文件夹 %d、归属 %d、素材 %d", len(e.folders.rows), len(e.items.rows), len(e.assets.rows))
		}
	})
	t.Run("删除：事务失败整体回滚；别人的 / 不存在 64004", func(t *testing.T) {
		e := newLibEnv()
		a, _ := e.svc.CreateFolder(ctx, me, "A")
		e.addAsset(1, me, "image")
		_, _, _ = e.svc.AddItem(ctx, me, 1, uint64(a.ID))

		e.folders.failTx = errors.New("提交失败")
		if _, err := e.svc.DeleteFolder(ctx, me, uint64(a.ID)); err == nil {
			t.Fatal("应返回错误")
		}
		if len(e.folders.rows) != 1 || len(e.items.rows) != 1 {
			t.Fatalf("回滚后应原样：文件夹 %d、归属 %d", len(e.folders.rows), len(e.items.rows))
		}
		e.folders.failTx = nil
		if _, err := e.svc.DeleteFolder(ctx, other, uint64(a.ID)); !libIsCode(err, errcode.ErrAssetFolderNotFound) {
			t.Fatalf("别人的期望 64004，实际 %v", err)
		}
		if len(e.folders.rows) != 1 || len(e.items.rows) != 1 {
			t.Fatal("别人删除不应有任何副作用")
		}
	})
}

func TestAssetLibraryService_Items(t *testing.T) {
	ctx := context.Background()
	const me, other = uint64(1), uint64(2)

	t.Run("加入：新增 created=true；重复加入幂等返回现有归属（不改文件夹）", func(t *testing.T) {
		e := newLibEnv()
		e.addAsset(1, me, "image")
		f, _ := e.svc.CreateFolder(ctx, me, "A")
		first, created, err := e.svc.AddItem(ctx, me, 1, 0)
		if err != nil || !created || first.FolderID != 0 || first.Asset.ID != 1 {
			t.Fatalf("%+v %v %v", first, created, err)
		}
		again, created, err := e.svc.AddItem(ctx, me, 1, uint64(f.ID))
		if err != nil || created || again.ID != first.ID || again.FolderID != 0 {
			t.Fatalf("重复加入应返回现有归属：%+v %v %v", again, created, err)
		}
		if len(e.items.rows) != 1 {
			t.Fatalf("不应新增归属：%d", len(e.items.rows))
		}
	})
	t.Run("加入：并发撞唯一索引按幂等返回已存在的那条", func(t *testing.T) {
		e := newLibEnv()
		e.addAsset(1, me, "image")
		e.items.dupOnce = true
		v, created, err := e.svc.AddItem(ctx, me, 1, 0)
		if err != nil || created || v == nil {
			t.Fatalf("%+v %v %v", v, created, err)
		}
	})
	t.Run("加入：别人的素材 / 不存在的素材 404(40007)，别人的文件夹 404(64004)", func(t *testing.T) {
		e := newLibEnv()
		e.addAsset(1, other, "image")
		e.addAsset(2, me, "image")
		f, _ := e.svc.CreateFolder(ctx, other, "别人的")
		if _, _, err := e.svc.AddItem(ctx, me, 1, 0); !libIsCode(err, errcode.ErrAssetNotFound) {
			t.Fatalf("别人的素材期望 40007，实际 %v", err)
		}
		if _, _, err := e.svc.AddItem(ctx, me, 999, 0); !libIsCode(err, errcode.ErrAssetNotFound) {
			t.Fatalf("不存在的素材期望 40007，实际 %v", err)
		}
		if _, _, err := e.svc.AddItem(ctx, me, 2, uint64(f.ID)); !libIsCode(err, errcode.ErrAssetFolderNotFound) {
			t.Fatalf("别人的文件夹期望 64004，实际 %v", err)
		}
		if len(e.items.rows) != 0 {
			t.Fatal("失败不应留下归属")
		}
	})
	t.Run("移动：到文件夹 / 回根目录；别人的条目与别人的文件夹 404", func(t *testing.T) {
		e := newLibEnv()
		e.addAsset(1, me, "image")
		f, _ := e.svc.CreateFolder(ctx, me, "A")
		of, _ := e.svc.CreateFolder(ctx, other, "O")
		it, _, _ := e.svc.AddItem(ctx, me, 1, 0)

		v, err := e.svc.MoveItem(ctx, me, uint64(it.ID), uint64(f.ID))
		if err != nil || v.FolderID != f.ID {
			t.Fatalf("%+v %v", v, err)
		}
		v, err = e.svc.MoveItem(ctx, me, uint64(it.ID), 0)
		if err != nil || v.FolderID != 0 {
			t.Fatalf("%+v %v", v, err)
		}
		if _, err := e.svc.MoveItem(ctx, other, uint64(it.ID), 0); !libIsCode(err, errcode.ErrLibraryItemNotFound) {
			t.Fatalf("别人的条目期望 64005，实际 %v", err)
		}
		if _, err := e.svc.MoveItem(ctx, me, uint64(it.ID), uint64(of.ID)); !libIsCode(err, errcode.ErrAssetFolderNotFound) {
			t.Fatalf("别人的文件夹期望 64004，实际 %v", err)
		}
	})
	t.Run("移出：只删归属不删素材；别人的 / 不存在 64005", func(t *testing.T) {
		e := newLibEnv()
		e.addAsset(1, me, "image")
		it, _, _ := e.svc.AddItem(ctx, me, 1, 0)
		if err := e.svc.RemoveItem(ctx, other, uint64(it.ID)); !libIsCode(err, errcode.ErrLibraryItemNotFound) {
			t.Fatalf("别人的期望 64005，实际 %v", err)
		}
		if err := e.svc.RemoveItem(ctx, me, uint64(it.ID)); err != nil {
			t.Fatal(err)
		}
		if len(e.items.rows) != 0 || len(e.assets.rows) != 1 {
			t.Fatalf("归属 %d、素材 %d", len(e.items.rows), len(e.assets.rows))
		}
		if err := e.svc.RemoveItem(ctx, me, uint64(it.ID)); !libIsCode(err, errcode.ErrLibraryItemNotFound) {
			t.Fatalf("重复移出期望 64005，实际 %v", err)
		}
	})
	t.Run("列表：文件夹范围校验，游标翻页稳定，坏游标 / 坏文件夹 id 400", func(t *testing.T) {
		e := newLibEnv()
		f, _ := e.svc.CreateFolder(ctx, me, "A")
		of, _ := e.svc.CreateFolder(ctx, other, "O")
		for id := uint64(1); id <= 5; id++ {
			e.addAsset(id, me, "image")
			fid := uint64(0)
			if id > 3 {
				fid = uint64(f.ID)
			}
			if _, _, err := e.svc.AddItem(ctx, me, id, fid); err != nil {
				t.Fatal(err)
			}
		}
		// 根目录 3 条，每页 2 条
		p1, err := e.svc.ListItems(ctx, me, LibraryListInput{Limit: 2})
		if err != nil || len(p1.Items) != 2 || p1.NextCursor == "" {
			t.Fatalf("%+v %v", p1, err)
		}
		p2, err := e.svc.ListItems(ctx, me, LibraryListInput{Limit: 2, Cursor: p1.NextCursor})
		if err != nil || len(p2.Items) != 1 || p2.NextCursor != "" {
			t.Fatalf("%+v %v", p2, err)
		}
		got := []uint64{p1.Items[0].Asset.ID, p1.Items[1].Asset.ID, p2.Items[0].Asset.ID}
		if !slices.Equal(got, []uint64{3, 2, 1}) {
			t.Fatalf("根目录应按加入时间倒序：%v", got)
		}
		all, _ := e.svc.ListItems(ctx, me, LibraryListInput{FolderID: LibraryFolderAll})
		if len(all.Items) != 5 {
			t.Fatalf("all 应 5 条：%d", len(all.Items))
		}
		inF, err := e.svc.ListItems(ctx, me, LibraryListInput{FolderID: idcodec.Encode(uint64(f.ID))})
		if err != nil || len(inF.Items) != 2 || inF.Items[0].FolderID != f.ID {
			t.Fatalf("%+v %v", inF, err)
		}
		if _, err := e.svc.ListItems(ctx, me, LibraryListInput{FolderID: idcodec.Encode(uint64(of.ID))}); !libIsCode(err, errcode.ErrAssetFolderNotFound) {
			t.Fatalf("别人的文件夹期望 64004，实际 %v", err)
		}
		if _, err := e.svc.ListItems(ctx, me, LibraryListInput{FolderID: "xyz"}); !libIsCode(err, errcode.ErrInvalidParams) {
			t.Fatalf("坏 id 期望 10001，实际 %v", err)
		}
		if _, err := e.svc.ListItems(ctx, me, LibraryListInput{Cursor: "!!"}); !libIsCode(err, errcode.ErrInvalidParams) {
			t.Fatalf("坏游标期望 10001，实际 %v", err)
		}
	})
}

func TestAssetLibraryService_ListGenerated(t *testing.T) {
	ctx := context.Background()
	const me = uint64(1)
	canvasLive, canvasGone, canvasMissing := uint64(10), uint64(11), uint64(12)

	row := func(id uint64, canvas *uint64, prompt string) model.GeneratedAssetRow {
		return model.GeneratedAssetRow{
			Asset:           model.Asset{ID: id, UserID: me, Kind: "image", StorageKey: "g" + string(rune('0'+id%10)), Source: model.AssetSourceGenerated, CreatedAt: time.Date(2026, 10, 1, 0, int(id), 0, 0, time.UTC)},
			CanvasProjectID: canvas,
			InputJSON:       []byte(`{"prompt":"` + prompt + `"}`),
			ConfigSnapshot:  []byte(`{"model":{"label":"天才猴子"}}`),
			TaskModelKey:    "m1",
		}
	}
	long := strings.Repeat("很长的提示词", 30) // 远超 80 字：不能被截断

	e := newLibEnv()
	e.assets.generated = []model.GeneratedAssetRow{
		row(6, nil, "对话来的"),
		row(5, &canvasLive, long),
		row(4, &canvasGone, "画布已软删除"),
		row(3, &canvasMissing, "画布查不到"),
		row(2, nil, "第二页"),
		row(1, nil, "最早"),
	}
	e.canvases.briefs[canvasLive] = model.CanvasBrief{ID: canvasLive, Title: "我的画布"}
	e.canvases.briefs[canvasGone] = model.CanvasBrief{ID: canvasGone, Title: "旧标题", Deleted: true}
	e.addAsset(5, me, "image")
	lib, _, _ := e.svc.AddItem(ctx, me, 5, 0) // 5 号已在资产库

	page, err := e.svc.ListGenerated(ctx, me, GeneratedListInput{Kind: "image", Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 4 || page.NextCursor == "" {
		t.Fatalf("第一页应 4 条且有下一页：%d %q", len(page.Items), page.NextCursor)
	}
	it := page.Items
	if it[0].Canvas != nil || it[0].Prompt != "对话来的" || it[0].ModelLabel != "天才猴子" || it[0].LibraryItemID != 0 {
		t.Fatalf("对话任务 canvas 应为 null：%+v", it[0])
	}
	if it[1].Canvas == nil || it[1].Canvas.ID != idcodec.ID(canvasLive) || it[1].Canvas.Title != "我的画布" || it[1].Canvas.Deleted {
		t.Fatalf("画布任务来源不对：%+v", it[1].Canvas)
	}
	if it[1].Prompt != long {
		t.Fatalf("提示词不能被截断：%d 字", len([]rune(it[1].Prompt)))
	}
	if it[1].LibraryItemID != lib.ID {
		t.Fatalf("已在库里的素材应带 library_item_id：%v", it[1].LibraryItemID)
	}
	for _, i := range []int{2, 3} { // 已软删除 / 查不到的画布都标 deleted，标题置空
		if c := it[i].Canvas; c == nil || !c.Deleted || c.Title != "" {
			t.Fatalf("第 %d 条应标已删除画布：%+v", i, c)
		}
	}
	if e.canvases.calls != 1 {
		t.Fatalf("画布信息应一次查询取齐：%d 次", e.canvases.calls)
	}
	if e.assets.lastF.Limit != 5 {
		t.Fatalf("应多取一条判断下一页：limit=%d", e.assets.lastF.Limit)
	}

	next, err := e.svc.ListGenerated(ctx, me, GeneratedListInput{Kind: "image", Limit: 4, Cursor: page.NextCursor})
	if err != nil || len(next.Items) != 2 || next.NextCursor != "" {
		t.Fatalf("第二页应 2 条且无更多：%+v %v", next, err)
	}
	if next.Items[0].Asset.ID != 2 || next.Items[1].Asset.ID != 1 {
		t.Fatalf("翻页应不重不漏：%d %d", next.Items[0].Asset.ID, next.Items[1].Asset.ID)
	}
	if e.assets.lastF.After == nil || e.assets.lastF.After.ID != 3 {
		t.Fatalf("游标应是上一页最后一条：%+v", e.assets.lastF.After)
	}

	// 入参透传与默认值
	if _, err := e.svc.ListGenerated(ctx, me, GeneratedListInput{Kind: "image", Source: "canvas", Asc: true, Q: " 猫 ", Limit: 500}); err != nil {
		t.Fatal(err)
	}
	if f := e.assets.lastF; f.Source != "canvas" || !f.Asc || f.Q != "猫" || f.Limit != 101 {
		t.Fatalf("过滤条件应透传（q 去空白，limit 收敛到 100 再 +1）：%+v", f)
	}
	if _, err := e.svc.ListGenerated(ctx, me, GeneratedListInput{Kind: "image", Cursor: "bad"}); !libIsCode(err, errcode.ErrInvalidParams) {
		t.Fatalf("坏游标期望 10001，实际 %v", err)
	}
	empty, err := e.svc.ListGenerated(ctx, 99, GeneratedListInput{Kind: "image"})
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != "" {
		t.Fatalf("别的用户没有历史，items 应是空数组：%+v %v", empty, err)
	}
}
