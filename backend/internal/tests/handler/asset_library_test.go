package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// hLib 是内存版资产库依赖：一个结构体同时充当文件夹、归属、素材、画布仓储与素材视图，只覆盖 handler 测试用到的行为。
type hLib struct {
	folders   map[uint64]*model.AssetFolder
	items     map[uint64]*model.AssetLibraryItem
	assets    map[uint64]*model.Asset
	generated []model.GeneratedAssetRow
	next      uint64
}

// 文件夹仓储
func (l *hLib) ListWithCount(_ context.Context, u uint64) ([]model.AssetFolderCount, error) {
	var out []model.AssetFolderCount
	for _, f := range l.folders {
		if f.UserID == u {
			n, _ := l.CountItems(context.Background(), u, f.ID)
			out = append(out, model.AssetFolderCount{AssetFolder: *f, ItemCount: int(n)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (l *hLib) Count(ctx context.Context, u uint64) (int64, error) {
	r, _ := l.ListWithCount(ctx, u)
	return int64(len(r)), nil
}
func (l *hLib) NameExists(_ context.Context, u uint64, name string, ex uint64) (bool, error) {
	for _, f := range l.folders {
		if f.UserID == u && f.ID != ex && strings.EqualFold(f.Name, name) {
			return true, nil
		}
	}
	return false, nil
}
func (l *hLib) id() uint64 { l.next++; return l.next }
func (l *hLib) Create(_ context.Context, f *model.AssetFolder) error {
	f.ID, f.CreatedAt = l.id(), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cp := *f
	l.folders[f.ID] = &cp
	return nil
}
func (l *hLib) GetByID(_ context.Context, u, id uint64) (*model.AssetFolder, error) {
	f, ok := l.folders[id]
	if !ok || f.UserID != u {
		return nil, repository.ErrNotFound
	}
	cp := *f
	return &cp, nil
}
func (l *hLib) Rename(_ context.Context, u, id uint64, name string) error {
	f, ok := l.folders[id]
	if !ok || f.UserID != u {
		return repository.ErrNotFound
	}
	f.Name = name
	return nil
}
func (l *hLib) CountItems(_ context.Context, u, fid uint64) (int64, error) {
	var n int64
	for _, it := range l.items {
		if it.UserID == u && it.FolderID != nil && *it.FolderID == fid {
			n++
		}
	}
	return n, nil
}
func (l *hLib) WithTx(_ context.Context, fn func(tx repository.AssetFolderTx) error) error {
	return fn(hLibTx{l})
}

type hLibTx struct{ l *hLib }

func (t hLibTx) GetByID(ctx context.Context, u, id uint64) (*model.AssetFolder, error) {
	return t.l.GetByID(ctx, u, id)
}
func (t hLibTx) DeleteItems(_ context.Context, u, fid uint64) (int64, error) {
	var n int64
	for id, it := range t.l.items {
		if it.UserID == u && it.FolderID != nil && *it.FolderID == fid {
			delete(t.l.items, id)
			n++
		}
	}
	return n, nil
}
func (t hLibTx) Delete(_ context.Context, u, id uint64) error {
	f, ok := t.l.folders[id]
	if !ok || f.UserID != u {
		return repository.ErrNotFound
	}
	delete(t.l.folders, id)
	return nil
}

// 归属仓储（Create / GetByID 与文件夹仓储同名同签名不冲突：参数类型不同，故归属用独立类型）
type hLibItems struct{ l *hLib }

func (r hLibItems) Create(_ context.Context, it *model.AssetLibraryItem) error {
	it.ID, it.CreatedAt = r.l.id(), time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	cp := *it
	r.l.items[it.ID] = &cp
	return nil
}
func (r hLibItems) GetByID(_ context.Context, u, id uint64) (*model.AssetLibraryItem, error) {
	it, ok := r.l.items[id]
	if !ok || it.UserID != u {
		return nil, repository.ErrNotFound
	}
	cp := *it
	return &cp, nil
}
func (r hLibItems) GetByAssetID(_ context.Context, u, aid uint64) (*model.AssetLibraryItem, error) {
	for _, it := range r.l.items {
		if it.UserID == u && it.AssetID == aid {
			cp := *it
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (r hLibItems) ListByAssetIDs(_ context.Context, u uint64, ids []uint64) ([]model.AssetLibraryItem, error) {
	var out []model.AssetLibraryItem
	for _, it := range r.l.items {
		for _, id := range ids {
			if it.UserID == u && it.AssetID == id {
				out = append(out, *it)
			}
		}
	}
	return out, nil
}
func (r hLibItems) SetFolder(_ context.Context, u, id uint64, fid *uint64) error {
	it, ok := r.l.items[id]
	if !ok || it.UserID != u {
		return repository.ErrNotFound
	}
	it.FolderID = fid
	return nil
}
func (r hLibItems) Delete(_ context.Context, u, id uint64) error {
	it, ok := r.l.items[id]
	if !ok || it.UserID != u {
		return repository.ErrNotFound
	}
	delete(r.l.items, id)
	return nil
}
func (r hLibItems) List(_ context.Context, u uint64, f repository.LibraryItemFilter) ([]model.AssetLibraryItem, error) {
	var out []model.AssetLibraryItem
	for _, it := range r.l.items {
		if it.UserID == u && (f.Scope == repository.LibraryScopeAll || (f.Scope == repository.LibraryScopeRoot) == (it.FolderID == nil)) {
			out = append(out, *it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// 素材仓储
type hLibAssets struct{ l *hLib }

func (r hLibAssets) GetByID(_ context.Context, u, id uint64) (*model.Asset, error) {
	a, ok := r.l.assets[id]
	if !ok || a.UserID != u {
		return nil, repository.ErrNotFound
	}
	cp := *a
	return &cp, nil
}
func (r hLibAssets) ListByIDs(_ context.Context, ids []uint64) ([]model.Asset, error) {
	var out []model.Asset
	for _, id := range ids {
		if a, ok := r.l.assets[id]; ok {
			out = append(out, *a)
		}
	}
	return out, nil
}
func (r hLibAssets) ListGeneratedByUser(_ context.Context, u uint64, f repository.GeneratedAssetFilter) ([]model.GeneratedAssetRow, error) {
	var out []model.GeneratedAssetRow
	for _, g := range r.l.generated {
		if g.UserID == u && g.Kind == f.Kind {
			out = append(out, g)
		}
	}
	return out, nil
}

// 画布与素材视图
type hLibCanvases struct{}

func (hLibCanvases) BriefsByIDs(_ context.Context, _ uint64, ids []uint64) ([]model.CanvasBrief, error) {
	var out []model.CanvasBrief
	for _, id := range ids {
		out = append(out, model.CanvasBrief{ID: id, Title: "画布"})
	}
	return out, nil
}

type hLibViewer struct{}

func (hLibViewer) ViewOf(_ context.Context, a *model.Asset) (*model.AssetView, error) {
	return &model.AssetView{ID: a.ID, Kind: a.Kind, URL: "/files/x", MimeType: "image/png"}, nil
}

func newLibRouter() (*gin.Engine, *hLib) {
	l := &hLib{folders: map[uint64]*model.AssetFolder{}, items: map[uint64]*model.AssetLibraryItem{}, assets: map[uint64]*model.Asset{}}
	svc := service.NewAssetLibraryService(service.AssetLibraryDeps{
		Folders: l, Items: hLibItems{l}, Assets: hLibAssets{l}, Canvases: hLibCanvases{}, Views: hLibViewer{},
	})
	h := NewAssetLibraryHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	r.GET("/assets/generated", h.ListGenerated)
	r.GET("/asset-folders", h.ListFolders)
	r.POST("/asset-folders", h.CreateFolder)
	r.PATCH("/asset-folders/:id", h.RenameFolder)
	r.DELETE("/asset-folders/:id", h.DeleteFolder)
	r.GET("/asset-library/items", h.ListItems)
	r.POST("/asset-library/items", h.AddItem)
	r.PATCH("/asset-library/items/:id", h.MoveItem)
	r.DELETE("/asset-library/items/:id", h.RemoveItem)
	return r, l
}

func libCall(t *testing.T, r *gin.Engine, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是 JSON：%v %s", err, w.Body.String())
	}
	return w.Code, resp
}

func libData(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	d, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("data 不是对象：%v", resp)
	}
	return d
}

func TestAssetLibraryHandler_Folders(t *testing.T) {
	r, _ := newLibRouter()

	code, resp := libCall(t, r, http.MethodPost, "/asset-folders", `{"name":"角色"}`)
	if code != http.StatusCreated || resp["code"].(float64) != 0 {
		t.Fatalf("新建应 201：%d %v", code, resp)
	}
	f := libData(t, resp)
	id, _ := f["id"].(string)
	if len(id) != idcodec.EncodedLen || f["name"] != "角色" || f["item_count"].(float64) != 0 || f["created_at"] == nil {
		t.Fatalf("Folder 字段不符合契约：%v", f)
	}

	t.Run("列表是 {items}", func(t *testing.T) {
		code, resp := libCall(t, r, http.MethodGet, "/asset-folders", "")
		items, _ := libData(t, resp)["items"].([]any)
		if code != 200 || len(items) != 1 {
			t.Fatalf("%d %v", code, resp)
		}
	})
	t.Run("重名 409 + 64002；名称为空 400 + 64001", func(t *testing.T) {
		code, resp := libCall(t, r, http.MethodPost, "/asset-folders", `{"name":"角色"}`)
		if code != http.StatusConflict || int(resp["code"].(float64)) != errcode.ErrAssetFolderDup.Code {
			t.Fatalf("%d %v", code, resp)
		}
		code, resp = libCall(t, r, http.MethodPost, "/asset-folders", `{"name":"  "}`)
		if code != http.StatusBadRequest || int(resp["code"].(float64)) != errcode.ErrAssetFolderInvalid.Code {
			t.Fatalf("%d %v", code, resp)
		}
	})
	t.Run("参数格式错误 400 + 10001：请求体不是 JSON、路径 id 不是十六进制串", func(t *testing.T) {
		code, resp := libCall(t, r, http.MethodPost, "/asset-folders", `不是 json`)
		if code != http.StatusBadRequest || int(resp["code"].(float64)) != errcode.ErrInvalidParams.Code {
			t.Fatalf("%d %v", code, resp)
		}
		code, resp = libCall(t, r, http.MethodPatch, "/asset-folders/123", `{"name":"x"}`)
		if code != http.StatusBadRequest || int(resp["code"].(float64)) != errcode.ErrInvalidParams.Code {
			t.Fatalf("%d %v", code, resp)
		}
	})
	t.Run("重命名返回 Folder；不存在 404 + 64004", func(t *testing.T) {
		code, resp := libCall(t, r, http.MethodPatch, "/asset-folders/"+id, `{"name":"场景"}`)
		if code != 200 || libData(t, resp)["name"] != "场景" {
			t.Fatalf("%d %v", code, resp)
		}
		code, resp = libCall(t, r, http.MethodPatch, "/asset-folders/"+idcodec.Encode(999), `{"name":"x"}`)
		if code != http.StatusNotFound || int(resp["code"].(float64)) != errcode.ErrAssetFolderNotFound.Code {
			t.Fatalf("%d %v", code, resp)
		}
	})
	t.Run("删除返回 {removed}", func(t *testing.T) {
		code, resp := libCall(t, r, http.MethodDelete, "/asset-folders/"+id, "")
		if code != 200 || libData(t, resp)["removed"].(float64) != 0 {
			t.Fatalf("%d %v", code, resp)
		}
		code, _ = libCall(t, r, http.MethodDelete, "/asset-folders/"+id, "")
		if code != http.StatusNotFound {
			t.Fatalf("重复删除应 404：%d", code)
		}
	})
}

func TestAssetLibraryHandler_Items(t *testing.T) {
	r, l := newLibRouter()
	l.assets[7] = &model.Asset{ID: 7, UserID: 1, Kind: "image"}
	l.assets[8] = &model.Asset{ID: 8, UserID: 2, Kind: "image"} // 别人的素材

	code, resp := libCall(t, r, http.MethodPost, "/asset-library/items", `{"asset_id":7}`)
	if code != http.StatusCreated {
		t.Fatalf("新增应 201：%d %v", code, resp)
	}
	it := libData(t, resp)
	itemID, _ := it["id"].(string)
	asset, _ := it["asset"].(map[string]any)
	if len(itemID) != idcodec.EncodedLen || it["folder_id"] != nil || asset["id"].(float64) != 7 || it["created_at"] == nil {
		t.Fatalf("LibraryItem 字段不符合契约：%v", it)
	}

	t.Run("已在库里 200 返回现有归属", func(t *testing.T) {
		code, resp := libCall(t, r, http.MethodPost, "/asset-library/items", `{"asset_id":7,"folder_id":null}`)
		if code != 200 || libData(t, resp)["id"] != itemID {
			t.Fatalf("%d %v", code, resp)
		}
	})
	t.Run("别人的素材 404；asset_id 缺失 400", func(t *testing.T) {
		code, resp := libCall(t, r, http.MethodPost, "/asset-library/items", `{"asset_id":8}`)
		if code != http.StatusNotFound || int(resp["code"].(float64)) != errcode.ErrAssetNotFound.Code {
			t.Fatalf("%d %v", code, resp)
		}
		code, resp = libCall(t, r, http.MethodPost, "/asset-library/items", `{}`)
		if code != http.StatusBadRequest || int(resp["code"].(float64)) != errcode.ErrInvalidParams.Code {
			t.Fatalf("%d %v", code, resp)
		}
	})
	t.Run("列表 {items,next_cursor}；kind 非法 400", func(t *testing.T) {
		code, resp := libCall(t, r, http.MethodGet, "/asset-library/items", "")
		d := libData(t, resp)
		items, _ := d["items"].([]any)
		if code != 200 || len(items) != 1 || d["next_cursor"] != "" {
			t.Fatalf("%d %v", code, resp)
		}
		code, resp = libCall(t, r, http.MethodGet, "/asset-library/items?kind=doc", "")
		if code != http.StatusBadRequest || int(resp["code"].(float64)) != errcode.ErrInvalidParams.Code {
			t.Fatalf("%d %v", code, resp)
		}
	})
	t.Run("移动到别人 / 不存在的文件夹 404；移回根目录 folder_id 为 null", func(t *testing.T) {
		code, resp := libCall(t, r, http.MethodPatch, "/asset-library/items/"+itemID, `{"folder_id":"`+idcodec.Encode(999)+`"}`)
		if code != http.StatusNotFound || int(resp["code"].(float64)) != errcode.ErrAssetFolderNotFound.Code {
			t.Fatalf("%d %v", code, resp)
		}
		code, resp = libCall(t, r, http.MethodPatch, "/asset-library/items/"+itemID, `{"folder_id":null}`)
		if code != 200 || libData(t, resp)["folder_id"] != nil {
			t.Fatalf("%d %v", code, resp)
		}
	})
	t.Run("移出返回成功空体，再移出 404 + 64005", func(t *testing.T) {
		code, resp := libCall(t, r, http.MethodDelete, "/asset-library/items/"+itemID, "")
		if code != 200 || resp["code"].(float64) != 0 || resp["data"] != nil {
			t.Fatalf("%d %v", code, resp)
		}
		code, resp = libCall(t, r, http.MethodDelete, "/asset-library/items/"+itemID, "")
		if code != http.StatusNotFound || int(resp["code"].(float64)) != errcode.ErrLibraryItemNotFound.Code {
			t.Fatalf("%d %v", code, resp)
		}
	})
}

func TestAssetLibraryHandler_ListGenerated(t *testing.T) {
	r, l := newLibRouter()
	canvas := uint64(5)
	l.generated = []model.GeneratedAssetRow{
		{Asset: model.Asset{ID: 1, UserID: 1, Kind: "image", CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}, CanvasProjectID: &canvas,
			InputJSON: []byte(`{"prompt":"雪地"}`), ConfigSnapshot: []byte(`{"model":{"label":"猴子"}}`)},
		{Asset: model.Asset{ID: 2, UserID: 1, Kind: "image"}, InputJSON: []byte(`{}`)},
	}

	code, resp := libCall(t, r, http.MethodGet, "/assets/generated?kind=image", "")
	d := libData(t, resp)
	items, _ := d["items"].([]any)
	if code != 200 || len(items) != 2 || d["next_cursor"] != "" {
		t.Fatalf("%d %v", code, resp)
	}
	first, _ := items[0].(map[string]any)
	cv, _ := first["canvas"].(map[string]any)
	if first["prompt"] != "雪地" || first["model_label"] != "猴子" || first["library_item_id"] != nil ||
		cv["id"] != idcodec.Encode(canvas) || cv["title"] != "画布" || first["asset"] == nil || first["created_at"] == nil {
		t.Fatalf("GeneratedItem 字段不符合契约：%v", first)
	}
	if second, _ := items[1].(map[string]any); second["canvas"] != nil {
		t.Fatalf("对话来源的 canvas 应为 null：%v", second)
	}

	for _, bad := range []string{
		"/assets/generated",                            // kind 必填
		"/assets/generated?kind=doc",                   // kind 非法
		"/assets/generated?kind=image&source=x",        // source 非法
		"/assets/generated?kind=image&canvas_id=1f",    // canvas_id 不是十六进制编码串
		"/assets/generated?kind=image&created_from=昨天", // created_from 不是 RFC3339
		"/assets/generated?kind=image&limit=101",       // limit 超上限
		"/assets/generated?kind=image&cursor=!!",       // 游标损坏
		"/assets/generated?kind=image&order=sideways",  // order 非法
	} {
		code, resp := libCall(t, r, http.MethodGet, bad, "")
		if code != http.StatusBadRequest || int(resp["code"].(float64)) != errcode.ErrInvalidParams.Code {
			t.Fatalf("%s 期望 400 + 10001，实际 %d %v", bad, code, resp)
		}
	}
	code, _ = libCall(t, r, http.MethodGet, "/assets/generated?kind=image&source=canvas&canvas_id="+idcodec.Encode(canvas)+"&created_from=2026-10-01T00:00:00%2B08:00&order=asc&q=雪", "")
	if code != 200 {
		t.Fatalf("合法的全部参数应 200：%d", code)
	}
}
