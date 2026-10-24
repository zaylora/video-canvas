package canvasdoc_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/canvasdoc"
)

// 角色卡十六进制 ID：cardLinXia 存在，cardDeleted 已被删除，
// cardNoAvatar 默认也不存在，个别用例里把它补成主图读不到的卡。
const (
	cardLinXia   = "0123456789abcdef0123456789abcdef"
	cardDeleted  = "deaddeaddeaddeaddeaddeaddeaddead"
	cardNoAvatar = "feedfeedfeedfeedfeedfeedfeedfeed"
)

// exportPayload 是一份 serializeGraph 形状的画布内容：组节点 g1、角色卡节点 n2、
// 覆盖所有素材引用位置的富节点 n3、引用已删除角色卡的 n4、引用主图读不到的角色卡的 n5。
// 素材 ID 用 904xx 这种不会和别的数字撞上的值，便于做泄漏扫描。
const exportPayload = `{
  "nodes": [
    {"id":"g1","type":"group","position":{"x":0,"y":0},"width":800,"height":600,"data":{"label":"第一场","color":"blue"}},
    {"id":"n2","type":"canvas","parentId":"g1","position":{"x":20,"y":40},
     "data":{"kind":"character","label":"林夏","characterId":"` + cardLinXia + `","labelCache":{"name":"林夏","coverAssetId":"90471"}}},
    {"id":"n3","type":"canvas","position":{"x":420,"y":140},
     "data":{
       "kind":"image","label":"街角镜头","prompt":"@[林夏](n2) 站在霓虹灯下","model":"image-model-a",
       "status":"running","taskId":"77003","pendingTaskIds":["77003","77004"],"errorTaskRef":"ref-77005",
       "assetId":"90417","src":"/files/u1/a.png","mediaType":"image","mediaAspect":1.7777,
       "uploaded":false,"fileName":"街角.png","text":null,"error":null,"outpaintFrom":"n2",
       "params":{"ratio":"16:9","images":["90421",90422],"videos":["90431"],"audios":["90441"]},
       "paramAssets":{
         "90421":{"url":"/files/u1/r1.png","fileName":"参考1.png","mediaType":"image"},
         "90431":{"url":"/files/u1/v.mp4","fileName":"参考.mp4","mediaType":"video"}
       },
       "outputs":[
         {"id":"90411","src":"/files/u1/o1.png","mediaType":"image","assetId":"90411","taskId":"77001","model":"image-model-a","createdAt":1761300000000},
         {"id":"90417","src":"/files/u1/a.png","mediaType":"image","assetId":"90417","taskId":"77002","model":"image-model-b","createdAt":1761300000001},
         {"id":"90412","src":"/files/u1/o2.png","mediaType":"image","assetId":"90412","model":"image-model-a","createdAt":1761300000002}
       ],
       "activeOutputId":"90417",
       "scene":{"version":1,"duration":5,"activeCameraId":"cam1","characters":[],"props":[],"cameras":[],
                "thumb":{"assetId":"90451","url":"/files/u1/thumb.png"}},
       "characterId":"` + cardLinXia + `",
       "x-extra":{"keep":"me","n":12345678901234567}
     }},
    {"id":"n4","type":"canvas","position":{"x":0,"y":700},
     "data":{"kind":"character","label":"老王","characterId":"` + cardDeleted + `","labelCache":{"name":"老王"}}},
    {"id":"n5","type":"canvas","position":{"x":300,"y":700},
     "data":{"kind":"character","label":"阿杰","characterId":"` + cardNoAvatar + `"}}
  ],
  "edges":[{"id":"e1","source":"n2","target":"n3","sourceHandle":"out","targetHandle":"in"}],
  "viewport":{"x":10,"y":20,"zoom":1.5}
}`

// hashOf 为夹具里的每个素材 ID 给一个固定的假哈希（单字符重复 64 次，不含任何素材 ID 子串）。
var hashOf = map[string]string{
	"90417": strings.Repeat("1", 64),
	"90421": strings.Repeat("2", 64),
	"90422": strings.Repeat("3", 64),
	"90431": strings.Repeat("4", 64),
	"90441": strings.Repeat("5", 64),
	"90451": strings.Repeat("6", 64),
	"90471": strings.Repeat("7", 64),
	"90472": strings.Repeat("8", 64),
	"90473": strings.Repeat("9", 64),
}

// allFixtureIDs 是夹具里出现过的全部素材 ID（包括只在非选中产物、labelCache 里的），用于泄漏扫描。
var allFixtureIDs = []string{
	"90411", "90412", "90417", "90421", "90422", "90431", "90441", "90451", "90471", "90472", "90473", "90481",
}

// exportInput 组装一份能解析全部素材的导出输入；drop 里的素材 ID 视为读不到。
func exportInput(drop ...string) canvasdoc.ExportInput {
	assets := map[string]canvasdoc.AssetEntry{}
	for id, h := range hashOf {
		assets[id] = canvasdoc.AssetEntry{Hash: h, Info: canvasdoc.AssetInfo{
			Kind: "image", MimeType: "image/png", ByteSize: 100, Width: 1, Height: 1, FileName: id + ".png",
		}}
	}
	for _, id := range drop {
		delete(assets, id)
	}
	return canvasdoc.ExportInput{
		Title:   "雨夜短片",
		Payload: []byte(exportPayload),
		Assets:  assets,
		Characters: map[string]canvasdoc.CharacterEntry{
			cardLinXia: {
				Name: "林夏", Description: "短发，黑色风衣", AvatarAssetID: "90471",
				ExtraAssetIDs: []string{"90472"}, VoiceAssetID: "90473",
			},
		},
		AppVersion: "0.4.16",
		Now:        time.Date(2026, 10, 24, 16, 0, 0, 0, time.FixedZone("CST", 8*3600)),
	}
}

// mustBuild 调用 Build，失败即终止测试，并断言结果能通过 Validate。
func mustBuild(t *testing.T, in canvasdoc.ExportInput) (*canvasdoc.Document, canvasdoc.ExportReport) {
	t.Helper()
	doc, report, err := canvasdoc.Build(in)
	if err != nil {
		t.Fatalf("Build 返回错误: %v", err)
	}
	if err := doc.Validate(canvasdoc.DefaultLimits()); err != nil {
		t.Fatalf("Build 产出的文档没通过 Validate: %v", err)
	}
	return doc, report
}

// nodeData 按 ID 找到文档里的节点并返回它的 data。
func nodeData(t *testing.T, doc *canvasdoc.Document, id string) map[string]any {
	t.Helper()
	for _, n := range doc.Canvas.Nodes {
		if n["id"] == id {
			data, _ := n["data"].(map[string]any)
			return data
		}
	}
	t.Fatalf("文档里没有节点 %s", id)
	return nil
}

func TestCollectRefs_FindsEveryAssetField(t *testing.T) {
	refs, err := canvasdoc.CollectRefs([]byte(exportPayload))
	if err != nil {
		t.Fatalf("CollectRefs 返回错误: %v", err)
	}
	// 非选中的产物 90411 / 90412 不导出，也就不必收集；labelCache 里的 90471 不是引用位置
	wantAssets := []string{"90417", "90421", "90422", "90431", "90441", "90451"}
	if !reflect.DeepEqual(refs.AssetIDs, wantAssets) {
		t.Errorf("AssetIDs = %v，期望 %v", refs.AssetIDs, wantAssets)
	}
	wantChars := []string{cardLinXia, cardDeleted, cardNoAvatar}
	if !reflect.DeepEqual(refs.CharacterIDs, wantChars) {
		t.Errorf("CharacterIDs = %v，期望 %v", refs.CharacterIDs, wantChars)
	}
}

func TestCollectRefs_InvalidPayload(t *testing.T) {
	if _, err := canvasdoc.CollectRefs([]byte(`not json`)); err == nil {
		t.Fatal("非法 JSON 应当返回错误")
	}
}

func TestBuild_TopLevelFields(t *testing.T) {
	doc, _ := mustBuild(t, exportInput())
	if doc.Format != canvasdoc.Format || doc.Version != canvasdoc.CurrentVersion {
		t.Errorf("format/version = %q/%d", doc.Format, doc.Version)
	}
	if doc.ExportedAt != "2026-10-24T08:00:00Z" {
		t.Errorf("ExportedAt = %q，期望 UTC 的 RFC3339", doc.ExportedAt)
	}
	if doc.AppVersion != "0.4.16" || doc.Canvas.Title != "雨夜短片" {
		t.Errorf("AppVersion/Title = %q/%q", doc.AppVersion, doc.Canvas.Title)
	}
}

func TestBuild_ReplacesAssetIDsWithHashes(t *testing.T) {
	doc, report := mustBuild(t, exportInput())
	if report.SkippedAssets != 0 {
		t.Errorf("SkippedAssets = %d，期望 0", report.SkippedAssets)
	}
	data := nodeData(t, doc, "n3")
	if data["assetHash"] != hashOf["90417"] {
		t.Errorf("assetHash = %v", data["assetHash"])
	}
	if _, ok := data["assetId"]; ok {
		t.Error("assetId 应当被删除")
	}
	params := data["params"].(map[string]any)
	wantLists := map[string][]any{
		"images": {hashOf["90421"], hashOf["90422"]},
		"videos": {hashOf["90431"]},
		"audios": {hashOf["90441"]},
	}
	for key, want := range wantLists {
		if !reflect.DeepEqual(params[key], want) {
			t.Errorf("params.%s = %v，期望 %v", key, params[key], want)
		}
	}
	if params["ratio"] != "16:9" {
		t.Errorf("params.ratio = %v，应原样保留", params["ratio"])
	}
	wantPA := map[string]any{
		hashOf["90421"]: map[string]any{"fileName": "参考1.png", "mediaType": "image"},
		hashOf["90431"]: map[string]any{"fileName": "参考.mp4", "mediaType": "video"},
	}
	if !reflect.DeepEqual(data["paramAssets"], wantPA) {
		t.Errorf("paramAssets = %v，期望 %v", data["paramAssets"], wantPA)
	}
	outputs := data["outputs"].([]any)
	if outputs[0].(map[string]any)["assetHash"] != hashOf["90417"] {
		t.Errorf("outputs[0].assetHash = %v", outputs[0])
	}
	scene := data["scene"].(map[string]any)
	if scene["thumbHash"] != hashOf["90451"] {
		t.Errorf("scene.thumbHash = %v", scene["thumbHash"])
	}
	if _, ok := scene["thumb"]; ok {
		t.Error("scene.thumb 应当被删除")
	}
	if scene["activeCameraId"] != "cam1" {
		t.Error("scene 的其余字段应原样保留")
	}
	// 用到的素材按哈希去重：n3 的 6 个 + 林夏卡的 3 个
	if len(doc.Assets) != 9 {
		t.Errorf("Assets 有 %d 条，期望 9", len(doc.Assets))
	}
	if doc.Assets[hashOf["90417"]].FileName != "90417.png" {
		t.Errorf("Assets 的元信息应取自输入: %+v", doc.Assets[hashOf["90417"]])
	}
}

func TestBuild_NoLeak(t *testing.T) {
	doc, _ := mustBuild(t, exportInput())
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	banned := []string{
		`/files/`, `"taskId"`, `"pendingTaskIds"`, `"revision"`, `"viewport"`, `"src"`, `"activeOutputId"`,
		`"errorTaskRef"`, `"labelCache"`, `"assetId"`, `"characterId"`, `"thumb"`, `"url"`,
		`77001`, `77002`, `77003`, `77004`, `77005`,
		cardLinXia, cardDeleted, cardNoAvatar,
	}
	for _, id := range allFixtureIDs {
		banned = append(banned, `"`+id+`"`)
	}
	for _, s := range banned {
		if strings.Contains(out, s) {
			t.Errorf("导出文档里出现了 %s", s)
		}
	}
}

func TestBuild_KeepsOnlyActiveOutput(t *testing.T) {
	doc, _ := mustBuild(t, exportInput())
	outputs := nodeData(t, doc, "n3")["outputs"].([]any)
	if len(outputs) != 1 {
		t.Fatalf("outputs 有 %d 项，期望只剩选中的 1 项", len(outputs))
	}
	want := map[string]any{
		"assetHash": hashOf["90417"],
		"mediaType": "image",
		"model":     "image-model-b",
		"createdAt": json.Number("1761300000001"),
	}
	if !reflect.DeepEqual(outputs[0], want) {
		t.Errorf("outputs[0] = %v，期望 %v", outputs[0], want)
	}
	raw, _ := json.Marshal(outputs[0])
	if !strings.Contains(string(raw), `"createdAt":1761300000001`) {
		t.Errorf("createdAt 序列化后变了: %s", raw)
	}
}

func TestBuild_ActiveOutputMissingKeepsLast(t *testing.T) {
	in := exportInput()
	in.Payload = []byte(`{"nodes":[{"id":"n1","type":"canvas","position":{"x":0,"y":0},"data":{
	  "kind":"image","label":"图","activeOutputId":"999",
	  "outputs":[
	    {"id":"90421","src":"/files/x","mediaType":"image","assetId":"90421","createdAt":1},
	    {"id":"90422","src":"/files/y","mediaType":"image","assetId":"90422","createdAt":2}
	  ]}}],"edges":[]}`)
	refs, err := canvasdoc.CollectRefs(in.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(refs.AssetIDs, []string{"90422"}) {
		t.Errorf("AssetIDs = %v，期望只收集最后一版", refs.AssetIDs)
	}
	doc, _ := mustBuild(t, in)
	outputs := nodeData(t, doc, "n1")["outputs"].([]any)
	if len(outputs) != 1 || outputs[0].(map[string]any)["assetHash"] != hashOf["90422"] {
		t.Errorf("找不到选中版时应保留最后一版，实际 %v", outputs)
	}
	if _, ok := outputs[0].(map[string]any)["model"]; ok {
		t.Error("原产物没有 model 时不应凭空加上")
	}
}

func TestBuild_RunningBecomesIdle(t *testing.T) {
	in := exportInput()
	in.Payload = []byte(`{"nodes":[
	  {"id":"a","type":"canvas","data":{"kind":"image","label":"a","status":"running"}},
	  {"id":"b","type":"canvas","data":{"kind":"image","label":"b","status":"done"}},
	  {"id":"c","type":"canvas","data":{"kind":"image","label":"c","status":"error"}},
	  {"id":"d","type":"canvas","data":{"kind":"image","label":"d","status":"idle"}}
	],"edges":[]}`)
	doc, _ := mustBuild(t, in)
	want := map[string]string{"a": "idle", "b": "done", "c": "error", "d": "idle"}
	for id, status := range want {
		if got := nodeData(t, doc, id)["status"]; got != status {
			t.Errorf("节点 %s 的 status = %v，期望 %s", id, got, status)
		}
	}
}

func TestBuild_KeepsUnknownFieldsAndIDs(t *testing.T) {
	doc, _ := mustBuild(t, exportInput())
	ids := make([]string, 0, len(doc.Canvas.Nodes))
	for _, n := range doc.Canvas.Nodes {
		ids = append(ids, n["id"].(string))
	}
	if !reflect.DeepEqual(ids, []string{"g1", "n2", "n3", "n4", "n5"}) {
		t.Errorf("节点 ID 或顺序变了: %v", ids)
	}
	group := doc.Canvas.Nodes[0]
	wantGroup := map[string]any{
		"id": "g1", "type": "group", "position": map[string]any{"x": json.Number("0"), "y": json.Number("0")},
		"width": json.Number("800"), "height": json.Number("600"),
		"data": map[string]any{"label": "第一场", "color": "blue"},
	}
	if !reflect.DeepEqual(group, wantGroup) {
		t.Errorf("组节点应原样保留，实际 %v", group)
	}
	if doc.Canvas.Nodes[1]["parentId"] != "g1" {
		t.Errorf("parentId = %v", doc.Canvas.Nodes[1]["parentId"])
	}
	data := nodeData(t, doc, "n3")
	if data["prompt"] != "@[林夏](n2) 站在霓虹灯下" {
		t.Errorf("prompt = %v", data["prompt"])
	}
	extra, _ := json.Marshal(data["x-extra"])
	if string(extra) != `{"keep":"me","n":12345678901234567}` {
		t.Errorf("x-extra = %s，应原样保留", extra)
	}
	keep := map[string]any{
		"kind": "image", "label": "街角镜头", "model": "image-model-a", "mediaType": "image",
		"mediaAspect": json.Number("1.7777"), "uploaded": false, "fileName": "街角.png",
		"text": nil, "error": nil, "outpaintFrom": "n2",
	}
	for k, v := range keep {
		if got, ok := data[k]; !ok || got != v {
			t.Errorf("data.%s = %v，期望原样 %v", k, got, v)
		}
	}
	wantEdges := []map[string]any{{"id": "e1", "source": "n2", "target": "n3", "sourceHandle": "out", "targetHandle": "in"}}
	if !reflect.DeepEqual(doc.Canvas.Edges, wantEdges) {
		t.Errorf("连线 = %v", doc.Canvas.Edges)
	}
}

func TestBuild_UnresolvedAssetIsSkippedAndCounted(t *testing.T) {
	doc, report := mustBuild(t, exportInput("90417", "90421"))
	data := nodeData(t, doc, "n3")
	if _, ok := data["assetHash"]; ok {
		t.Error("读不到的素材不应产生 assetHash")
	}
	if _, ok := data["outputs"]; ok {
		t.Error("选中版读不到时不应保留 outputs")
	}
	params := data["params"].(map[string]any)
	if !reflect.DeepEqual(params["images"], []any{hashOf["90422"]}) {
		t.Errorf("params.images = %v，读不到的那项应去掉", params["images"])
	}
	pa := data["paramAssets"].(map[string]any)
	if _, ok := pa[hashOf["90431"]]; !ok || len(pa) != 1 {
		t.Errorf("paramAssets = %v，读不到的那项应去掉", pa)
	}
	// 90417 同时是 assetId 和选中产物，只算一个
	if report.SkippedAssets != 2 {
		t.Errorf("SkippedAssets = %d，期望 2（按不同素材 ID 计数）", report.SkippedAssets)
	}
	raw, _ := json.Marshal(doc)
	for _, id := range []string{`"90417"`, `"90421"`} {
		if strings.Contains(string(raw), id) {
			t.Errorf("文档里不应出现读不到的素材 ID %s", id)
		}
	}
}

func TestBuild_UnresolvedThumbIsDropped(t *testing.T) {
	doc, report := mustBuild(t, exportInput("90451"))
	scene := nodeData(t, doc, "n3")["scene"].(map[string]any)
	if _, ok := scene["thumbHash"]; ok {
		t.Error("读不到的缩略图不应产生 thumbHash")
	}
	if _, ok := scene["thumb"]; ok {
		t.Error("scene.thumb 应当被删除")
	}
	if report.SkippedAssets != 1 {
		t.Errorf("SkippedAssets = %d，期望 1", report.SkippedAssets)
	}
}

func TestBuild_EmbedsCharacter(t *testing.T) {
	in := exportInput()
	// 主图 90481 不在 Assets 里
	in.Characters[cardNoAvatar] = canvasdoc.CharacterEntry{Name: "阿杰", AvatarAssetID: "90481"}
	doc, report := mustBuild(t, in)
	if len(doc.Characters) != 1 {
		t.Fatalf("Characters 有 %d 张，期望 1: %v", len(doc.Characters), doc.Characters)
	}
	voice := hashOf["90473"]
	want := canvasdoc.CharacterInfo{
		Name: "林夏", Description: "短发，黑色风衣", Avatar: hashOf["90471"],
		Extras: []string{hashOf["90472"]}, Voice: &voice,
	}
	if !reflect.DeepEqual(doc.Characters["c1"], want) {
		t.Errorf("c1 = %+v，期望 %+v", doc.Characters["c1"], want)
	}
	for _, h := range []string{hashOf["90471"], hashOf["90472"], hashOf["90473"]} {
		if _, ok := doc.Assets[h]; !ok {
			t.Errorf("角色卡素材 %s 不在 Assets 里", h[:8])
		}
	}
	// n2、n3 引用同一张卡，共用 c1
	for _, id := range []string{"n2", "n3"} {
		data := nodeData(t, doc, id)
		if data["characterRef"] != "c1" {
			t.Errorf("节点 %s 的 characterRef = %v，期望 c1", id, data["characterRef"])
		}
	}
	// 已删除的卡、主图读不到的卡：节点不带 characterRef
	for _, id := range []string{"n4", "n5"} {
		data := nodeData(t, doc, id)
		if _, ok := data["characterRef"]; ok {
			t.Errorf("节点 %s 不应有 characterRef", id)
		}
		if _, ok := data["characterId"]; ok {
			t.Errorf("节点 %s 的 characterId 应当被删除", id)
		}
	}
	// 读不到的主图 90481 计入跳过
	if report.SkippedAssets != 1 {
		t.Errorf("SkippedAssets = %d，期望 1", report.SkippedAssets)
	}
}

func TestBuild_CharacterWithoutVoiceOrExtras(t *testing.T) {
	in := exportInput()
	in.Characters[cardLinXia] = canvasdoc.CharacterEntry{Name: "林夏", AvatarAssetID: "90471"}
	doc, _ := mustBuild(t, in)
	c := doc.Characters["c1"]
	if c.Voice != nil || c.Extras == nil || len(c.Extras) != 0 {
		t.Errorf("没有声音和补充图时应为 voice=null、extras=[]，实际 %+v", c)
	}
}

func TestBuild_EmptyCanvas(t *testing.T) {
	in := exportInput()
	in.Payload = []byte(`{"nodes":[],"edges":[],"viewport":{"x":0,"y":0,"zoom":1}}`)
	doc, report := mustBuild(t, in)
	if doc.Assets == nil || doc.Characters == nil {
		t.Fatal("Assets / Characters 应为非 nil map")
	}
	if report.SkippedAssets != 0 {
		t.Errorf("SkippedAssets = %d", report.SkippedAssets)
	}
	raw, _ := json.Marshal(doc)
	for _, s := range []string{`"assets":{}`, `"characters":{}`, `"nodes":[]`, `"edges":[]`} {
		if !strings.Contains(string(raw), s) {
			t.Errorf("序列化结果缺少 %s: %s", s, raw)
		}
	}
}

func TestBuild_InvalidPayload(t *testing.T) {
	in := exportInput()
	in.Payload = []byte(`{"nodes":"oops"}`)
	if _, _, err := canvasdoc.Build(in); err == nil {
		t.Fatal("nodes 不是数组时应当返回错误")
	}
}

func TestBuild_RejectsUnregisteredAssetField(t *testing.T) {
	in := exportInput()
	// 假设将来新增了「封面素材」字段却没登记改写规则：宁可导出失败，也不泄出原素材 ID
	in.Payload = []byte(`{"nodes":[{"id":"n1","type":"canvas","data":{"kind":"image","label":"a",
	  "cover":{"assetId":"90417","url":"/files/u1/c.png"}}}],"edges":[]}`)
	if _, _, err := canvasdoc.Build(in); err == nil {
		t.Fatal("残留 assetId 时应当返回错误")
	}
}
