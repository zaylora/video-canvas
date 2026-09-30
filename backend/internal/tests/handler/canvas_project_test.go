package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// fakeCanvasRepo 是内存版画布仓储，只覆盖 handler 测试用到的行为。
type fakeCanvasRepo struct {
	items map[uint64]*model.CanvasProject
	next  uint64
}

func (r *fakeCanvasRepo) Create(_ context.Context, p *model.CanvasProject) error {
	r.next++
	p.ID = r.next
	cp := *p
	r.items[p.ID] = &cp
	return nil
}

func (r *fakeCanvasRepo) GetByID(_ context.Context, userID, id uint64) (*model.CanvasProject, error) {
	p, ok := r.items[id]
	if !ok || p.UserID != userID {
		return nil, repository.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (r *fakeCanvasRepo) List(_ context.Context, userID uint64, _ string, _, _ int) ([]model.CanvasProjectItem, int64, error) {
	var out []model.CanvasProjectItem
	for _, p := range r.items {
		if p.UserID == userID {
			out = append(out, model.CanvasProjectItem{ID: p.ID, Title: p.Title, Revision: p.Revision})
		}
	}
	return out, int64(len(out)), nil
}

func (r *fakeCanvasRepo) Update(_ context.Context, userID, id, revision uint64, fields map[string]any) error {
	p, ok := r.items[id]
	if !ok || p.UserID != userID {
		return repository.ErrNotFound
	}
	if p.Revision != revision {
		return repository.ErrRevisionConflict
	}
	if t, ok := fields["title"].(string); ok {
		p.Title = t
	}
	p.Revision++
	return nil
}

func (r *fakeCanvasRepo) Delete(_ context.Context, userID, id uint64) error {
	p, ok := r.items[id]
	if !ok || p.UserID != userID {
		return repository.ErrNotFound
	}
	delete(r.items, id)
	return nil
}

func newCanvasRouter() (*gin.Engine, *fakeCanvasRepo) {
	repo := &fakeCanvasRepo{items: map[uint64]*model.CanvasProject{}}
	h := NewCanvasProjectHandler(service.NewCanvasProjectService(repo))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	g := r.Group("/api/v1/canvas")
	g.POST("", h.Create)
	g.GET("", h.List)
	g.GET("/:id", h.Get)
	g.PUT("/:id", h.Update)
	g.DELETE("/:id", h.Delete)
	return r, repo
}

func canvasCall(t *testing.T, r *gin.Engine, method, path, body string) (int, map[string]any) {
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

// 画布 ID 对外是十六进制串：创建、列表、详情、更新、删除都用它，数字 ID 和乱串一律拒绝。
func TestCanvasProjectHandler_HexID(t *testing.T) {
	r, repo := newCanvasRouter()

	status, resp := canvasCall(t, r, http.MethodPost, "/api/v1/canvas", `{"title":"a"}`)
	data, _ := resp["data"].(map[string]any)
	id, _ := data["id"].(string)
	if status != http.StatusOK || id != idcodec.Encode(1) || len(id) != idcodec.EncodedLen {
		t.Fatalf("创建应返回十六进制 ID：%d %v", status, resp)
	}

	status, resp = canvasCall(t, r, http.MethodGet, "/api/v1/canvas", "")
	page, _ := resp["data"].(map[string]any)
	list, _ := page["list"].([]any)
	if status != http.StatusOK || len(list) != 1 || list[0].(map[string]any)["id"] != id {
		t.Fatalf("列表应返回十六进制 ID：%d %v", status, resp)
	}

	if status, resp = canvasCall(t, r, http.MethodGet, "/api/v1/canvas/"+id, ""); status != http.StatusOK || resp["data"].(map[string]any)["id"] != id {
		t.Fatalf("详情失败：%d %v", status, resp)
	}
	if status, resp = canvasCall(t, r, http.MethodPut, "/api/v1/canvas/"+id, `{"title":"b","revision":1}`); status != http.StatusOK || resp["data"].(map[string]any)["revision"] != float64(2) {
		t.Fatalf("更新失败：%d %v", status, resp)
	}

	for _, bad := range []string{"1", "abc", strings.Repeat("0", 32)} {
		status, resp = canvasCall(t, r, http.MethodGet, "/api/v1/canvas/"+bad, "")
		if status != http.StatusBadRequest || int(resp["code"].(float64)) != errcode.ErrInvalidParams.Code {
			t.Errorf("非法 ID %q 应报 400 + 10001：%d %v", bad, status, resp)
		}
	}
	// 合法格式但不存在的画布仍是 404
	if status, _ = canvasCall(t, r, http.MethodGet, "/api/v1/canvas/"+idcodec.Encode(99), ""); status != http.StatusNotFound {
		t.Errorf("不存在的画布应 404：%d", status)
	}

	if status, _ = canvasCall(t, r, http.MethodDelete, "/api/v1/canvas/"+id, ""); status != http.StatusOK || len(repo.items) != 0 {
		t.Errorf("删除失败：%d", status)
	}
}
