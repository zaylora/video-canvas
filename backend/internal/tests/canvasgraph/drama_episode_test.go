package canvasgraph_test

import (
	"encoding/json"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// dramaEpisodesOf 读节点 data.drama.episodes 的原始 JSON（null / [] / [1,2] 原样返回）。
func dramaEpisodesOf(t *testing.T, g *canvasgraph.Graph, id string) string {
	t.Helper()
	d, _ := g.Node(id).Data()["drama"].(map[string]any)
	b, err := json.Marshal(d["episodes"])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRemoveDramaEpisodeShiftsLaterEpisodes(t *testing.T) {
	g, err := canvasgraph.Parse([]byte(`{"nodes":[
		{"id":"both","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","drama":{"type":"scene","name":"街道","episodes":[1,2,4]}}},
		{"id":"only","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"character","drama":{"type":"character","name":"林薇","episodes":[2]}}},
		{"id":"all","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","drama":{"type":"prop","name":"玉佩","episodes":[]}}},
		{"id":"later","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","drama":{"type":"prop","name":"酒杯","episodes":[3,5]}}},
		{"id":"plain","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"随手建的"}}
	],"edges":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	r := canvasgraph.RemoveDramaEpisode(g, 2)
	for id, want := range map[string]string{
		// 删掉第 2 集，后面的集号减 1
		"both":  `[1,3]`,
		"later": `[2,4]`,
		// 只出现在被删那一集：变成未分类（null），不能变成 [] 被当成全集
		"only": `null`,
		// 全集保持全集
		"all": `[]`,
	} {
		if got := dramaEpisodesOf(t, r.Graph, id); got != want {
			t.Fatalf("%s: episodes=%s want %s", id, got, want)
		}
	}
	if _, ok := r.Graph.Node("plain").Data()["drama"]; ok {
		t.Fatal("没有短剧元数据的节点不该被改")
	}
	// 类型、名字保留，未分类后仍认得出是哪个资产
	d := r.Graph.Node("only").Data()["drama"].(map[string]any)
	if d["type"] != "character" || d["name"] != "林薇" {
		t.Fatalf("未分类后应保留身份：%+v", d)
	}
	if len(r.Changes) != 3 {
		t.Fatalf("只有出现集数变了的 3 个节点有改动，得到 %d", len(r.Changes))
	}
	// 输入图不被修改
	if got := dramaEpisodesOf(t, g, "both"); got != `[1,2,4]` {
		t.Fatalf("输入图被改了：%s", got)
	}
}

func TestMergeDramaClassifiesUnassignedAsset(t *testing.T) {
	g, err := canvasgraph.Parse([]byte(`{"nodes":[
		{"id":"orphan","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"character","drama":{"type":"character","name":"林薇","look":"","episodes":null}}}
	],"edges":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	// 未分类的资产再次被拆出来：认得出是同一个，直接归到新集，不重复建节点
	r, err := canvasgraph.MergeDrama(g, []canvasgraph.DramaAsset{{Type: "character", Name: "林薇", Episodes: []int{3}}}, canvasgraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Created) != 0 || len(r.Graph.Nodes) != 1 {
		t.Fatalf("应合并进已有节点：created=%+v", r.Created)
	}
	if got := dramaEpisodesOf(t, r.Graph, "orphan"); got != `[3]` {
		t.Fatalf("episodes=%s want [3]", got)
	}
}
