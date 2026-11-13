package canvasgraph_test

import (
	"encoding/json"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

func TestBuildMarketing_NodesEdgesAndNoShotsError(t *testing.T) {
	spec := canvasgraph.MarketingSpec{
		Images:   []canvasgraph.MarketingMedia{{AssetID: "1", URL: "/files/a"}, {AssetID: "2", URL: "/files/b", Label: "模特图"}},
		RefVideo: &canvasgraph.MarketingMedia{AssetID: "3", URL: "/files/v"},
		Final:    &canvasgraph.MarketingMedia{AssetID: "4", URL: "/files/f"},
		Shots:    []canvasgraph.MarketingShot{{Prompt: "镜头一"}, {Prompt: "镜头二"}},
		Model:    "m", Params: map[string]any{"aspect_ratio": "9:16"},
	}
	raw, err := canvasgraph.BuildMarketing(spec, canvasgraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	g, err := canvasgraph.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 2+1+2+1 || len(g.Edges) != 3*2 {
		t.Fatalf("节点 %d 连线 %d", len(g.Nodes), len(g.Edges))
	}
	ids := map[string]bool{}
	for _, n := range g.Nodes {
		if ids[n.ID()] {
			t.Fatal("节点 id 重复")
		}
		ids[n.ID()] = true
	}
	for _, e := range g.Edges {
		if !ids[e["source"].(string)] || !ids[e["target"].(string)] {
			t.Fatalf("连线指向不存在的节点：%v", e)
		}
	}
	if !json.Valid(raw) {
		t.Fatal("payload 不是合法 JSON")
	}
	if _, err := canvasgraph.BuildMarketing(canvasgraph.MarketingSpec{}, canvasgraph.Options{}); err == nil {
		t.Fatal("没有镜头应报错")
	}
}
