package canvasgraph_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// 这两个用例读与前端共用的 fixture（web/src/tests/utils/canvas/agent-graph-fixture.test.ts 读同一份），
// 保证 Agent 在后端改画布的规则与前端手动操作一致。

func loadFixture(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile("../../testdata/canvasgraph/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func TestConnectRulesMatchFrontend(t *testing.T) {
	var fx struct {
		Cases []struct {
			From, To string
			OK       bool
		} `json:"cases"`
	}
	loadFixture(t, "connect-rules.json", &fx)
	if len(fx.Cases) != 16 {
		t.Fatalf("fixture 应覆盖 4×4 种组合，实际 %d", len(fx.Cases))
	}
	for _, c := range fx.Cases {
		t.Run(c.From+"→"+c.To, func(t *testing.T) {
			g := mustParse(t, `{}`)
			ops := mustOps(t, fmt.Sprintf(`[{"op":"create_node","tempId":"a","kind":%q},{"op":"create_node","tempId":"b","kind":%q},{"op":"connect","source":"a","target":"b"}]`, c.From, c.To))
			_, err := canvasgraph.Apply(g, ops, opts())
			if (err == nil) != c.OK {
				t.Errorf("%s → %s: ok=%v，实际 err=%v", c.From, c.To, c.OK, err)
			}
		})
	}
}

func TestArrangeMatchesFrontend(t *testing.T) {
	var fx struct {
		Cases []struct {
			Name   string
			Layout string
			Nodes  []struct {
				ID, Kind string
				X, Y     float64
			}
			Expected map[string]struct{ X, Y float64 }
		} `json:"cases"`
	}
	loadFixture(t, "arrange-cases.json", &fx)
	for _, c := range fx.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var sb strings.Builder
			ids := make([]string, 0, len(c.Nodes))
			for i, n := range c.Nodes {
				if i > 0 {
					sb.WriteString(",")
				}
				fmt.Fprintf(&sb, `{"id":%q,"type":"canvas","position":{"x":%v,"y":%v},"data":{"kind":%q,"label":%q}}`, n.ID, n.X, n.Y, n.Kind, n.ID)
				ids = append(ids, n.ID)
			}
			g := mustParse(t, `{"nodes":[`+sb.String()+`]}`)
			res, err := canvasgraph.Arrange(g, canvasgraph.ArrangeTarget{NodeIDs: ids}, canvasgraph.Layout(c.Layout), nil)
			if err != nil {
				t.Fatal(err)
			}
			for id, want := range c.Expected {
				got := node(t, res.Graph, id).Position()
				if got.X != want.X || got.Y != want.Y {
					t.Errorf("%s: 应在 (%v,%v)，实际 (%v,%v)", id, want.X, want.Y, got.X, got.Y)
				}
			}
		})
	}
}

type groupCase struct {
	Name   string
	Op     string
	Groups []struct {
		ID         string
		X, Y, W, H float64
	}
	Nodes []struct {
		ID, Kind, Parent string
		X, Y             float64
	}
	Members  []string
	Expected struct {
		Group *struct{ X, Y, W, H float64 }
		Nodes map[string]struct {
			X, Y   float64
			Parent *string
		}
	}
}

// buildGroupGraph 按 fixture 的描述拼出一张画布。
func buildGroupGraph(t *testing.T, c groupCase) *canvasgraph.Graph {
	var parts []string
	for _, g := range c.Groups {
		parts = append(parts, fmt.Sprintf(`{"id":%q,"type":"group","position":{"x":%v,"y":%v},"width":%v,"height":%v,"data":{"label":%q}}`, g.ID, g.X, g.Y, g.W, g.H, g.ID))
	}
	for _, n := range c.Nodes {
		parent := ""
		if n.Parent != "" {
			parent = fmt.Sprintf(`"parentId":%q,`, n.Parent)
		}
		parts = append(parts, fmt.Sprintf(`{"id":%q,"type":"canvas",%s"position":{"x":%v,"y":%v},"data":{"kind":%q,"label":%q}}`, n.ID, parent, n.X, n.Y, n.Kind, n.ID))
	}
	return mustParse(t, `{"nodes":[`+strings.Join(parts, ",")+`]}`)
}

func TestGroupMatchesFrontend(t *testing.T) {
	var fx struct{ Cases []groupCase }
	loadFixture(t, "group-cases.json", &fx)
	for _, c := range fx.Cases {
		t.Run(c.Name, func(t *testing.T) {
			g := buildGroupGraph(t, c)
			var res *canvasgraph.Result
			var err error
			switch c.Op {
			case "group":
				ids, _ := json.Marshal(c.Members)
				res, err = canvasgraph.Apply(g, mustOps(t, `[{"op":"create_group","tempId":"g","label":"组","memberIds":`+string(ids)+`}]`), canvasgraph.Options{NewID: func(string) string { return "g" }})
			case "ungroup":
				res, err = canvasgraph.Delete(g, []string{"g"}, nil)
			case "fit":
				// 把第一个成员移到它自己当前的位置：成员有变动，组框随之贴合。
				first := c.Nodes[0]
				res, err = canvasgraph.Apply(g, mustOps(t, fmt.Sprintf(`[{"op":"move","id":%q,"position":{"x":%v,"y":%v}}]`, first.ID, first.X, first.Y)), opts())
			default:
				t.Fatalf("未知 op %s", c.Op)
			}
			if err != nil {
				t.Fatal(err)
			}
			if want := c.Expected.Group; want != nil {
				grp := node(t, res.Graph, "g")
				got := struct{ X, Y, W, H float64 }{grp.Position().X, grp.Position().Y, grp.Width(), grp.Height()}
				if got != *want {
					t.Errorf("组框应为 %+v，实际 %+v", *want, got)
				}
			}
			for id, want := range c.Expected.Nodes {
				n := node(t, res.Graph, id)
				if p := n.Position(); p.X != want.X || p.Y != want.Y {
					t.Errorf("%s: 应在 (%v,%v)，实际 (%v,%v)", id, want.X, want.Y, p.X, p.Y)
				}
				switch {
				case want.Parent == nil && n.ParentID() != "":
					t.Errorf("%s: 不应有父组，实际 %s", id, n.ParentID())
				case want.Parent != nil && n.ParentID() != *want.Parent:
					t.Errorf("%s: 父组应为 %s，实际 %s", id, *want.Parent, n.ParentID())
				}
			}
		})
	}
}

// TestComposePromptMatchesFrontend 读与前端共用的提示词合成 fixture
// （web/src/tests/utils/tasks/prompt-compose-fixture.test.ts 读同一份），保证 Agent 生成和画布上点生成合成出一样的提示词。
func TestComposePromptMatchesFrontend(t *testing.T) {
	var fx struct {
		Cases []struct {
			Name  string
			Nodes []struct {
				ID, Kind, Label, Text string
				AssetID               string `json:"assetId"`
				// Character 是角色卡的人物数据：补充图和声音的素材 id
				Character *struct {
					Name   string
					Extras []string
					Voice  string
				}
			}
			Prompt string
			Expect string
		} `json:"cases"`
	}
	loadFixture(t, "prompt-compose.json", &fx)
	if len(fx.Cases) == 0 {
		t.Fatal("fixture 没有用例")
	}
	for _, c := range fx.Cases {
		t.Run(c.Name, func(t *testing.T) {
			nodes := []map[string]any{{"id": "target", "type": "canvas", "data": map[string]any{
				"kind": "video", "label": "目标", "params": map[string]any{"prompt": c.Prompt}}}}
			edges := []map[string]any{}
			for i, n := range c.Nodes {
				data := map[string]any{"kind": n.Kind, "label": n.Label}
				if n.Text != "" {
					data["text"] = n.Text
				}
				if n.AssetID != "" {
					data["assetId"] = n.AssetID
				}
				if c := n.Character; c != nil {
					extras := []any{}
					for _, id := range c.Extras {
						extras = append(extras, map[string]any{"assetId": id, "src": "/files/" + id})
					}
					var voice any
					if c.Voice != "" {
						voice = map[string]any{"mode": "audio", "assetId": c.Voice, "src": "/files/" + c.Voice}
					}
					data["character"] = map[string]any{"name": c.Name, "look": "基础形象", "extras": extras, "voice": voice}
				}
				nodes = append(nodes, map[string]any{"id": n.ID, "type": "canvas", "data": data})
				edges = append(edges, map[string]any{"id": fmt.Sprintf("e%d", i), "source": n.ID, "target": "target"})
			}
			raw, err := json.Marshal(map[string]any{"nodes": nodes, "edges": edges})
			if err != nil {
				t.Fatal(err)
			}
			src, err := canvasgraph.Generation(mustParse(t, string(raw)), "target")
			if err != nil {
				t.Fatal(err)
			}
			got := canvasgraph.ComposePrompt(src, []string{"image", "video", "audio"})
			if got != c.Expect {
				t.Errorf("提示词 = %q，期望 %q", got, c.Expect)
			}
		})
	}
}
