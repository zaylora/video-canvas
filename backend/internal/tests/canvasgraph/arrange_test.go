package canvasgraph_test

import (
	"testing"

	"video-canvas/internal/canvasgraph"
)

const threeNodes = `{"nodes":[
  {"id":"a","type":"canvas","position":{"x":100,"y":50},"data":{"kind":"image","label":"A"}},
  {"id":"b","type":"canvas","position":{"x":900,"y":400},"data":{"kind":"image","label":"B"}},
  {"id":"c","type":"canvas","position":{"x":300,"y":700},"data":{"kind":"video","label":"C"}}]}`

func TestArrangeRow(t *testing.T) {
	g := mustParse(t, threeNodes)
	res, err := canvasgraph.Arrange(g, canvasgraph.ArrangeTarget{NodeIDs: []string{"a", "b", "c"}}, canvasgraph.LayoutRow, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 按 x 排序 a(100) c(300) b(900)；起点是左上角 (100,50)；图片宽 384、间距 120，视频宽 432。
	wantX := map[string]float64{"a": 100, "c": 100 + 384 + 120, "b": 100 + 384 + 120 + 432 + 120}
	for id, x := range wantX {
		p := node(t, res.Graph, id).Position()
		if p.X != x || p.Y != 50 {
			t.Errorf("%s 应在 (%.0f,50)，实际 (%.0f,%.0f)", id, x, p.X, p.Y)
		}
	}
	// a 本来就在起点，位置没变，不应出现在差异里。
	if len(res.Changes) != 2 {
		t.Errorf("应记录 2 个节点的位置变化: %d", len(res.Changes))
	}
}

func TestArrangeGridColumns(t *testing.T) {
	g := mustParse(t, `{"nodes":[`+
		`{"id":"1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"1"}},`+
		`{"id":"2","type":"canvas","position":{"x":10,"y":0},"data":{"kind":"image","label":"2"}},`+
		`{"id":"3","type":"canvas","position":{"x":20,"y":0},"data":{"kind":"image","label":"3"}},`+
		`{"id":"4","type":"canvas","position":{"x":30,"y":0},"data":{"kind":"image","label":"4"}}]}`)
	res, err := canvasgraph.Arrange(g, canvasgraph.ArrangeTarget{NodeIDs: []string{"1", "2", "3", "4"}}, canvasgraph.LayoutGrid, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 4 个节点 → 2 列；第 4 个在第 2 行第 2 列。
	p := node(t, res.Graph, "4").Position()
	if p.X != 384+120 || p.Y != 216+80 {
		t.Errorf("第 4 个应在 (504,296)，实际 (%.0f,%.0f)", p.X, p.Y)
	}
}

func TestArrangeGroupKeepsMembersInsideAndRefits(t *testing.T) {
	g := mustParse(t, basePayload)
	built := mustApply(t, g, shotOps)
	gid := built.IDMap["g1"]
	res, err := canvasgraph.Arrange(built.Graph, canvasgraph.ArrangeTarget{GroupID: gid}, canvasgraph.LayoutColumn, nil)
	if err != nil {
		t.Fatal(err)
	}
	grp := node(t, res.Graph, gid)
	for _, temp := range []string{"a", "b", "c"} {
		n := node(t, res.Graph, built.IDMap[temp])
		if n["parentId"] != gid {
			t.Errorf("%s 排列后仍应在组里", temp)
		}
		p := n.Position()
		if p.X < 0 || p.Y < 0 || p.X > grp.Width() || p.Y > grp.Height() {
			t.Errorf("%s 在组框外: %+v 组 %.0fx%.0f", temp, p, grp.Width(), grp.Height())
		}
	}
	// 竖排后组应该比横排时更高。
	if grp.Height() <= node(t, built.Graph, gid).Height() {
		t.Errorf("竖排后组应变高: %.0f → %.0f", node(t, built.Graph, gid).Height(), grp.Height())
	}
}

func TestArrangeErrors(t *testing.T) {
	g := mustParse(t, threeNodes)
	for name, tgt := range map[string]canvasgraph.ArrangeTarget{
		"空目标":    {},
		"不存在的节点": {NodeIDs: []string{"zzz"}},
		"不存在的组":  {GroupID: "zzz"},
	} {
		if _, err := canvasgraph.Arrange(g, tgt, canvasgraph.LayoutRow, nil); err == nil {
			t.Errorf("%s 应报错", name)
		}
	}
	if _, err := canvasgraph.Arrange(g, canvasgraph.ArrangeTarget{NodeIDs: []string{"a"}}, "spiral", nil); err == nil {
		t.Error("未知排列方式应报错")
	}
}
