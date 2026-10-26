package canvasgraph_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// assertTopLevelSpacing 用分离轴判定间距：横向至少 120 或纵向至少 80；无需两轴同时满足。
func assertTopLevelSpacing(t *testing.T, g *canvasgraph.Graph) {
	t.Helper()
	for i, a := range g.Nodes {
		if a.ParentID() != "" {
			continue
		}
		for _, b := range g.Nodes[i+1:] {
			if b.ParentID() != "" {
				continue
			}
			x, y := boxOf(g, a), boxOf(g, b)
			if x.x+x.w+120 > y.x && y.x+y.w+120 > x.x && x.y+x.h+80 > y.y && y.y+y.h+80 > x.y {
				t.Errorf("顶层对象应无重叠且留出间距：%+v / %+v", x, y)
			}
		}
	}
}

func applyGrowthOps(t *testing.T, g *canvasgraph.Graph, ops []canvasgraph.Op, opt canvasgraph.Options) *canvasgraph.Result {
	t.Helper()
	raw, err := json.Marshal(ops)
	if err != nil {
		t.Fatal(err)
	}
	r, err := canvasgraph.Apply(g, mustOps(t, string(raw)), opt)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestGroupGrowthAcrossBatches(t *testing.T) {
	g := mustParse(t, `{}`)
	opt := opts() // 跨批保持真实 id 唯一；tempId 只在当前批内有效。
	var ops []canvasgraph.Op
	for i := range 6 {
		ops = append(ops, canvasgraph.Op{Op: canvasgraph.OpCreateGroup, TempID: fmt.Sprintf("g%d", i)})
	}
	ops = append(ops, canvasgraph.Op{Op: canvasgraph.OpCreateNode, Kind: canvasgraph.KindScript})
	built := applyGrowthOps(t, g, ops, opt)
	g = built.Graph
	counts := []int{3, 6, 9, 21, 6, 3}
	wantRelative := map[string]canvasgraph.Point{}
	for round := range 7 { // 每批最多给每组加 3 个成员，复现多轮调用。
		ops = nil
		for i, count := range counts {
			gid := built.IDMap[fmt.Sprintf("g%d", i)]
			for j := round * 3; j < min(count, (round+1)*3); j++ {
				aspect := "16:9"
				if j%2 == 1 {
					aspect = "9:16"
				}
				ops = append(ops, canvasgraph.Op{Op: canvasgraph.OpCreateNode, Kind: canvasgraph.KindImage,
					ParentGroup: gid, Params: map[string]any{"aspect_ratio": aspect}})
			}
		}
		res := applyGrowthOps(t, g, ops, opt)
		for id, want := range wantRelative {
			if got := node(t, res.Graph, id).Position(); got != want {
				t.Errorf("批 %d：已有成员 %s 的相对坐标被改乱：%+v → %+v", round, id, want, got)
			}
		}
		for _, n := range res.Graph.Nodes {
			if n.ParentID() == "" {
				continue
			}
			wantRelative[n.ID()] = n.Position()
			grp := node(t, res.Graph, n.ParentID())
			p, w, h := n.Position(), 0.0, 0.0
			w, h = frameOf(n)
			if p.X < 48 || p.Y < 72 || p.X+w+48 > grp.Width() || p.Y+h+48 > grp.Height() {
				t.Errorf("成员 %s 未完整处于组框内", n.ID())
			}
		}
		assertTopLevelSpacing(t, res.Graph)
		if len(res.Warnings) != 0 {
			t.Errorf("自动消解后不应有提醒：%v", res.Warnings)
		}
		g = res.Graph
	}
}

// growthFixture 只包含占位标题，g 将从 200×140 撑大到 672×444。
const growthFixture = `{"nodes":[
 {"id":"g","type":"group","position":{"x":0,"y":0},"width":200,"height":140,"data":{"label":"组A"}},
 {"id":"right","type":"group","position":{"x":320,"y":0},"width":672,"height":444,"data":{"label":"组B"}},
 {"id":"above","type":"group","position":{"x":0,"y":-1000},"width":200,"height":140,"data":{"label":"组C"}},
 {"id":"member","type":"canvas","parentId":"right","position":{"x":48,"y":72},"data":{"kind":"image"}},
 {"id":"chain","type":"canvas","position":{"x":1112,"y":0},"data":{"kind":"image"}},
 {"id":"far","type":"canvas","position":{"x":9000,"y":0},"data":{"kind":"image"}},
 {"id":"left","type":"canvas","position":{"x":-1000,"y":0},"data":{"kind":"image"}}
]}`

func TestGroupGrowthMovesOnlyCollisionChainAndReverts(t *testing.T) {
	g := mustParse(t, growthFixture)
	before := marshal(t, g)
	r := mustApply(t, g, `[{"op":"create_node","kind":"image","parentGroup":"g"}]`)
	for _, id := range []string{"g", "left", "above", "far", "member"} {
		if got, want := node(t, r.Graph, id).Position(), node(t, g, id).Position(); got != want {
			t.Errorf("%s 不该移动：%+v → %+v", id, want, got)
		}
	}
	for id, wantX := range map[string]float64{"right": 792, "chain": 1584} {
		if p := node(t, r.Graph, id).Position(); p.X != wantX || p.Y != 0 {
			t.Errorf("%s 级联推动位置 = %+v，应为 (%.0f,0)", id, p, wantX)
		}
		found := false
		for _, c := range r.Changes {
			if c.ID == id && c.Op == canvasgraph.ChangeUpdate && c.Before["position"] != nil && c.After["position"] != nil {
				found = true
			}
		}
		if !found {
			t.Errorf("%s 的推动必须进入 diff/patch", id)
		}
	}
	assertTopLevelSpacing(t, r.Graph)
	rr := canvasgraph.Revert(r.Graph, r.Changes)
	if len(rr.Skipped) != 0 || marshal(t, rr.Graph) != before {
		t.Fatalf("撤销应完整还原推动和建节点：%+v", rr.Skipped)
	}
}

func TestGroupGrowthKeepsNonOverlappingLayout(t *testing.T) {
	g := mustParse(t, strings.Replace(growthFixture, `"x":320`, `"x":3000`, 1))
	r := mustApply(t, g, `[{"op":"create_node","kind":"image","parentGroup":"g"}]`)
	for _, n := range g.Nodes {
		if got := node(t, r.Graph, n.ID()).Position(); got != n.Position() {
			t.Errorf("不相交时 %s 位置不应变化", n.ID())
		}
	}
}

func TestGroupGrowthDoesNotRepairOldOverlapWhenFrameDoesNotGrow(t *testing.T) {
	for _, x := range []float64{48, 100} {
		g := mustParse(t, `{"nodes":[
 {"id":"g","type":"group","position":{"x":0,"y":0},"width":1200,"height":444},
 {"id":"a","type":"canvas","parentId":"g","position":{"x":48,"y":72},"data":{"kind":"image"}},
 {"id":"b","type":"canvas","parentId":"g","position":{"x":576,"y":72},"data":{"kind":"image"}},
 {"id":"n","type":"canvas","position":{"x":800,"y":0},"data":{"kind":"image"}}]}`)
		r := applyGrowthOps(t, g, []canvasgraph.Op{{Op: canvasgraph.OpMove, ID: "b", Position: &canvasgraph.Point{X: x, Y: 72}}}, opts())
		if p := node(t, r.Graph, "n").Position(); p != node(t, g, "n").Position() {
			t.Errorf("只缩小组框不应修复旧重叠：%+v", p)
		}
	}
}

func TestGroupGrowthLongCascade(t *testing.T) {
	g := mustParse(t, `{"nodes":[{"id":"g","type":"group","position":{"x":0,"y":0},"width":200,"height":140}]}`)
	for i := range 100 {
		n := canvasgraph.Node{"id": fmt.Sprintf("n%03d", i), "type": "canvas", "data": map[string]any{"kind": "image"}}
		n.SetPosition(canvasgraph.Point{X: 320 + float64(i)*696})
		g.Nodes = append(g.Nodes, n)
	}
	r := mustApply(t, g, `[{"op":"create_node","kind":"image","parentGroup":"g"}]`)
	assertTopLevelSpacing(t, r.Graph)
	if p := node(t, r.Graph, "n099").Position(); p.X != 792+99*696 {
		t.Errorf("推动应传到链尾：%+v", p)
	}
}

func TestGroupGrowthPushesBelowAndPreservesUpstreamOverlap(t *testing.T) {
	for _, tc := range []struct {
		name     string
		x, y     float64
		want     canvasgraph.Point
		warnings int
	}{
		{"同列下方", 0, 220, canvasgraph.Point{X: 0, Y: 524}, 0},
		{"左方已有重叠", -100, 0, canvasgraph.Point{X: -100, Y: 0}, 1},
		{"上方已有重叠", 0, -100, canvasgraph.Point{X: 0, Y: -100}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := mustParse(t, fmt.Sprintf(`{"nodes":[
 {"id":"g","type":"group","position":{"x":0,"y":0},"width":200,"height":140},
 {"id":"n","type":"canvas","position":{"x":%v,"y":%v},"data":{"kind":"image"}}]}`, tc.x, tc.y))
			r := mustApply(t, g, `[{"op":"create_node","kind":"image","parentGroup":"g"}]`)
			if p := node(t, r.Graph, "n").Position(); p != tc.want || len(r.Warnings) != tc.warnings {
				t.Errorf("位置=%+v 提醒=%v，应为 %+v / %d 条", p, r.Warnings, tc.want, tc.warnings)
			}
		})
	}
}

func TestGroupGrowthHonorsExplicitPositions(t *testing.T) {
	for _, op := range []string{
		`{"op":"move","id":"right","position":{"x":320,"y":0}}`,
		`{"op":"move","id":"chain","position":{"x":400,"y":0}}`,
		`{"op":"create_node","tempId":"fixed","kind":"image","position":{"x":400,"y":0}}`,
	} {
		t.Run(op, func(t *testing.T) {
			g := mustParse(t, growthFixture)
			r := mustApply(t, g, `[{"op":"create_node","kind":"image","parentGroup":"g"},`+op+`]`)
			var specified canvasgraph.Op
			if err := json.Unmarshal([]byte(op), &specified); err != nil {
				t.Fatal(err)
			}
			id := specified.ID
			if id == "" {
				id = r.IDMap[specified.TempID]
			}
			if got := node(t, r.Graph, id).Position(); got != *specified.Position {
				t.Errorf("显式定位不可推动：%+v", got)
			}
			if len(r.Warnings) == 0 || len(r.Warnings) > 5 {
				t.Errorf("无法消解应保留最多 5 条提醒：%v", r.Warnings)
			}
		})
	}
}

func TestGroupGrowthAvoidsExplicitObstacleFurtherRight(t *testing.T) {
	g := mustParse(t, `{"nodes":[
 {"id":"g","type":"group","position":{"x":0,"y":0},"width":200,"height":140},
 {"id":"n","type":"canvas","position":{"x":320,"y":0},"data":{"kind":"image"}},
 {"id":"fixed","type":"canvas","position":{"x":1200,"y":0},"data":{"kind":"image"}}]}`)
	r := mustApply(t, g, `[{"op":"create_node","kind":"image","parentGroup":"g"},
 {"op":"move","id":"fixed","position":{"x":1200,"y":0}}]`)
	if p := node(t, r.Graph, "fixed").Position(); p.X != 1200 || p.Y != 0 {
		t.Errorf("显式障碍应固定：%+v", p)
	}
	if p := node(t, r.Graph, "n").Position(); p.X != 1896 || p.Y != 0 {
		t.Errorf("被推对象应绕过固定障碍：%+v", p)
	}
	assertTopLevelSpacing(t, r.Graph)
	if len(r.Warnings) != 0 {
		t.Errorf("可绕过显式障碍时不应遗留碰撞：%v", r.Warnings)
	}
}

func TestOverlapWarningsKeepOrphanMemberAsObject(t *testing.T) {
	g := mustParse(t, `{"nodes":[
 {"id":"orphan","type":"canvas","parentId":"missing","position":{"x":100,"y":0},"data":{"kind":"image"}},
 {"id":"n","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image"}}]}`)
	r := mustApply(t, g, `[{"op":"move","id":"n","position":{"x":0,"y":0}}]`)
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "orphan") {
		t.Errorf("旧存档缺失父组时仍应按实际节点提醒：%v", r.Warnings)
	}
}

func TestGroupGrowthDeterministic(t *testing.T) {
	g := mustParse(t, growthFixture)
	var want string
	var changes []canvasgraph.Change
	for i := range 30 {
		r := mustApply(t, g, `[{"op":"create_node","kind":"image","parentGroup":"g"},
 {"op":"create_node","kind":"image","parentGroup":"right"}]`)
		got := marshal(t, r.Graph)
		if i > 0 && (got != want || !reflect.DeepEqual(r.Changes, changes)) {
			t.Fatal("相同输入的图或 diff 不确定")
		}
		want, changes = got, r.Changes
	}
}

func TestArrangeGroupGrowthPushesNeighbors(t *testing.T) {
	for _, target := range []canvasgraph.ArrangeTarget{{GroupID: "g"}, {NodeIDs: []string{"a", "b"}}} {
		g := mustParse(t, `{"nodes":[
 {"id":"g","type":"group","position":{"x":0,"y":0},"width":672,"height":848},
 {"id":"a","type":"canvas","parentId":"g","position":{"x":48,"y":72},"data":{"kind":"image"}},
 {"id":"b","type":"canvas","parentId":"g","position":{"x":48,"y":476},"data":{"kind":"image"}},
 {"id":"n","type":"canvas","position":{"x":792,"y":0},"data":{"kind":"image"}}]}`)
		r, err := canvasgraph.Arrange(g, target, canvasgraph.LayoutRow, nil)
		if err != nil {
			t.Fatal(err)
		}
		if node(t, r.Graph, "g").Position() != node(t, g, "g").Position() || node(t, r.Graph, "n").Position().X != 1488 {
			t.Errorf("排组成员应保持组锚点并推开邻居：%s", marshal(t, r.Graph))
		}
		assertTopLevelSpacing(t, r.Graph)
		if rr := canvasgraph.Revert(r.Graph, r.Changes); marshal(t, rr.Graph) != marshal(t, g) {
			t.Fatal("排列及其推动应可撤销")
		}
	}
}

func TestOverlapWarningsCollapseForeignMembersAndDiversifyPairs(t *testing.T) {
	g := mustParse(t, `{"nodes":[
 {"id":"a","type":"group","position":{"x":0,"y":0},"width":2000,"height":1000,"data":{"label":"A"}},
 {"id":"b","type":"group","position":{"x":300,"y":0},"width":1000,"height":800,"data":{"label":"B"}},
 {"id":"bm1","type":"canvas","parentId":"b","position":{"x":48,"y":72},"data":{"kind":"image"}},
 {"id":"bm2","type":"canvas","parentId":"b","position":{"x":48,"y":450},"data":{"kind":"image"}},
 {"id":"c","type":"group","position":{"x":700,"y":0},"width":1000,"height":800,"data":{"label":"C"}},
 {"id":"d","type":"group","position":{"x":900,"y":0},"width":1000,"height":800,"data":{"label":"D"}}
 ]}`)
	r := mustApply(t, g, `[
 {"op":"move","id":"a","position":{"x":0,"y":0}},
 {"op":"move","id":"b","position":{"x":300,"y":0}},
 {"op":"move","id":"c","position":{"x":700,"y":0}},
 {"op":"move","id":"d","position":{"x":900,"y":0}}]`)
	if len(r.Warnings) != 5 {
		t.Fatalf("6 对重叠应最多提醒 5 条：%v", r.Warnings)
	}
	for _, w := range r.Warnings {
		if strings.Contains(w, "bm1") || strings.Contains(w, "bm2") || !strings.Contains(w, "canvas_arrange 只重排组内成员") || !strings.Contains(w, "逐个 move") {
			t.Errorf("应按顶层对象对去重并明确组的移动方法：%s", w)
		}
	}
	if !strings.Contains(r.Warnings[0], "（a）") || !strings.Contains(r.Warnings[0], "（b）") ||
		!strings.Contains(r.Warnings[1], "（c）") || !strings.Contains(r.Warnings[1], "（d）") {
		t.Errorf("应先覆盖不同对象对，不能同一组刷满：%v", r.Warnings)
	}
}

func TestGroupGrowthCanvas19Geometry(t *testing.T) {
	// 只摘录组的位置、尺寸和成员数；成员坐标按占位网格构造，不包含原用户的文本、id 或媒体。
	shapes := []struct {
		x, y, w, h float64
		count      int
	}{
		{1436, 155.2, 1812, 1352, 6}, {1756, 155.2, 1812, 1352, 6},
		{2076, 155.2, 1812, 2008, 9}, {2396, 155.2, 3456, 3320, 21},
		{2716, 155.2, 1812, 1352, 6}, {3036, 155.2, 1812, 1352, 6},
		{3308, 83.2, 2760, 1252, 10},
	}
	g := mustParse(t, `{}`)
	var ops []canvasgraph.Op
	for i, s := range shapes {
		gid := fmt.Sprintf("g%d", i)
		grp := canvasgraph.Node{"id": gid, "type": "group", "width": s.w, "height": s.h, "data": map[string]any{"label": "占位组"}}
		grp.SetPosition(canvasgraph.Point{X: s.x, Y: s.y})
		g.Nodes = append(g.Nodes, grp)
		for j := range s.count {
			id := fmt.Sprintf("m%d_%d", i, j)
			p := canvasgraph.Point{X: 48 + float64(j%3)*500, Y: 72 + float64(j/3)*300}
			if j == s.count-1 {
				p = canvasgraph.Point{X: s.w - 48 - 576, Y: s.h - 48 - 324}
			}
			n := canvasgraph.Node{"id": id, "type": "canvas", "parentId": gid, "data": map[string]any{"kind": "image", "label": "占位节点"}}
			n.SetPosition(p)
			g.Nodes = append(g.Nodes, n)
			if j == s.count-1 {
				p.X++ // 每组向右扩 1，走真实的 Apply → refit → 消解路径。
				ops = append(ops, canvasgraph.Op{Op: canvasgraph.OpMove, ID: id, Position: &p})
			}
		}
	}
	r := applyGrowthOps(t, g, ops, opts())
	assertTopLevelSpacing(t, r.Graph)
}

func TestCaptureFrameDoesNotRefitSourceGroup(t *testing.T) {
	g := mustParse(t, `{"nodes":[
 {"id":"g","type":"group","position":{"x":0,"y":0},"width":672,"height":444},
 {"id":"v","type":"canvas","parentId":"g","position":{"x":48,"y":72},"data":{"kind":"video","assetId":"11"}},
 {"id":"other","type":"group","position":{"x":792,"y":0},"width":200,"height":140}]}`)
	r, err := canvasgraph.AddFrameNodes(g, "v", []canvasgraph.FrameSpec{frame("占位帧")}, opts())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"g", "other"} {
		if !reflect.DeepEqual(node(t, g, id), node(t, r.Graph, id)) {
			t.Errorf("截帧不改源组或邻居：%s", id)
		}
	}
	if node(t, r.Graph, r.IDMap["frame0"]).ParentID() != "" {
		t.Fatal("当前截帧创建的是顶层节点")
	}
}
