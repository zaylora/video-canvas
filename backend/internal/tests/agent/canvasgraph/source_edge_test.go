package canvasgraph_test

import (
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// 视频截出来的帧图用「来源线」连回视频：只表示派生关系，不是参考素材。
const sourceEdgePayload = `{
  "nodes": [
    {"id":"n_vid","type":"canvas","data":{"kind":"video","label":"镜头1","assetId":"7","src":"/files/7","status":"done"}},
    {"id":"n_frame","type":"canvas","data":{"kind":"image","label":"镜头1-首帧","assetId":"8","src":"/files/8","status":"done","uploaded":true}},
    {"id":"n_next","type":"canvas","data":{"kind":"video","label":"镜头2","model":"seedance"}}
  ],
  "edges": [
    {"id":"e1","source":"n_vid","target":"n_frame","relation":"source"},
    {"id":"e2","source":"n_frame","target":"n_next"}
  ]
}`

func TestGenerationIgnoresSourceEdges(t *testing.T) {
	g := mustParse(t, sourceEdgePayload)

	t.Run("来源线不当上游素材", func(t *testing.T) {
		src, err := canvasgraph.Generation(g, "n_frame")
		if err != nil {
			t.Fatal(err)
		}
		if len(src.Assets) != 0 || src.Pending != 0 {
			t.Errorf("来源线被当成了上游: assets=%v pending=%d", src.Assets, src.Pending)
		}
	})

	t.Run("普通连线照常当上游素材", func(t *testing.T) {
		src, err := canvasgraph.Generation(g, "n_next")
		if err != nil {
			t.Fatal(err)
		}
		want := canvasgraph.GenAsset{Kind: "image", AssetID: "8"}
		if len(src.Assets) != 1 || src.Assets[0] != want {
			t.Errorf("素材: %v", src.Assets)
		}
	})
}

func TestDetailMarksSourceEdges(t *testing.T) {
	g := mustParse(t, sourceEdgePayload)
	d, err := canvasgraph.Detail(g, []string{"n_frame"})
	if err != nil {
		t.Fatal(err)
	}
	relations := map[string]string{}
	for _, e := range d.Edges {
		relations[e.ID] = e.Relation
	}
	if relations["e1"] != "source" || relations["e2"] != "" {
		t.Errorf("来源线要标出 relation，普通连线不带: %v", relations)
	}
}

func TestSourceEdgeSurvivesRoundTrip(t *testing.T) {
	g := mustParse(t, sourceEdgePayload)
	out, err := g.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back := mustParse(t, string(out))
	if back.Edges[0]["relation"] != "source" {
		t.Errorf("往返后 relation 丢了: %v", back.Edges[0])
	}
}
