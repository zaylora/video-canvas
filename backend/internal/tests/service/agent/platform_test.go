package agent_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service/agent"
)

// 平台 API 的「上下文模式」：Agent 作为独立服务时，运行记录在它那边，Go 里没有 agent_runs 这一行。
// 调用方随每个请求带上运行上下文（RunContext），Go 据此执行工具；Go 不发事件、不记步数、不改运行状态
// （这些都归 Agent 服务），但画布写入、联网次数上限、技能固定版本这些平台规则照旧。

// 运行 id 故意用假仓储里不存在的值：证明执行不依赖运行记录。
func platformCtx(runID uint64, mode string) RunContext {
	return RunContext{RunID: runID, UserID: 1, SessionID: 5, CanvasID: 7, Mode: mode, ModelKey: "claude"}
}

func execPlatform(ctx context.Context, t *testing.T, b *bridgeEnv, rc RunContext, callID, name string, args any) *ToolResult {
	t.Helper()
	raw, _ := json.Marshal(args)
	res, err := b.br.ExecutePlatformTool(ctx, rc, callID, name, raw)
	if err != nil {
		t.Fatalf("ExecutePlatformTool(%s): %v", name, err)
	}
	return res
}

func TestExecutePlatformTool_RunsWithoutARunRecord(t *testing.T) {
	b := newBridgeEnv(t)
	res := execPlatform(context.Background(), t, b, platformCtx(900001, "all"), "call_1", "canvas_get_state", map[string]any{})
	if res.IsError || res.Content == "" {
		t.Fatalf("读画布应成功: %+v", res)
	}
	if !strings.Contains(res.Content, "n_script") {
		t.Errorf("应读到画布里的节点: %s", res.Content)
	}
}

func TestExecutePlatformTool_WritesCanvasAndIsIdempotentWithKey(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(900002, "all")
	ops := map[string]any{"ops": []map[string]any{
		{"op": "create_node", "tempId": "a", "kind": "image", "label": "角色"},
		{"op": "connect", "source": "n_script", "target": "a"},
	}}
	ctx := WithIdempotencyKey(context.Background(), "task-aaa")
	first := execPlatform(ctx, t, b, rc, "call_0", "canvas_apply_ops", ops)
	again := execPlatform(ctx, t, b, rc, "call_0", "canvas_apply_ops", ops)
	if first.IsError || again.IsError {
		t.Fatalf("写画布应成功: %+v / %+v", first, again)
	}
	if first.Content != again.Content {
		t.Errorf("重放要返回和第一次一样的结果（含 id_map）:\n%s\n%s", first.Content, again.Content)
	}
	if n := len(modeGraphOf(t, b).Nodes); n != 2 {
		t.Errorf("重放不能再建一个节点: %d", n)
	}
	// 同一个模型 tool_call_id、不同的幂等键：是两次真实的写入
	execPlatform(WithIdempotencyKey(context.Background(), "task-bbb"), t, b, rc, "call_0", "canvas_apply_ops", ops)
	if n := len(modeGraphOf(t, b).Nodes); n != 3 {
		t.Errorf("键不同就是另一次调用: %d", n)
	}
}

func TestExecutePlatformTool_ModeRulesStillApply(t *testing.T) {
	b := newBridgeEnv(t)
	res := execPlatform(context.Background(), t, b, platformCtx(900003, "prompt"), "c1", "canvas_arrange", map[string]any{"nodeIds": []string{"n_script"}})
	if !res.IsError || !strings.Contains(res.Content, "不能使用") {
		t.Errorf("提示词模式不能整理布局，平台规则不依赖调用方自觉: %+v", res)
	}
	ops := map[string]any{"ops": []map[string]any{{"op": "create_node", "tempId": "a", "kind": "video", "label": "镜头"}}}
	res = execPlatform(context.Background(), t, b, platformCtx(900003, "prompt"), "c2", "canvas_apply_ops", ops)
	if !res.IsError {
		t.Errorf("提示词模式只能改提示词，建节点要被拒绝（modeAllowsOps）: %+v", res)
	}
}

func TestExecutePlatformTool_UnknownAndAgentSideTools(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(900004, "all")
	res := execPlatform(context.Background(), t, b, rc, "c1", "no_such_tool", map[string]any{})
	if !res.IsError || !strings.Contains(res.Content, "未知的工具") {
		t.Errorf("未知工具是给模型看的工具错误: %+v", res)
	}
	// 这几个工具要么只在 Agent 服务里（计划、提问），要么会创建审批（生成、删除）：审批归 Agent 服务，
	// Go 只提供「预估」和「执行」两个接口，不能经这个通用入口调用。这是调用方的编码错误，返回参数错误而不是工具错误
	for _, name := range []string{"plan_update", "ask_user", "canvas_delete", "generate_media"} {
		_, err := b.br.ExecutePlatformTool(context.Background(), rc, "c2", name, []byte(`{}`))
		wantAgentCode(t, err, errcode.ErrInvalidParams)
	}
}

func TestExecutePlatformTool_ValidatesTheContext(t *testing.T) {
	b := newBridgeEnv(t)
	cases := map[string]RunContext{
		"没有运行 id": {UserID: 1, SessionID: 5, CanvasID: 7},
		"没有用户 id": {RunID: 9, SessionID: 5, CanvasID: 7},
		"没有画布 id": {RunID: 9, UserID: 1, SessionID: 5},
		"没有会话 id": {RunID: 9, UserID: 1, CanvasID: 7},
		"任务模式不认识": {RunID: 9, UserID: 1, SessionID: 5, CanvasID: 7, Mode: "hack"},
	}
	for name, rc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := b.br.ExecutePlatformTool(context.Background(), rc, "c1", "canvas_get_state", []byte(`{}`))
			wantAgentCode(t, err, errcode.ErrInvalidParams)
		})
	}
	// 空模式按全能创作算，和现有行为一致
	if res := execPlatform(context.Background(), t, b, platformCtx(9, ""), "c1", "canvas_get_state", map[string]any{}); res.IsError {
		t.Errorf("空模式应当可用: %+v", res)
	}
}

func TestExecutePlatformTool_OtherUsersCanvasIsNotFound(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(900005, "all")
	rc.UserID = 2 // 别人的画布：按 id + user_id 查，不存在和不属于你一样处理，不能因为调用方在内网就信它
	res := execPlatform(context.Background(), t, b, rc, "c1", "canvas_get_state", map[string]any{})
	if !res.IsError || !strings.Contains(res.Content, "画布不存在") || strings.Contains(res.Content, "n_script") {
		t.Fatalf("读别人的画布要得到「画布不存在」，且不能泄露内容: %+v", res)
	}
	// 写也一样
	wres := execPlatform(context.Background(), t, b, rc, "c2", "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "create_node", "tempId": "a", "kind": "image", "label": "x"}}})
	if !wres.IsError || len(modeGraphOf(t, b).Nodes) != 1 {
		t.Fatalf("不能往别人的画布里写: %+v", wres)
	}
}

func TestExecutePlatformTool_WebCallLimitIsPerRunAcrossRequests(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(900006, "all")
	// 没有令牌，每次请求都是独立的 HTTP 调用，联网次数上限要按运行累计，而不是每次请求重新数
	for i := 0; i < 20; i++ {
		if res := execPlatform(context.Background(), t, b, rc, "c", "web_search", map[string]any{"query": "x"}); res.IsError {
			t.Fatalf("第 %d 次不该被拦: %+v", i+1, res)
		}
	}
	res := execPlatform(context.Background(), t, b, rc, "c", "web_search", map[string]any{"query": "x"})
	if !res.IsError || !strings.Contains(res.Content, "上限") {
		t.Errorf("第 21 次应被挡住: %+v", res)
	}
	// 另一个运行不受影响
	if res := execPlatform(context.Background(), t, b, platformCtx(900007, "all"), "c", "web_search", map[string]any{"query": "x"}); res.IsError {
		t.Errorf("别的运行有自己的次数: %+v", res)
	}
	// 运行结束后释放这个运行在内存里的状态
	b.br.EndPlatformRun(900006)
	if res := execPlatform(context.Background(), t, b, rc, "c", "web_search", map[string]any{"query": "x"}); res.IsError {
		t.Errorf("EndPlatformRun 之后计数清零: %+v", res)
	}
}

func TestExecutePlatformTool_RunStateIsEvictedWhenIdle(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(900008, "all")
	for i := 0; i < 20; i++ {
		execPlatform(context.Background(), t, b, rc, "c", "web_search", map[string]any{"query": "x"})
	}
	// 运行崩了、Agent 服务没来得及调 EndPlatformRun：闲置超过保留时间后自动清掉，内存不会一直涨
	*b.now = b.now.Add(PlatformRunIdleTTL + 1)
	if res := execPlatform(context.Background(), t, b, platformCtx(900009, "all"), "c", "canvas_get_state", map[string]any{}); res.IsError { // 任意一次调用顺手清理
		t.Fatalf("%+v", res)
	}
	if res := execPlatform(context.Background(), t, b, rc, "c", "web_search", map[string]any{"query": "x"}); res.IsError {
		t.Errorf("闲置过期的运行状态应已被清掉: %+v", res)
	}
}

func TestExecutePlatformTool_ToolFailureIsAToolResultNotAnError(t *testing.T) {
	b := newBridgeEnv(t)
	res := execPlatform(context.Background(), t, b, platformCtx(900010, "all"), "c1", "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "update_node", "id": "不存在", "label": "x"}}})
	if !res.IsError || res.Content == "" {
		t.Errorf("校验不过是给模型看的工具错误，不是 HTTP 错误: %+v", res)
	}
}

// ---- tool_end：事件内容由 Go 给出 ----
//
// tool.start / tool.end 事件由 Agent 服务落库和推送，但事件里的摘要、被改动的节点、界面名称这些只有 Go 知道。
// 所以平台工具结果带一个 tool_end，Agent 服务原样用。

func TestExecutePlatformTool_ToolEndCarriesWhatOnlyGoKnows(t *testing.T) {
	b := newBridgeEnv(t)
	ops := map[string]any{"ops": []map[string]any{{"op": "create_node", "tempId": "a", "kind": "image", "label": "角色"}}}
	cases := []struct {
		name, tool string
		args       any
		wantErr    bool
		wantLabel  string
	}{
		{"读画布", "canvas_get_state", map[string]any{}, false, "读取画布"},
		{"改画布", "canvas_apply_ops", ops, false, "修改画布"},
		{"整理布局", "canvas_arrange", map[string]any{"layout": "row", "nodeIds": []string{"n_script"}}, false, "整理布局"},
		{"查模型", "model_list", map[string]any{"kind": "image"}, false, "查看可用模型"},
		{"未知工具", "no_such_tool", map[string]any{}, true, ""},
		{"校验不过", "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "update_node", "id": "不存在", "label": "x"}}}, true, "修改画布"},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, _ := json.Marshal(c.args)
			id := "tc-" + c.tool + strconv.Itoa(i)
			res, err := b.br.ExecutePlatformTool(context.Background(), platformCtx(uint64(910001+i), "all"), id, c.tool, raw)
			if err != nil {
				t.Fatal(err)
			}
			end := res.ToolEnd
			if end == nil {
				t.Fatalf("平台工具结果要带 tool_end: %+v", res)
			}
			if end["id"] != id || end["name"] != c.tool || end["is_error"] != c.wantErr {
				t.Errorf("tool_end 的 id、名字、是否出错: %v", end)
			}
			if s, _ := end["summary"].(string); s == "" {
				t.Errorf("摘要要有（前端工具行、导出和统计读它）: %v", end)
			}
			if c.wantLabel != "" && end["label"] != c.wantLabel {
				t.Errorf("界面名称: %v", end["label"])
			}
		})
	}
}
