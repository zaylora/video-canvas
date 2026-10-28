package canvasdoc_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"video-canvas/internal/canvasdoc"
)

// restoredPayload 是 Restore 返回的画布内容，数字用 json.Number 解码以便逐字比较。
type restoredPayload struct {
	Nodes    []map[string]any `json:"nodes"`
	Edges    []map[string]any `json:"edges"`
	Viewport map[string]any   `json:"viewport"`
}

// decodeRestored 解析 Restore 的输出。
func decodeRestored(t *testing.T, raw []byte) restoredPayload {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var p restoredPayload
	if err := dec.Decode(&p); err != nil {
		t.Fatalf("Restore 输出不是合法 JSON: %v\n%s", err, raw)
	}
	return p
}

// restoredData 按 ID 找到还原结果里的节点并返回它的 data。
func restoredData(t *testing.T, p restoredPayload, id string) map[string]any {
	t.Helper()
	for _, n := range p.Nodes {
		if n["id"] == id {
			data, _ := n["data"].(map[string]any)
			return data
		}
	}
	t.Fatalf("还原结果里没有节点 %s", id)
	return nil
}

// newAssets 给文档里的每个哈希分配一个新素材 ID（6000 起按哈希排序递增）和新地址；skip 里的哈希视为复制失败。
func newAssets(doc *canvasdoc.Document, skip ...string) map[string]canvasdoc.RestoredAsset {
	hashes := make([]string, 0, len(doc.Assets))
	for h := range doc.Assets {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	out := map[string]canvasdoc.RestoredAsset{}
	for i, h := range hashes {
		id := strconv.Itoa(6000 + i)
		out[h] = canvasdoc.RestoredAsset{ID: id, URL: "/files/u2/" + id + ".png"}
	}
	for _, h := range skip {
		delete(out, h)
	}
	return out
}

// mustRestore 调用 Restore，失败即终止测试。
func mustRestore(t *testing.T, in canvasdoc.RestoreInput) (restoredPayload, []byte, canvasdoc.RestoreReport) {
	t.Helper()
	raw, rep, err := canvasdoc.Restore(in)
	if err != nil {
		t.Fatalf("Restore 返回错误: %v", err)
	}
	return decodeRestored(t, raw), raw, rep
}

// mustParseDoc 解析并校验一份文档 JSON。
func mustParseDoc(t *testing.T, raw string) *canvasdoc.Document {
	t.Helper()
	doc, err := canvasdoc.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	if err := doc.Validate(canvasdoc.DefaultLimits()); err != nil {
		t.Fatalf("Validate 返回错误: %v", err)
	}
	return doc
}

func TestRestore_RoundTripWithBuild(t *testing.T) {
	doc, _ := mustBuild(t, exportInput())
	before, _ := json.Marshal(doc.Canvas)
	assets := newAssets(doc)
	p, raw, rep := mustRestore(t, canvasdoc.RestoreInput{Doc: doc, Assets: assets})

	if len(rep.MissingAssets) != 0 || rep.EdgesDropped != 0 {
		t.Errorf("报告应为空，实际 %+v", rep)
	}
	after, _ := json.Marshal(doc.Canvas)
	if !bytes.Equal(before, after) {
		t.Error("Restore 不应改动传入的文档")
	}

	// 1. 节点 ID、顺序、连线、视口
	ids := make([]string, 0, len(p.Nodes))
	for _, n := range p.Nodes {
		ids = append(ids, n["id"].(string))
	}
	if !reflect.DeepEqual(ids, []string{"g1", "n2", "n3", "n4", "n5"}) {
		t.Errorf("节点 ID 或顺序变了: %v", ids)
	}
	wantEdges := []map[string]any{{"id": "e1", "source": "n2", "target": "n3", "sourceHandle": "out", "targetHandle": "in"}}
	if !reflect.DeepEqual(p.Edges, wantEdges) {
		t.Errorf("连线 = %v", p.Edges)
	}
	wantViewport := map[string]any{"x": json.Number("0"), "y": json.Number("0"), "zoom": json.Number("1")}
	if !reflect.DeepEqual(p.Viewport, wantViewport) {
		t.Errorf("viewport = %v，期望默认值", p.Viewport)
	}
	if !reflect.DeepEqual(p.Nodes[0], doc.Canvas.Nodes[0]) {
		t.Errorf("组节点应原样，实际 %v", p.Nodes[0])
	}

	// 2. 富节点 n3 的每个素材位置都换成新 ID / 新地址
	newOf := func(oldID string) canvasdoc.RestoredAsset { return assets[hashOf[oldID]] }
	main := newOf("90417")
	data := restoredData(t, p, "n3")
	if data["assetId"] != main.ID || data["src"] != main.URL {
		t.Errorf("assetId/src = %v/%v，期望 %s/%s", data["assetId"], data["src"], main.ID, main.URL)
	}
	if data["activeOutputId"] != main.ID {
		t.Errorf("activeOutputId = %v，期望 %s", data["activeOutputId"], main.ID)
	}
	wantOutputs := []any{map[string]any{
		"id": main.ID, "src": main.URL, "assetId": main.ID, "mediaType": "image",
		"model": "image-model-b", "createdAt": json.Number("1761300000001"),
	}}
	if !reflect.DeepEqual(data["outputs"], wantOutputs) {
		t.Errorf("outputs = %v，期望 %v", data["outputs"], wantOutputs)
	}
	params := data["params"].(map[string]any)
	wantLists := map[string][]any{
		"images": {newOf("90421").ID, newOf("90422").ID},
		"videos": {newOf("90431").ID},
		"audios": {newOf("90441").ID},
	}
	for key, want := range wantLists {
		if !reflect.DeepEqual(params[key], want) {
			t.Errorf("params.%s = %v，期望 %v", key, params[key], want)
		}
	}
	if params["ratio"] != "16:9" {
		t.Errorf("params.ratio = %v", params["ratio"])
	}
	wantPA := map[string]any{
		newOf("90421").ID: map[string]any{"url": newOf("90421").URL, "fileName": "参考1.png", "mediaType": "image"},
		newOf("90431").ID: map[string]any{"url": newOf("90431").URL, "fileName": "参考.mp4", "mediaType": "video"},
	}
	if !reflect.DeepEqual(data["paramAssets"], wantPA) {
		t.Errorf("paramAssets = %v，期望 %v", data["paramAssets"], wantPA)
	}
	scene := data["scene"].(map[string]any)
	wantThumb := map[string]any{"assetId": newOf("90451").ID, "url": newOf("90451").URL}
	if !reflect.DeepEqual(scene["thumb"], wantThumb) || scene["activeCameraId"] != "cam1" {
		t.Errorf("scene = %v，期望 thumb %v 且其余字段原样", scene, wantThumb)
	}
	if data["prompt"] != "@[林夏](n2) 站在霓虹灯下" || data["status"] != "idle" {
		t.Errorf("prompt/status = %v/%v", data["prompt"], data["status"])
	}
	extra, _ := json.Marshal(data["x-extra"])
	if string(extra) != `{"keep":"me","n":12345678901234567}` {
		t.Errorf("x-extra = %s，应原样保留", extra)
	}

	// 3. 角色节点：主图、补充图、音频音色换成新素材 ID 与地址，其余角色数据原样；文本音色原样
	lin := restoredData(t, p, "n2")
	if lin["assetId"] != newOf("90471").ID || lin["src"] != newOf("90471").URL {
		t.Errorf("n2 主图 = %v/%v", lin["assetId"], lin["src"])
	}
	wantChar := map[string]any{
		"name": "林夏", "look": "基础形象", "description": "短发，黑色风衣",
		"extras": []any{map[string]any{"assetId": newOf("90472").ID, "src": newOf("90472").URL}},
		"voice":  map[string]any{"mode": "audio", "assetId": newOf("90473").ID, "src": newOf("90473").URL, "origin": "upload"},
	}
	if !reflect.DeepEqual(lin["character"], wantChar) {
		t.Errorf("n2 character = %#v，期望 %#v", lin["character"], wantChar)
	}
	text := restoredData(t, p, "n4")["character"].(map[string]any)
	if !reflect.DeepEqual(text["voice"], map[string]any{"mode": "text", "text": "沙哑的中年男声"}) {
		t.Errorf("n4 文本音色 = %#v", text["voice"])
	}
	for _, id := range []string{"n2", "n3", "n4", "n5"} {
		if _, ok := restoredData(t, p, id)["characterId"]; ok {
			t.Errorf("节点 %s 不应有 characterId", id)
		}
	}

	// 4. 不残留文件里的哈希、局部编号字段，也不出现原环境的素材 ID
	out := string(raw)
	for h := range doc.Assets {
		if strings.Contains(out, h) {
			t.Errorf("还原结果里残留哈希 %s", h[:8])
		}
	}
	for _, s := range []string{`"assetHash"`, `"thumbHash"`, `"characterRef"`, `/files/u1/`} {
		if strings.Contains(out, s) {
			t.Errorf("还原结果里残留 %s", s)
		}
	}
	for _, id := range allFixtureIDs {
		if strings.Contains(out, `"`+id+`"`) {
			t.Errorf("还原结果里出现了原素材 ID %s", id)
		}
	}

	// 5. 往返不变：把还原结果按新 ID → 原哈希再导出一次，得到的画布和素材清单和第一次导出完全相同
	idToHash := map[string]canvasdoc.AssetEntry{}
	for h, a := range assets {
		idToHash[a.ID] = canvasdoc.AssetEntry{Hash: h, Info: doc.Assets[h]}
	}
	in2 := exportInput()
	in2.Payload = raw
	in2.Assets = idToHash
	doc2, _ := mustBuild(t, in2)
	again, _ := json.Marshal(doc2.Canvas)
	if !bytes.Equal(before, again) {
		t.Errorf("再次导出的画布与第一次不同:\n第一次 %s\n第二次 %s", before, again)
	}
	if !reflect.DeepEqual(doc2.Assets, doc.Assets) {
		t.Error("再次导出的素材与第一次不同")
	}
}

func TestRestore_MissingAssetKeepsNode(t *testing.T) {
	doc, _ := mustBuild(t, exportInput())
	// 主图和选中产物同为 90417，参考图 90421 同时在 params.images 和 paramAssets 里：各只报一次
	assets := newAssets(doc, hashOf["90417"], hashOf["90421"])
	p, raw, rep := mustRestore(t, canvasdoc.RestoreInput{Doc: doc, Assets: assets})

	if len(p.Nodes) != 5 {
		t.Fatalf("节点数 = %d，缺素材的节点也应保留", len(p.Nodes))
	}
	data := restoredData(t, p, "n3")
	for _, key := range []string{"assetId", "src", "outputs", "activeOutputId", "assetHash"} {
		if _, ok := data[key]; ok {
			t.Errorf("主素材缺失时不应有 %s", key)
		}
	}
	params := data["params"].(map[string]any)
	if !reflect.DeepEqual(params["images"], []any{assets[hashOf["90422"]].ID}) {
		t.Errorf("params.images = %v，缺失的那项应去掉", params["images"])
	}
	pa := data["paramAssets"].(map[string]any)
	if _, ok := pa[assets[hashOf["90431"]].ID]; !ok || len(pa) != 1 {
		t.Errorf("paramAssets = %v，缺失的那项应去掉", pa)
	}
	if restoredData(t, p, "n2")["assetId"] != assets[hashOf["90471"]].ID {
		t.Error("其他节点不应受影响")
	}
	want := []canvasdoc.MissingAsset{{NodeID: "n3", FileName: "90417.png"}, {NodeID: "n3", FileName: "90421.png"}}
	if !reflect.DeepEqual(rep.MissingAssets, want) {
		t.Errorf("MissingAssets = %+v，期望 %+v", rep.MissingAssets, want)
	}
	for _, h := range []string{hashOf["90417"], hashOf["90421"]} {
		if strings.Contains(string(raw), h) {
			t.Errorf("缺失的哈希 %s 不应残留", h[:8])
		}
	}
}

// sameHashDoc 里两个节点在每个素材位置都引用 h1，角色节点的补充图和音频音色也是 h1。
func sameHashDoc(t *testing.T) *canvasdoc.Document {
	t.Helper()
	nodes := `[
	  {"id":"a","type":"canvas","data":{"kind":"image","label":"a","assetHash":"` + h1 + `",
	    "outputs":[{"assetHash":"` + h1 + `","mediaType":"image","createdAt":1}],
	    "params":{"images":["` + h1 + `"],"videos":[],"audios":["` + h1 + `"]},
	    "paramAssets":{"` + h1 + `":{"fileName":"a.png","mediaType":"image"}},
	    "scene":{"version":1,"thumbHash":"` + h1 + `"},
	    "character":{"name":"a","extras":[{"assetHash":"` + h1 + `"}],"voice":{"mode":"audio","assetHash":"` + h1 + `","origin":"generated"}}}},
	  {"id":"b","type":"canvas","data":{"kind":"image","label":"b","assetHash":"` + h1 + `"}}
	]`
	return mustParseDoc(t, docJSON(nodes))
}

func TestRestore_SameHashUsedEverywhereMapsToOneID(t *testing.T) {
	doc := sameHashDoc(t)
	a := canvasdoc.RestoredAsset{ID: "7001", URL: "/files/u2/7001.png"}
	p, _, rep := mustRestore(t, canvasdoc.RestoreInput{Doc: doc, Assets: map[string]canvasdoc.RestoredAsset{h1: a}})
	if len(rep.MissingAssets) != 0 {
		t.Errorf("报告应为空，实际 %+v", rep)
	}
	data := restoredData(t, p, "a")
	if data["assetId"] != a.ID || data["src"] != a.URL || data["activeOutputId"] != a.ID {
		t.Errorf("assetId/src/activeOutputId = %v/%v/%v", data["assetId"], data["src"], data["activeOutputId"])
	}
	out := data["outputs"].([]any)[0].(map[string]any)
	if out["id"] != a.ID || out["assetId"] != a.ID || out["src"] != a.URL {
		t.Errorf("outputs[0] = %v", out)
	}
	params := data["params"].(map[string]any)
	if !reflect.DeepEqual(params["images"], []any{a.ID}) || !reflect.DeepEqual(params["audios"], []any{a.ID}) {
		t.Errorf("params = %v", params)
	}
	if !reflect.DeepEqual(params["videos"], []any{}) {
		t.Errorf("空数组应保持为空数组，实际 %v", params["videos"])
	}
	wantPA := map[string]any{a.ID: map[string]any{"url": a.URL, "fileName": "a.png", "mediaType": "image"}}
	if !reflect.DeepEqual(data["paramAssets"], wantPA) {
		t.Errorf("paramAssets = %v", data["paramAssets"])
	}
	thumb := data["scene"].(map[string]any)["thumb"]
	if !reflect.DeepEqual(thumb, map[string]any{"assetId": a.ID, "url": a.URL}) {
		t.Errorf("scene.thumb = %v", thumb)
	}
	wantChar := map[string]any{
		"name":   "a",
		"extras": []any{map[string]any{"assetId": a.ID, "src": a.URL}},
		"voice":  map[string]any{"mode": "audio", "assetId": a.ID, "src": a.URL, "origin": "generated"},
	}
	if !reflect.DeepEqual(data["character"], wantChar) {
		t.Errorf("character = %#v，期望 %#v", data["character"], wantChar)
	}
	if restoredData(t, p, "b")["assetId"] != a.ID {
		t.Error("另一个节点引用同一哈希也应得到同一个新 ID")
	}

	// 同一哈希缺失：每个受影响的节点各报一次
	p, _, rep = mustRestore(t, canvasdoc.RestoreInput{Doc: doc})
	want := []canvasdoc.MissingAsset{{NodeID: "a", FileName: "a.png"}, {NodeID: "b", FileName: "a.png"}}
	if !reflect.DeepEqual(rep.MissingAssets, want) {
		t.Errorf("MissingAssets = %+v，期望 %+v", rep.MissingAssets, want)
	}
	// 素材缺失时角色的补充图去掉、音频音色按「无音色」，其余角色数据保留
	c := restoredData(t, p, "a")["character"].(map[string]any)
	if extras, ok := c["extras"].([]any); !ok || len(extras) != 0 {
		t.Errorf("extras = %#v，缺失的补充图应去掉", c["extras"])
	}
	if v, ok := c["voice"]; !ok || v != nil {
		t.Errorf("voice = %#v，缺失的音频音色应为 null", c["voice"])
	}
	if c["name"] != "a" {
		t.Errorf("character.name = %v", c["name"])
	}
}

func TestRestore_DropsDanglingEdges(t *testing.T) {
	raw := strings.Replace(docJSON(`[{"id":"n1","type":"canvas","data":{}},{"id":"n2","type":"canvas","data":{}}]`),
		`"edges": []`, `"edges": [
		  {"id":"e1","source":"n1","target":"n2"},
		  {"id":"e2","source":"n1","target":"ghost"},
		  {"id":"e3","source":"ghost","target":"n2"},
		  {"id":"e4","source":"n1"},
		  {"id":"e5","source":"n1","target":2}
		]`, 1)
	doc := mustParseDoc(t, raw)
	p, _, rep := mustRestore(t, canvasdoc.RestoreInput{Doc: doc})
	if len(p.Edges) != 1 || p.Edges[0]["id"] != "e1" {
		t.Errorf("连线 = %v，期望只剩 e1", p.Edges)
	}
	if rep.EdgesDropped != 4 {
		t.Errorf("EdgesDropped = %d，期望 4", rep.EdgesDropped)
	}
}

// 角色卡下线前导出的旧文件里，节点带 characterRef：导入时只丢弃这个引用，节点保留，不产生 characterId。
func TestRestore_LegacyCharacterRefIsDropped(t *testing.T) {
	doc := mustParseDoc(t, docJSON(`[
	  {"id":"n1","type":"canvas","data":{"kind":"character","label":"林夏","characterRef":"c1"}},
	  {"id":"n2","type":"canvas","data":{"kind":"character","label":"林夏 2","characterRef":5}}
	]`))
	p, raw, rep := mustRestore(t, canvasdoc.RestoreInput{Doc: doc})
	if len(p.Nodes) != 2 {
		t.Fatalf("节点数 = %d，旧角色节点也应保留", len(p.Nodes))
	}
	for _, s := range []string{`"characterRef"`, `"characterId"`} {
		if strings.Contains(string(raw), s) {
			t.Errorf("%s 不应残留", s)
		}
	}
	if len(rep.MissingAssets) != 0 {
		t.Errorf("报告应为空，实际 %+v", rep)
	}
}

func TestRestore_EmptyCanvas(t *testing.T) {
	doc := mustParseDoc(t, `{"format":"video-canvas/canvas","version":1,"canvas":{"title":"空","nodes":null}}`)
	raw, rep, err := canvasdoc.Restore(canvasdoc.RestoreInput{Doc: doc})
	if err != nil {
		t.Fatalf("Restore 返回错误: %v", err)
	}
	want := `{"nodes":[],"edges":[],"viewport":{"x":0,"y":0,"zoom":1}}`
	if string(raw) != want {
		t.Errorf("payload = %s，期望 %s", raw, want)
	}
	if !reflect.DeepEqual(rep, canvasdoc.RestoreReport{}) {
		t.Errorf("报告应为空，实际 %+v", rep)
	}
}

func TestRestore_NilDocument(t *testing.T) {
	if _, _, err := canvasdoc.Restore(canvasdoc.RestoreInput{}); err == nil {
		t.Fatal("Doc 为 nil 时应当返回错误")
	}
}

func TestRestore_HandEditedNodesDoNotPanic(t *testing.T) {
	// 手改的文件：缺 id、id 重复、data 不是对象、outputs 不是数组、params 里混入数字，
	// 以及直接写了原环境字段（assetId / src / characterId / scene.thumb / 任务字段）
	doc := mustParseDoc(t, docJSON(`[
	  {"type":"canvas","data":{"kind":"image","label":"无 id","assetHash":"`+h1+`"}},
	  {"id":"d","type":"canvas","data":{"kind":"image","label":"d1"}},
	  {"id":"d","type":"canvas","data":"oops"},
	  {"id":"x","type":"canvas","data":{"kind":"image","label":"x",
	    "assetId":"90417","src":"/files/u1/a.png","characterId":"`+legacyCardID+`","activeOutputId":"90417",
	    "taskId":"1","pendingTaskIds":["1"],"errorTaskRef":"r","labelCache":{"name":"n"},
	    "outputs":"oops","params":{"images":[90417,"`+h1+`"]},"paramAssets":"oops",
	    "scene":{"thumb":{"assetId":"90451","url":"/files/u1/t.png"}},"characterRef":5}}
	]`))
	a := canvasdoc.RestoredAsset{ID: "7001", URL: "/files/u2/7001.png"}
	p, raw, rep := mustRestore(t, canvasdoc.RestoreInput{Doc: doc})
	if len(p.Nodes) != 4 {
		t.Fatalf("节点数 = %d，期望 4", len(p.Nodes))
	}
	if want := []canvasdoc.MissingAsset{{NodeID: "", FileName: "a.png"}, {NodeID: "x", FileName: "a.png"}}; !reflect.DeepEqual(rep.MissingAssets, want) {
		t.Errorf("MissingAssets = %+v，期望 %+v", rep.MissingAssets, want)
	}
	for _, s := range []string{`90417`, `90451`, `/files/u1/`, legacyCardID, `"taskId"`, `"pendingTaskIds"`, `"errorTaskRef"`, `"labelCache"`, `"activeOutputId"`, `"outputs"`} {
		if strings.Contains(string(raw), s) {
			t.Errorf("还原结果里不应出现文件自带的原环境字段 %s", s)
		}
	}

	p, _, _ = mustRestore(t, canvasdoc.RestoreInput{Doc: doc, Assets: map[string]canvasdoc.RestoredAsset{h1: a}})
	data := p.Nodes[3]["data"].(map[string]any)
	if !reflect.DeepEqual(data["params"].(map[string]any)["images"], []any{a.ID}) {
		t.Errorf("params.images = %v，不是哈希的项应去掉", data["params"])
	}
	if p.Nodes[0]["data"].(map[string]any)["assetId"] != a.ID {
		t.Error("缺 id 的节点也应正常还原")
	}
}
