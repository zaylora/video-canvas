package canvasgraph_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// 本文件覆盖 Agent 建节点、建组时的尺寸与放置：节点框按画幅算（与前端 node-frame.ts 一致），
// 新节点和新组延后放置，同一批 ops 里不论顺序如何都不重叠。

// frameOf 按画幅算节点框：短边 324。画幅取 data.mediaAspect，其次 params.aspect_ratio，都没有按 16:9。
// 测试用它还原「前端真实会渲染多大」，和被测代码各算各的。
func frameOf(n canvasgraph.Node) (w, h float64) {
	if n.IsGroup() {
		return n.Width(), n.Height()
	}
	ratio := 16.0 / 9
	if a, _ := n.Data()["mediaAspect"].(float64); a > 0 {
		ratio = a
	} else if p, _ := n.Data()["params"].(map[string]any); p != nil {
		if s, _ := p["aspect_ratio"].(string); s != "" {
			var a, b float64
			if _, err := fmt.Sscanf(s, "%f:%f", &a, &b); err == nil && a > 0 && b > 0 {
				ratio = a / b
			}
		}
	}
	if n.Kind() == "audio" {
		ratio = 1
	}
	if ratio >= 1 {
		return math.Round(324 * ratio), 324
	}
	return 324, math.Round(324 / ratio)
}

type box struct {
	id         string
	x, y, w, h float64
}

// boxOf 返回节点的画布绝对矩形。
func boxOf(g *canvasgraph.Graph, n canvasgraph.Node) box {
	p := n.Position()
	if pid := n.ParentID(); pid != "" {
		pp := g.Node(pid).Position()
		p.X, p.Y = p.X+pp.X, p.Y+pp.Y
	}
	w, h := frameOf(n)
	return box{n.ID(), p.X, p.Y, w, h}
}

func hit(a, b box) bool {
	return a.x < b.x+b.w && b.x < a.x+a.w && a.y < b.y+b.h && b.y < a.y+a.h
}

// overlapPairs 列出画布上重叠的节点对；组和它自己的成员之间的包含关系不算。
func overlapPairs(g *canvasgraph.Graph) []string {
	var out []string
	for i, a := range g.Nodes {
		for _, b := range g.Nodes[i+1:] {
			if a.ParentID() == b.ID() || b.ParentID() == a.ID() {
				continue
			}
			if hit(boxOf(g, a), boxOf(g, b)) {
				out = append(out, a.ID()+"×"+b.ID())
			}
		}
	}
	return out
}

func TestNodeFrameMatchesFixture(t *testing.T) {
	var fx struct {
		Cases []struct {
			Name        string
			Kind        string
			MediaAspect *float64
			Params      map[string]any
			W, H        float64
		} `json:"cases"`
	}
	loadFixture(t, "node-frame-cases.json", &fx)
	if len(fx.Cases) == 0 {
		t.Fatal("fixture 为空")
	}
	for _, c := range fx.Cases {
		t.Run(c.Name, func(t *testing.T) {
			data := map[string]any{"kind": c.Kind, "label": "n"}
			if c.MediaAspect != nil {
				data["mediaAspect"] = *c.MediaAspect
			}
			if c.Params != nil {
				data["params"] = c.Params
			}
			g := &canvasgraph.Graph{Nodes: []canvasgraph.Node{
				{"id": "n", "type": "canvas", "position": map[string]any{"x": 0.0, "y": 0.0}, "data": data},
			}}
			// 只有一个成员的组：组框 = 成员框 + 左右各 48、上 72、下 48，由此反推后端认定的节点框。
			res, err := canvasgraph.Apply(g, mustOps(t, `[{"op":"create_group","tempId":"g","memberIds":["n"]}]`), opts())
			if err != nil {
				t.Fatal(err)
			}
			grp := node(t, res.Graph, res.IDMap["g"])
			if w, h := grp.Width()-96, grp.Height()-120; w != c.W || h != c.H {
				t.Errorf("节点框应为 %.0f×%.0f，后端按 %.0f×%.0f 算", c.W, c.H, w, h)
			}
		})
	}
}

func TestCreateGroupFramesPortraitMembers(t *testing.T) {
	g := mustParse(t, `{"nodes":[
	  {"id":"a","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"立绘","mediaAspect":0.5625}},
	  {"id":"b","type":"canvas","position":{"x":500,"y":0},"data":{"kind":"image","label":"道具","params":{"aspect_ratio":"3:4"}}}]}`)
	res := mustApply(t, g, `[{"op":"create_group","tempId":"g","label":"组","memberIds":["a","b"]}]`)
	grp := boxOf(res.Graph, node(t, res.Graph, res.IDMap["g"]))
	for _, id := range []string{"a", "b"} {
		m := boxOf(res.Graph, node(t, res.Graph, id))
		if m.x < grp.x+48 || m.x+m.w > grp.x+grp.w-48 || m.y < grp.y+72 || m.y+m.h > grp.y+grp.h-48 {
			t.Errorf("组框 %+v 没有按真实尺寸框住 %s %+v", grp, id, m)
		}
	}
}

func TestAudioNodeArrangesAsSquare(t *testing.T) {
	g := mustParse(t, `{"nodes":[
	  {"id":"a","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"audio","label":"配音"}},
	  {"id":"b","type":"canvas","position":{"x":900,"y":0},"data":{"kind":"image","label":"图"}}]}`)
	res, err := canvasgraph.Arrange(g, canvasgraph.ArrangeTarget{NodeIDs: []string{"a", "b"}}, canvasgraph.LayoutRow, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 音频节点宽 324，所以 b 紧跟在 a 之后：324 + 120。
	if p := node(t, res.Graph, "b").Position(); p.X != 324+120 {
		t.Errorf("b 应在 x=444，实际 %.0f", p.X)
	}
}

// batchOps 是一批典型的 ops：建空组 G、两个自由节点、往 G 里加三个不同画幅的成员。
// 每个操作单独成项，测试里会打乱顺序。
var batchOps = map[string]string{
	"G":  `{"op":"create_group","tempId":"G","label":"组"}`,
	"F1": `{"op":"create_node","tempId":"F1","kind":"image","params":{"aspect_ratio":"16:9"}}`,
	"F2": `{"op":"create_node","tempId":"F2","kind":"image","params":{"aspect_ratio":"9:16"}}`,
	"M1": `{"op":"create_node","tempId":"M1","kind":"image","parentGroup":"G","params":{"aspect_ratio":"16:9"}}`,
	"M2": `{"op":"create_node","tempId":"M2","kind":"video","parentGroup":"G","params":{"aspect_ratio":"21:9"}}`,
	"M3": `{"op":"create_node","tempId":"M3","kind":"image","parentGroup":"G","params":{"aspect_ratio":"9:16"}}`,
}

const oneExisting = `{"nodes":[{"id":"E","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"已有"}}]}`

func permutations(a []string) [][]string {
	if len(a) <= 1 {
		return [][]string{append([]string(nil), a...)}
	}
	var out [][]string
	for i := range a {
		rest := append(append([]string(nil), a[:i]...), a[i+1:]...)
		for _, p := range permutations(rest) {
			out = append(out, append([]string{a[i]}, p...))
		}
	}
	return out
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func TestBatchCreateNeverOverlapsInAnyOrder(t *testing.T) {
	valid := 0
	for _, order := range permutations([]string{"G", "F1", "F2", "M1", "M2", "M3"}) {
		gi := indexOf(order, "G")
		if indexOf(order, "M1") < gi || indexOf(order, "M2") < gi || indexOf(order, "M3") < gi {
			continue // 成员引用的组必须先建，否则整批会被拒绝
		}
		valid++
		parts := make([]string, len(order))
		for i, k := range order {
			parts[i] = batchOps[k]
		}
		res := mustApply(t, mustParse(t, oneExisting), "["+strings.Join(parts, ",")+"]")
		if pairs := overlapPairs(res.Graph); len(pairs) > 0 {
			t.Fatalf("顺序 %v 出现重叠：%v", order, pairs)
		}
		// 三个成员都要在组框里。
		grp := boxOf(res.Graph, node(t, res.Graph, res.IDMap["G"]))
		for _, m := range []string{"M1", "M2", "M3"} {
			b := boxOf(res.Graph, node(t, res.Graph, res.IDMap[m]))
			if b.x < grp.x || b.y < grp.y || b.x+b.w > grp.x+grp.w || b.y+b.h > grp.y+grp.h {
				t.Fatalf("顺序 %v：%s %+v 不在组框 %+v 里", order, m, b, grp)
			}
		}
	}
	if valid != 180 {
		t.Fatalf("合法顺序应有 180 种，实际 %d", valid)
	}
}

func TestDeferredPlacementKeepsCreationOrderLeftToRight(t *testing.T) {
	res := mustApply(t, mustParse(t, oneExisting), `[
	  {"op":"create_node","tempId":"a","kind":"image"},
	  {"op":"create_node","tempId":"b","kind":"image"}]`)
	a, b := node(t, res.Graph, res.IDMap["a"]).Position(), node(t, res.Graph, res.IDMap["b"]).Position()
	if !(a.X > 576 && b.X > a.X+576) {
		t.Errorf("新节点应依次排在已有内容右侧：a=%+v b=%+v", a, b)
	}
	if a.Y != 0 || b.Y != 0 {
		t.Errorf("应与已有内容顶部对齐：a=%+v b=%+v", a, b)
	}
}

func TestExplicitPositionIsKeptAndOverlapReported(t *testing.T) {
	res := mustApply(t, mustParse(t, oneExisting),
		`[{"op":"create_node","tempId":"x","kind":"image","position":{"x":100,"y":50}}]`)
	x := node(t, res.Graph, res.IDMap["x"])
	if p := x.Position(); p.X != 100 || p.Y != 50 {
		t.Errorf("显式位置应原样保留，实际 %+v", p)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "重叠") ||
		!strings.Contains(res.Warnings[0], x.ID()) || !strings.Contains(res.Warnings[0], "E") {
		t.Errorf("应提醒 %s 与 E 重叠：%v", x.ID(), res.Warnings)
	}
}

func TestNoWarningWhenNothingOverlaps(t *testing.T) {
	res := mustApply(t, mustParse(t, oneExisting),
		`[{"op":"create_node","tempId":"x","kind":"image","position":{"x":2000,"y":0}}]`)
	if len(res.Warnings) != 0 {
		t.Errorf("不重叠不该有提醒：%v", res.Warnings)
	}
}

func TestMovedNewNodeIsNotReplaced(t *testing.T) {
	res := mustApply(t, mustParse(t, oneExisting), `[
	  {"op":"create_node","tempId":"x","kind":"image"},
	  {"op":"move","id":"x","position":{"x":3000,"y":900}}]`)
	if p := node(t, res.Graph, res.IDMap["x"]).Position(); p.X != 3000 || p.Y != 900 {
		t.Errorf("move 之后不应再被延后放置挪走，实际 %+v", p)
	}
}

func TestAddingExistingNodesToNewGroupKeepsTheirPositions(t *testing.T) {
	g := mustParse(t, `{"nodes":[
	  {"id":"a","type":"canvas","position":{"x":500,"y":300},"data":{"kind":"image","label":"A"}},
	  {"id":"b","type":"canvas","position":{"x":1300,"y":300},"data":{"kind":"image","label":"B"}}]}`)
	res := mustApply(t, g, `[
	  {"op":"create_group","tempId":"g","label":"组"},
	  {"op":"set_group","id":"g","addMembers":["a","b"]}]`)
	for id, want := range map[string]float64{"a": 500, "b": 1300} {
		if got := boxOf(res.Graph, node(t, res.Graph, id)); got.x != want || got.y != 300 {
			t.Errorf("%s 入组后视觉位置应不变 (%.0f,300)，实际 (%.0f,%.0f)", id, want, got.x, got.y)
		}
	}
	if pairs := overlapPairs(res.Graph); len(pairs) > 0 {
		t.Errorf("不应重叠：%v", pairs)
	}
}

func TestNewMemberGoesRightOfAllExistingMembers(t *testing.T) {
	g := mustParse(t, `{"nodes":[
	  {"id":"g","type":"group","position":{"x":0,"y":0},"width":1500,"height":600,"data":{"label":"组"}},
	  {"id":"a","type":"canvas","parentId":"g","position":{"x":800,"y":72},"data":{"kind":"image","label":"A"}},
	  {"id":"b","type":"canvas","parentId":"g","position":{"x":48,"y":72},"data":{"kind":"image","label":"B"}}]}`)
	res := mustApply(t, g, `[{"op":"create_node","tempId":"c","kind":"image","parentGroup":"g"}]`)
	if pairs := overlapPairs(res.Graph); len(pairs) > 0 {
		t.Errorf("新成员不该压住已有成员（最后一个成员不一定在最右）：%v", pairs)
	}
}
