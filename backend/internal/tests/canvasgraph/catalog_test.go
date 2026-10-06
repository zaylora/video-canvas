package canvasgraph_test

import (
	"fmt"
	"strings"
	"testing"

	"video-canvas/internal/canvasgraph"
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
	  "data":{"kind":"image","label":"A","src":"https://cdn/x.png","assetId":"9","outputs":[{"id":"o1","src":"https://cdn/x.png"}],"prompt":"p"}}],
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
