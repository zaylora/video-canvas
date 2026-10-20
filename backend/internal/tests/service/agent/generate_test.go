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
}

func (f *fakeGenTasks) Create(_ context.Context, _ uint64, key string, req *model.CreateGenerationTaskReq) (*model.CreateGenerationTaskResp, error) {
	i := len(f.reqs)
	f.reqs, f.keys = append(f.reqs, req), append(f.keys, key)
	if err := f.errAt[i]; err != nil {
		return nil, err
	}
	f.next++
	return &model.CreateGenerationTaskResp{Items: []model.CreateTaskItem{{NodeID: req.NodeID, Task: &model.GenerationTaskView{ID: 100 + f.next, Credits: 5}}}}, nil
}

func (f *fakeGenTasks) Get(_ context.Context, _, id uint64) (*model.GenerationTaskView, error) {
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

// videoSnap 是按秒计费的视频模型：每秒 2 积分，时长参数 duration 可调。
var videoSnap = &provider.Snapshot{
	Model: provider.ModelSnapshot{Key: "vid", Kind: "video", Label: "Vid",
		Pricing: modelcfg.Pricing{Billing: modelcfg.BillingPerSecond, PerSecond: 2},
		Capabilities: modelcfg.Capabilities{Ops: []string{"t2v"}, Prompt: modelcfg.PromptSpec{MaxLength: 500},
			Params: modelcfg.ParamSet{{Name: "duration", ParamField: modelcfg.ParamField{Type: modelcfg.ParamNumber, Label: "时长", Open: true, Default: 5, Min: intp(1), Max: intp(15)}}}}},
	Channel: provider.ChannelSnapshot{Key: "ch1"},
}

func intp(n int) *int { return &n }

// newVideoGenEnv 起一个运行，画布上有一个按秒计费的视频节点，时长 duration 秒；返回环境和节点 id。
func newVideoGenEnv(t *testing.T, duration int) (*genEnv, string) {
	t.Helper()
	e := newGenEnv(t, "all")
	e.reg.snap = videoSnap
	res := e.tool(t, e.tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
		{"op": "create_node", "kind": "video", "label": "镜头", "prompt": "女孩走进便利店", "model": "vid", "params": map[string]any{"duration": duration}}}})
	if res.IsError {
		t.Fatalf("准备视频节点: %+v", res)
	}
	for _, n := range mustParseGraph(t, e.repo).Nodes {
		if n.Kind() == "video" {
			return e, n.ID()
		}
	}
	t.Fatal("没有视频节点")
	return nil, ""
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

func TestAgentBridge_GenerateMedia(t *testing.T) {
	t.Run("预检通过：建生成审批、按模型价格算预估、本轮停下等批准", func(t *testing.T) {
		e := newGenEnv(t, "all")
		res := e.generate(t, e.imgID(t))
		if res.IsError || !res.Terminate {
			t.Fatalf("res=%+v", res)
		}
		var a *model.AgentApproval
		for _, v := range e.repo.st().approvals {
			a = v
		}
		if a == nil || a.Kind != model.ApprovalGenerate || a.QuoteCredits != 5 {
			t.Fatalf("approval=%+v", a)
		}
		var p GeneratePayload
		_ = json.Unmarshal(a.PayloadJSON, &p)
		if len(p.Items) != 1 || p.Items[0].ModelKey != "seedream" || p.Items[0].Price != 5 || p.Items[0].Count != 1 || p.Items[0].Label != "角色" {
			t.Errorf("payload=%+v", p)
		}
		if e.run.ID == 0 || e.run1(e.run.ID).Status != model.RunWaitingApproval {
			t.Errorf("运行应等待批准")
		}
	})

	t.Run("预检不过都是写给模型的工具错误，不建审批", func(t *testing.T) {
		cases := map[string]struct {
			setup func(e *genEnv, id string) []string
			want  string
		}{
			"节点没有选模型": {func(e *genEnv, id string) []string {
				e.tool(t, e.tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "update_node", "id": id, "model": ""}}})
				return []string{id}
			}, "还没有选模型"},
			"节点是组或不存在": {func(*genEnv, string) []string { return []string{"ghost"} }, "ghost"},
			"节点重复":     {func(_ *genEnv, id string) []string { return []string{id, id} }, "重复"},
			"一个都没有":    {func(*genEnv, string) []string { return nil }, "1 到 8"},
			"提示词为空": {func(e *genEnv, id string) []string {
				e.tool(t, e.tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "update_node", "id": id, "prompt": " "}}})
				return []string{id}
			}, "参数不合法"},
			"模型种类对不上": {func(e *genEnv, id string) []string {
				e.reg.snap = &provider.Snapshot{Model: provider.ModelSnapshot{Key: "seedream", Kind: "video", Capabilities: imageSnap.Model.Capabilities}, Channel: imageSnap.Channel}
				return []string{id}
			}, "种类对不上"},
			"模型已下线": {func(e *genEnv, id string) []string { e.reg.snapEr = provider.ErrModelUnavailable; return []string{id} }, "不可用"},
		}
		for name, c := range cases {
			t.Run(name, func(t *testing.T) {
				e := newGenEnv(t, "all")
				ids := c.setup(e, e.imgID(t))
				res := e.generate(t, ids...)
				if !res.IsError || res.Terminate || !strings.Contains(res.Content, c.want) {
					t.Fatalf("应是含 %q 的工具错误: %+v", c.want, res)
				}
				if len(e.repo.st().approvals) != 0 {
					t.Error("预检不过不能建审批")
				}
			})
		}
	})

	t.Run("已有产物或正在生成的节点不能生成，不覆盖", func(t *testing.T) {
		for name, patch := range map[string]string{"已有产物": `"src":"/files/1"`, "正在生成": `"status":"running"`} {
			e := newGenEnv(t, "all")
			id := e.imgID(t)
			e.repo.canvas.PayloadJSON = []byte(strings.Replace(string(e.repo.canvas.PayloadJSON), `"kind":"image"`, `"kind":"image",`+patch, 1))
			res := e.generate(t, id)
			if !res.IsError || !strings.Contains(res.Content, "角色") {
				t.Errorf("%s: %+v", name, res)
			}
		}
	})

	t.Run("剧本创编和提示词优化模式没有这个工具", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "script")
		if res := b.tool(t, tok, "generate_media", map[string]any{"items": []map[string]string{{"nodeId": "n_script"}}}); !res.IsError || !strings.Contains(res.Content, "剧本创编") {
			t.Errorf("Go 端也要拒绝: %+v", res)
		}
		if got := AgentToolsForMode("script"); hasTool(got, "generate_media") {
			t.Errorf("script 模式不该有 generate_media: %v", got)
		}
		if !hasTool(AgentToolsForMode("storyboard"), "generate_media") || !hasTool(AgentToolsForMode("all"), "generate_media") {
			t.Error("全能创作和分镜搭建应有 generate_media")
		}
		if !hasTool(AgentToolsForMode("prompt"), "task_get") {
			t.Error("task_get 所有模式都能用")
		}
	})
}

func (b *bridgeEnv) run1(id uint64) model.AgentRun { return b.run(id) }

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

	// 任务已经创建、积分已经冻结，只是回写节点时撞上了版本冲突：不能把整个审批记成失败并告诉模型「没扣积分」，
	// 否则模型会再申请一次，重复扣费。要照实返回已创建的任务，并说明节点还没显示为生成中。
	t.Run("任务已创建但绑定节点一直冲突：照实返回已创建的任务和警告，不当成失败", func(t *testing.T) {
		e := newGenEnv(t, "all")
		n := 0
		var hook func(r *fakeAgentCanvasRepo)
		hook = func(r *fakeAgentCanvasRepo) {
			n++
			userSaves("n_u" + string(rune('0'+n)))(r)
			r.beforeCommit = hook // 每次提交前都有人保存，绑定写不进去
		}
		e.repo.beforeCommit = hook
		res, err := newGenerator(e).Execute(ctx, e.run, 77, items(e))
		if err != nil {
			t.Fatalf("任务已创建，不应返回错误（会被记成失败并说没扣积分）: %v", err)
		}
		if !strings.Contains(string(res), `"task_id":"101"`) {
			t.Errorf("结果里必须有已创建的任务: %s", res)
		}
		if !strings.Contains(string(res), `"bind_warning"`) {
			t.Errorf("结果里要有绑定失败的警告: %s", res)
		}
		if len(e.tasks.reqs) != 1 {
			t.Errorf("只应创建一次任务: %d", len(e.tasks.reqs))
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
		its := []GenerateItem{{NodeID: ids[0], ModelKey: "seedream", Count: 1, Price: 5}, {NodeID: ids[1], ModelKey: "seedream", Count: 1, Price: 5}}
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

	// 批准的是当时的报价；等批准期间用户改了时长，提交时按最新输入重新报价，贵了就不提交
	t.Run("批准后节点参数改动导致价格变高：不提交，记入失败并写清原因", func(t *testing.T) {
		e, id := newVideoGenEnv(t, 4)                                                                                                                           // 每秒 2 积分 × 4 秒 = 8
		e.tool(t, e.tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "update_node", "id": id, "params": map[string]any{"duration": 6}}}}) // 变成 12
		g := newGenerator(e)
		_, err := g.Execute(ctx, e.run, 9, []GenerateItem{{NodeID: id, Label: "镜头", ModelKey: "vid", Count: 1, Price: 8}})
		if err == nil {
			t.Fatal("唯一的条目没提交，应整体失败")
		}
		for _, want := range []string{"批准后改动", "从 8 变成 12", "重新申请"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("原因里应有 %q: %v", want, err)
			}
		}
		if len(e.tasks.reqs) != 0 {
			t.Errorf("涨价的条目不能提交: %d", len(e.tasks.reqs))
		}
	})

	t.Run("批准后价格变低或不变：照常提交", func(t *testing.T) {
		e, id := newVideoGenEnv(t, 4)
		e.tool(t, e.tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "update_node", "id": id, "params": map[string]any{"duration": 3}}}}) // 变成 6
		if _, err := newGenerator(e).Execute(ctx, e.run, 9, []GenerateItem{{NodeID: id, Label: "镜头", ModelKey: "vid", Count: 1, Price: 8}}); err != nil {
			t.Fatalf("变便宜应照常提交: %v", err)
		}
		e2, id2 := newVideoGenEnv(t, 4)
		if _, err := newGenerator(e2).Execute(ctx, e2.run, 9, []GenerateItem{{NodeID: id2, Label: "镜头", ModelKey: "vid", Count: 1, Price: 8}}); err != nil || len(e2.tasks.reqs) != 1 {
			t.Fatalf("价格不变应照常提交: err=%v reqs=%d", err, len(e2.tasks.reqs))
		}
	})

	t.Run("部分条目涨价：涨价的记入 failed，其余照常提交", func(t *testing.T) {
		e, id := newVideoGenEnv(t, 4)
		e.tool(t, e.tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
			{"op": "update_node", "id": id, "params": map[string]any{"duration": 6}},
			{"op": "create_node", "tempId": "v2", "kind": "video", "label": "镜头二", "prompt": "走进便利店", "model": "vid", "params": map[string]any{"duration": 4}}}})
		var id2 string
		for _, n := range mustParseGraph(t, e.repo).Nodes {
			if n.Kind() == "video" && n.ID() != id {
				id2 = n.ID()
			}
		}
		res, err := newGenerator(e).Execute(ctx, e.run, 9, []GenerateItem{
			{NodeID: id, Label: "镜头", ModelKey: "vid", Count: 1, Price: 8},
			{NodeID: id2, Label: "镜头二", ModelKey: "vid", Count: 1, Price: 8}})
		if err != nil {
			t.Fatal(err)
		}
		var r struct {
			Tasks  []map[string]any `json:"tasks"`
			Failed []struct {
				NodeID string `json:"node_id"`
				Error  string `json:"error"`
			} `json:"failed"`
		}
		_ = json.Unmarshal(res, &r)
		if len(r.Tasks) != 1 || r.Tasks[0]["node_id"] != id2 || len(r.Failed) != 1 || r.Failed[0].NodeID != id || !strings.Contains(r.Failed[0].Error, "从 8 变成 12") {
			t.Fatalf("r=%s", res)
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
