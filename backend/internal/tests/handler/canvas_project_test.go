package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"

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
	// covers 是画布 id 对应的封面素材 key，没有表示没封面
	covers map[uint64]string
	// afterUpdate 模拟本次保存后、响应返回前发生的另一次写入。
	afterUpdate func(*model.CanvasProject)
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
			out = append(out, model.CanvasProjectItem{ID: p.ID, Title: p.Title, Revision: p.Revision, CoverKey: r.covers[p.ID]})
		}
	}
	return out, int64(len(out)), nil
}

func (r *fakeCanvasRepo) Update(_ context.Context, userID, id, revision uint64, fields map[string]any) (*model.CanvasProject, error) {
	p, ok := r.items[id]
	if !ok || p.UserID != userID {
		return nil, repository.ErrNotFound
	}
	if p.Revision != revision {
		return nil, repository.ErrRevisionConflict
	}
	if t, ok := fields["title"].(string); ok {
		p.Title = t
	}
	if j, ok := fields["payload_json"].(datatypes.JSON); ok {
		p.PayloadJSON = j
	}
	p.Revision++
	saved := *p
	if r.afterUpdate != nil {
		r.afterUpdate(p)
	}
	return &saved, nil
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

// 保存响应必须对应本次写入，不能把随后 Agent 的版本认成本地内容的版本。
func TestCanvasProjectHandler_UpdateSnapshot(t *testing.T) {
	r, repo := newCanvasRouter()
	repo.items[1] = &model.CanvasProject{
		BaseModel: model.BaseModel{ID: 1}, UserID: 1, Title: "画布",
		PayloadJSON: datatypes.JSON(`{"writer":"initial"}`), Revision: 10,
	}
	repo.afterUpdate = func(p *model.CanvasProject) {
		p.PayloadJSON = datatypes.JSON(`{"writer":"agent"}`)
		p.Revision++
	}
	status, resp := canvasCall(t, r, http.MethodPut, "/api/v1/canvas/"+idcodec.Encode(1),
		`{"revision":10,"payload_json":{"writer":"browser"}}`)
	if status != http.StatusOK {
		t.Fatalf("保存失败：%d %v", status, resp)
	}
	data := resp["data"].(map[string]any)
	if data["revision"] != float64(11) || data["payload_json"].(map[string]any)["writer"] != "browser" {
		t.Fatalf("应返回本次保存的版本 11 和 browser 内容，不能混入随后 Agent 的写入：%v", data)
	}
	if repo.items[1].Revision != 12 {
		t.Fatal("测试未制造随后发生的 Agent 写入")
	}
}

func TestCanvasProjectHandler_UpdateErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		body   string
		userID uint64
		status int
		code   int
	}{
		{"缺少版本号", `{"title":"新标题"}`, 1, http.StatusBadRequest, errcode.ErrInvalidParams.Code},
		{"版本过期", `{"revision":9,"title":"新标题"}`, 1, http.StatusConflict, errcode.ErrCanvasConflict.Code},
		{"不属于当前用户", `{"revision":10,"title":"新标题"}`, 2, http.StatusNotFound, errcode.ErrCanvasNotFound.Code},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, repo := newCanvasRouter()
			repo.items[1] = &model.CanvasProject{BaseModel: model.BaseModel{ID: 1}, UserID: tt.userID, Title: "画布", Revision: 10}
			status, resp := canvasCall(t, r, http.MethodPut, "/api/v1/canvas/"+idcodec.Encode(1), tt.body)
			if status != tt.status || resp["code"] != float64(tt.code) {
				t.Fatalf("期望 %d + %d，实际 %d %v", tt.status, tt.code, status, resp)
			}
			if repo.items[1].Revision != 10 || repo.items[1].Title != "画布" {
				t.Fatal("失败请求不能修改画布")
			}
		})
	}
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

// 画布里的组节点（type=group、width/height、成员的 parentId）整体存在 payload_json 里，
// 后端只要求它是 JSON 对象，不认识节点字段：创建、更新、读取后必须原样返回，不丢任何组字段。
func TestCanvasProjectHandler_GroupNodesRoundTrip(t *testing.T) {
	r, _ := newCanvasRouter()
	payload := `{"nodes":[` +
		`{"id":"g1","type":"group","position":{"x":100,"y":200},"width":448,"height":300,"data":{"label":"第一场","color":"blue","labelColor":"orange"}},` +
		`{"id":"n1","type":"canvas","parentId":"g1","position":{"x":24,"y":24},"data":{"kind":"image","label":"镜头 1"}}` +
		`],"edges":[],"viewport":{"x":0,"y":0,"zoom":1}}`

	status, resp := canvasCall(t, r, http.MethodPost, "/api/v1/canvas", `{"title":"a","payload_json":`+payload+`}`)
	data, _ := resp["data"].(map[string]any)
	id, _ := data["id"].(string)
	if status != http.StatusOK || id == "" {
		t.Fatalf("创建带组的画布失败：%d %v", status, resp)
	}

	// 更新一次：改掉组名和颜色，成员关系不动
	updated := strings.Replace(payload, `"第一场"`, `"第二场"`, 1)
	updated = strings.Replace(updated, `"color":"blue"`, `"color":"red"`, 1)
	if status, resp = canvasCall(t, r, http.MethodPut, "/api/v1/canvas/"+id, `{"revision":1,"payload_json":`+updated+`}`); status != http.StatusOK {
		t.Fatalf("更新带组的画布失败：%d %v", status, resp)
	}

	status, resp = canvasCall(t, r, http.MethodGet, "/api/v1/canvas/"+id, "")
	if status != http.StatusOK {
		t.Fatalf("读取失败：%d %v", status, resp)
	}
	got, _ := resp["data"].(map[string]any)["payload_json"].(map[string]any)
	nodes, _ := got["nodes"].([]any)
	if len(nodes) != 2 {
		t.Fatalf("节点数应为 2：%v", got)
	}
	group := nodes[0].(map[string]any)
	groupData := group["data"].(map[string]any)
	if group["type"] != "group" || group["width"] != float64(448) || group["height"] != float64(300) ||
		groupData["label"] != "第二场" || groupData["color"] != "red" || groupData["labelColor"] != "orange" {
		t.Errorf("组节点字段丢失或被改写：%v", group)
	}
	if member := nodes[1].(map[string]any); member["parentId"] != "g1" {
		t.Errorf("成员的 parentId 丢失：%v", member)
	}
}

// 列表里的每张画布带封面地址：没有封面的画布返回空串，前端据此回退到节点线框。
func TestCanvasList_CoverURL(t *testing.T) {
	repo := &fakeCanvasRepo{
		items: map[uint64]*model.CanvasProject{
			1: {BaseModel: model.BaseModel{ID: 1}, UserID: 1, Title: "有封面", Revision: 1},
			2: {BaseModel: model.BaseModel{ID: 2}, UserID: 1, Title: "没封面", Revision: 1},
		},
		covers: map[uint64]string{1: "u/1/a.png"},
	}
	h := NewCanvasProjectHandler(service.NewCanvasProjectService(repo,
		service.WithCoverURL(func(key string) string { return "/files/" + key + "?v=thumb" })))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	r.GET("/api/v1/canvas", h.List)

	_, resp := canvasCall(t, r, http.MethodGet, "/api/v1/canvas", "")
	list := resp["data"].(map[string]any)["list"].([]any)
	byTitle := map[string]string{}
	for _, it := range list {
		m := it.(map[string]any)
		byTitle[m["title"].(string)], _ = m["cover_url"].(string)
	}
	if byTitle["有封面"] != "/files/u/1/a.png?v=thumb" {
		t.Fatalf("封面地址不对：%+v", byTitle)
	}
	if v, ok := byTitle["没封面"]; !ok || v != "" {
		t.Fatalf("没有封面应返回空串：%+v", byTitle)
	}
}
