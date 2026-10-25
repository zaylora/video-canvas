package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service/agent"
)

// 方案二里会话、运行归 Agent 服务，REST 接口由它实现；但画布归属、模型清单、消息里引用的校验、撤销所依据的改动记录
// 都在 Go（Postgres），Agent 服务通过这几个平台接口来问。它们都不碰运行记录。

// refsBridge 用带模型注册表的 AgentService 装配一座桥：引用校验要它。
func refsBridge(t *testing.T) (*AgentBridge, *agentEnv) {
	t.Helper()
	e := refsEnv(t)
	br := NewAgentBridge(BridgeDeps{Repo: e.repo, Canvas: NewAgentCanvasService(e.repo, e.bc), Agent: e.svc})
	return br, e
}

func TestCheckCanvas(t *testing.T) {
	b := newBridgeEnv(t)
	ctx := context.Background()
	if err := b.br.CheckCanvas(ctx, 1, 7); err != nil {
		t.Fatalf("自己的画布应通过: %v", err)
	}
	// 别人的画布和不存在的画布一样处理，不泄露存在与否
	wantAgentCode(t, b.br.CheckCanvas(ctx, 2, 7), errcode.ErrCanvasNotFound)
	wantAgentCode(t, b.br.CheckCanvas(ctx, 1, 999), errcode.ErrCanvasNotFound)
	wantAgentCode(t, b.br.CheckCanvas(ctx, 0, 7), errcode.ErrInvalidParams)
	wantAgentCode(t, b.br.CheckCanvas(ctx, 1, 0), errcode.ErrInvalidParams)
}

func TestAgentModels(t *testing.T) {
	b := newBridgeEnv(t)
	got, err := b.br.AgentModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(defaultAgentModels) || got[0].Key != "claude" || !got[0].Vision {
		t.Errorf("模型清单要和令牌模式下 GET /agent/models 一致: %+v", got)
	}
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), `"context_window"`) {
		t.Errorf("字段名: %s", raw)
	}
}

func TestAgentModels_EmptyIsAnEmptyListNotNull(t *testing.T) {
	e := newAgentEnv(t, defaultAgentModels...)
	e.svc = NewAgentService(AgentDeps{Repo: e.repo, Canvas: NewAgentCanvasService(e.repo, e.bc), Models: fakeAgentModels{list: nil}, Runtime: e.rt, Broadcaster: e.bc})
	br := NewAgentBridge(BridgeDeps{Repo: e.repo, Canvas: NewAgentCanvasService(e.repo, e.bc), Agent: e.svc})
	got, err := br.AgentModels(context.Background())
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("没有任何已发布的模型时是空数组（前端据此显示「没有可用模型」）: %v %v", got, err)
	}
}

// 消息里的引用校验：规则就是 StartRun 里用的那一份（checkRefs），这里只验证经平台接口得到同样的结果。
func TestCheckMessage_SameRulesAsStartRun(t *testing.T) {
	br, e := refsBridge(t)
	ctx := context.Background()
	ok := "把 @[剧本](node:n_script) 拆开，角色用 @[Seedream](model:seedream)"
	if sel, err := br.CheckMessage(ctx, 1, 7, ok, []string{"n_script"}); err != nil || len(sel) != 1 || sel[0] != "n_script" {
		t.Fatalf("合法的引用放行: %v %v", sel, err)
	}
	// 选中的节点里已经不存在的（界面状态过期）悄悄去掉，不拦发送
	if sel, err := br.CheckMessage(ctx, 1, 7, "优化一下", []string{"n_script", "ghost"}); err != nil || len(sel) != 1 || sel[0] != "n_script" {
		t.Errorf("selection=%v err=%v", sel, err)
	}
	for name, c := range map[string]struct{ msg, want string }{
		"节点不存在":    {"拆 @[幽灵](node:ghost)", "幽灵"},
		"模型没发布":    {"用 @[某模型](model:nope)", "某模型"},
		"agent 模型": {"用 @[Claude](model:claude)", "Claude"},
		"技能不存在":    {"参考 @[瞎编](skill:made-up)", "瞎编"},
		"附件暂不支持":   {"看 @[图](asset:1)", "附件"},
	} {
		t.Run(name, func(t *testing.T) {
			_, viaPlatform := br.CheckMessage(ctx, 1, 7, c.msg, nil)
			wantAgentCode(t, viaPlatform, errcode.ErrInvalidParams)
			if !strings.Contains(viaPlatform.Error(), c.want) {
				t.Errorf("错误应点名 %q: %v", c.want, viaPlatform)
			}
			// 和 StartRun 报的是同一句话
			viaStart := startWith(e, c.msg)
			if viaStart == nil || viaStart.Error() != viaPlatform.Error() {
				t.Errorf("两条路径的错误应一致:\n平台 %v\nStartRun %v", viaPlatform, viaStart)
			}
		})
	}
	// 别人的画布：读不到画布就报画布不存在（选中节点或节点引用才需要读画布）
	_, err := br.CheckMessage(ctx, 2, 7, "拆 @[剧本](node:n_script)", nil)
	wantAgentCode(t, err, errcode.ErrCanvasNotFound)
	_, err = br.CheckMessage(ctx, 0, 7, "x", nil)
	wantAgentCode(t, err, errcode.ErrInvalidParams)
}

// 撤销本轮：改动记录在 Postgres（agent_mutations），由 Go 执行；运行记录在 Agent 服务里，所以「运行是自己的、已经结束」
// 由 Agent 服务校验，Go 这边只按运行上下文做画布上的事。
func TestUndoPlatformRun(t *testing.T) {
	b := newBridgeEnv(t)
	ctx := context.Background()
	rc := platformCtx(930001, "all")
	ops := map[string]any{"ops": []map[string]any{
		{"op": "create_node", "tempId": "a", "kind": "image", "label": "角色"},
		{"op": "connect", "source": "n_script", "target": "a"},
	}}
	res := execPlatform(WithIdempotencyKey(ctx, "task-1"), t, b, rc, "c1", "canvas_apply_ops", ops)
	if res.IsError {
		t.Fatalf("准备改动: %+v", res)
	}
	if n := len(modeGraphOf(t, b).Nodes); n != 2 {
		t.Fatalf("前提：画布应有 2 个节点: %d", n)
	}

	got, err := b.br.UndoPlatformRun(ctx, rc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reverted == 0 || got.Revision == 0 || got.Skipped == nil {
		t.Errorf("撤销结果: %+v", got)
	}
	if n := len(modeGraphOf(t, b).Nodes); n != 1 {
		t.Errorf("新建的节点应被撤销: %d", n)
	}
	// 再撤销一次：已经撤销过
	_, err = b.br.UndoPlatformRun(ctx, rc)
	wantAgentCode(t, err, errcode.ErrAgentAlreadyUndone)
}

func TestUndoPlatformRun_NothingToUndo(t *testing.T) {
	b := newBridgeEnv(t)
	got, err := b.br.UndoPlatformRun(context.Background(), platformCtx(930002, "all"))
	if err != nil {
		t.Fatal(err)
	}
	// 运行根本没改过画布：什么也没发生，Revision 为 0（调用方据此不发 run.undone）
	if got.Reverted != 0 || got.Revision != 0 || got.Skipped == nil || len(got.Skipped) != 0 {
		t.Errorf("%+v", got)
	}
}

func TestUndoPlatformRun_OtherUsersCanvasIsNotTouched(t *testing.T) {
	b := newBridgeEnv(t)
	ctx := context.Background()
	rc := platformCtx(930003, "all")
	execPlatform(WithIdempotencyKey(ctx, "task-2"), t, b, rc, "c1", "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "create_node", "tempId": "a", "kind": "image", "label": "x"}}})
	other := rc
	other.UserID = 2
	if _, err := b.br.UndoPlatformRun(ctx, other); err == nil {
		t.Error("别人的画布不能撤销")
	}
	if n := len(modeGraphOf(t, b).Nodes); n != 2 {
		t.Errorf("画布不能被动: %d", n)
	}
	_, err := b.br.UndoPlatformRun(ctx, RunContext{})
	wantAgentCode(t, err, errcode.ErrInvalidParams)
}
