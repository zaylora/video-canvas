package canvasgraph_test

import (
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

func TestRevertRestoresOriginal(t *testing.T) {
	g := mustParse(t, basePayload)
	before := marshal(t, g)
	built := mustApply(t, g, shotOps)
	edited := mustApply(t, built.Graph, `[{"op":"update_node","id":"n_script","prompt":"黄昏"}]`)

	all := append(append([]canvasgraph.Change{}, built.Changes...), edited.Changes...)
	// 撤销要对"当前最新的图"操作。
	rr := canvasgraph.Revert(edited.Graph, all)
	if len(rr.Skipped) != 0 {
		t.Fatalf("没人动过，不应有跳过项: %+v", rr.Skipped)
	}
	if got := marshal(t, rr.Graph); got != before {
		t.Errorf("撤销后应回到原样\n原: %s\n现: %s", before, got)
	}
}

func TestRevertSkipsUserEditedField(t *testing.T) {
	g := mustParse(t, basePayload)
	res := mustApply(t, g, `[{"op":"update_node","id":"n_script","prompt":"黄昏","label":"剧本 v2"}]`)
	// 用户之后把提示词改成了「清晨」，标题没动。
	cur := res.Graph.Clone()
	d := node(t, cur, "n_script").Data()
	d["prompt"] = "清晨"
	d["params"].(map[string]any)["prompt"] = "清晨"

	rr := canvasgraph.Revert(cur, res.Changes)
	got := node(t, rr.Graph, "n_script").Data()
	if got["prompt"] != "清晨" {
		t.Errorf("用户改过的字段应保留，实际 %v", got["prompt"])
	}
	if got["label"] != "剧本" {
		t.Errorf("没被改过的标题应恢复: %v", got["label"])
	}
	if len(rr.Skipped) == 0 {
		t.Fatal("应列出被跳过的字段")
	}
	found := false
	for _, s := range rr.Skipped {
		if s.NodeID == "n_script" && s.Field == "data.prompt" {
			found = true
		}
	}
	if !found {
		t.Errorf("跳过项里应有 n_script 的 data.prompt: %+v", rr.Skipped)
	}
}

func TestRevertKeepsNodesWithOutputsOrEdits(t *testing.T) {
	g := mustParse(t, `{}`)
	built := mustApply(t, g, `[{"op":"create_node","tempId":"a","kind":"image","label":"A"},{"op":"create_node","tempId":"b","kind":"image","label":"B"},{"op":"create_node","tempId":"c","kind":"image","label":"C"},{"op":"connect","source":"a","target":"b"}]`)
	cur := built.Graph.Clone()
	aID, bID, cID := built.IDMap["a"], built.IDMap["b"], built.IDMap["c"]
	node(t, cur, aID).Data()["taskId"] = "t1" // a 已批准生成
	node(t, cur, bID).Data()["label"] = "我改的" // b 被用户改过名

	rr := canvasgraph.Revert(cur, built.Changes)
	if rr.Graph.Node(aID) == nil || rr.Graph.Node(bID) == nil {
		t.Error("有产物或被用户改过的节点必须保留")
	}
	if rr.Graph.Node(cID) != nil {
		t.Error("没人动过的新节点应被删除")
	}
	if len(rr.Skipped) != 2 {
		t.Errorf("应列出 2 个保留项: %+v", rr.Skipped)
	}
	// a→b 的边：两端都被保留，边也留着，不会丢用户手里节点的连接。
	if len(rr.Graph.Edges) != 1 {
		t.Errorf("保留节点之间的连线应保留: %d", len(rr.Graph.Edges))
	}
	if rr.Reverted != 1 {
		t.Errorf("Reverted 应只计 c: %d", rr.Reverted)
	}
}

func TestRevertRestoresDeletedNodesAndEdges(t *testing.T) {
	g := mustParse(t, basePayload)
	built := mustApply(t, g, shotOps)
	delRes, err := canvasgraph.Delete(built.Graph, []string{built.IDMap["b"]}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := canvasgraph.Revert(delRes.Graph, delRes.Changes)
	if rr.Graph.Node(built.IDMap["b"]) == nil {
		t.Fatal("被删的节点应恢复")
	}
	if len(rr.Graph.Edges) != 3 {
		t.Errorf("被连带删除的边应恢复: %d", len(rr.Graph.Edges))
	}
	if marshal(t, rr.Graph) != marshal(t, built.Graph) {
		t.Errorf("恢复后应与删除前一致")
	}
}

func TestRevertArrangeSkipsMovedNode(t *testing.T) {
	g := mustParse(t, threeNodes)
	res, _ := canvasgraph.Arrange(g, canvasgraph.ArrangeTarget{NodeIDs: []string{"a", "b", "c"}}, canvasgraph.LayoutRow, nil)
	cur := res.Graph.Clone()
	node(t, cur, "b")["position"] = map[string]any{"x": 5000.0, "y": 5000.0} // 用户拖走了 b
	rr := canvasgraph.Revert(cur, res.Changes)
	if p := node(t, rr.Graph, "b").Position(); p.X != 5000 {
		t.Errorf("用户拖过的节点不应被挪回: %+v", p)
	}
	if p := node(t, rr.Graph, "c").Position(); p.X != 300 || p.Y != 700 {
		t.Errorf("没动过的节点应恢复: %+v", p)
	}
	if rr.Reverted != 1 || len(rr.Skipped) != 1 {
		t.Errorf("Reverted=%d Skipped=%d", rr.Reverted, len(rr.Skipped))
	}
}

// 成员的 position 是相对组左上角的：parentId 和 position 只能一起恢复或者一起保留，
// 不然撤销之后节点挂着新的组却带着旧的坐标，或者脱离了组却带着相对坐标，会飞到别处。
func TestRevertKeepsParentAndPositionTogetherWhenUserMovedMember(t *testing.T) {
	g := mustParse(t, `{"nodes":[
	  {"id":"a","type":"canvas","position":{"x":500,"y":300},"data":{"kind":"image","label":"A"}},
	  {"id":"b","type":"canvas","position":{"x":950,"y":300},"data":{"kind":"image","label":"B"}}]}`)
	res := mustApply(t, g, `[{"op":"create_group","tempId":"g","label":"组","memberIds":["a","b"]}]`)
	gid := res.IDMap["g"]

	// 用户之后拖了 a：只改了 position，parentId 还在。
	cur := res.Graph.Clone()
	node(t, cur, "a").SetPosition(canvasgraph.Point{X: 999, Y: 111})

	rr := canvasgraph.Revert(cur, res.Changes)
	a := node(t, rr.Graph, "a")
	if a.ParentID() != gid {
		t.Errorf("用户拖过 a，撤销不应只把它移出组（位置还是相对坐标，会飞走）: parent=%q pos=%+v", a.ParentID(), a.Position())
	}
	if p := a.Position(); p.X != 999 || p.Y != 111 {
		t.Errorf("用户拖过的位置应保留: %+v", p)
	}
	// 没动过的 b 照常撤销：回到组外、绝对坐标。
	b := node(t, rr.Graph, "b")
	if b.ParentID() != "" || b.Position().X != 950 || b.Position().Y != 300 {
		t.Errorf("没动过的 b 应整体恢复: parent=%q pos=%+v", b.ParentID(), b.Position())
	}
	if rr.Graph.Node(gid) == nil {
		t.Error("组里还有 a，组应保留")
	}
}

func TestRevertKeepsParentAndPositionTogetherWhenUserRegrouped(t *testing.T) {
	// Agent 把 a 移出组；用户之后把 a 又拖进了另一个组（parentId 变了，position 也变了）。
	g := mustParse(t, `{"nodes":[
	  {"id":"a","type":"canvas","position":{"x":500,"y":300},"data":{"kind":"image","label":"A"}},
	  {"id":"b","type":"canvas","position":{"x":950,"y":300},"data":{"kind":"image","label":"B"}}]}`)
	grouped := mustApply(t, g, `[{"op":"create_group","tempId":"g","label":"组","memberIds":["a","b"]}]`)
	gid := grouped.IDMap["g"]
	out := mustApply(t, grouped.Graph, `[{"op":"set_group","id":"`+gid+`","removeMembers":["a"]}]`)

	cur := out.Graph.Clone()
	node(t, cur, "a")["parentId"] = gid // 用户把 a 拖回了组里，同时坐标也是相对的了
	node(t, cur, "a").SetPosition(canvasgraph.Point{X: 70, Y: 90})

	rr := canvasgraph.Revert(cur, out.Changes)
	a := node(t, rr.Graph, "a")
	if a.ParentID() != gid || a.Position().X != 70 || a.Position().Y != 90 {
		t.Errorf("用户改过归属和位置，撤销应整体保留: parent=%q pos=%+v", a.ParentID(), a.Position())
	}
}
