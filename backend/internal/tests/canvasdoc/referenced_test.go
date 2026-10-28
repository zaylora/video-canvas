package canvasdoc_test

import (
	"slices"
	"strings"
	"testing"

	"video-canvas/internal/canvasdoc"
)

func TestReferencedSets(t *testing.T) {
	var (
		hA = strings.Repeat("1", 64) // 节点主素材
		hB = strings.Repeat("2", 64) // 参考图 / paramAssets
		hC = strings.Repeat("3", 64) // 导演台缩略图
		hD = strings.Repeat("4", 64) // 角色节点补充图
		hE = strings.Repeat("5", 64) // 角色节点音频音色
		hX = strings.Repeat("7", 64) // 没人引用的素材
	)
	assets := map[string]canvasdoc.AssetInfo{}
	for _, h := range []string{hA, hB, hC, hD, hE, hX} {
		assets[h] = canvasdoc.AssetInfo{Kind: "image", ByteSize: 1}
	}
	node := func(id string, data map[string]any) map[string]any {
		return map[string]any{"id": id, "data": data}
	}

	cases := []struct {
		name     string
		nodes    []map[string]any
		wantHash []string
	}{
		{"空画布", nil, []string{}},
		{
			"各处素材引用",
			[]map[string]any{
				node("n1", map[string]any{
					"assetHash":   hA,
					"outputs":     []any{map[string]any{"assetHash": hA}},
					"params":      map[string]any{"images": []any{hB}},
					"paramAssets": map[string]any{hB: map[string]any{}},
				}),
				node("n3", map[string]any{"scene": map[string]any{"thumbHash": hC}}),
				node("n4", map[string]any{"character": map[string]any{
					"extras": []any{map[string]any{"assetHash": hD}},
					"voice":  map[string]any{"mode": "audio", "assetHash": hE},
				}}),
			},
			[]string{hA, hB, hC, hD, hE},
		},
		{"组节点和没有 data 的节点不贡献引用", []map[string]any{{"id": "g1", "type": "group"}}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &canvasdoc.Document{Canvas: canvasdoc.CanvasPart{Nodes: tc.nodes}, Assets: assets}
			hashes := canvasdoc.ReferencedSets(doc)
			if !slices.Equal(hashes, tc.wantHash) {
				t.Errorf("hashes = %v，期望 %v", hashes, tc.wantHash)
			}
			if hashes == nil {
				t.Error("没有引用时应返回空切片而不是 nil")
			}
		})
	}
}
