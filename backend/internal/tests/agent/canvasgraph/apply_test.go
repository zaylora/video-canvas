package canvasgraph_test

import (
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

const shotOps = `[
  {"op":"create_group","tempId":"g1","label":"镜头 01"},
  {"op":"create_node","tempId":"a","kind":"script","label":"镜头01","prompt":"近景，林夏抬头","parentGroup":"g1"},
  {"op":"create_node","tempId":"b","kind":"image","label":"镜头01 关键帧","parentGroup":"g1"},
  {"op":"create_node","tempId":"c","kind":"video","label":"镜头01 视频","parentGroup":"g1"},
  {"op":"connect","source":"n_script","target":"a"},
  {"op":"connect","source":"a","target":"b"},
  {"op":"connect","source":"b","target":"c"}
]`

func TestApplyBuildsShotGroup(t *testing.T) {
	g := mustParse(t, basePayload)
	res := mustApply(t, g, shotOps)

	if len(res.IDMap) != 4 {
		t.Fatalf("IDMap=%v", res.IDMap)
	}
	grp := node(t, res.Graph, res.IDMap["g1"])
	if grp["type"] != "group" {
		t.Fatalf("g1 应是组: %v", grp["type"])
	}
	if res.Graph.Nodes[0].ID() != grp.ID() {
		t.Errorf("组必须排在成员之前，首个节点是 %s", res.Graph.Nodes[0].ID())
	}
	for _, temp := range []string{"a", "b", "c"} {
		n := node(t, res.Graph, res.IDMap[temp])
		if n["parentId"] != grp.ID() {
			t.Errorf("%s 应挂在组下: %v", temp, n["parentId"])
		}
	}
	if len(res.Graph.Edges) != 3 {
		t.Fatalf("应有 3 条边: %v", res.Graph.Edges)
	}
	// 三个成员横向依次排开，组框要能框住最右边的成员。
	last := node(t, res.Graph, res.IDMap["c"])
	right := last.Position().X + 576
	if grp.Width() < right {
		t.Errorf("组宽 %.0f 框不住最右成员（右边界 %.0f）", grp.Width(), right)
	}
	// 原画布不被修改。
	if len(g.Nodes) != 1 {
		t.Errorf("Apply 不应修改入参，节点数=%d", len(g.Nodes))
	}
}

func TestApplyCreateNodeWritesPromptInBothPlaces(t *testing.T) {
	g := mustParse(t, `{}`)
	res := mustApply(t, g, `[{"op":"create_node","tempId":"x","kind":"image","label":"角色","prompt":"短发少女","model":"seedream-4"}]`)
	n := node(t, res.Graph, res.IDMap["x"])
	data := n.Data()
	if data["prompt"] != "短发少女" {
		t.Errorf("data.prompt=%v", data["prompt"])
	}
	if p, _ := data["params"].(map[string]any); p["prompt"] != "短发少女" {
		t.Errorf("params.prompt 应与 data.prompt 一致: %v", data["params"])
	}
	if data["model"] != "seedream-4" || data["kind"] != "image" {
		t.Errorf("data=%v", data)
	}
	if n["type"] != "canvas" {
		t.Errorf("type=%v", n["type"])
	}
}

func TestApplyIsAtomicAndReportsEveryIssue(t *testing.T) {
	g := mustParse(t, basePayload)
	before := marshal(t, g)
	_, err := canvasgraph.Apply(g, mustOps(t, `[
	  {"op":"create_node","tempId":"a","kind":"image","label":"图"},
	  {"op":"connect","source":"a","target":"n_script"},
	  {"op":"update_node","id":"nope","label":"x"},
	  {"op":"create_node","tempId":"bad","kind":"podcast"}
	]`), opts())
	var ve *canvasgraph.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("应返回 ValidationError: %v", err)
	}
	if len(ve.Issues) != 3 {
		t.Fatalf("应报告 3 项问题，实际 %d: %+v", len(ve.Issues), ve.Issues)
	}
	idx := []int{ve.Issues[0].Index, ve.Issues[1].Index, ve.Issues[2].Index}
	if idx[0] != 1 || idx[1] != 2 || idx[2] != 3 {
		t.Errorf("问题序号应为 1,2,3: %v", idx)
	}
	if !strings.Contains(ve.Issues[0].Message, "image") || !strings.Contains(ve.Issues[0].Message, "script") {
		t.Errorf("连线错误应说明两端种类: %s", ve.Issues[0].Message)
	}
	if marshal(t, g) != before {
		t.Error("失败时入参必须保持不变")
	}
}

func TestApplyConnectRules(t *testing.T) {
	cases := []struct {
		name       string
		from, to   string
		wantReject bool
	}{
		{"文本到图片", "script", "image", false},
		{"文本到文本", "script", "script", false},
		{"图片到视频", "image", "video", false},
		{"视频到图片", "video", "image", true},
		{"音频到视频", "audio", "video", false},
		{"图片到音频", "image", "audio", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := mustParse(t, `{}`)
			ops := mustOps(t, `[{"op":"create_node","tempId":"a","kind":"`+tc.from+`"},{"op":"create_node","tempId":"b","kind":"`+tc.to+`"},{"op":"connect","source":"a","target":"b"}]`)
			_, err := canvasgraph.Apply(g, ops, opts())
			if (err != nil) != tc.wantReject {
				t.Fatalf("err=%v wantReject=%v", err, tc.wantReject)
			}
		})
	}
}

func TestApplyConnectIdempotentAndNoSelfLoop(t *testing.T) {
	g := mustParse(t, `{}`)
	res := mustApply(t, g, `[{"op":"create_node","tempId":"a","kind":"script"},{"op":"create_node","tempId":"b","kind":"image"},
	  {"op":"connect","source":"a","target":"b"},{"op":"connect","source":"a","target":"b"}]`)
	if len(res.Graph.Edges) != 1 {
		t.Errorf("重复连线应幂等，边数=%d", len(res.Graph.Edges))
	}
	_, err := canvasgraph.Apply(g, mustOps(t, `[{"op":"create_node","tempId":"a","kind":"script"},{"op":"connect","source":"a","target":"a"}]`), opts())
	if err == nil {
		t.Error("自连应被拒绝")
	}
}

func TestParseOpsRejectsProtectedAndTooMany(t *testing.T) {
	if _, err := canvasgraph.ParseOps([]byte(`[{"op":"update_node","id":"n","src":"http://x"}]`)); err == nil {
		t.Error("不认识的字段（如 src）必须被拒绝，产物字段不能由 Agent 写")
	}
	var many strings.Builder
	many.WriteString("[")
	for i := 0; i < canvasgraph.MaxOps+1; i++ {
		if i > 0 {
			many.WriteString(",")
		}
		many.WriteString(`{"op":"move","id":"n","position":{"x":1,"y":1}}`)
	}
	many.WriteString("]")
	if _, err := canvasgraph.ParseOps([]byte(many.String())); err == nil {
		t.Errorf("超过 %d 项应被拒绝", canvasgraph.MaxOps)
	}
	if _, err := canvasgraph.ParseOps([]byte(`[]`)); err == nil {
		t.Error("空 ops 应被拒绝")
	}
}

func TestUpdateNodeRules(t *testing.T) {
	g := mustParse(t, basePayload)
	res := mustApply(t, g, `[{"op":"update_node","id":"n_script","prompt":"黄昏","label":"剧本 v2"}]`)
	d := node(t, res.Graph, "n_script").Data()
	if d["label"] != "剧本 v2" || d["prompt"] != "黄昏" || d["params"].(map[string]any)["prompt"] != "黄昏" {
		t.Errorf("data=%v", d)
	}
	for _, bad := range []string{
		`[{"op":"update_node","id":"n_script","params":{"images":["a1"]}}]`,
		`[{"op":"update_node","id":"n_script","params":{"audios":["a1"]}}]`,
	} {
		if _, err := canvasgraph.Apply(g, mustOps(t, bad), opts()); err == nil {
			t.Errorf("params 里的素材引用应被拒绝: %s", bad)
		}
	}
	// 组不能用 update_node。
	g2 := mustApply(t, g, `[{"op":"create_group","tempId":"g","label":"组"}]`).Graph
	gid := ""
	for _, n := range g2.Nodes {
		if n["type"] == "group" {
			gid = n.ID()
		}
	}
	if _, err := canvasgraph.Apply(g2, mustOps(t, `[{"op":"update_node","id":"`+gid+`","label":"x"}]`), opts()); err == nil {
		t.Error("update_node 不能改组")
	}
}

func TestSetGroupMembersKeepsAbsolutePosition(t *testing.T) {
	g := mustParse(t, `{"nodes":[
	  {"id":"a","type":"canvas","position":{"x":500,"y":300},"data":{"kind":"image","label":"A"}},
	  {"id":"b","type":"canvas","position":{"x":950,"y":300},"data":{"kind":"image","label":"B"}}]}`)
	res := mustApply(t, g, `[{"op":"create_group","tempId":"g","label":"组","memberIds":["a","b"]}]`)
	gid := res.IDMap["g"]
	grp := node(t, res.Graph, gid)
	a := node(t, res.Graph, "a")
	if a["parentId"] != gid {
		t.Fatalf("a 应入组")
	}
	abs := grp.Position().X + a.Position().X
	if abs != 500 {
		t.Errorf("入组后绝对横坐标应保持 500，实际 %.0f", abs)
	}
	// 移出组：回到绝对坐标。
	res2 := mustApply(t, res.Graph, `[{"op":"set_group","id":"`+gid+`","removeMembers":["a"]}]`)
	a2 := node(t, res2.Graph, "a")
	if _, has := a2["parentId"]; has {
		t.Error("移出后不应再有 parentId")
	}
	if a2.Position().X != 500 || a2.Position().Y != 300 {
		t.Errorf("移出后应回到绝对坐标 (500,300): %+v", a2.Position())
	}
}

func TestSetGroupColorValidation(t *testing.T) {
	g := mustParse(t, `{}`)
	ok := mustApply(t, g, `[{"op":"create_group","tempId":"g","label":"组","color":"blue"}]`)
	if node(t, ok.Graph, ok.IDMap["g"]).Data()["color"] != "blue" {
		t.Error("应写入颜色")
	}
	if _, err := canvasgraph.Apply(g, mustOps(t, `[{"op":"create_group","tempId":"g","label":"组","color":"magenta"}]`), opts()); err == nil {
		t.Error("不认识的颜色应被拒绝")
	}
}

func TestDeleteRemovesIncidentEdgesAndUngroups(t *testing.T) {
	g := mustParse(t, basePayload)
	built := mustApply(t, g, shotOps)
	gid := built.IDMap["g1"]
	a := built.IDMap["a"]
	absBefore := node(t, built.Graph, gid).Position().X + node(t, built.Graph, a).Position().X

	// 删掉组：成员保留并回到绝对坐标，连线不受影响。
	res, err := canvasgraph.Delete(built.Graph, []string{gid}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Graph.Node(gid) != nil {
		t.Error("组应被删除")
	}
	n := node(t, res.Graph, a)
	if _, has := n["parentId"]; has {
		t.Error("成员应脱离已删除的组")
	}
	if n.Position().X != absBefore {
		t.Errorf("成员应回到绝对坐标 %.0f，实际 %.0f", absBefore, n.Position().X)
	}
	if len(res.Graph.Edges) != 3 {
		t.Errorf("删组不应动连线: %d", len(res.Graph.Edges))
	}

	// 删节点：关联连线一并删除。
	res2, err := canvasgraph.Delete(built.Graph, []string{built.IDMap["b"]}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Graph.Edges) != 1 {
		t.Errorf("删中间节点后应只剩 1 条边: %d", len(res2.Graph.Edges))
	}

	if _, err := canvasgraph.Delete(built.Graph, []string{"nope"}, nil); err == nil {
		t.Error("删除不存在的节点应报错")
	}
	if _, err := canvasgraph.Delete(built.Graph, nil, nil); err == nil {
		t.Error("什么都不删应报错")
	}
}
