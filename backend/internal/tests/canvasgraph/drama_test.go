package canvasgraph_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

func TestMergeDramaMergesAliasesAndPreservesUnclassified(t *testing.T) {
	g, err := canvasgraph.Parse([]byte(`{"nodes":[{"id":"old","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"character","label":"保留标题","characterId":"abc","params":{"prompt":"保留提示"},"drama":{"type":"character","name":"Alice","look":"Young","episodes":[1]}}},{"id":"manual","type":"canvas","position":{"x":700,"y":0},"data":{"kind":"image","label":"手动"}}],"edges":[],"viewport":{"zoom":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(g.Node("manual"))
	r, err := canvasgraph.MergeDrama(g, []canvasgraph.DramaAsset{
		{Type: "character", Name: " ALICE ", Look: " young ", Episodes: []int{2}},
		{Type: "character", Name: "小艾", Look: "Young", Aliases: []string{" Alice "}, Episodes: []int{3}},
		{Type: "scene", Name: "街道", Episodes: []int{}},
		{Type: "scene", Name: "街道", Episodes: []int{2}},
	}, canvasgraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Created) != 1 || len(r.Graph.Nodes) != 3 {
		t.Fatalf("created=%+v nodes=%+v", r.Created, r.Graph.Nodes)
	}
	d := r.Graph.Node("old").Data()
	if d["label"] != "保留标题" || d["characterId"] != "abc" {
		t.Fatalf("existing node overwritten: %+v", d)
	}
	b, _ := json.Marshal(d["drama"])
	var meta struct {
		Episodes []int `json:"episodes"`
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(meta.Episodes, []int{1, 2, 3}) {
		t.Fatalf("episodes=%v", meta.Episodes)
	}
	after, _ := json.Marshal(r.Graph.Node("manual"))
	if string(before) != string(after) {
		t.Fatal("unclassified node changed")
	}
	newNode := r.Graph.Node(r.Created[0].NodeID)
	b, _ = json.Marshal(newNode.Data()["drama"])
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatal(err)
	}
	if len(meta.Episodes) != 0 {
		t.Fatalf("all-episodes lost: %s", b)
	}
	if len(g.Nodes) != 2 {
		t.Fatal("input graph mutated")
	}
}

func TestMergeDramaKeepsVariantsTypesAndAllEpisodesDistinct(t *testing.T) {
	g, _ := canvasgraph.Parse([]byte(`{"nodes":[],"edges":[]}`))
	r, err := canvasgraph.MergeDrama(g, []canvasgraph.DramaAsset{
		{Type: "character", Name: "Alice", Look: "Young", Episodes: []int{1}},
		{Type: "character", Name: "Alice", Look: "Adult", Aliases: []string{"Alice"}, Episodes: []int{2}},
		{Type: "scene", Name: "Alice", Look: "Young", Episodes: []int{1}},
		{Type: "prop", Name: "Alice", Look: "Young", Episodes: []int{1}},
	}, canvasgraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Created) != 4 {
		t.Fatalf("different types or looks merged: %+v", r.Created)
	}
	rowY := map[string]float64{}
	for _, node := range r.Graph.Nodes {
		meta := node.Data()["drama"].(map[string]any)
		kind := meta["type"].(string)
		if y, ok := rowY[kind]; ok && y != node.Position().Y {
			t.Fatalf("same type split across rows: %+v", r.Graph.Nodes)
		}
		rowY[kind] = node.Position().Y
	}
	if rowY["character"] == rowY["scene"] || rowY["scene"] == rowY["prop"] {
		t.Fatalf("rows=%v", rowY)
	}
	all, err := canvasgraph.MergeDrama(r.Graph, []canvasgraph.DramaAsset{{Type: "character", Name: "Alice", Look: "Young", Episodes: []int{}}}, canvasgraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Created) != 0 || len(all.Changes) != 1 {
		t.Fatalf("all-episode merge: %+v", all)
	}
	again, err := canvasgraph.MergeDrama(all.Graph, []canvasgraph.DramaAsset{{Type: "character", Name: "alice", Look: " young ", Episodes: []int{9}}}, canvasgraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Changes) != 0 {
		t.Fatalf("all-episodes narrowed or no-op changed: %+v", again.Changes)
	}
}

func TestMergeDramaAppendsPastExistingWideAndGroupedNodes(t *testing.T) {
	g, _ := canvasgraph.Parse([]byte(`{"nodes":[{"id":"scene","type":"canvas","position":{"x":10,"y":800},"data":{"kind":"image","params":{"ratio":"21:9"},"drama":{"type":"scene","name":"旧场景","look":"","episodes":[1]}}},{"id":"group","type":"group","position":{"x":1000,"y":700},"width":1000,"height":1000},{"id":"manual","parentId":"group","type":"canvas","position":{"x":100,"y":100},"data":{"kind":"image"}}],"edges":[]}`))
	r, err := canvasgraph.MergeDrama(g, []canvasgraph.DramaAsset{{Type: "scene", Name: "新场景", Episodes: []int{2}}, {Type: "scene", Name: "另一场景", Episodes: []int{2}}}, canvasgraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	first, second := r.Graph.Node(r.Created[0].NodeID), r.Graph.Node(r.Created[1].NodeID)
	if first.Position().X < 2120 || second.Position().X < first.Position().X+696 || first.Position().Y != 800 || second.Position().Y != 800 {
		t.Fatalf("overlap or row changed: first=%+v second=%+v", first, second)
	}
	for _, id := range []string{"scene", "group", "manual"} {
		before, _ := json.Marshal(g.Node(id))
		after, _ := json.Marshal(r.Graph.Node(id))
		if string(before) != string(after) {
			t.Fatalf("existing %s moved", id)
		}
	}
}

func TestDramaGraphRejectsInvalidBatchWithoutChangingInput(t *testing.T) {
	g, _ := canvasgraph.Parse([]byte(`{"nodes":[],"edges":[]}`))
	for _, asset := range []canvasgraph.DramaAsset{{Type: "other", Name: "x"}, {Type: "scene", Name: " "}, {Type: "scene", Name: "x", Episodes: []int{0}}} {
		if _, err := canvasgraph.MergeDrama(g, []canvasgraph.DramaAsset{asset}, canvasgraph.Options{}); err == nil {
			t.Fatalf("accepted %+v", asset)
		}
		if len(g.Nodes) != 0 {
			t.Fatal("invalid asset changed input")
		}
	}
}
