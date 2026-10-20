package canvasgraph_test

import (
	"fmt"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// 本文件覆盖 Agent 搭画布时的排版：节点按真实尺寸算、组框随成员立刻贴合、
// 本批新建的对象互相避让（不动已有对象）、canvas_arrange 能排列组。
// 复现场景见 docs/design/Agent调优Harness/Agent调优Harness.md 的 6.A。

type box struct{ x, y, w, h float64 }

func (a box) hit(b box) bool {
	return a.x < b.x+b.w && b.x < a.x+a.w && a.y < b.y+b.h && b.y < a.y+a.h
}

func (a box) inside(b box) bool {
	return a.x >= b.x && a.y >= b.y && a.x+a.w <= b.x+b.w && a.y+a.h <= b.y+b.h
}

// absBox 返回节点在画布上的绝对矩形，尺寸按前后端共用的规则算。
func absBox(g *canvasgraph.Graph, n canvasgraph.Node) box {
	p := n.Position()
	if pid := n.ParentID(); pid != "" {
		pp := g.Node(pid).Position()
		p = canvasgraph.Point{X: pp.X + p.X, Y: pp.Y + p.Y}
	}
	w, h := canvasgraph.NodeSize(n, canvasgraph.SizeHint{})
	return box{p.X, p.Y, w, h}
}

// topLevel 返回所有不属于任何组的对象（组和散落的节点）。
func topLevel(g *canvasgraph.Graph) []canvasgraph.Node {
	var out []canvasgraph.Node
	for _, n := range g.Nodes {
		if n.ParentID() == "" {
			out = append(out, n)
		}
	}
	return out
}

// assertNoOverlap 断言顶层对象两两不相交，每个组内的成员两两不相交并且完全落在组框内。
func assertNoOverlap(t *testing.T, g *canvasgraph.Graph) {
	t.Helper()
	tops := topLevel(g)
	for i := range tops {
		for j := i + 1; j < len(tops); j++ {
			a, b := absBox(g, tops[i]), absBox(g, tops[j])
			if a.hit(b) {
				t.Errorf("顶层对象 %s 与 %s 重叠: %+v %+v", tops[i].ID(), tops[j].ID(), a, b)
			}
		}
	}
	for _, grp := range g.Nodes {
		if !grp.IsGroup() {
			continue
		}
		var members []canvasgraph.Node
		for _, n := range g.Nodes {
			if n.ParentID() == grp.ID() {
				members = append(members, n)
			}
		}
		frame := absBox(g, grp)
		for i, m := range members {
			mb := absBox(g, m)
			if !mb.inside(frame) {
				t.Errorf("成员 %s 越出组 %s 的框: 成员 %+v 组框 %+v", m.ID(), grp.ID(), mb, frame)
			}
			for j := i + 1; j < len(members); j++ {
				if mb.hit(absBox(g, members[j])) {
					t.Errorf("组 %s 内成员 %s 与 %s 重叠", grp.ID(), m.ID(), members[j].ID())
				}
			}
		}
	}
}

// shotGroupOps 生成「一个镜头组」的操作：组 + n 个 9:16 的图片成员。
func shotGroupOps(tag string, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"op":"create_group","tempId":"g_%s","label":"镜头 %s"}`, tag, tag)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `,{"op":"create_node","tempId":"%s_%d","kind":"image","parentGroup":"g_%s","params":{"aspect_ratio":"9:16"}}`, tag, i, tag)
	}
	return b.String()
}

func TestGroupFrameCoversTallMembers(t *testing.T) {
	g := mustParse(t, basePayload)
	res := mustApply(t, g, "["+shotGroupOps("a", 3)+"]")
	grp := node(t, res.Graph, res.IDMap["g_a"])
	// 9:16 的图片是 324×576：组框高至少 上留白 72 + 576 + 下留白 48。
	if grp.Height() < 72+576+48 {
		t.Errorf("组框高 %.0f 框不住 576 高的成员", grp.Height())
	}
	assertNoOverlap(t, res.Graph)
}

func TestTwoShotGroupsInOneBatchDoNotOverlap(t *testing.T) {
	g := mustParse(t, basePayload)
	res := mustApply(t, g, "["+shotGroupOps("a", 3)+","+shotGroupOps("b", 3)+","+shotGroupOps("c", 3)+"]")
	assertNoOverlap(t, res.Graph)
}

func TestEmptyGroupsCreatedFirstStillDoNotOverlap(t *testing.T) {
	// 先建两个空组，再往里放节点：建组时两个组都是最小尺寸，放完节点后必须被推开。
	g := mustParse(t, basePayload)
	ops := `[{"op":"create_group","tempId":"g1","label":"一"},{"op":"create_group","tempId":"g2","label":"二"}`
	for i := 0; i < 3; i++ {
		ops += fmt.Sprintf(`,{"op":"create_node","tempId":"a%d","kind":"image","parentGroup":"g1","params":{"aspect_ratio":"9:16"}}`, i)
		ops += fmt.Sprintf(`,{"op":"create_node","tempId":"b%d","kind":"image","parentGroup":"g2","params":{"aspect_ratio":"9:16"}}`, i)
	}
	res := mustApply(t, g, ops+"]")
	assertNoOverlap(t, res.Graph)
}

func TestFreeNodesCreatedInOneBatchDoNotOverlap(t *testing.T) {
	g := mustParse(t, basePayload)
	ops := ""
	for i := 0; i < 4; i++ {
		if i > 0 {
			ops += ","
		}
		ops += fmt.Sprintf(`{"op":"create_node","tempId":"f%d","kind":"image","params":{"aspect_ratio":"9:16"}}`, i)
	}
	res := mustApply(t, g, "["+ops+"]")
	assertNoOverlap(t, res.Graph)
}

func TestNewGroupDoesNotLandOnExistingContent(t *testing.T) {
	g := mustParse(t, basePayload)
	res := mustApply(t, g, "["+shotGroupOps("a", 2)+"]")
	script := absBox(res.Graph, node(t, res.Graph, "n_script"))
	grp := absBox(res.Graph, node(t, res.Graph, res.IDMap["g_a"]))
	if script.hit(grp) {
		t.Errorf("新组压在了已有的剧本节点上: 剧本 %+v 组 %+v", script, grp)
	}
}

func TestExistingObjectsAreNeverMoved(t *testing.T) {
	// 用户原有的节点故意摆在新对象最可能落脚的地方，一个都不能被挪。
	g := mustParse(t, `{"nodes":[
	  {"id":"u1","type":"canvas","position":{"x":0,"y":404},"data":{"kind":"image","label":"U1"}},
	  {"id":"u2","type":"canvas","position":{"x":100,"y":900},"data":{"kind":"video","label":"U2"}},
	  {"id":"u3","type":"canvas","position":{"x":700,"y":50},"data":{"kind":"script","label":"U3"}}]}`)
	res := mustApply(t, g, "["+shotGroupOps("a", 3)+","+shotGroupOps("b", 3)+"]")
	for _, id := range []string{"u1", "u2", "u3"} {
		before, after := node(t, g, id).Position(), node(t, res.Graph, id).Position()
		if before != after {
			t.Errorf("已有节点 %s 被挪动: %+v → %+v", id, before, after)
		}
	}
	// 新建的组没有压住任何已有节点。
	for _, id := range []string{"u1", "u2", "u3"} {
		for _, tmp := range []string{"g_a", "g_b"} {
			if absBox(res.Graph, node(t, res.Graph, id)).hit(absBox(res.Graph, node(t, res.Graph, res.IDMap[tmp]))) {
				t.Errorf("新组 %s 压住了已有节点 %s", tmp, id)
			}
		}
	}
}

func TestGroupMembersWrapAfterFourPerRow(t *testing.T) {
	g := mustParse(t, `{}`)
	res := mustApply(t, g, "["+shotGroupOps("a", 5)+"]")
	first, fifth := node(t, res.Graph, res.IDMap["a_0"]), node(t, res.Graph, res.IDMap["a_4"])
	if fifth.Position().Y <= first.Position().Y {
		t.Errorf("第 5 个成员应换到第二行: 第 1 个 %+v 第 5 个 %+v", first.Position(), fifth.Position())
	}
	if fifth.Position().X != first.Position().X {
		t.Errorf("换行后应回到第一列: %+v vs %+v", first.Position(), fifth.Position())
	}
	assertNoOverlap(t, res.Graph)
}

// overlappingGroups 造两个压在一起的镜头组：先各自建好，再把第二个组挪到第一个组上。
func overlappingGroups(t *testing.T) (*canvasgraph.Graph, string, string) {
	t.Helper()
	ids := seqID() // 多次 Apply 共用同一个计数器，id 才不会撞
	apply := func(g *canvasgraph.Graph, raw string) *canvasgraph.Result {
		res, err := canvasgraph.Apply(g, mustOps(t, raw), canvasgraph.Options{NewID: ids})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		return res
	}
	built := apply(mustParse(t, `{}`), "["+shotGroupOps("a", 2)+"]")
	built2 := apply(built.Graph, "["+shotGroupOps("b", 2)+"]")
	a, b := built.IDMap["g_a"], built2.IDMap["g_b"]
	g := built2.Graph
	node(t, g, b).SetPosition(node(t, g, a).Position()) // 直接改图：Apply 的 move 不是用来制造重叠的
	return g, a, b
}

func TestArrangeCanArrangeGroups(t *testing.T) {
	g, a, b := overlappingGroups(t)
	if absBox(g, node(t, g, a)).hit(absBox(g, node(t, g, b))) == false {
		t.Fatal("前置条件：两个组应当是重叠的")
	}
	var members []canvasgraph.Node
	for _, n := range g.Nodes {
		if n.ParentID() == a {
			members = append(members, n)
		}
	}
	relBefore := members[0].Position()

	res, err := canvasgraph.Arrange(g, canvasgraph.ArrangeTarget{NodeIDs: []string{a, b}}, canvasgraph.LayoutColumn, nil)
	if err != nil {
		t.Fatalf("组应能参与排列: %v", err)
	}
	ga, gb := absBox(res.Graph, node(t, res.Graph, a)), absBox(res.Graph, node(t, res.Graph, b))
	top, bottom := ga, gb
	if gb.y < ga.y {
		top, bottom = gb, ga
	}
	if bottom.y != top.y+top.h+80 {
		t.Errorf("竖排后上下两组应隔 80: 上 %+v 下 %+v", top, bottom)
	}
	assertNoOverlap(t, res.Graph)
	// 成员坐标是相对组的，排列组时不该变。
	for _, n := range res.Graph.Nodes {
		if n.ParentID() == a && n.ID() == members[0].ID() && n.Position() != relBefore {
			t.Errorf("组内成员的相对坐标不应变: %+v → %+v", relBefore, n.Position())
		}
	}
}

func TestArrangeRejectsMixingGroupsWithMembers(t *testing.T) {
	g, a, b := overlappingGroups(t)
	var member string
	for _, n := range g.Nodes {
		if n.ParentID() == b {
			member = n.ID()
			break
		}
	}
	_, err := canvasgraph.Arrange(g, canvasgraph.ArrangeTarget{NodeIDs: []string{a, member}}, canvasgraph.LayoutRow, nil)
	if err == nil {
		t.Fatal("顶层的组和组内的成员不能一起排列")
	}
}
