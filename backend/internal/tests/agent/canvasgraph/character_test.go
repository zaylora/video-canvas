package canvasgraph_test

import (
	"strconv"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// applyOps 在 basePayload 上应用操作，返回结果和错误（不直接判失败）。
func applyOps(t *testing.T, payload, raw string) (*canvasgraph.Result, error) {
	t.Helper()
	return canvasgraph.Apply(mustParse(t, payload), mustOps(t, raw), opts())
}

// characterMeta 取节点内嵌的角色数据。
func characterMeta(t *testing.T, n canvasgraph.Node) map[string]any {
	t.Helper()
	meta, ok := n.Data()["character"].(map[string]any)
	if !ok {
		t.Fatalf("节点没有 character 数据：%v", n.Data())
	}
	return meta
}

func TestCreateCharacter_BuildsCardWithMetaAndMainImagePrompt(t *testing.T) {
	res, err := applyOps(t, basePayload, `[{"op":"create_node","tempId":"c","kind":"character",
		"character":{"name":"林夏","description":"二十五岁，短发，米色风衣","voicePrompt":"清亮的年轻女声"},
		"prompt":"林夏三视图，白底","model":"seedream-4"}]`)
	if err != nil {
		t.Fatal(err)
	}
	n := node(t, res.Graph, res.IDMap["c"])
	d := n.Data()
	if d["kind"] != "character" || d["label"] != "林夏 · 基础形象" || d["model"] != "seedream-4" {
		t.Fatalf("角色卡字段不对：%v", d)
	}
	if p, _ := d["prompt"].(string); !strings.Contains(p, "三视图") {
		t.Errorf("主图提示词应写进节点：%v", d)
	}
	meta := characterMeta(t, n)
	if meta["name"] != "林夏" || meta["look"] != "基础形象" || meta["description"] != "二十五岁，短发，米色风衣" ||
		meta["voicePrompt"] != "清亮的年轻女声" || meta["voice"] != nil {
		t.Errorf("角色数据不对：%v", meta)
	}
	if extras, ok := meta["extras"].([]any); !ok || len(extras) != 0 {
		t.Errorf("补充图初始为空数组：%v", meta["extras"])
	}
}

func TestCreateCharacter_FromAttachmentKeepsCharacterKind(t *testing.T) {
	res, err := applyWithAssets(t, `[{"op":"create_node","tempId":"c","kind":"character","assetId":"101",
		"character":{"name":"林夏","look":"雨夜造型"}}]`, attachmentAsset)
	if err != nil {
		t.Fatal(err)
	}
	d := node(t, res.Graph, res.IDMap["c"]).Data()
	if d["kind"] != "character" || d["assetId"] != "101" || d["status"] != "done" || d["label"] != "林夏 · 雨夜造型" {
		t.Fatalf("用附件当主图的角色卡：%v", d)
	}
}

func TestCreateCharacter_CanBeReferencedByVideo(t *testing.T) {
	if _, err := applyOps(t, basePayload, `[
		{"op":"create_node","tempId":"c","kind":"character","character":{"name":"林夏"}},
		{"op":"create_node","tempId":"v","kind":"video","label":"镜头"},
		{"op":"connect","source":"c","target":"v"}
	]`); err != nil {
		t.Fatalf("角色卡连到视频作参考：%v", err)
	}
}

func TestCreateCharacter_RejectsBadInput(t *testing.T) {
	long := strings.Repeat("长", 501)
	for raw, want := range map[string]string{
		`[{"op":"create_node","kind":"character"}]`:                                                        "名字",
		`[{"op":"create_node","kind":"character","character":{"name":"  "}}]`:                              "名字",
		`[{"op":"create_node","kind":"character","character":{"name":"` + strings.Repeat("名", 65) + `"}}]`: "64",
		`[{"op":"create_node","kind":"character","character":{"name":"林夏","description":"` + long + `"}}]`: "500",
		`[{"op":"create_node","kind":"image","character":{"name":"林夏"}}]`:                                  "角色",
	} {
		if _, err := applyOps(t, basePayload, raw); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s：应报含「%s」的错，得到 %v", raw, want, err)
		}
	}
}

// charPayload 是一张已有角色卡（带补充图）和一个图片节点的画布。
const charPayload = `{"nodes":[
  {"id":"n_c","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"character","label":"林夏 · 基础形象",
   "character":{"name":"林夏","look":"基础形象","description":"短发","extras":[{"assetId":"7","src":"/files/7"}],"voice":null}}},
  {"id":"n_img","type":"canvas","position":{"x":400,"y":0},"data":{"kind":"image","label":"场景"}}
],"edges":[]}`

func TestUpdateCharacter_MergesFieldsAndKeepsExtras(t *testing.T) {
	res, err := applyOps(t, charPayload, `[{"op":"update_node","id":"n_c","character":{"description":"短发，左眉有疤","voicePrompt":"低沉"}}]`)
	if err != nil {
		t.Fatal(err)
	}
	meta := characterMeta(t, node(t, res.Graph, "n_c"))
	if meta["description"] != "短发，左眉有疤" || meta["voicePrompt"] != "低沉" || meta["name"] != "林夏" {
		t.Errorf("改后的角色数据：%v", meta)
	}
	if extras, _ := meta["extras"].([]any); len(extras) != 1 {
		t.Errorf("补充图不能被冲掉：%v", meta["extras"])
	}
}

func TestUpdateCharacter_OnlyOnCharacterNodes(t *testing.T) {
	if _, err := applyOps(t, charPayload, `[{"op":"update_node","id":"n_img","character":{"name":"林夏"}}]`); err == nil || !strings.Contains(err.Error(), "角色") {
		t.Errorf("非角色节点写 character 应报错：%v", err)
	}
}

// genCharPayload：角色卡（主图 21、补充图 22、声音 31）和一张普通图片都连到视频；另有一张还没主图的角色卡。
const genCharPayload = `{"nodes":[
  {"id":"v","type":"canvas","data":{"kind":"video","label":"镜头","params":{"prompt":"走路"}}},
  {"id":"c1","type":"canvas","data":{"kind":"character","label":"林夏 · 基础形象","assetId":"21","status":"done",
   "character":{"name":"林夏","look":"基础形象","extras":[{"assetId":"22","src":"/files/22"}],"voice":{"mode":"audio","assetId":"31","src":"/files/31"}}}},
  {"id":"i1","type":"canvas","data":{"kind":"image","label":"街景","assetId":"11","status":"done"}},
  {"id":"c2","type":"canvas","data":{"kind":"character","label":"阿遥","character":{"name":"阿遥","look":"","extras":[],"voice":null}}}
],"edges":[
  {"id":"e1","source":"c1","target":"v"},{"id":"e2","source":"i1","target":"v"},{"id":"e3","source":"c2","target":"v"}
]}`

func TestGeneration_ExpandsCharacterCardsAfterOrdinaryRefs(t *testing.T) {
	src, err := canvasgraph.Generation(mustParse(t, genCharPayload), "v")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range src.Assets {
		got = append(got, a.Kind+":"+a.AssetID)
	}
	want := []string{"image:11", "image:21", "image:22", "audio:31"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("参考素材顺序 = %v，期望普通连线在前，角色卡按主图、补充图、声音展开 %v", got, want)
	}
	if src.Pending != 0 {
		t.Errorf("还没主图的角色卡只是少一份参考，不算没产物的上游：Pending=%d", src.Pending)
	}
}

func TestRefIDs_CharacterFillsRemainingSlotsAndNeedsImages(t *testing.T) {
	src, err := canvasgraph.Generation(mustParse(t, genCharPayload), "v")
	if err != nil {
		t.Fatal(err)
	}
	limit := func(caps map[string]int) func(string) int { return func(kind string) int { return caps[kind] } }

	all := src.RefIDs([]string{"image", "video", "audio"}, limit(map[string]int{"image": 9, "audio": 3}))
	if got := join(all["image"]) + "|" + join(all["audio"]); got != "11,21,22|31" {
		t.Fatalf("放得下时全带：%v", all)
	}

	tight := src.RefIDs([]string{"image", "audio"}, limit(map[string]int{"image": 2, "audio": 3}))
	if join(tight["image"]) != "11,21" {
		t.Errorf("普通参考图先占名额，角色卡的图按顺序填剩下的、放不下的截掉：%v", tight["image"])
	}

	noImage := src.RefIDs([]string{"video", "audio"}, limit(map[string]int{"audio": 3}))
	if len(noImage["audio"]) != 0 || len(noImage["image"]) != 0 {
		t.Errorf("当前方式不收参考图时角色卡整张不带，声音也不带：%v", noImage)
	}
}

func join(ids []uint64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatUint(id, 10)
	}
	return strings.Join(parts, ",")
}

func TestComposePrompt_CharacterBindingDoesNotReplaceEmptyStory(t *testing.T) {
	g := mustParse(t, strings.Replace(genCharPayload, `"prompt":"走路"`, `"prompt":""`, 1))
	src, err := canvasgraph.Generation(g, "v")
	if err != nil {
		t.Fatal(err)
	}
	refs := src.RefIDs([]string{"image", "audio"}, func(string) int { return 9 })
	if got := canvasgraph.ComposePrompt(src, refs); got != "" {
		t.Fatalf("没有剧情时不应只提交角色对应说明：%q", got)
	}
}
