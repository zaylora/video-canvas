package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	. "video-canvas/internal/service/agent"
)

// fakeGenTasks 记下收到的提交，按脚本回应；Get 返回预置的任务。
type fakeGenTasks struct {
	reqs  []*model.CreateGenerationTaskReq
	keys  []string
	errAt map[int]error // 第 i 次提交失败
	next  uint64
	task  *model.GenerationTaskView
	views map[uint64]*model.GenerationTaskView // 按 id 预置的任务；没有的 Get 返回不存在
	// onCreate 在每次提交开始时调用，用来模拟提交过程中请求被取消
	onCreate func()
}

func (f *fakeGenTasks) Create(_ context.Context, _ uint64, key string, req *model.CreateGenerationTaskReq) (*model.CreateGenerationTaskResp, error) {
	if f.onCreate != nil {
		f.onCreate()
	}
	i := len(f.reqs)
	f.reqs, f.keys = append(f.reqs, req), append(f.keys, key)
	if err := f.errAt[i]; err != nil {
		return nil, err
	}
	f.next++
	return &model.CreateGenerationTaskResp{Items: []model.CreateTaskItem{{NodeID: req.NodeID, Task: &model.GenerationTaskView{ID: 100 + f.next, Credits: 5}}}}, nil
}

func (f *fakeGenTasks) Get(_ context.Context, _, id uint64) (*model.GenerationTaskView, error) {
	if v, ok := f.views[id]; ok {
		return v, nil
	}
	if f.task == nil || f.task.ID != id {
		return nil, errcode.ErrNotFound
	}
	return f.task, nil
}

var imageSnap = &provider.Snapshot{
	Model: provider.ModelSnapshot{Key: "seedream", Kind: "image", Label: "Seedream",
		Pricing:      modelcfg.Pricing{Billing: modelcfg.BillingPerCall, Unit: 5},
		Capabilities: modelcfg.Capabilities{Ops: []string{"t2i", "i2i"}, Refs: modelcfg.Refs{Image: modelcfg.RefSpec{On: true, Max: 4, MaxMB: 10}}, Prompt: modelcfg.PromptSpec{MaxLength: 500}}},
	Channel: provider.ChannelSnapshot{Key: "ch1"},
}

type genEnv struct {
	*bridgeEnv
	tasks *fakeGenTasks
	tok   string
	run   *model.AgentRun
}

// newGenEnv 起一个运行，画布上有剧本节点和一个选好模型、写好提示词的图片节点 n_img（连着剧本）。
func newGenEnv(t *testing.T, mode string) *genEnv {
	t.Helper()
	b := newBridgeEnv(t)
	b.reg.snap = imageSnap
	tasks := &fakeGenTasks{}
	b.br = NewAgentBridge(BridgeDeps{
		Repo: b.repo, Canvas: NewAgentCanvasService(b.repo, b.bc), Agent: b.svc, Billing: NewAgentBilling(b.bill),
		Registry: b.reg, Secrets: fakeSecrets{}, Streamer: b.str, Tasks: tasks,
	})
	tok, run := b.token(t, mode)
	e := &genEnv{bridgeEnv: b, tasks: tasks, tok: tok, run: run}
	res := b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
		{"op": "create_node", "tempId": "img", "kind": "image", "label": "角色", "prompt": "红衣女孩", "model": "seedream"},
		{"op": "connect", "source": "n_script", "target": "img"}}})
	if res.IsError {
		t.Fatalf("准备画布: %+v", res)
	}
	return e
}

func (e *genEnv) imgID(t *testing.T) string {
	t.Helper()
	for _, n := range mustParseGraph(t, e.repo).Nodes {
		if n.Kind() == "image" {
			return n.ID()
		}
	}
	t.Fatal("没有图片节点")
	return ""
}

func (e *genEnv) generate(t *testing.T, ids ...string) *ToolResult {
	t.Helper()
	items := make([]map[string]string, len(ids))
	for i, id := range ids {
		items[i] = map[string]string{"nodeId": id}
	}
	return e.tool(t, e.tok, "generate_media", map[string]any{"items": items, "reason": "出角色图"})
}

func TestAgentBridge_TaskGet(t *testing.T) {
	e := newGenEnv(t, "all")
	e.tasks.task = &model.GenerationTaskView{ID: 101, Status: "succeeded", NodeID: "n1", Outputs: []model.TaskOutput{{MediaType: "image", AssetID: 7}}}
	res := e.tool(t, e.tok, "task_get", map[string]any{"taskId": "101"})
	if res.IsError || !strings.Contains(res.Content, `"succeeded"`) || !strings.Contains(res.Content, `"7"`) {
		t.Fatalf("res=%+v", res)
	}
	for _, id := range []string{"abc", "0", "999"} {
		if r := e.tool(t, e.tok, "task_get", map[string]any{"taskId": id}); !r.IsError {
			t.Errorf("taskId=%s 应是工具错误: %+v", id, r)
		}
	}
}

// ---- 执行器 ----

func newGenerator(e *genEnv) *AgentGenerator {
	return NewAgentGenerator(e.tasks, NewAgentCanvasService(e.repo, e.bc), e.reg)
}

func TestAgentGenerator_Execute(t *testing.T) {
	ctx := context.Background()
	items := func(e *genEnv) []GenerateItem {
		return []GenerateItem{{NodeID: e.imgID(t), Label: "角色", ModelKey: "seedream", Count: 1, Price: 5}}
	}

	t.Run("按节点当前内容提交：提示词、上游文字、幂等键；成功后任务绑定到节点", func(t *testing.T) {
		e := newGenEnv(t, "all")
		res, err := newGenerator(e).Execute(ctx, e.run, 77, items(e))
		if err != nil {
			t.Fatal(err)
		}
		req := e.tasks.reqs[0]
		if req.Kind != "image" || req.ModelID != "seedream" || req.Input["prompt"] != "红衣女孩" || req.Input["op"] != "t2i" {
			t.Errorf("req=%+v", req)
		}
		if e.tasks.keys[0] != "agent-77-0" {
			t.Errorf("幂等键 = %s", e.tasks.keys[0])
		}
		if req.AgentRunID != e.run.ID {
			t.Errorf("任务应记下发起它的运行 %d，实际 %d：任务结束时靠它通知 Agent", e.run.ID, req.AgentRunID)
		}
		if !strings.Contains(string(res), `"task_id":"101"`) {
			t.Errorf("结果=%s", res)
		}
		d := node(t, e.repo, e.imgID(t)).Data()
		if d["taskId"] != "101" || d["status"] != "running" {
			t.Errorf("节点应绑定任务: %v", d)
		}
		muts, _ := e.repo.ListMutations(ctx, e.run.ID)
		if muts[len(muts)-1].Kind != model.MutationBind {
			t.Errorf("绑定记为 bind 改动: %s", muts[len(muts)-1].Kind)
		}
	})

	t.Run("提示词为空时用上游文字；有上游素材时按图生图并带参考图", func(t *testing.T) {
		e := newGenEnv(t, "all")
		id := e.imgID(t)
		e.tool(t, e.tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "update_node", "id": id, "prompt": ""}}})
		e.repo.canvas.PayloadJSON = []byte(strings.Replace(string(e.repo.canvas.PayloadJSON),
			`"label":"剧本"`, `"label":"剧本","text":"男主走进便利店","assetId":"42"`, 1))
		// 剧本节点既有文字也有 assetId 是不真实的；这里只验证文字被用作提示词
		if _, err := newGenerator(e).Execute(ctx, e.run, 1, items(e)); err != nil {
			t.Fatal(err)
		}
		if got := e.tasks.reqs[0].Input["prompt"]; got != "男主走进便利店" {
			t.Errorf("prompt = %v", got)
		}
	})

	t.Run("手填了提示词：没被 @ 的上游文字追加在后面，被 @ 的换成正文且不重复", func(t *testing.T) {
		e := newGenEnv(t, "all")
		e.repo.canvas.PayloadJSON = []byte(strings.Replace(string(e.repo.canvas.PayloadJSON),
			`"label":"剧本"`, `"label":"剧本","text":"男主走进便利店"`, 1))
		if _, err := newGenerator(e).Execute(ctx, e.run, 1, items(e)); err != nil {
			t.Fatal(err)
		}
		if got := e.tasks.reqs[0].Input["prompt"]; got != "红衣女孩\n\n男主走进便利店" {
			t.Errorf("没 @ 时追加: prompt = %q", got)
		}

		e2 := newGenEnv(t, "all")
		e2.repo.canvas.PayloadJSON = []byte(strings.Replace(string(e2.repo.canvas.PayloadJSON),
			`"label":"剧本"`, `"label":"剧本","text":"男主走进便利店"`, 1))
		e2.tool(t, e2.tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
			{"op": "update_node", "id": e2.imgID(t), "prompt": "红衣女孩，场景：@[剧本](n_script)"}}})
		if _, err := newGenerator(e2).Execute(ctx, e2.run, 2, items(e2)); err != nil {
			t.Fatal(err)
		}
		if got := e2.tasks.reqs[0].Input["prompt"]; got != "红衣女孩，场景：男主走进便利店" {
			t.Errorf("已 @ 时展开且不重复: prompt = %q", got)
		}
	})

	t.Run("部分失败：成功的绑定，失败的写在结果里", func(t *testing.T) {
		e := newGenEnv(t, "all")
		e.tool(t, e.tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
			{"op": "create_node", "kind": "image", "label": "场景", "prompt": "便利店", "model": "seedream"}}})
		var ids []string
		for _, n := range mustParseGraph(t, e.repo).Nodes {
			if n.Kind() == "image" {
				ids = append(ids, n.ID())
			}
		}
		e.tasks.errAt = map[int]error{1: errcode.ErrInsufficientCredits}
		its := []GenerateItem{{NodeID: ids[0], ModelKey: "seedream", Count: 1}, {NodeID: ids[1], ModelKey: "seedream", Count: 1}}
		res, err := newGenerator(e).Execute(ctx, e.run, 5, its)
		if err != nil {
			t.Fatal(err)
		}
		var r struct {
			Tasks  []map[string]any `json:"tasks"`
			Failed []map[string]any `json:"failed"`
		}
		_ = json.Unmarshal(res, &r)
		if len(r.Tasks) != 1 || len(r.Failed) != 1 || !strings.Contains(r.Failed[0]["error"].(string), "积分") {
			t.Errorf("r=%+v", r)
		}
		if node(t, e.repo, ids[1]).Data()["taskId"] != nil {
			t.Error("失败的节点不绑定")
		}
	})

	t.Run("全部失败返回 error；批准期间节点已有产物不重复提交", func(t *testing.T) {
		e := newGenEnv(t, "all")
		e.tasks.errAt = map[int]error{0: errors.New("db down")}
		if _, err := newGenerator(e).Execute(ctx, e.run, 1, items(e)); err == nil || strings.Contains(err.Error(), "db down") {
			t.Errorf("全部失败应返回 error，且不泄露内部错误: %v", err)
		}
		e2 := newGenEnv(t, "all")
		its := items(e2)
		e2.repo.canvas.PayloadJSON = []byte(strings.Replace(string(e2.repo.canvas.PayloadJSON), `"kind":"image"`, `"kind":"image","src":"/files/1"`, 1))
		if _, err := newGenerator(e2).Execute(ctx, e2.run, 2, its); err == nil || len(e2.tasks.reqs) != 0 {
			t.Errorf("已有产物不应提交: err=%v reqs=%d", err, len(e2.tasks.reqs))
		}
	})
}

func node(t *testing.T, f *fakeAgentCanvasRepo, id string) canvasgraph.Node {
	t.Helper()
	n := mustParseGraph(t, f).Node(id)
	if n == nil {
		t.Fatalf("节点 %s 不存在", id)
	}
	return n
}

func hasTool(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Agent 查一批任务的状态：自己的任务给出状态、节点、产物；别人的和不存在的一律 missing，不泄露存在与否。
func TestAgentBridge_PlatformTaskStates(t *testing.T) {
	ctx := context.Background()
	e := newGenEnv(t, "all")
	progress := 60
	e.tasks.views = map[uint64]*model.GenerationTaskView{
		101: {ID: 101, NodeID: "n1", Kind: "image", Status: model.TaskSucceeded, Credits: 5,
			Outputs: []model.TaskOutput{{MediaType: "image", AssetID: 42}, {MediaType: "text", Text: "说明"}}},
		102: {ID: 102, NodeID: "n2", Kind: "video", Status: model.TaskRunning, Progress: &progress, Credits: 20},
		103: {ID: 103, NodeID: "n3", Kind: "image", Status: model.TaskFailed, ErrorMessage: "触发内容安全风控", Credits: 5},
	}
	got, err := e.br.PlatformTaskStates(ctx, e.run.UserID, []uint64{101, 102, 103, 999})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("应按请求顺序返回 4 项：%+v", got)
	}
	if g := got[0]; g.TaskID != "101" || g.Status != "succeeded" || g.NodeID != "n1" || len(g.Outputs) != 2 || g.Outputs[0].AssetID != "42" || g.Outputs[0].MediaType != "image" {
		t.Errorf("成功的任务带节点和产物：%+v", g)
	}
	if g := got[1]; g.Status != "running" || g.Progress == nil || *g.Progress != 60 {
		t.Errorf("进行中的任务带进度：%+v", g)
	}
	if g := got[2]; g.Status != "failed" || g.Error != "触发内容安全风控" {
		t.Errorf("失败的任务带原因：%+v", g)
	}
	if g := got[3]; g.TaskID != "999" || g.Status != "missing" {
		t.Errorf("查不到的任务标 missing：%+v", g)
	}
}
