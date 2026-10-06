package canvasgraph_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"video-canvas/internal/canvasgraph"
)

// 这两个用例读与前端共用的 fixture（web/src/tests/utils/canvas/agent-graph-fixture.test.ts 读同一份），
// 保证 Agent 在后端改画布的规则与前端手动操作一致。

func loadFixture(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile("../testdata/canvasgraph/" + name)
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
