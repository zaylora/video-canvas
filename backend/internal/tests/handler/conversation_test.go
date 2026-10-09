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
	"gorm.io/datatypes"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// hConvRepo 是内存版对话仓储，只覆盖 handler 测试用到的行为。
type hConvRepo struct {
	next    uint64
	convs   map[uint64]*model.Conversation
	records map[uint64]*model.ConversationRecord
	deleted map[uint64]bool
}

func (r *hConvRepo) id() uint64 { r.next++; return r.next }

func (r *hConvRepo) EnsureDefault(_ context.Context, userID uint64) (*model.Conversation, error) {
	for _, c := range r.convs {
		if c.UserID == userID && c.IsDefault {
			cp := *c
			return &cp, nil
		}
	}
	c := &model.Conversation{UserID: userID, Title: "默认创作", IsDefault: true}
	c.ID = r.id()
	r.convs[c.ID] = c
	cp := *c
	return &cp, nil
}

func (r *hConvRepo) Create(_ context.Context, c *model.Conversation) error {
	c.ID = r.id()
	cp := *c
	r.convs[c.ID] = &cp
	return nil
}

func (r *hConvRepo) GetByID(_ context.Context, userID, id uint64) (*model.Conversation, error) {
	c, ok := r.convs[id]
	if !ok || c.UserID != userID {
		return nil, repository.ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (r *hConvRepo) List(_ context.Context, userID uint64) ([]model.Conversation, error) {
	var out []model.Conversation
	for _, c := range r.convs {
		if c.UserID == userID {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IsDefault && !out[j].IsDefault })
	return out, nil
}

func (r *hConvRepo) Count(_ context.Context, userID uint64) (int64, error) {
	list, _ := r.List(context.Background(), userID)
	return int64(len(list)), nil
}

func (r *hConvRepo) Rename(_ context.Context, userID, id uint64, title string) error {
	c, ok := r.convs[id]
	if !ok || c.UserID != userID {
		return repository.ErrNotFound
	}
	c.Title = title
	return nil
}

func (r *hConvRepo) Delete(_ context.Context, userID, id uint64) error {
	c, ok := r.convs[id]
	if !ok || c.UserID != userID {
		return repository.ErrNotFound
	}
	delete(r.convs, id)
	return nil
}

func (r *hConvRepo) AddRecordCount(_ context.Context, id uint64, delta int, _ *time.Time) error {
	if c, ok := r.convs[id]; ok {
		c.RecordCount += delta
	}
	return nil
}

func (r *hConvRepo) CreateRecord(_ context.Context, rec *model.ConversationRecord) error {
	rec.ID = r.id()
	cp := *rec
	r.records[rec.ID] = &cp
	return nil
}

func (r *hConvRepo) GetRecord(_ context.Context, userID, id uint64) (*model.ConversationRecord, error) {
	rec, ok := r.records[id]
	if !ok || rec.UserID != userID || r.deleted[id] {
		return nil, repository.ErrNotFound
	}
	cp := *rec
	return &cp, nil
}

func (r *hConvRepo) FindRecordByIdem(context.Context, uint64, string) (*model.ConversationRecord, error) {
	return nil, repository.ErrNotFound
}

func (r *hConvRepo) ListRecords(_ context.Context, userID, convID, before uint64, limit int) ([]model.ConversationRecord, error) {
	var out []model.ConversationRecord
	for id, rec := range r.records {
		if rec.UserID == userID && rec.ConversationID == convID && !r.deleted[id] && (before == 0 || id < before) {
			out = append(out, *rec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *hConvRepo) SaveRecordTasks(_ context.Context, id uint64, taskIDs, submitErrors datatypes.JSON, quote int) error {
	rec := r.records[id]
	rec.TaskIDsJSON, rec.SubmitErrorsJSON, rec.QuoteCredits = taskIDs, submitErrors, quote
	return nil
}

func (r *hConvRepo) DeleteRecord(_ context.Context, userID, id uint64) error {
	rec, ok := r.records[id]
	if !ok || rec.UserID != userID || r.deleted[id] {
		return repository.ErrNotFound
	}
	r.deleted[id] = true
	return nil
}

func (r *hConvRepo) ConversationIDsByRecordIDs(context.Context, uint64, []uint64) ([]uint64, error) {
	return nil, nil
}

// hConvTasks 是假的生成任务服务。
type hConvTasks struct {
	next      uint64
	createErr error
	views     map[uint64]*model.GenerationTaskView
	canceled  []uint64
}

func (t *hConvTasks) Create(_ context.Context, _ uint64, _ string, req *model.CreateGenerationTaskReq) (*model.CreateGenerationTaskResp, error) {
	if t.createErr != nil {
		return nil, t.createErr
	}
	resp := &model.CreateGenerationTaskResp{}
	for _, node := range req.NodeIDs {
		t.next++
		v := &model.GenerationTaskView{ID: 500 + t.next, NodeID: node, Kind: req.Kind, ModelID: req.ModelID, Status: model.TaskQueued, Credits: 2}
		t.views[v.ID] = v
		resp.Items = append(resp.Items, model.CreateTaskItem{NodeID: node, Task: v})
	}
	return resp, nil
}

func (t *hConvTasks) Cancel(_ context.Context, _ uint64, id uint64) (*model.GenerationTaskView, error) {
	t.canceled = append(t.canceled, id)
	return t.views[id], nil
}

func (t *hConvTasks) ViewsByIDs(_ context.Context, _ uint64, ids []uint64) ([]model.GenerationTaskView, error) {
	var out []model.GenerationTaskView
	for _, id := range ids {
		if v, ok := t.views[id]; ok {
			out = append(out, *v)
		}
	}
	return out, nil
}

func (t *hConvTasks) List(context.Context, uint64, *model.ListGenerationTaskReq) ([]model.GenerationTaskView, error) {
	return nil, nil
}

func newConvRouter() (*gin.Engine, *hConvRepo, *hConvTasks) {
	repo := &hConvRepo{convs: map[uint64]*model.Conversation{}, records: map[uint64]*model.ConversationRecord{}, deleted: map[uint64]bool{}}
	tasks := &hConvTasks{views: map[uint64]*model.GenerationTaskView{}}
	h := NewConversationHandler(service.NewConversationService(repo, tasks))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	g := r.Group("/api/v1/conversations")
	g.GET("", h.List)
	g.POST("", h.Create)
	g.PATCH("/:id", h.Rename)
	g.DELETE("/:id", h.Delete)
	g.GET("/:id/records", h.ListRecords)
	g.POST("/:id/records", h.Submit)
	g.DELETE("/:id/records/:rid", h.DeleteRecord)
	return r, repo, tasks
}

func convCall(t *testing.T, r *gin.Engine, method, path, body string, headers ...string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是 JSON：%v %s", err, w.Body.String())
	}
	return w.Code, resp
}

func respCode(resp map[string]any) int { return int(resp["code"].(float64)) }

const submitBody = `{"kind":"image","model_id":"m1","prompt":"雨夜","input":{"ratio":"16:9"},"count":2}`

// 列表：第一次访问自动出现默认创作，id 是十六进制串。
func TestConversationHandler_List(t *testing.T) {
	r, _, _ := newConvRouter()
	status, resp := convCall(t, r, http.MethodGet, "/api/v1/conversations", "")
	items, _ := resp["data"].([]any)
	if status != http.StatusOK || len(items) != 1 {
		t.Fatalf("应返回默认创作：%d %v", status, resp)
	}
	first := items[0].(map[string]any)
	if first["is_default"] != true || len(first["id"].(string)) != idcodec.EncodedLen {
		t.Errorf("默认创作和十六进制 ID 不对：%v", first)
	}
}

func TestConversationHandler_CreateRenameDelete(t *testing.T) {
	r, _, _ := newConvRouter()
	status, resp := convCall(t, r, http.MethodPost, "/api/v1/conversations", `{"title":"雨夜霓虹"}`)
	data, _ := resp["data"].(map[string]any)
	id, _ := data["id"].(string)
	if status != http.StatusOK || data["title"] != "雨夜霓虹" || id == "" {
		t.Fatalf("创建失败：%d %v", status, resp)
	}
	if status, resp = convCall(t, r, http.MethodPatch, "/api/v1/conversations/"+id, `{"title":"新名字"}`); status != http.StatusOK {
		t.Fatalf("改名失败：%d %v", status, resp)
	}
	// 参数校验：标题必填
	if status, resp = convCall(t, r, http.MethodPatch, "/api/v1/conversations/"+id, `{"title":""}`); status != http.StatusBadRequest || respCode(resp) != errcode.ErrInvalidParams.Code {
		t.Errorf("空标题应 400 + 10001：%d %v", status, resp)
	}
	// 非法 id 与不存在的 id 要区分
	if status, resp = convCall(t, r, http.MethodPatch, "/api/v1/conversations/abc", `{"title":"x"}`); status != http.StatusBadRequest || respCode(resp) != errcode.ErrInvalidParams.Code {
		t.Errorf("非法 ID 应 400 + 10001：%d %v", status, resp)
	}
	if status, resp = convCall(t, r, http.MethodPatch, "/api/v1/conversations/"+idcodec.Encode(999), `{"title":"x"}`); status != http.StatusNotFound || respCode(resp) != errcode.ErrConversationNotFound.Code {
		t.Errorf("不存在应 404 + 62001：%d %v", status, resp)
	}
	if status, _ = convCall(t, r, http.MethodDelete, "/api/v1/conversations/"+id, ""); status != http.StatusOK {
		t.Errorf("删除失败：%d", status)
	}
}

// 默认创作不能删除。
func TestConversationHandler_DeleteDefault(t *testing.T) {
	r, _, _ := newConvRouter()
	_, resp := convCall(t, r, http.MethodGet, "/api/v1/conversations", "")
	id := resp["data"].([]any)[0].(map[string]any)["id"].(string)
	status, resp := convCall(t, r, http.MethodDelete, "/api/v1/conversations/"+id, "")
	if status != http.StatusConflict || respCode(resp) != errcode.ErrConversationDefault.Code {
		t.Errorf("删默认创作应 409 + 62003：%d %v", status, resp)
	}
}

// 提交：default / new / 具体 id 三种目标；成功返回 202，带记录和任务快照。
func TestConversationHandler_Submit(t *testing.T) {
	r, repo, _ := newConvRouter()

	status, resp := convCall(t, r, http.MethodPost, "/api/v1/conversations/default/records", submitBody, "Idempotency-Key", "k1")
	data, _ := resp["data"].(map[string]any)
	if status != http.StatusAccepted || data == nil {
		t.Fatalf("提交到默认创作应 202：%d %v", status, resp)
	}
	rec := data["record"].(map[string]any)
	tasks := rec["tasks"].([]any)
	if len(tasks) != 2 || rec["quote_credits"] != float64(4) || len(data["conversation_id"].(string)) != idcodec.EncodedLen {
		t.Errorf("记录应有 2 个任务、冻结 4 积分：%v", data)
	}

	status, resp = convCall(t, r, http.MethodPost, "/api/v1/conversations/new/records", submitBody)
	if status != http.StatusAccepted || len(repo.convs) != 2 {
		t.Fatalf("提交到 new 应新建一段对话：%d %v", status, resp)
	}
	convID := resp["data"].(map[string]any)["conversation_id"].(string)
	if status, _ = convCall(t, r, http.MethodPost, "/api/v1/conversations/"+convID+"/records", submitBody); status != http.StatusAccepted {
		t.Errorf("提交到具体对话应 202：%d", status)
	}
}

func TestConversationHandler_SubmitValidation(t *testing.T) {
	r, _, _ := newConvRouter()
	cases := map[string]string{
		"缺少模型":     `{"kind":"image","prompt":"x","input":{},"count":1}`,
		"种类不支持":    `{"kind":"text","model_id":"m","input":{},"count":1}`,
		"数量为 0":    `{"kind":"image","model_id":"m","input":{},"count":0}`,
		"数量超过 4":   `{"kind":"image","model_id":"m","input":{},"count":5}`,
		"缺少 input": `{"kind":"image","model_id":"m","count":1}`,
	}
	for name, body := range cases {
		status, resp := convCall(t, r, http.MethodPost, "/api/v1/conversations/default/records", body)
		if status != http.StatusBadRequest || respCode(resp) != errcode.ErrInvalidParams.Code {
			t.Errorf("%s：应 400 + 10001，得到 %d %v", name, status, resp)
		}
	}
	// 目标既不是 default / new，也不是合法的十六进制串
	if status, resp := convCall(t, r, http.MethodPost, "/api/v1/conversations/xyz/records", submitBody); status != http.StatusBadRequest || respCode(resp) != errcode.ErrInvalidParams.Code {
		t.Errorf("非法目标应 400 + 10001：%d %v", status, resp)
	}
}

// 业务错误原样透出：模型下线 → 400 + 40003，且不留下记录。
func TestConversationHandler_SubmitBusinessError(t *testing.T) {
	r, repo, tasks := newConvRouter()
	tasks.createErr = errcode.ErrModelUnavailable
	status, resp := convCall(t, r, http.MethodPost, "/api/v1/conversations/default/records", submitBody)
	if status != http.StatusBadRequest || respCode(resp) != errcode.ErrModelUnavailable.Code {
		t.Fatalf("应 400 + 40003：%d %v", status, resp)
	}
	live := 0
	for id := range repo.records {
		if !repo.deleted[id] {
			live++
		}
	}
	if live != 0 {
		t.Errorf("失败的提交不应留下记录：%d", live)
	}
}

func TestConversationHandler_SubmitIdempotencyKeyTooLong(t *testing.T) {
	r, _, _ := newConvRouter()
	status, resp := convCall(t, r, http.MethodPost, "/api/v1/conversations/default/records", submitBody, "Idempotency-Key", strings.Repeat("a", 200))
	if status != http.StatusBadRequest || respCode(resp) != errcode.ErrInvalidParams.Code {
		t.Errorf("过长的幂等键应 400 + 10001：%d %v", status, resp)
	}
}

func TestConversationHandler_ListRecords(t *testing.T) {
	r, _, _ := newConvRouter()
	_, resp := convCall(t, r, http.MethodPost, "/api/v1/conversations/default/records", submitBody)
	convID := resp["data"].(map[string]any)["conversation_id"].(string)
	for i := 0; i < 2; i++ {
		convCall(t, r, http.MethodPost, "/api/v1/conversations/"+convID+"/records", submitBody)
	}

	status, resp := convCall(t, r, http.MethodGet, "/api/v1/conversations/"+convID+"/records?limit=2", "")
	page, _ := resp["data"].(map[string]any)
	items, _ := page["items"].([]any)
	if status != http.StatusOK || len(items) != 2 || page["next"] == nil {
		t.Fatalf("第一页应有 2 条并给出游标：%d %v", status, resp)
	}
	next := page["next"].(string)
	status, resp = convCall(t, r, http.MethodGet, "/api/v1/conversations/"+convID+"/records?limit=2&before="+next, "")
	items, _ = resp["data"].(map[string]any)["items"].([]any)
	if status != http.StatusOK || len(items) != 1 {
		t.Errorf("第二页应剩 1 条：%d %v", status, resp)
	}

	if status, resp = convCall(t, r, http.MethodGet, "/api/v1/conversations/"+convID+"/records?before=zzz", ""); status != http.StatusBadRequest || respCode(resp) != errcode.ErrInvalidParams.Code {
		t.Errorf("非法游标应 400 + 10001：%d %v", status, resp)
	}
	if status, resp = convCall(t, r, http.MethodGet, "/api/v1/conversations/"+idcodec.Encode(999)+"/records", ""); status != http.StatusNotFound || respCode(resp) != errcode.ErrConversationNotFound.Code {
		t.Errorf("不存在的对话应 404 + 62001：%d %v", status, resp)
	}
}

func TestConversationHandler_DeleteRecord(t *testing.T) {
	r, _, tasks := newConvRouter()
	_, resp := convCall(t, r, http.MethodPost, "/api/v1/conversations/default/records", submitBody)
	data := resp["data"].(map[string]any)
	convID, recID := data["conversation_id"].(string), data["record"].(map[string]any)["id"].(string)

	status, resp := convCall(t, r, http.MethodDelete, "/api/v1/conversations/"+convID+"/records/"+recID+"?cancel_active=true", "")
	if status != http.StatusOK || len(tasks.canceled) != 2 {
		t.Fatalf("cancel_active=true 应取消 2 个进行中的任务：%d %v %v", status, resp, tasks.canceled)
	}
	status, resp = convCall(t, r, http.MethodDelete, "/api/v1/conversations/"+convID+"/records/"+recID, "")
	if status != http.StatusNotFound || respCode(resp) != errcode.ErrRecordNotFound.Code {
		t.Errorf("重复删除应 404 + 62005：%d %v", status, resp)
	}
	if status, resp = convCall(t, r, http.MethodDelete, "/api/v1/conversations/"+convID+"/records/abc", ""); status != http.StatusBadRequest || respCode(resp) != errcode.ErrInvalidParams.Code {
		t.Errorf("非法记录 ID 应 400 + 10001：%d %v", status, resp)
	}
}
