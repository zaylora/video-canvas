package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// convRepo 是内存版对话仓储，只实现对话服务用到的行为。
type convRepo struct {
	next    uint64
	convs   map[uint64]*model.Conversation
	records map[uint64]*model.ConversationRecord
	deleted map[uint64]bool // 被软删除的记录
	// activeRecordConvs 模拟「这些记录 id 属于哪个对话」，供活跃标记查询
}

func newConvRepo() *convRepo {
	return &convRepo{convs: map[uint64]*model.Conversation{}, records: map[uint64]*model.ConversationRecord{}, deleted: map[uint64]bool{}}
}

func (r *convRepo) id() uint64 { r.next++; return r.next }

func (r *convRepo) Create(_ context.Context, c *model.Conversation) error {
	c.ID = r.id()
	cp := *c
	r.convs[c.ID] = &cp
	return nil
}

func (r *convRepo) GetByID(_ context.Context, userID, id uint64) (*model.Conversation, error) {
	c, ok := r.convs[id]
	if !ok || c.UserID != userID {
		return nil, repository.ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (r *convRepo) List(_ context.Context, userID uint64) ([]model.Conversation, error) {
	var out []model.Conversation
	for _, c := range r.convs {
		if c.UserID == userID {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (r *convRepo) Count(_ context.Context, userID uint64) (int64, error) {
	var n int64
	for _, c := range r.convs {
		if c.UserID == userID {
			n++
		}
	}
	return n, nil
}

func (r *convRepo) Rename(_ context.Context, userID, id uint64, title string) error {
	c, ok := r.convs[id]
	if !ok || c.UserID != userID {
		return repository.ErrNotFound
	}
	c.Title = title
	return nil
}

func (r *convRepo) Delete(_ context.Context, userID, id uint64) error {
	c, ok := r.convs[id]
	if !ok || c.UserID != userID {
		return repository.ErrNotFound
	}
	delete(r.convs, id)
	return nil
}

func (r *convRepo) AddRecordCount(_ context.Context, id uint64, delta int, at *time.Time) error {
	c, ok := r.convs[id]
	if !ok {
		return repository.ErrNotFound
	}
	c.RecordCount += delta
	if at != nil {
		c.LastRecordAt = at
	}
	return nil
}

func (r *convRepo) CreateRecord(_ context.Context, rec *model.ConversationRecord) error {
	rec.ID = r.id()
	cp := *rec
	r.records[rec.ID] = &cp
	return nil
}

func (r *convRepo) GetRecord(_ context.Context, userID, id uint64) (*model.ConversationRecord, error) {
	rec, ok := r.records[id]
	if !ok || rec.UserID != userID || r.deleted[id] {
		return nil, repository.ErrNotFound
	}
	cp := *rec
	return &cp, nil
}

func (r *convRepo) FindRecordByIdem(_ context.Context, userID uint64, key string) (*model.ConversationRecord, error) {
	for id, rec := range r.records {
		if rec.UserID == userID && rec.IdempotencyKey == key && !r.deleted[id] {
			cp := *rec
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *convRepo) ListRecords(_ context.Context, userID, convID, before uint64, limit int) ([]model.ConversationRecord, error) {
	var out []model.ConversationRecord
	for id, rec := range r.records {
		if rec.UserID != userID || rec.ConversationID != convID || r.deleted[id] || (before > 0 && id >= before) {
			continue
		}
		out = append(out, *rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *convRepo) SaveRecordTasks(_ context.Context, id uint64, taskIDs, submitErrors datatypes.JSON, quote int) error {
	rec, ok := r.records[id]
	if !ok {
		return repository.ErrNotFound
	}
	rec.TaskIDsJSON, rec.SubmitErrorsJSON, rec.QuoteCredits = taskIDs, submitErrors, quote
	return nil
}

func (r *convRepo) DeleteRecord(_ context.Context, userID, id uint64) error {
	rec, ok := r.records[id]
	if !ok || rec.UserID != userID || r.deleted[id] {
		return repository.ErrNotFound
	}
	r.deleted[id] = true
	return nil
}

func (r *convRepo) ConversationIDsByRecordIDs(_ context.Context, userID uint64, recordIDs []uint64) ([]uint64, error) {
	seen := map[uint64]bool{}
	var out []uint64
	for _, id := range recordIDs {
		if rec, ok := r.records[id]; ok && rec.UserID == userID && !r.deleted[id] && !seen[rec.ConversationID] {
			seen[rec.ConversationID] = true
			out = append(out, rec.ConversationID)
		}
	}
	return out, nil
}

// convTasks 是假的生成任务服务：按配置返回逐项结果，并记录调用。
type convTasks struct {
	next      uint64
	creates   []*model.CreateGenerationTaskReq
	keys      []string
	createErr error
	failIdx   map[int]*errcode.Error // 第 i 个节点提交失败
	credits   int                    // 每个任务冻结的积分
	views     map[uint64]*model.GenerationTaskView
	canceled  []uint64
	active    []model.GenerationTaskView
}

func newConvTasks() *convTasks {
	return &convTasks{credits: 3, views: map[uint64]*model.GenerationTaskView{}, failIdx: map[int]*errcode.Error{}}
}

func (t *convTasks) Create(_ context.Context, _ uint64, key string, req *model.CreateGenerationTaskReq) (*model.CreateGenerationTaskResp, error) {
	t.creates = append(t.creates, req)
	t.keys = append(t.keys, key)
	if t.createErr != nil {
		return nil, t.createErr
	}
	resp := &model.CreateGenerationTaskResp{}
	for i, node := range req.NodeIDs {
		if e := t.failIdx[i]; e != nil {
			resp.Items = append(resp.Items, model.CreateTaskItem{NodeID: node, Error: &model.TaskItemError{Status: e.HTTPStatus(), Code: e.Code, Message: e.Msg}})
			continue
		}
		t.next++
		v := &model.GenerationTaskView{ID: 1000 + t.next, NodeID: node, Kind: req.Kind, ModelID: req.ModelID, Status: model.TaskQueued, Credits: t.credits}
		t.views[v.ID] = v
		resp.Items = append(resp.Items, model.CreateTaskItem{NodeID: node, Task: v})
	}
	return resp, nil
}

func (t *convTasks) Cancel(_ context.Context, _ uint64, id uint64) (*model.GenerationTaskView, error) {
	t.canceled = append(t.canceled, id)
	return t.views[id], nil
}

func (t *convTasks) ViewsByIDs(_ context.Context, _ uint64, ids []uint64) ([]model.GenerationTaskView, error) {
	var out []model.GenerationTaskView
	for _, id := range ids {
		if v, ok := t.views[id]; ok {
			out = append(out, *v)
		}
	}
	return out, nil
}

func (t *convTasks) List(_ context.Context, _ uint64, _ *model.ListGenerationTaskReq) ([]model.GenerationTaskView, error) {
	return t.active, nil
}

func newConvSvc() (*service.ConversationService, *convRepo, *convTasks) {
	repo, tasks := newConvRepo(), newConvTasks()
	return service.NewConversationService(repo, tasks), repo, tasks
}

func submitReq(count int) *model.SubmitConversationRecordReq {
	return &model.SubmitConversationRecordReq{Kind: "image", ModelID: "m1", Prompt: "雨夜的城市，外卖骑手穿过霓虹路口", Input: map[string]any{"ratio": "16:9"}, Count: count}
}

func wantConvErr(t *testing.T, err error, want *errcode.Error) {
	t.Helper()
	var e *errcode.Error
	if !errors.As(err, &e) || e.Code != want.Code {
		t.Fatalf("应返回业务错误 %d，实际 %v", want.Code, err)
	}
}

// 列表：没有任何对话时是空的（不再自动创建「默认创作」），只返回自己的对话，所有对话地位相同。
func TestConversationService_ListHasNoSpecialConversation(t *testing.T) {
	svc, _, _ := newConvSvc()
	ctx := context.Background()
	if items, err := svc.List(ctx, 1); err != nil || len(items) != 0 {
		t.Fatalf("没有对话时列表应为空：%+v %v", items, err)
	}
	for _, title := range []string{"雨夜霓虹", "海边日落"} {
		if _, err := svc.Create(ctx, 1, &model.CreateConversationReq{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := svc.List(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Title != "海边日落" || items[1].Title != "雨夜霓虹" {
		t.Fatalf("应只有自己建的两段对话，新的在前：%+v", items)
	}
	if other, _ := svc.List(ctx, 2); len(other) != 0 {
		t.Errorf("别的用户看不到这些对话：%+v", other)
	}
}

// 列表：有进行中任务的对话标记 active，靠任务的 node_id 反查记录所属的对话。
func TestConversationService_ListMarksActive(t *testing.T) {
	svc, repo, tasks := newConvSvc()
	ctx := context.Background()
	if _, err := svc.Submit(ctx, 1, service.ConversationTarget{New: true}, "k1", submitReq(1)); err != nil {
		t.Fatal(err)
	}
	var recID uint64
	for id := range repo.records {
		recID = id
	}
	tasks.active = []model.GenerationTaskView{
		{ID: 1001, NodeID: fmt.Sprintf("rec:%d:0", recID)},
		{ID: 9, NodeID: "n-from-canvas"}, // 画布节点的任务不影响对话
	}
	items, err := svc.List(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !items[0].Active {
		t.Errorf("有进行中的任务，应标记 active：%+v", items[0])
	}
}

func TestConversationService_Create(t *testing.T) {
	cases := []struct {
		name  string
		title string
		want  string
	}{
		{"不传标题叫新对话", "", "新对话"},
		{"标题去掉首尾空白", "  雨夜霓虹  ", "雨夜霓虹"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, _, _ := newConvSvc()
			v, err := svc.Create(context.Background(), 1, &model.CreateConversationReq{Title: c.title})
			if err != nil || v.Title != c.want {
				t.Fatalf("得到 %+v %v", v, err)
			}
		})
	}
}

func TestConversationService_CreateLimit(t *testing.T) {
	svc, repo, _ := newConvSvc()
	for i := 0; i < service.MaxConversations; i++ {
		_ = repo.Create(context.Background(), &model.Conversation{UserID: 1, Title: "x"})
	}
	_, err := svc.Create(context.Background(), 1, &model.CreateConversationReq{})
	wantConvErr(t, err, errcode.ErrConversationLimit)
}

func TestConversationService_Rename(t *testing.T) {
	svc, _, _ := newConvSvc()
	ctx := context.Background()
	v, _ := svc.Create(ctx, 1, &model.CreateConversationReq{Title: "a"})
	id := uint64(v.ID)

	if err := svc.Rename(ctx, 1, id, "  新名字 "); err != nil {
		t.Fatalf("改名失败：%v", err)
	}
	if items, _ := svc.List(ctx, 1); items[0].Title != "新名字" {
		t.Errorf("标题应已更新并去掉空白：%+v", items)
	}
	wantConvErr(t, svc.Rename(ctx, 1, id, "   "), errcode.ErrInvalidParams)
	wantConvErr(t, svc.Rename(ctx, 2, id, "x"), errcode.ErrConversationNotFound) // 别人的对话当作不存在
	wantConvErr(t, svc.Rename(ctx, 1, 999, "x"), errcode.ErrConversationNotFound)
}

func TestConversationService_Delete(t *testing.T) {
	svc, _, _ := newConvSvc()
	ctx := context.Background()
	v, _ := svc.Create(ctx, 1, &model.CreateConversationReq{Title: "a"})
	wantConvErr(t, svc.Delete(ctx, 2, uint64(v.ID)), errcode.ErrConversationNotFound)
	if err := svc.Delete(ctx, 1, uint64(v.ID)); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	wantConvErr(t, svc.Delete(ctx, 1, uint64(v.ID)), errcode.ErrConversationNotFound)
}

// 提交到已有对话：建记录、按 rec:{记录id}:{序号} 给任务节点 id、保存任务 id 和冻结积分、更新对话计数。
func TestConversationService_SubmitToExisting(t *testing.T) {
	svc, repo, tasks := newConvSvc()
	ctx := context.Background()
	v, _ := svc.Create(ctx, 1, &model.CreateConversationReq{Title: "a"})

	resp, err := svc.Submit(ctx, 1, service.ConversationTarget{ID: uint64(v.ID)}, "key-1", submitReq(2))
	if err != nil {
		t.Fatalf("提交失败：%v", err)
	}
	rec := resp.Record
	if rec.Count != 2 || len(rec.Tasks) != 2 || rec.Tasks[0] == nil || rec.Tasks[1] == nil || rec.QuoteCredits != 6 {
		t.Fatalf("记录应有 2 个任务、冻结 6 积分：%+v", rec)
	}
	req := tasks.creates[0]
	wantNodes := []string{fmt.Sprintf("rec:%d:0", uint64(rec.ID)), fmt.Sprintf("rec:%d:1", uint64(rec.ID))}
	if req.Kind != "image" || req.ModelID != "m1" || strings.Join(req.NodeIDs, ",") != strings.Join(wantNodes, ",") || req.CanvasID != 0 {
		t.Errorf("提交给任务服务的参数不对：%+v", req)
	}
	if tasks.keys[0] != "key-1" {
		t.Errorf("幂等键应原样传给任务服务：%v", tasks.keys)
	}
	stored := repo.records[uint64(rec.ID)]
	var ids []*uint64
	_ = json.Unmarshal(stored.TaskIDsJSON, &ids)
	if len(ids) != 2 || ids[0] == nil || *ids[0] != 1001 || stored.Prompt != submitReq(2).Prompt || !strings.Contains(string(stored.InputJSON), "16:9") {
		t.Errorf("库里应保存任务 id 和完整输入快照：%+v %s", ids, stored.InputJSON)
	}
	conv := repo.convs[uint64(resp.ConversationID)]
	if resp.ConversationID != v.ID || conv.RecordCount != 1 || conv.LastRecordAt == nil {
		t.Errorf("对话的计数应更新：%+v", conv)
	}
}

// 提交到 new：同时创建对话，标题取提示词前 16 个字（按字不按字节），也可以显式传标题。
func TestConversationService_SubmitToNew(t *testing.T) {
	svc, repo, _ := newConvSvc()
	ctx := context.Background()

	long := submitReq(1)
	long.Prompt = "雨夜的城市，外卖骑手穿过霓虹路口，最后停在一扇亮着灯的窗前"
	resp, err := svc.Submit(ctx, 1, service.ConversationTarget{New: true}, "", long)
	if err != nil {
		t.Fatal(err)
	}
	conv := repo.convs[uint64(resp.ConversationID)]
	if conv.Title != "雨夜的城市，外卖骑手穿过霓虹路口" {
		t.Errorf("标题应取提示词前 16 个字：%q", conv.Title)
	}

	req := submitReq(1)
	req.Title = "雨夜霓虹"
	resp, _ = svc.Submit(ctx, 1, service.ConversationTarget{New: true}, "", req)
	if repo.convs[uint64(resp.ConversationID)].Title != "雨夜霓虹" {
		t.Errorf("显式标题优先")
	}

	empty := submitReq(1)
	empty.Prompt = "   "
	resp, _ = svc.Submit(ctx, 1, service.ConversationTarget{New: true}, "", empty)
	if repo.convs[uint64(resp.ConversationID)].Title != "新对话" {
		t.Errorf("没有提示词时叫新对话")
	}
}

func TestConversationService_SubmitNewHitsLimit(t *testing.T) {
	svc, repo, tasks := newConvSvc()
	for i := 0; i < service.MaxConversations; i++ {
		_ = repo.Create(context.Background(), &model.Conversation{UserID: 1, Title: "x"})
	}
	_, err := svc.Submit(context.Background(), 1, service.ConversationTarget{New: true}, "", submitReq(1))
	wantConvErr(t, err, errcode.ErrConversationLimit)
	if len(tasks.creates) != 0 {
		t.Errorf("超过上限时不应创建任务")
	}
}

// 只能提交到自己的对话；别人的和不存在的一样返回「对话不存在」。
func TestConversationService_SubmitToOthers(t *testing.T) {
	svc, _, tasks := newConvSvc()
	ctx := context.Background()
	v, _ := svc.Create(ctx, 1, &model.CreateConversationReq{Title: "a"})

	_, err := svc.Submit(ctx, 2, service.ConversationTarget{ID: uint64(v.ID)}, "", submitReq(1))
	wantConvErr(t, err, errcode.ErrConversationNotFound)
	if len(tasks.creates) != 0 {
		t.Errorf("不应创建任务")
	}
}

// 部分失败：失败的格子 task 为 null，原因记进 submit_errors；已创建的任务保留，记录仍然创建。
func TestConversationService_SubmitPartialFailure(t *testing.T) {
	svc, _, tasks := newConvSvc()
	tasks.failIdx[1] = errcode.ErrInsufficientCredits

	resp, err := svc.Submit(context.Background(), 1, service.ConversationTarget{New: true}, "", submitReq(2))
	if err != nil {
		t.Fatalf("部分失败不应让整个请求失败：%v", err)
	}
	rec := resp.Record
	if rec.Tasks[0] == nil || rec.Tasks[1] != nil || rec.QuoteCredits != 3 {
		t.Fatalf("第 1 格成功、第 2 格为 null，冻结只算成功的：%+v", rec)
	}
	if len(rec.SubmitErrors) != 1 || rec.SubmitErrors[0].Index != 1 || rec.SubmitErrors[0].Code != errcode.ErrInsufficientCredits.Code {
		t.Errorf("应记录第 2 格的失败原因：%+v", rec.SubmitErrors)
	}
}

// 请求级错误（参数不合法、模型下线）：一个任务也没建，回收刚建的记录和对话，原样返回错误。
func TestConversationService_SubmitRequestLevelErrorCleansUp(t *testing.T) {
	svc, repo, tasks := newConvSvc()
	tasks.createErr = errcode.ErrModelUnavailable

	_, err := svc.Submit(context.Background(), 1, service.ConversationTarget{New: true}, "", submitReq(1))
	wantConvErr(t, err, errcode.ErrModelUnavailable)
	live := 0
	for id := range repo.records {
		if !repo.deleted[id] {
			live++
		}
	}
	if live != 0 || len(repo.convs) != 0 {
		t.Errorf("失败后不应留下记录和新建的对话：记录 %d 对话 %d", live, len(repo.convs))
	}

	// 提交到已有对话失败时，对话本身要保留
	svc2, repo2, tasks2 := newConvSvc()
	v, _ := svc2.Create(context.Background(), 1, &model.CreateConversationReq{Title: "a"})
	tasks2.createErr = errcode.ErrModelUnavailable
	_, _ = svc2.Submit(context.Background(), 1, service.ConversationTarget{ID: uint64(v.ID)}, "", submitReq(1))
	if _, ok := repo2.convs[uint64(v.ID)]; !ok {
		t.Errorf("已有对话不应被回收")
	}
}

// 幂等：同一个 Idempotency-Key 重复提交，返回同一条记录，不会再建任务。
func TestConversationService_SubmitIdempotent(t *testing.T) {
	svc, _, tasks := newConvSvc()
	ctx := context.Background()
	first, err := svc.Submit(ctx, 1, service.ConversationTarget{New: true}, "same", submitReq(1))
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Submit(ctx, 1, service.ConversationTarget{New: true}, "same", submitReq(1))
	if err != nil || second.Record.ID != first.Record.ID || second.ConversationID != first.ConversationID {
		t.Fatalf("重复提交应返回同一条记录：%+v %v", second, err)
	}
	if len(tasks.creates) != 1 {
		t.Errorf("任务只应创建一次：%d", len(tasks.creates))
	}
	if second.Record.Tasks[0] == nil {
		t.Errorf("重复提交也应带上任务快照")
	}
}

func TestConversationService_ListRecordsPaging(t *testing.T) {
	svc, _, tasks := newConvSvc()
	ctx := context.Background()
	tasks.failIdx[1] = errcode.ErrTooManyTasks
	conv, _ := svc.Create(ctx, 1, &model.CreateConversationReq{Title: "a"})
	convID := conv.ID
	for i := 0; i < 3; i++ {
		if _, err := svc.Submit(ctx, 1, service.ConversationTarget{ID: uint64(convID)}, "", submitReq(2)); err != nil {
			t.Fatal(err)
		}
	}

	page, err := svc.ListRecords(ctx, 1, uint64(convID), &model.ListConversationRecordsReq{Limit: 2}, 0)
	if err != nil || len(page.Items) != 2 || page.Next == nil {
		t.Fatalf("第一页应有 2 条并给出下一页游标：%+v %v", page, err)
	}
	if page.Items[0].ID < page.Items[1].ID {
		t.Errorf("每页按新到旧排列")
	}
	if page.Items[0].Tasks[0] == nil || page.Items[0].Tasks[1] != nil || len(page.Items[0].SubmitErrors) != 1 {
		t.Errorf("任务快照应按格子对齐，失败的格子是 null：%+v", page.Items[0])
	}
	page2, err := svc.ListRecords(ctx, 1, uint64(convID), &model.ListConversationRecordsReq{Limit: 2}, uint64(*page.Next))
	if err != nil || len(page2.Items) != 1 || page2.Next != nil {
		t.Fatalf("第二页应剩 1 条且没有下一页：%+v %v", page2, err)
	}

	_, err = svc.ListRecords(ctx, 2, uint64(convID), &model.ListConversationRecordsReq{}, 0)
	wantConvErr(t, err, errcode.ErrConversationNotFound)
}

func TestConversationService_ListRecordsLimitDefaults(t *testing.T) {
	svc, repo, _ := newConvSvc()
	ctx := context.Background()
	conv, _ := svc.Create(ctx, 1, &model.CreateConversationReq{Title: "a"})
	convID := uint64(conv.ID)
	for i := 0; i < 25; i++ {
		_ = repo.CreateRecord(ctx, &model.ConversationRecord{ConversationID: convID, UserID: 1, Kind: "image", Count: 1})
	}
	page, _ := svc.ListRecords(ctx, 1, convID, &model.ListConversationRecordsReq{}, 0)
	if len(page.Items) != 20 {
		t.Errorf("默认每页 20 条：%d", len(page.Items))
	}
	page, _ = svc.ListRecords(ctx, 1, convID, &model.ListConversationRecordsReq{Limit: 500}, 0)
	if len(page.Items) != 25 {
		t.Errorf("最多每页 50 条，这里一共 25 条应全部返回：%d", len(page.Items))
	}
}

// 删除记录：cancel_active=true 时取消仍在进行的任务（已结束的不碰），并把对话的记录数减 1。
func TestConversationService_DeleteRecord(t *testing.T) {
	svc, repo, tasks := newConvSvc()
	ctx := context.Background()
	resp, _ := svc.Submit(ctx, 1, service.ConversationTarget{New: true}, "", submitReq(2))
	tasks.views[resp.Record.Tasks[1].ID].Status = model.TaskSucceeded

	if err := svc.DeleteRecord(ctx, 1, uint64(resp.ConversationID), uint64(resp.Record.ID), true); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	if len(tasks.canceled) != 1 || tasks.canceled[0] != resp.Record.Tasks[0].ID {
		t.Errorf("只应取消进行中的任务：%v", tasks.canceled)
	}
	if repo.convs[uint64(resp.ConversationID)].RecordCount != 0 {
		t.Errorf("记录数应减回 0")
	}
	wantConvErr(t, svc.DeleteRecord(ctx, 1, uint64(resp.ConversationID), uint64(resp.Record.ID), true), errcode.ErrRecordNotFound)
}

func TestConversationService_DeleteRecordWithoutCancelKeepsTasks(t *testing.T) {
	svc, _, tasks := newConvSvc()
	ctx := context.Background()
	resp, _ := svc.Submit(ctx, 1, service.ConversationTarget{New: true}, "", submitReq(1))
	if err := svc.DeleteRecord(ctx, 1, uint64(resp.ConversationID), uint64(resp.Record.ID), false); err != nil {
		t.Fatal(err)
	}
	if len(tasks.canceled) != 0 {
		t.Errorf("没带 cancel_active 时进行中的任务继续跑：%v", tasks.canceled)
	}
}

func TestConversationService_DeleteRecordGuards(t *testing.T) {
	svc, _, _ := newConvSvc()
	ctx := context.Background()
	resp, _ := svc.Submit(ctx, 1, service.ConversationTarget{New: true}, "", submitReq(1))
	conv, rec := uint64(resp.ConversationID), uint64(resp.Record.ID)

	wantConvErr(t, svc.DeleteRecord(ctx, 2, conv, rec, false), errcode.ErrConversationNotFound)
	other, _ := svc.Create(ctx, 1, &model.CreateConversationReq{Title: "b"})
	wantConvErr(t, svc.DeleteRecord(ctx, 1, uint64(other.ID), rec, false), errcode.ErrRecordNotFound) // 记录不在这个对话里
}

func TestConversationService_SubmitRejectsLongIdempotencyKey(t *testing.T) {
	svc, _, tasks := newConvSvc()
	_, err := svc.Submit(context.Background(), 1, service.ConversationTarget{New: true}, strings.Repeat("a", 200), submitReq(1))
	wantConvErr(t, err, errcode.ErrInvalidParams)
	if len(tasks.creates) != 0 {
		t.Errorf("不应创建任务")
	}
}
