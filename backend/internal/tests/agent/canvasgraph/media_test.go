package canvasgraph_test

import (
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// mediaPayload 覆盖素材引用的五种形态：产物列表、老节点的 assetId 镜像、成片、
// 角色卡的补充图与声音、分镜表旧手传图。
const mediaPayload = `{
  "nodes": [
    {"id":"v1","type":"canvas","position":{"x":0,"y":0},"data":{
      "kind":"video","label":"S1",
      "outputs":[
        {"id":"11","assetId":"11","mediaType":"video"},
        {"id":"12","assetId":"12","mediaType":"video"}
      ],
      "assetId":"12","mediaType":"video","activeOutputId":"11"}},
    {"id":"i1","type":"canvas","position":{"x":200,"y":0},"data":{
      "kind":"image","label":"老图","assetId":"40","mediaType":"image","src":"http://x/40.png"}},
    {"id":"c1","type":"canvas","position":{"x":400,"y":0},"data":{
      "kind":"compose","label":"成片","finalAssetId":"20"}},
    {"id":"r1","type":"canvas","position":{"x":600,"y":0},"data":{
      "kind":"character","label":"角色","assetId":"50","mediaType":"image",
      "character":{"name":"阿七","look":"基础形象","description":"修表匠","voicePrompt":"沙哑",
        "extras":[{"assetId":"51"}],"voice":{"mode":"audio","assetId":"52","name":"沙哑男声"}}}},
    {"id":"b1","type":"canvas","position":{"x":800,"y":0},"data":{
      "kind":"storyboard","label":"分镜表","storyboard":{"shots":[
        {"id":"s1","scene":"开场","shot":"特写","camera":"推","action":"手部","dialogue":"（电流）","duration":3.5,
         "image":{"assetId":"30"}},
        {"id":"s2","scene":"副歌","duration":4,"videoNodeId":"v1"}
      ]}}},
    {"id":"t1","type":"canvas","position":{"x":1000,"y":0},"data":{
      "kind":"script","label":"剧本","text":"第一场 夜 外"}}
  ]
}`

func mediaNode(t *testing.T, id string) canvasgraph.Node {
	t.Helper()
	g, err := canvasgraph.Parse([]byte(mediaPayload))
	if err != nil {
		t.Fatalf("解析画布: %v", err)
	}
	n := g.Node(id)
	if n == nil {
		t.Fatalf("节点 %q 不存在", id)
	}
	return n
}

func TestMediaRefsOutputsWin(t *testing.T) {
	refs := canvasgraph.MediaRefs(mediaNode(t, "v1"))
	if len(refs) != 2 {
		t.Fatalf("产物两版应该各算一次，实际 %+v", refs)
	}
	// 产物非空时只认产物，当前版 12 已经在里面，不能重复追加
	if refs[0].AssetID != "11" || refs[1].AssetID != "12" || refs[0].Kind != canvasgraph.MediaVideo {
		t.Fatalf("素材列表不对: %+v", refs)
	}
}

func TestMediaRefsLegacyAssetID(t *testing.T) {
	refs := canvasgraph.MediaRefs(mediaNode(t, "i1"))
	if len(refs) != 1 || refs[0].AssetID != "40" || refs[0].Kind != canvasgraph.MediaImage {
		t.Fatalf("老节点应该按 assetId 算一版，实际 %+v", refs)
	}
}

func TestMediaRefsFinal(t *testing.T) {
	refs := canvasgraph.MediaRefs(mediaNode(t, "c1"))
	if len(refs) != 1 || refs[0].AssetID != "20" || !refs[0].Final || refs[0].Kind != canvasgraph.MediaVideo {
		t.Fatalf("成片应该单独标出来，实际 %+v", refs)
	}
}

func TestMediaRefsCharacter(t *testing.T) {
	refs := canvasgraph.MediaRefs(mediaNode(t, "r1"))
	want := []struct {
		id   string
		kind string
	}{{"50", canvasgraph.MediaImage}, {"51", canvasgraph.MediaImage}, {"52", canvasgraph.MediaAudio}}
	if len(refs) != len(want) {
		t.Fatalf("角色卡素材数 = %d，期望 %d（%+v）", len(refs), len(want), refs)
	}
	for i, w := range want {
		if refs[i].AssetID != w.id || refs[i].Kind != w.kind {
			t.Fatalf("第 %d 个 = %+v，期望 %s/%s", i, refs[i], w.id, w.kind)
		}
	}
}

func TestMediaRefsStoryboardLegacyImage(t *testing.T) {
	refs := canvasgraph.MediaRefs(mediaNode(t, "b1"))
	if len(refs) != 1 || refs[0].AssetID != "30" || refs[0].Shot != 1 {
		t.Fatalf("分镜旧手传图应该带上镜号，实际 %+v", refs)
	}
}

func TestShotsSnapshot(t *testing.T) {
	shots := canvasgraph.Shots(mediaNode(t, "b1"))
	if len(shots) != 2 {
		t.Fatalf("镜头数 = %d", len(shots))
	}
	first := shots[0]
	if first.Seq != 1 || first.Size != "特写" || first.Camera != "推" || first.Duration != 3.5 || !first.HasImage {
		t.Fatalf("第一镜 = %+v", first)
	}
	if shots[1].VideoNodeID != "v1" || shots[1].ImageNodeID != "" {
		t.Fatalf("第二镜关联 = %+v", shots[1])
	}
	if got := canvasgraph.Shots(mediaNode(t, "i1")); got != nil {
		t.Fatalf("不是分镜表应该返回 nil，实际 %+v", got)
	}
}

func TestCharacterOf(t *testing.T) {
	info, ok := canvasgraph.CharacterOf(mediaNode(t, "r1"))
	if !ok || info.Name != "阿七" || info.Extras != 1 || !info.HasVoice || info.VoiceName != "沙哑男声" {
		t.Fatalf("角色快照 = %+v（ok=%v）", info, ok)
	}
	if _, ok := canvasgraph.CharacterOf(mediaNode(t, "i1")); ok {
		t.Fatal("图片节点不该被当成角色卡")
	}
}

func TestBody(t *testing.T) {
	if got := canvasgraph.Body(mediaNode(t, "t1")); got != "第一场 夜 外" {
		t.Fatalf("正文 = %q", got)
	}
	if got := canvasgraph.Body(mediaNode(t, "i1")); got != "" {
		t.Fatalf("图片节点没有正文，实际 %q", got)
	}
}
