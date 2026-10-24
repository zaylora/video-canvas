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
		hD = strings.Repeat("4", 64) // 被引用角色卡的主图
		hE = strings.Repeat("5", 64) // 被引用角色卡的补充图
		hF = strings.Repeat("6", 64) // 被引用角色卡的声音
		hX = strings.Repeat("7", 64) // 没人引用的素材
		hY = strings.Repeat("8", 64) // 只被未引用角色卡用到的素材
	)
	voice := hF
	assets := map[string]canvasdoc.AssetInfo{}
	for _, h := range []string{hA, hB, hC, hD, hE, hF, hX, hY} {
		assets[h] = canvasdoc.AssetInfo{Kind: "image", ByteSize: 1}
	}
	characters := map[string]canvasdoc.CharacterInfo{
		"c1":  {Name: "未引用", Avatar: hY},
		"c2":  {Name: "被引用", Avatar: hD, Extras: []string{hE}, Voice: &voice},
		"c10": {Name: "也被引用", Avatar: hD},
		"c9":  {Name: "被引用 9", Avatar: hD},
	}
	node := func(id string, data map[string]any) map[string]any {
		return map[string]any{"id": id, "data": data}
	}

	cases := []struct {
		name      string
		nodes     []map[string]any
		wantHash  []string
		wantChars []string
	}{
		{"空画布", nil, []string{}, []string{}},
		{
			"各处素材引用与角色卡引用",
			[]map[string]any{
				node("n1", map[string]any{
					"assetHash":   hA,
					"outputs":     []any{map[string]any{"assetHash": hA}},
					"params":      map[string]any{"images": []any{hB}},
					"paramAssets": map[string]any{hB: map[string]any{}},
				}),
				node("n2", map[string]any{"characterRef": "c10"}),
				node("n3", map[string]any{"scene": map[string]any{"thumbHash": hC}}),
				node("n4", map[string]any{"characterRef": "c2"}),
				node("n5", map[string]any{"characterRef": "c9"}),
				node("n6", map[string]any{"characterRef": "c2"}),
			},
			[]string{hA, hB, hC, hD, hE, hF},
			[]string{"c2", "c9", "c10"},
		},
		{"组节点和没有 data 的节点不贡献引用", []map[string]any{{"id": "g1", "type": "group"}}, []string{}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &canvasdoc.Document{
				Canvas: canvasdoc.CanvasPart{Nodes: tc.nodes}, Assets: assets, Characters: characters,
			}
			hashes, refs := canvasdoc.ReferencedSets(doc)
			if !slices.Equal(hashes, tc.wantHash) {
				t.Errorf("hashes = %v，期望 %v", hashes, tc.wantHash)
			}
			if !slices.Equal(refs, tc.wantChars) {
				t.Errorf("characterRefs = %v，期望 %v", refs, tc.wantChars)
			}
			if hashes == nil || refs == nil {
				t.Error("没有引用时应返回空切片而不是 nil")
			}
		})
	}
}
