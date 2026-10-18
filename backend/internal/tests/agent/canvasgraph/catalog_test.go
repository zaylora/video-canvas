package canvasgraph_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

func bigGraph(t *testing.T, n int) *canvasgraph.Graph {
	var sb strings.Builder
	sb.WriteString(`{"nodes":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"id":"n%d","type":"canvas","position":{"x":%d,"y":0},"data":{"kind":"image","label":"图%d","prompt":"%s"}}`,
			i, i*10, i, strings.Repeat("长", 100))
	}
	sb.WriteString(`],"edges":[{"id":"e","source":"n0","target":"n1"}]}`)
	return mustParse(t, sb.String())
}

func TestCatalogTruncatesPromptAndNodes(t *testing.T) {
	g := bigGraph(t, 10)
	c := canvasgraph.BuildCatalog(g, canvasgraph.CatalogOptions{Max: 4, PromptRunes: 60})
	if c.Total != 10 || len(c.Nodes) != 4 || c.Omitted != 6 {
		t.Fatalf("Total=%d len=%d Omitted=%d", c.Total, len(c.Nodes), c.Omitted)
	}
	if got := []rune(c.Nodes[0].Prompt); len(got) != 61 || got[60] != '…' {
		t.Errorf("提示词应截到 60 字并加省略号: %d 字", len(got))
	}
	if c.EdgeCount != 1 {
		t.Errorf("EdgeCount=%d", c.EdgeCount)
	}
}

func TestCatalogPriorityFirst(t *testing.T) {
	g := bigGraph(t, 10)
	c := canvasgraph.BuildCatalog(g, canvasgraph.CatalogOptions{Max: 3, Priority: []string{"n9", "n5"}})
	if c.Nodes[0].ID != "n9" || c.Nodes[1].ID != "n5" {
		t.Errorf("选中和 @ 的节点应排在最前: %+v", c.Nodes)
	}
}

func TestCatalogMarksGroupMembershipAndOutputs(t *testing.T) {
	g := mustParse(t, basePayload)
	built := mustApply(t, g, shotOps)
	cur := built.Graph.Clone()
	node(t, cur, built.IDMap["b"]).Data()["outputs"] = []any{map[string]any{"id": "o1"}}
	c := canvasgraph.BuildCatalog(cur, canvasgraph.CatalogOptions{Max: 50})
	byID := map[string]canvasgraph.CatalogNode{}
	for _, n := range c.Nodes {
		byID[n.ID] = n
	}
	if byID[built.IDMap["a"]].Group != built.IDMap["g1"] {
		t.Errorf("成员应带所属组 id")
	}
	if !byID[built.IDMap["b"]].HasOutput || byID[built.IDMap["a"]].HasOutput {
		t.Errorf("HasOutput 应只在有产物的节点上为真")
	}
	if byID[built.IDMap["g1"]].Members != 3 {
		t.Errorf("组应标出成员数: %+v", byID[built.IDMap["g1"]])
	}
}

func TestDetailSanitizesMediaAndPages(t *testing.T) {
	g := mustParse(t, `{"nodes":[{"id":"a","type":"canvas","position":{"x":0,"y":0},
	  "data":{"kind":"image","label":"A","src":"https://cdn/x.png","assetId":"9","outputs":[{"id":"o1","src":"https://cdn/x.png"}],"pendingTaskIds":["11","12"],"prompt":"p"}}],
	  "edges":[{"id":"e1","source":"a","target":"b"}]}`)
	d, err := canvasgraph.Detail(g, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	data := d.Nodes[0].Data
	if _, has := data["src"]; has {
		t.Error("详情不应包含媒体地址 src")
	}
	if _, has := data["outputs"]; has {
		t.Error("详情不应包含 outputs 列表")
	}
	if _, has := data["pendingTaskIds"]; has {
		t.Error("详情不应包含进行中的任务 id 集合 pendingTaskIds")
	}
	if data["outputCount"] != 1 || data["hasOutput"] != true {
		t.Errorf("应改为产物数量和标记: %v", data)
	}
	if data["prompt"] != "p" {
		t.Errorf("提示词应原样给出")
	}
	if len(d.Edges) != 1 {
		t.Errorf("应带出关联连线: %d", len(d.Edges))
	}
	if _, err := canvasgraph.Detail(g, []string{"zzz"}); err == nil {
		t.Error("不存在的节点应报错")
	}
	ids := make([]string, canvasgraph.MaxDetailNodes+1)
	for i := range ids {
		ids[i] = "a"
	}
	if _, err := canvasgraph.Detail(g, ids); err == nil {
		t.Errorf("超过 %d 个应报错", canvasgraph.MaxDetailNodes)
	}
}

// 目录里要明确写出用户选中了什么：光把选中的排在最前，模型不知道排到哪一个为止；选中的组还要说清楚里面有哪些成员。
func TestCatalogSelectedListsWhatTheUserPicked(t *testing.T) {
	g := mustParse(t, basePayload)
	built := mustApply(t, g, shotOps)
	cur := built.Graph.Clone()
	group, shot := built.IDMap["g1"], built.IDMap["b"]
	c := canvasgraph.BuildCatalog(cur, canvasgraph.CatalogOptions{Max: 50, Selected: []string{group, shot, "no-such-node", group}})
	if len(c.Selected) != 2 {
		t.Fatalf("按选中的顺序、去重、画布上没有的丢掉: %+v", c.Selected)
	}
	g0, n1 := c.Selected[0], c.Selected[1]
	if g0.ID != group || g0.Type != "group" || g0.Label != "镜头 01" || len(g0.MemberIDs) != 3 {
		t.Errorf("选中的组要带类型、名字和成员 id: %+v", g0)
	}
	for _, id := range []string{built.IDMap["a"], built.IDMap["b"], built.IDMap["c"]} {
		found := false
		for _, m := range g0.MemberIDs {
			found = found || m == id
		}
		if !found {
			t.Errorf("成员 %s 应在选中的组里: %v", id, g0.MemberIDs)
		}
	}
	if n1.ID != shot || n1.Type != "canvas" || n1.Kind != "image" || n1.Label != "镜头01 关键帧" || len(n1.MemberIDs) != 0 {
		t.Errorf("选中的节点带种类和名字，没有成员: %+v", n1)
	}
}

func TestCatalogSelectedOmittedWhenNothingSelected(t *testing.T) {
	g := bigGraph(t, 3)
	c := canvasgraph.BuildCatalog(g, canvasgraph.CatalogOptions{Max: 50})
	if len(c.Selected) != 0 {
		t.Errorf("没选就没有: %+v", c.Selected)
	}
	b, _ := json.Marshal(c)
	if strings.Contains(string(b), "selected") {
		t.Errorf("没选时 JSON 里不该有 selected 字段，免得白占上下文: %s", b)
	}
}

func TestCatalogSelectedGroupMembersAreCapped(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"nodes":[{"id":"g","type":"group","position":{"x":0,"y":0},"data":{"label":"大组"}}`)
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&sb, `,{"id":"m%d","type":"canvas","parentId":"g","position":{"x":0,"y":0},"data":{"kind":"image","label":"图%d"}}`, i, i)
	}
	sb.WriteString(`],"edges":[]}`)
	c := canvasgraph.BuildCatalog(mustParse(t, sb.String()), canvasgraph.CatalogOptions{Max: 200, Selected: []string{"g"}})
	if len(c.Selected) != 1 || len(c.Selected[0].MemberIDs) != 50 || c.Selected[0].MoreMembers != 30 {
		t.Errorf("成员太多只列前 50 个，并说明还有多少: %+v", c.Selected)
	}
}
