package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// fakeAgentCanvasRepo 是内存版 AgentCanvasRepo：画布带 revision，提交时按乐观锁校验，
// beforeCommit 钩子用来在两次读写之间模拟「用户同时保存了画布」。
type fakeAgentCanvasRepo struct {
	mu           sync.Mutex
	canvas       *model.CanvasProject
	runs         map[uint64]*model.AgentRun
	muts         []model.AgentMutation
	commits      int
	beforeCommit func(r *fakeAgentCanvasRepo)
}

func newFakeAgentCanvasRepo(payload string) *fakeAgentCanvasRepo {
	return &fakeAgentCanvasRepo{
		canvas: &model.CanvasProject{BaseModel: model.BaseModel{ID: 7}, UserID: 1, Title: "画布", PayloadJSON: datatypes.JSON(payload), Revision: 1},
		runs:   map[uint64]*model.AgentRun{},
	}
}

func (f *fakeAgentCanvasRepo) GetCanvas(_ context.Context, userID, id uint64) (*model.CanvasProject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.canvas.UserID != userID || f.canvas.ID != id {
		return nil, repository.ErrNotFound
	}
	c := *f.canvas
	c.PayloadJSON = append(datatypes.JSON(nil), f.canvas.PayloadJSON...)
	return &c, nil
}

func (f *fakeAgentCanvasRepo) GetRun(_ context.Context, userID, id uint64) (*model.AgentRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runs[id]
	if !ok || r.UserID != userID {
		return nil, repository.ErrNotFound
	}
	c := *r
	return &c, nil
}

func (f *fakeAgentCanvasRepo) ListMutations(_ context.Context, runID uint64) ([]model.AgentMutation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.AgentMutation
	for _, m := range f.muts {
		if m.RunID == runID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeAgentCanvasRepo) CommitCanvasMutation(_ context.Context, in repository.CommitInput) error {
	if f.beforeCommit != nil {
		hook := f.beforeCommit
		f.beforeCommit = nil
		hook(f)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.canvas.UserID != in.UserID || f.canvas.ID != in.CanvasID {
		return repository.ErrNotFound
	}
	if f.canvas.Revision != in.BaseRevision {
		return repository.ErrRevisionConflict
	}
	f.commits++
	f.canvas.PayloadJSON = datatypes.JSON(in.Payload)
	f.canvas.Revision++
	m := in.Mutation
	m.ID = uint64(len(f.muts) + 1)
	m.Seq = 1
	for _, x := range f.muts {
		if x.RunID == m.RunID && x.Seq >= m.Seq {
			m.Seq = x.Seq + 1
		}
	}
	m.CanvasID, m.UserID, m.RevisionBefore, m.RevisionAfter = in.CanvasID, in.UserID, in.BaseRevision, f.canvas.Revision
	if in.UndoRunID != 0 {
		now := time.Now()
		for i := range f.muts {
			if f.muts[i].RunID == in.UndoRunID && f.muts[i].UndoneAt == nil && f.muts[i].Kind != model.MutationUndo {
				f.muts[i].UndoneAt = &now
			}
		}
	}
	f.muts = append(f.muts, *m)
	return nil
}

// userSaves 模拟用户在 Agent 读画布之后、写画布之前保存了一次：往画布里加一个节点，revision+1。
func userSaves(extraNodeID string) func(r *fakeAgentCanvasRepo) {
	return func(r *fakeAgentCanvasRepo) {
		r.mu.Lock()
		defer r.mu.Unlock()
		g, _ := canvasgraph.Parse(r.canvas.PayloadJSON)
		g.Nodes = append(g.Nodes, canvasgraph.Node{"id": extraNodeID, "type": "canvas", "position": map[string]any{"x": 0.0, "y": 900.0},
			"data": map[string]any{"kind": "image", "label": "用户加的"}})
		b, _ := g.Marshal()
		r.canvas.PayloadJSON = datatypes.JSON(b)
		r.canvas.Revision++
	}
}

const agentBase = `{"nodes":[{"id":"n_script","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"script","label":"剧本","prompt":"雨夜"}}],"edges":[],"viewport":{"x":0,"y":0,"zoom":1}}`

const buildOps = `[{"op":"create_node","tempId":"a","kind":"image","label":"角色"},{"op":"connect","source":"n_script","target":"a"}]`

type agentCanvasEnv struct {
	repo *fakeAgentCanvasRepo
	bc   *fakeTaskBroadcaster
	svc  *AgentCanvasService
	run  *model.AgentRun
}

func newAgentCanvasEnv(t *testing.T) *agentCanvasEnv {
	t.Helper()
	repo := newFakeAgentCanvasRepo(agentBase)
	bc := &fakeTaskBroadcaster{}
	run := &model.AgentRun{ID: 1, SessionID: 1, CanvasID: 7, UserID: 1, Status: model.RunRunning}
	repo.runs[1] = run
	return &agentCanvasEnv{repo: repo, bc: bc, svc: NewAgentCanvasService(repo, bc), run: run}
}

func (e *agentCanvasEnv) payload(t *testing.T) *canvasgraph.Graph {
	t.Helper()
	g, err := canvasgraph.Parse(e.repo.canvas.PayloadJSON)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestAgentApplyOps_SuccessWritesAndPushesPatch(t *testing.T) {
	e := newAgentCanvasEnv(t)
	res, err := e.svc.ApplyOps(context.Background(), e.run, "tc1", []byte(buildOps))
	if err != nil {
		t.Fatal(err)
	}
	if res.MutationID == 0 || res.Revision != 2 || res.IDMap["a"] == "" {
		t.Fatalf("res=%+v", res)
	}
	if e.repo.canvas.Revision != 2 || len(e.payload(t).Nodes) != 2 || len(e.payload(t).Edges) != 1 {
		t.Fatalf("画布没有写入: rev=%d", e.repo.canvas.Revision)
	}
	if e.repo.muts[0].Kind != model.MutationApplyOps || e.repo.muts[0].ToolCallID != "tc1" {
		t.Errorf("改动日志不对: %+v", e.repo.muts[0])
	}
	if len(e.bc.msgs) != 1 || e.bc.channels[0] != "user:1" || e.bc.msgs[0].Type != ws.TypeCanvasPatch {
		t.Fatalf("应向 user:1 推送一条 canvas.patch: %+v %v", e.bc.msgs, e.bc.channels)
	}
	raw, _ := json.Marshal(e.bc.msgs[0].Data)
	var data struct {
		MutationID     string `json:"mutation_id"`
		RunID          string `json:"run_id"`
		CanvasID       string `json:"canvas_id"`
		RevisionBefore uint64 `json:"revision_before"`
		RevisionAfter  uint64 `json:"revision_after"`
		Changes        []any  `json:"changes"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if data.RunID != idcodec.Encode(1) || data.CanvasID != idcodec.Encode(7) || len(data.MutationID) != idcodec.EncodedLen {
		t.Errorf("patch 里的 id 应是编码串，前端才能和自己手里的画布 id 比对: %s", raw)
	}
	if data.RevisionBefore != 1 || data.RevisionAfter != 2 || len(data.Changes) == 0 {
		t.Errorf("patch 内容不对: %s", raw)
	}
}

func TestAgentApplyOps_RetriesOnRevisionConflictKeepingUserChange(t *testing.T) {
	e := newAgentCanvasEnv(t)
	e.repo.beforeCommit = userSaves("n_user")
	res, err := e.svc.ApplyOps(context.Background(), e.run, "tc1", []byte(buildOps))
	if err != nil {
		t.Fatalf("冲突后应在最新画布上重试成功: %v", err)
	}
	if res.Revision != 3 {
		t.Errorf("用户保存了一次(rev 2)，Agent 写入应是 rev 3: %d", res.Revision)
	}
	g := e.payload(t)
	if g.Node("n_user") == nil {
		t.Error("用户同时保存的节点不能被覆盖")
	}
	if g.Node(res.IDMap["a"]) == nil || len(g.Edges) != 1 {
		t.Error("Agent 的编辑应在最新画布上重新应用")
	}
	if e.repo.commits != 1 {
		t.Errorf("成功提交应只有 1 次: %d", e.repo.commits)
	}
}

func TestAgentApplyOps_GivesUpAfterThreeConflicts(t *testing.T) {
	e := newAgentCanvasEnv(t)
	n := 0
	var hook func(r *fakeAgentCanvasRepo)
	hook = func(r *fakeAgentCanvasRepo) {
		n++
		userSaves("n_u" + string(rune('0'+n)))(r)
		r.beforeCommit = hook // 每次提交前都有人保存
	}
	e.repo.beforeCommit = hook
	_, err := e.svc.ApplyOps(context.Background(), e.run, "tc1", []byte(buildOps))
	var ec *errcode.Error
	if !errors.As(err, &ec) || ec.Code != errcode.ErrAgentWriteConflict.Code {
		t.Fatalf("应返回 60006: %v", err)
	}
	if e.repo.commits != 0 || len(e.bc.msgs) != 0 {
		t.Errorf("放弃时不能写入也不能推送: commits=%d msgs=%d", e.repo.commits, len(e.bc.msgs))
	}
}

func TestAgentApplyOps_ValidationErrorIsAtomic(t *testing.T) {
	e := newAgentCanvasEnv(t)
	_, err := e.svc.ApplyOps(context.Background(), e.run, "tc1", []byte(`[{"op":"create_node","tempId":"a","kind":"video"},{"op":"connect","source":"a","target":"n_script"}]`))
	var ve *canvasgraph.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("应返回 ValidationError: %v", err)
	}
	if e.repo.commits != 0 || e.repo.canvas.Revision != 1 || len(e.bc.msgs) != 0 {
		t.Error("校验失败时不能写入、不能推送")
	}
	if _, err := e.svc.ApplyOps(context.Background(), e.run, "tc1", []byte(`[{"op":"update_node","id":"n_script","src":"http://x"}]`)); err == nil {
		t.Error("产物字段应被拒绝")
	}
	if _, err := e.svc.ApplyOps(context.Background(), e.run, "tc1", []byte(`{"not":"array"}`)); err == nil {
		t.Error("格式不对应报错")
	}
}

func TestAgentApplyOps_OtherUsersCanvasIsNotFound(t *testing.T) {
	e := newAgentCanvasEnv(t)
	stranger := &model.AgentRun{ID: 9, CanvasID: 7, UserID: 2, Status: model.RunRunning}
	_, err := e.svc.ApplyOps(context.Background(), stranger, "tc1", []byte(buildOps))
	var ec *errcode.Error
	if !errors.As(err, &ec) || ec.Code != errcode.ErrCanvasNotFound.Code {
		t.Fatalf("应返回 30001: %v", err)
	}
}

func TestAgentApplyOps_NoChangeDoesNotBumpRevision(t *testing.T) {
	e := newAgentCanvasEnv(t)
	if _, err := e.svc.ApplyOps(context.Background(), e.run, "tc1", []byte(buildOps)); err != nil {
		t.Fatal(err)
	}
	g := e.payload(t)
	target := ""
	for _, n := range g.Nodes {
		if n.ID() != "n_script" {
			target = n.ID()
		}
	}
	e.bc.msgs, e.bc.channels = nil, nil
	res, err := e.svc.ApplyOps(context.Background(), e.run, "tc2", []byte(`[{"op":"connect","source":"n_script","target":"`+target+`"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if res.MutationID != 0 || e.repo.canvas.Revision != 2 || len(e.bc.msgs) != 0 {
		t.Errorf("重复连线什么都没变，不应提交、不应推送: %+v rev=%d", res, e.repo.canvas.Revision)
	}
}

func TestAgentArrangeAndDeleteApproved(t *testing.T) {
	e := newAgentCanvasEnv(t)
	built, err := e.svc.ApplyOps(context.Background(), e.run, "tc1", []byte(buildOps))
	if err != nil {
		t.Fatal(err)
	}
	a := built.IDMap["a"]
	if _, err := e.svc.Arrange(context.Background(), e.run, "tc2", canvasgraph.ArrangeTarget{NodeIDs: []string{"n_script", a}}, canvasgraph.LayoutColumn); err != nil {
		t.Fatal(err)
	}
	if e.repo.muts[len(e.repo.muts)-1].Kind != model.MutationArrange {
		t.Error("排列应记为 arrange")
	}
	if _, err := e.svc.Arrange(context.Background(), e.run, "tc2", canvasgraph.ArrangeTarget{NodeIDs: []string{"zzz"}}, canvasgraph.LayoutRow); err == nil {
		t.Error("不存在的节点应报错")
	}
	if _, err := e.svc.DeleteApproved(context.Background(), e.run, "tc3", []string{a}, nil); err != nil {
		t.Fatal(err)
	}
	g := e.payload(t)
	if g.Node(a) != nil || len(g.Edges) != 0 {
		t.Error("节点和它的连线应被删除")
	}
	if e.repo.muts[len(e.repo.muts)-1].Kind != model.MutationDelete {
		t.Error("删除应记为 delete")
	}
}

func TestAgentUndo(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*agentCanvasEnv, string) {
		e := newAgentCanvasEnv(t)
		res, err := e.svc.ApplyOps(ctx, e.run, "tc1", []byte(buildOps))
		if err != nil {
			t.Fatal(err)
		}
		e.repo.runs[1].Status = model.RunSucceeded
		return e, res.IDMap["a"]
	}

	t.Run("撤销后回到原样，原改动标记已撤销，并推送 patch", func(t *testing.T) {
		e, _ := setup(t)
		e.bc.msgs, e.bc.channels = nil, nil
		res, err := e.svc.Undo(ctx, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if res.Reverted == 0 || len(res.Skipped) != 0 {
			t.Fatalf("res=%+v", res)
		}
		g := e.payload(t)
		if len(g.Nodes) != 1 || len(g.Edges) != 0 {
			t.Errorf("应回到只有剧本: nodes=%d edges=%d", len(g.Nodes), len(g.Edges))
		}
		last := e.repo.muts[len(e.repo.muts)-1]
		if last.Kind != model.MutationUndo || e.repo.muts[0].UndoneAt == nil {
			t.Errorf("应记录 undo 并标记原改动: %+v", e.repo.muts)
		}
		if len(e.bc.msgs) != 1 || e.bc.msgs[0].Type != ws.TypeCanvasPatch {
			t.Errorf("撤销也要推 patch: %+v", e.bc.msgs)
		}
	})

	t.Run("不能撤销两次", func(t *testing.T) {
		e, _ := setup(t)
		if _, err := e.svc.Undo(ctx, 1, 1); err != nil {
			t.Fatal(err)
		}
		_, err := e.svc.Undo(ctx, 1, 1)
		var ec *errcode.Error
		if !errors.As(err, &ec) || ec.Code != errcode.ErrAgentAlreadyUndone.Code {
			t.Errorf("应返回 60007: %v", err)
		}
	})

	t.Run("运行中不能撤销", func(t *testing.T) {
		e, _ := setup(t)
		e.repo.runs[1].Status = model.RunRunning
		_, err := e.svc.Undo(ctx, 1, 1)
		var ec *errcode.Error
		if !errors.As(err, &ec) || ec.Code != errcode.ErrAgentState.Code {
			t.Errorf("应返回 60003: %v", err)
		}
	})

	t.Run("别人的运行返回不存在", func(t *testing.T) {
		e, _ := setup(t)
		_, err := e.svc.Undo(ctx, 2, 1)
		var ec *errcode.Error
		if !errors.As(err, &ec) || ec.Code != errcode.ErrAgentRunMissing.Code {
			t.Errorf("应返回 60012: %v", err)
		}
	})

	t.Run("用户改过的节点保留并列出", func(t *testing.T) {
		e, a := setup(t)
		g := e.payload(t)
		g.Node(a).Data()["label"] = "我改的名"
		b, _ := g.Marshal()
		e.repo.canvas.PayloadJSON = datatypes.JSON(b)
		e.repo.canvas.Revision++

		res, err := e.svc.Undo(ctx, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Skipped) != 1 || res.Skipped[0].NodeID != a {
			t.Fatalf("应跳过被改过的节点: %+v", res.Skipped)
		}
		after := e.payload(t)
		if after.Node(a) == nil || after.Node(a).Label() != "我改的名" {
			t.Error("用户改过的节点必须保留")
		}
	})

	t.Run("撤销时用户同时保存：在最新画布上重试", func(t *testing.T) {
		e, _ := setup(t)
		e.repo.beforeCommit = userSaves("n_user")
		if _, err := e.svc.Undo(ctx, 1, 1); err != nil {
			t.Fatal(err)
		}
		g := e.payload(t)
		if g.Node("n_user") == nil || len(g.Nodes) != 2 {
			t.Errorf("用户新加的节点要保留，Agent 的撤掉: %d 个节点", len(g.Nodes))
		}
	})

	t.Run("所有东西都被跳过：不写入，列出原因，改动仍可再次撤销", func(t *testing.T) {
		e, a := setup(t)
		g := e.payload(t)
		g.Node(a).Data()["label"] = "我改的名" // 唯一的新节点被用户改过
		b, _ := g.Marshal()
		e.repo.canvas.PayloadJSON = datatypes.JSON(b)
		e.repo.canvas.Revision++
		commits, rev := e.repo.commits, e.repo.canvas.Revision

		res, err := e.svc.Undo(ctx, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if res.Reverted != 0 || len(res.Skipped) == 0 {
			t.Fatalf("应什么都没撤，只列出跳过项: %+v", res)
		}
		if e.repo.commits != commits || e.repo.canvas.Revision != rev || e.repo.muts[0].UndoneAt != nil {
			t.Error("没有实际变化时不应写入，也不应把改动标记为已撤销")
		}
		if _, err := e.svc.Undo(ctx, 1, 1); err != nil {
			t.Errorf("用户处理完冲突后应还能再撤销: %v", err)
		}
	})

	t.Run("没有任何改动的运行：什么都不做", func(t *testing.T) {
		e := newAgentCanvasEnv(t)
		e.repo.runs[1].Status = model.RunSucceeded
		res, err := e.svc.Undo(ctx, 1, 1)
		if err != nil || res.Reverted != 0 || e.repo.commits != 0 {
			t.Errorf("res=%+v err=%v commits=%d", res, err, e.repo.commits)
		}
	})
}

func TestAgentCatalogAndDetail(t *testing.T) {
	e := newAgentCanvasEnv(t)
	cat, err := e.svc.Catalog(context.Background(), e.run, canvasgraph.CatalogOptions{Priority: []string{"n_script"}})
	if err != nil || cat.Total != 1 || cat.Nodes[0].ID != "n_script" {
		t.Fatalf("cat=%+v err=%v", cat, err)
	}
	d, err := e.svc.Detail(context.Background(), e.run, []string{"n_script"})
	if err != nil || len(d.Nodes) != 1 {
		t.Fatalf("detail=%+v err=%v", d, err)
	}
	if _, err := e.svc.Detail(context.Background(), e.run, []string{"nope"}); err == nil {
		t.Error("不存在的节点应报错")
	}
}
