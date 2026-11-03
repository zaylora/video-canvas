package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service/agent"
)

// show_card 是纯展示工具：校验和事件都在 Agent 服务里，Go 只负责声明、登记和排除。

func TestShowCard_DeclaredInEveryMode(t *testing.T) {
	for _, mode := range []string{"", "all", "script", "storyboard", "prompt", "drama", "admin"} {
		if !hasTool(AgentToolsForMode(mode), "show_card") {
			t.Errorf("模式 %q 应注册 show_card", mode)
		}
	}
}

func TestShowCard_DeclAndLabel(t *testing.T) {
	label, verb := ToolLabel("show_card")
	if label != "展示卡片" || verb != "展示" {
		t.Errorf("label/verb = %q/%q", label, verb)
	}
	for _, d := range AgentToolDecls("admin", false, false) {
		if d.Name != "show_card" {
			continue
		}
		if d.Scriptable {
			t.Error("展示卡片不给脚本调用")
		}
		for _, want := range []string{"ask_user", "ref", "summary", "table", "3 张"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("说明缺少 %q: %s", want, d.Description)
			}
		}
		var schema struct {
			Required   []string       `json:"required"`
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(d.Parameters, &schema); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"kind", "title", "tone", "rows", "columns", "notes", "sources"} {
			if _, ok := schema.Properties[k]; !ok {
				t.Errorf("参数缺少 %s", k)
			}
		}
		if len(schema.Required) != 3 {
			t.Errorf("required = %v，应为 kind/title/rows", schema.Required)
		}
		return
	}
	t.Fatal("admin 模式没有 show_card")
}

func TestShowCard_NotExecutableThroughPlatform(t *testing.T) {
	b := newBridgeEnv(t)
	_, err := b.br.ExecutePlatformTool(context.Background(), platformCtx(900005, "all"), "c1", "show_card", []byte(`{}`))
	wantAgentCode(t, err, errcode.ErrInvalidParams)
}
