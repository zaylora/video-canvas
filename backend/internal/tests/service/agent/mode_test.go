package agent_test

import (
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// newModeEnv 给桥配好视频、图片模型：veo 三种方式都支持，kling 只有文生视频，flux 是自动判断的图片模型。
func newModeEnv(t *testing.T) (*bridgeEnv, string) {
	t.Helper()
	b := newBridgeEnv(t)
	b.reg.models = append(b.reg.models,
		provider.ModelInfo{Key: "veo", Kind: "video", Label: "Veo", Capabilities: modelcfg.Capabilities{Ops: []string{"t2v", "i2v", "omni"}}},
		provider.ModelInfo{Key: "kling", Kind: "video", Label: "Kling", Capabilities: modelcfg.Capabilities{Ops: []string{"t2v"}}},
		provider.ModelInfo{Key: "flux", Kind: "image", Label: "Flux", Capabilities: modelcfg.Capabilities{Ops: []string{"t2i", "i2i"}}},
	)
	b.repo.canvas.PayloadJSON = []byte(`{"nodes":[],"edges":[]}`)
	tok, _ := b.token(t, "all")
	return b, tok
}

func modeGraphOf(t *testing.T, b *bridgeEnv) *canvasgraph.Graph {
	t.Helper()
	g, err := canvasgraph.Parse(b.repo.canvas.PayloadJSON)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func paramOpOf(n canvasgraph.Node) any {
	params, _ := n.Data()["params"].(map[string]any)
	return params["op"]
}

func TestApplyOps_SetMode(t *testing.T) {
	t.Run("建视频节点时选方式：写进 params.op", func(t *testing.T) {
		b, tok := newModeEnv(t)
		res := b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
			{"op": "create_node", "tempId": "v", "kind": "video", "label": "镜头", "model": "veo", "mode": "omni"},
		}})
		if res.IsError {
			t.Fatalf("res=%+v", res)
		}
		g := modeGraphOf(t, b)
		if len(g.Nodes) != 1 || paramOpOf(g.Nodes[0]) != "omni" {
			t.Fatalf("params.op 应为 omni: %+v", g.Nodes)
		}
	})

	t.Run("模型不支持这种方式：整批不生效，说明可选项", func(t *testing.T) {
		b, tok := newModeEnv(t)
		res := b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
			{"op": "create_node", "kind": "video", "model": "kling", "mode": "omni"},
		}})
		if !res.IsError || !strings.Contains(res.Content, "不支持") || !strings.Contains(res.Content, "t2v") {
			t.Fatalf("res=%+v", res)
		}
		if len(modeGraphOf(t, b).Nodes) != 0 {
			t.Error("失败时不该改画布")
		}
	})

	t.Run("自动判断的图片模型不用设方式", func(t *testing.T) {
		b, tok := newModeEnv(t)
		res := b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
			{"op": "create_node", "kind": "image", "model": "flux", "mode": "i2i"},
		}})
		if !res.IsError || !strings.Contains(res.Content, "自动") {
			t.Fatalf("res=%+v", res)
		}
	})

	t.Run("模型不在可用清单里：不校验是否支持，只检查种类", func(t *testing.T) {
		b, tok := newModeEnv(t)
		ok := b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
			{"op": "create_node", "kind": "video", "model": "ghost", "mode": "i2v"},
		}})
		if ok.IsError {
			t.Fatalf("未知模型不该因 mode 被拒: %+v", ok)
		}
	})

	t.Run("直接往 params 里写 op 被拒绝并指向 mode", func(t *testing.T) {
		b, tok := newModeEnv(t)
		res := b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
			{"op": "create_node", "kind": "video", "model": "veo", "params": map[string]any{"op": "i2v"}},
		}})
		if !res.IsError || !strings.Contains(res.Content, "mode") {
			t.Fatalf("res=%+v", res)
		}
	})
}

func TestApplyOps_ModeWarnings(t *testing.T) {
	apply := func(b *bridgeEnv, tok, mode string) map[string]any {
		t.Helper()
		video := map[string]any{"op": "create_node", "tempId": "v", "kind": "video", "label": "镜头", "model": "veo"}
		if mode != "" {
			video["mode"] = mode
		}
		res := b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
			{"op": "create_node", "tempId": "i", "kind": "image", "label": "角色"},
			video,
			{"op": "connect", "source": "i", "target": "v"},
		}})
		if res.IsError {
			t.Fatalf("res=%+v", res)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	t.Run("文生视频却连了参考图：结果里带警告，但照常写入", func(t *testing.T) {
		b, tok := newModeEnv(t)
		out := apply(b, tok, "t2v")
		warns, _ := out["warnings"].([]any)
		if len(warns) != 1 || !strings.Contains(warns[0].(string), "文生视频") {
			t.Fatalf("warnings = %v", out["warnings"])
		}
		if len(modeGraphOf(t, b).Edges) != 1 {
			t.Error("警告不拦截，连线应已写入")
		}
	})

	t.Run("方式和参考对得上：没有警告字段", func(t *testing.T) {
		b, tok := newModeEnv(t)
		if out := apply(b, tok, "i2v"); out["warnings"] != nil {
			t.Fatalf("不该有警告: %v", out["warnings"])
		}
	})

	t.Run("没选方式但连了图：生成时按图生走，不警告", func(t *testing.T) {
		b, tok := newModeEnv(t)
		if out := apply(b, tok, ""); out["warnings"] != nil {
			t.Fatalf("不该有警告: %v", out["warnings"])
		}
	})
}

func TestGetState_ShowsMode(t *testing.T) {
	b, tok := newModeEnv(t)
	b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
		{"op": "create_node", "tempId": "v", "kind": "video", "label": "镜头", "model": "veo", "mode": "i2v"},
		{"op": "create_node", "tempId": "p", "kind": "image", "label": "图", "model": "flux"},
		{"op": "create_node", "tempId": "n", "kind": "video", "label": "没选", "model": "veo"},
	}})
	g := modeGraphOf(t, b)
	var ids []string
	for _, n := range g.Nodes {
		ids = append(ids, n.ID())
	}
	res := b.tool(t, tok, "canvas_get_state", map[string]any{"nodeIds": ids})
	if res.IsError {
		t.Fatalf("res=%+v", res)
	}
	var out struct {
		Nodes []struct {
			Data map[string]any `json:"data"`
			Mode *struct {
				Op      string   `json:"op"`
				Options []string `json:"options"`
				Auto    bool     `json:"auto"`
			} `json:"mode"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatal(err)
	}
	byLabel := map[string]int{}
	for i, n := range out.Nodes {
		byLabel[n.Data["label"].(string)] = i
	}
	if m := out.Nodes[byLabel["镜头"]].Mode; m == nil || m.Op != "i2v" || len(m.Options) != 3 {
		t.Errorf("镜头 mode = %+v", m)
	}
	if m := out.Nodes[byLabel["没选"]].Mode; m == nil || m.Op != "" || len(m.Options) != 3 {
		t.Errorf("没选方式的节点应给出可选项、op 为空: %+v", m)
	}
	if m := out.Nodes[byLabel["图"]].Mode; m == nil || !m.Auto {
		t.Errorf("自动判断的图片模型: %+v", m)
	}
}
