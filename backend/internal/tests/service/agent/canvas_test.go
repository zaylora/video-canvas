package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service/agent"
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

func TestAgentApplyOps_ReportsCreatedAndUpdatedNodes(t *testing.T) {
	e := newAgentCanvasEnv(t)
	res, err := e.svc.ApplyOps(context.Background(), e.run, "tc1", []byte(buildOps))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 1 || res.Created[0] != res.IDMap["a"] || len(res.Updated) != 0 {
		t.Fatalf("新建一个节点、连一条线：created 只有新节点，连线不算: %+v", res)
	}
	res, err = e.svc.ApplyOps(context.Background(), e.run, "tc2", []byte(`[{"op":"update_node","id":"n_script","prompt":"黄昏"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 0 || len(res.Updated) != 1 || res.Updated[0] != "n_script" {
		t.Fatalf("改提示词：updated 是被改的节点: %+v", res)
	}
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

// ---- 写入幂等：调用方带幂等键（阶段 3 里是 Pi 的工具任务 id）时，同一个 (运行, 键, 种类) 重复到达不再写画布 ----
//
// 不能用模型给的 tool_call_id 去重：有些兼容网关每个回答都从 call_0 开始编号，同一个运行的不同回合会出现相同的 id，
// 那样会悄悄丢掉真实的写入。所以幂等是显式的、可选的，旧路径不带键，行为不变。

func keyed(key string) context.Context { return WithIdempotencyKey(context.Background(), key) }

func TestAgentApplyOps_ReplayWithSameKeyReturnsOriginalResult(t *testing.T) {
	e := newAgentCanvasEnv(t)
	first, err := e.svc.ApplyOps(keyed("k1"), e.run, "tc1", []byte(buildOps))
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.svc.ApplyOps(keyed("k1"), e.run, "tc1", []byte(buildOps))
	if err != nil {
		t.Fatal(err)
	}
	if e.repo.commits != 1 || len(e.repo.muts) != 1 {
		t.Fatalf("重放不能再写一次：commits=%d muts=%d", e.repo.commits, len(e.repo.muts))
	}
	if len(e.payload(t).Nodes) != 2 || e.repo.canvas.Revision != 2 {
		t.Fatalf("画布只该多一个节点、revision 只加一次: nodes=%d rev=%d", len(e.payload(t).Nodes), e.repo.canvas.Revision)
	}
	// 模型靠 tempId→真实 id 的映射去引用刚建的节点，重放必须原样还原，否则它拿到的 id 是错的
	if first.IDMap["a"] == "" || again.IDMap["a"] != first.IDMap["a"] {
		t.Errorf("id_map 要原样还原: %v / %v", first.IDMap, again.IDMap)
	}
	if again.MutationID != first.MutationID || again.Revision != first.Revision || again.Changes != first.Changes {
		t.Errorf("重放的结果要和第一次一致: %+v / %+v", first, again)
	}
	if len(again.Created) != 1 || again.Created[0] != first.Created[0] {
		t.Errorf("新建的节点列表要还原: %v / %v", first.Created, again.Created)
	}
	if len(e.bc.msgs) != 1 {
		t.Errorf("重放不能再推一次 canvas.patch: %d", len(e.bc.msgs))
	}
}

func TestAgentApplyOps_ReplayKeepsWarnings(t *testing.T) {
	e := newAgentCanvasEnv(t)
	e.svc.SetModelOps(func(context.Context) (map[string][]string, error) {
		return map[string][]string{"veo": {"t2v", "i2v"}}, nil
	})
	// 文生视频却连了参考图：写入照常成功，但结果里带一条提醒（同 mode_test.go）。提醒文字里带着节点 id，所以必须原样存下来还原
	ops := `[{"op":"create_node","tempId":"i","kind":"image","label":"角色"},` +
		`{"op":"create_node","tempId":"v","kind":"video","label":"镜头","model":"veo","mode":"t2v"},` +
		`{"op":"connect","source":"i","target":"v"}]`
	first, err := e.svc.ApplyOps(keyed("kw"), e.run, "tcw", []byte(ops))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Warnings) != 1 {
		t.Fatalf("这个构造应当产生一条提醒: %v", first.Warnings)
	}
	again, err := e.svc.ApplyOps(keyed("kw"), e.run, "tcw", []byte(ops))
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Warnings) != 1 || again.Warnings[0] != first.Warnings[0] {
		t.Errorf("提醒要原样还原: %v / %v", first.Warnings, again.Warnings)
	}
	if len(e.repo.muts) != 1 {
		t.Errorf("重放不能再写: %d", len(e.repo.muts))
	}
}

func TestAgentApplyOps_ReportsOverlapOfExplicitPosition(t *testing.T) {
	e := newAgentCanvasEnv(t)
	// 模型显式把新节点放在已有的 n_script 上：位置原样保留，结果里带一条重叠提醒，重放时原样还原
	ops := `[{"op":"create_node","tempId":"x","kind":"image","label":"叠上去","position":{"x":100,"y":50}}]`
	first, err := e.svc.ApplyOps(keyed("kov"), e.run, "tco", []byte(ops))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Warnings) != 1 || !strings.Contains(first.Warnings[0], "重叠") || !strings.Contains(first.Warnings[0], "n_script") {
		t.Fatalf("应提醒与 n_script 重叠: %v", first.Warnings)
	}
	if p := e.payload(t).Node(first.IDMap["x"]).Position(); p.X != 100 || p.Y != 50 {
		t.Errorf("显式位置要原样写入，实际 %+v", p)
	}
	again, err := e.svc.ApplyOps(keyed("kov"), e.run, "tco", []byte(ops))
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Warnings) != 1 || again.Warnings[0] != first.Warnings[0] {
		t.Errorf("提醒要原样还原: %v / %v", first.Warnings, again.Warnings)
	}
}

func TestAgentApplyOps_ReplayAfterUserSavedStillReturnsOriginal(t *testing.T) {
	e := newAgentCanvasEnv(t)
	first, err := e.svc.ApplyOps(keyed("k1"), e.run, "tc1", []byte(buildOps))
	if err != nil {
		t.Fatal(err)
	}
	userSaves("n_user")(e.repo) // 用户在两次到达之间保存了画布：revision 变成 3
	again, err := e.svc.ApplyOps(keyed("k1"), e.run, "tc1", []byte(buildOps))
	if err != nil {
		t.Fatal(err)
	}
	if again.MutationID != first.MutationID || again.Revision != first.Revision {
		t.Errorf("返回的是当初那次写入，不是现在的 revision: %+v / %+v", first, again)
	}
	if e.repo.commits != 1 || len(e.payload(t).Nodes) != 3 {
		t.Errorf("不能再改画布: commits=%d nodes=%d", e.repo.commits, len(e.payload(t).Nodes))
	}
}

// 这是不能用模型的 tool_call_id 去重的原因：同一个运行里，不同回合的工具调用可能拿到相同的 id，它们是两次真实的写入
func TestAgentApplyOps_ReusedModelToolCallIDWithDistinctKeysWritesBoth(t *testing.T) {
	e := newAgentCanvasEnv(t)
	for _, key := range []string{"task-aaa", "task-bbb"} {
		if _, err := e.svc.ApplyOps(keyed(key), e.run, "call_0", []byte(`[{"op":"create_node","tempId":"x","kind":"image","label":"图"}]`)); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.repo.muts) != 2 || len(e.payload(t).Nodes) != 3 {
		t.Fatalf("模型复用了 call_0 但幂等键不同，两次都是真实的写入: muts=%d nodes=%d", len(e.repo.muts), len(e.payload(t).Nodes))
	}
}

// 旧路径（进程模式、http 模式）不带幂等键：行为必须和以前完全一样，即使模型的 id 重复也照常写
func TestAgentApplyOps_WithoutKeyNeverDeduplicates(t *testing.T) {
	e := newAgentCanvasEnv(t)
	for i := 0; i < 2; i++ {
		if _, err := e.svc.ApplyOps(context.Background(), e.run, "call_0", []byte(`[{"op":"create_node","tempId":"x","kind":"image","label":"图"}]`)); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.repo.muts) != 2 || len(e.payload(t).Nodes) != 3 {
		t.Fatalf("没有幂等键就不去重: muts=%d nodes=%d", len(e.repo.muts), len(e.payload(t).Nodes))
	}
}

func TestAgentApplyOps_SameKeyInAnotherRunIsIndependent(t *testing.T) {
	e := newAgentCanvasEnv(t)
	other := &model.AgentRun{ID: 2, SessionID: 1, CanvasID: 7, UserID: 1, Status: model.RunRunning}
	e.repo.runs[2] = other
	for _, run := range []*model.AgentRun{e.run, other} {
		if _, err := e.svc.ApplyOps(keyed("k1"), run, "call_1", []byte(`[{"op":"create_node","tempId":"x","kind":"image","label":"图"}]`)); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.repo.muts) != 2 {
		t.Fatalf("幂等键只在一个运行内唯一，不同运行不能互相去重: %d", len(e.repo.muts))
	}
}

func TestAgentMutate_DeduplicatesPerKind(t *testing.T) {
	e := newAgentCanvasEnv(t)
	if _, err := e.svc.ApplyOps(keyed("k1"), e.run, "tc1", []byte(buildOps)); err != nil {
		t.Fatal(err)
	}
	// 同一个键做另一种写入（例如生成后绑定任务）是另一件事，不能被当成重放
	node := e.payload(t).Nodes[1]["id"].(string)
	if _, err := e.svc.BindTasks(keyed("k1"), e.run, "tc1", map[string]string{node: "99"}); err != nil {
		t.Fatal(err)
	}
	if len(e.repo.muts) != 2 || e.repo.muts[0].Kind == e.repo.muts[1].Kind {
		t.Fatalf("同一个键、不同种类要各写一条: %+v", e.repo.muts)
	}
	// 同种类再来一次才是重放
	if _, err := e.svc.BindTasks(keyed("k1"), e.run, "tc1", map[string]string{node: "99"}); err != nil {
		t.Fatal(err)
	}
	if len(e.repo.muts) != 2 {
		t.Fatalf("同种类重复是重放: %d", len(e.repo.muts))
	}
}

func TestAgentDeleteApproved_ReplayDoesNotDeleteTwiceOrFail(t *testing.T) {
	e := newAgentCanvasEnv(t)
	res, err := e.svc.ApplyOps(keyed("k1"), e.run, "tc1", []byte(buildOps))
	if err != nil {
		t.Fatal(err)
	}
	id := res.IDMap["a"]
	first, err := e.svc.DeleteApproved(keyed("kd"), e.run, "tcd", []string{id}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 重放时节点已经没了：不去重的话这里会因为「节点不存在」报错
	again, err := e.svc.DeleteApproved(keyed("kd"), e.run, "tcd", []string{id}, nil)
	if err != nil {
		t.Fatalf("重放一个已完成的删除不该报错: %v", err)
	}
	if again.MutationID != first.MutationID || again.Revision != first.Revision || again.Changes != first.Changes {
		t.Errorf("重放结果要一致: %+v / %+v", first, again)
	}
}

func TestAgentApplyOps_ConcurrentDuplicatesWriteOnce(t *testing.T) {
	e := newAgentCanvasEnv(t)
	const n = 8
	results := make([]*WriteResult, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = e.svc.ApplyOps(keyed("k1"), e.run, "tc1", []byte(buildOps))
		}()
	}
	close(start)
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("第 %d 个调用出错: %v", i, errs[i])
		}
	}
	if len(e.repo.muts) != 1 || e.repo.commits != 1 || len(e.payload(t).Nodes) != 2 {
		t.Fatalf("并发的重复调用只能写一次: muts=%d commits=%d nodes=%d", len(e.repo.muts), e.repo.commits, len(e.payload(t).Nodes))
	}
	for i, r := range results {
		if r.IDMap["a"] == "" || r.IDMap["a"] != results[0].IDMap["a"] || r.MutationID != results[0].MutationID {
			t.Errorf("第 %d 个调用的结果和别的不一致: %+v / %+v", i, r, results[0])
		}
	}
}
