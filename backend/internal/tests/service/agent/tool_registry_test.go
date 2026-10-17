package agent_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	. "video-canvas/internal/service/agent"
)

// baselineDecl 是迁移前 Node 里 TOOL_DEFS 导出的声明快照：说明和参数一个字都不能变，顺序也不能变，
// 否则模型的行为和提示词缓存都会受影响。有意改工具时同步更新 testdata/tool_defs_baseline.json。
type baselineDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

func loadBaseline(t *testing.T) []baselineDecl {
	t.Helper()
	raw, err := os.ReadFile("testdata/tool_defs_baseline.json")
	if err != nil {
		t.Fatal(err)
	}
	var out []baselineDecl
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func declNames(ds []ToolDecl) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Name
	}
	return out
}

func TestToolRegistry_DeclsMatchBaseline(t *testing.T) {
	base := loadBaseline(t)
	got := AgentToolDecls("all", true)
	if len(got) != len(base) {
		t.Fatalf("工具数 %d，基准 %d: %v", len(got), len(base), declNames(got))
	}
	for i, b := range base {
		d := got[i]
		if d.Name != b.Name {
			t.Fatalf("第 %d 个工具应是 %s（顺序不能变），实际 %s", i, b.Name, d.Name)
		}
		if d.Description != b.Description {
			t.Errorf("%s 的说明被改动:\n got: %s\nwant: %s", b.Name, d.Description, b.Description)
		}
		var params map[string]any
		if err := json.Unmarshal(d.Parameters, &params); err != nil {
			t.Fatalf("%s 的参数不是合法 JSON: %v", b.Name, err)
		}
		if !reflect.DeepEqual(params, b.Parameters) {
			t.Errorf("%s 的参数 schema 和基准不一致:\n got: %s\nwant: %v", b.Name, d.Parameters, b.Parameters)
		}
		if d.Executor != "server" {
			t.Errorf("%s 的执行方应是 server，实际 %q", b.Name, d.Executor)
		}
	}
}

func TestToolRegistry_FilterByModeAndVision(t *testing.T) {
	has := func(ds []ToolDecl, name string) bool {
		for _, d := range ds {
			if d.Name == name {
				return true
			}
		}
		return false
	}
	for _, mode := range []string{"script", "prompt"} {
		ds := AgentToolDecls(mode, true)
		for _, n := range []string{"canvas_arrange", "canvas_delete", "generate_media"} {
			if has(ds, n) {
				t.Errorf("%s 模式不该有 %s", mode, n)
			}
		}
		if !has(ds, "canvas_get_state") || !has(ds, "canvas_apply_ops") {
			t.Errorf("%s 模式要能读和改画布: %v", mode, declNames(ds))
		}
	}
	for _, mode := range []string{"", "all", "storyboard"} {
		if !has(AgentToolDecls(mode, true), "canvas_delete") {
			t.Errorf("模式 %q 应有 canvas_delete", mode)
		}
	}
	if has(AgentToolDecls("all", false), "canvas_inspect_image") {
		t.Error("不能看图的模型拿不到 canvas_inspect_image")
	}
	if !has(AgentToolDecls("all", true), "canvas_inspect_image") {
		t.Error("能看图的模型要有 canvas_inspect_image")
	}
	// 顺序：过滤后保持登记的相对顺序
	names := declNames(AgentToolDecls("prompt", false))
	want := []string{"canvas_get_state", "canvas_apply_ops", "plan_update", "ask_user", "model_list", "task_get", "skill_search", "skill_read"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("过滤后的顺序 %v，应为 %v", names, want)
	}
}

func TestToolRegistry_Scriptable(t *testing.T) {
	notScriptable := map[string]bool{"canvas_delete": true, "ask_user": true, "generate_media": true, "canvas_inspect_image": true}
	for _, d := range AgentToolDecls("all", true) {
		if d.Scriptable == notScriptable[d.Name] {
			t.Errorf("%s 的 scriptable=%v：会创建审批或返回图片的工具不能给脚本调用，其余都可以", d.Name, d.Scriptable)
		}
	}
}

func TestToolRegistry_LabelsMatchFrontend(t *testing.T) {
	// 前端 constants/agent.ts 里现在的名称：Go 下发后前端界面不变
	want := map[string][2]string{
		"canvas_get_state": {"读取画布", "读取"}, "canvas_apply_ops": {"修改画布", "修改"}, "canvas_arrange": {"整理布局", "整理"},
		"canvas_delete": {"申请删除", "删除"}, "canvas_inspect_image": {"查看图片", "看图"}, "plan_update": {"更新计划", "计划"},
		"ask_user": {"向你提问", "提问"}, "model_list": {"查看可用模型", "查模型"}, "generate_media": {"申请生成", "生成"},
		"task_get": {"查询任务", "查任务"}, "skill_search": {"搜索技能", "技能"}, "skill_read": {"读取技能", "技能"},
	}
	for _, d := range AgentToolDecls("all", true) {
		w, ok := want[d.Name]
		if !ok {
			t.Fatalf("多出未知工具 %s", d.Name)
		}
		if d.Label != w[0] {
			t.Errorf("%s 的 label=%q，应为 %q", d.Name, d.Label, w[0])
		}
	}
	for name, w := range want {
		label, verb := ToolLabel(name)
		if label != w[0] || verb != w[1] {
			t.Errorf("ToolLabel(%s)=(%q,%q)，应为 (%q,%q)", name, label, verb, w[0], w[1])
		}
	}
	if l, v := ToolLabel("no_such_tool"); l != "" || v != "" {
		t.Errorf("未知工具没有名称: (%q,%q)", l, v)
	}
}

func TestAgentBridge_ToolEventsCarryLabel(t *testing.T) {
	b := newBridgeEnv(t)
	tok, _ := b.token(t, "all")
	b.tool(t, tok, "canvas_get_state", map[string]any{})
	for _, typ := range []string{"tool.start", "tool.end"} {
		ev := b.repo.lastEvent(typ)
		if ev["label"] != "读取画布" || ev["verb"] != "读取" {
			t.Errorf("%s 要带界面名称，前端才不用硬编码: %v", typ, ev)
		}
	}
}
