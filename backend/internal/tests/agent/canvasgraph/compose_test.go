package canvasgraph_test

import (
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// composePayload 是一张「三段视频 + 一个成片节点」的画布，视频自上而下排，
// 所以不写 composeOrder 时的默认顺序就是 V1 → V2 → V3。
const composePayload = `{
  "nodes": [
    {"id":"v1","type":"canvas","position":{"x":0,"y":0},
     "data":{"kind":"video","label":"S1","src":"http://x/1.mp4","assetId":"1","status":"done"}},
    {"id":"v2","type":"canvas","position":{"x":0,"y":400},
     "data":{"kind":"video","label":"S2","src":"http://x/2.mp4","assetId":"2","status":"done"}},
    {"id":"v3","type":"canvas","position":{"x":0,"y":800},
     "data":{"kind":"video","label":"S3","src":"http://x/3.mp4","assetId":"3","status":"done"}},
    {"id":"c1","type":"canvas","position":{"x":600,"y":200},
     "data":{"kind":"compose","label":"成片","finalAssetId":"902"}}
  ],
  "edges": [
    {"id":"e1","source":"v1","target":"c1"},
    {"id":"e2","source":"v2","target":"c1"},
    {"id":"e3","source":"v3","target":"c1"}
  ]
}`

// TestApplyComposeNodeTakesVideosOnly 成片节点只接视频：视频连进来可以，图片连进来、成片连出去都不行。
func TestApplyComposeNodeTakesVideosOnly(t *testing.T) {
	g := mustParse(t, `{}`)
	res := mustApply(t, g, `[
	  {"op":"create_node","tempId":"v","kind":"video","label":"S1"},
	  {"op":"create_node","tempId":"i","kind":"image","label":"首帧"},
	  {"op":"create_node","tempId":"c","kind":"compose","label":"成片"},
	  {"op":"connect","source":"v","target":"c"}
	]`)
	if len(res.Graph.Edges) != 1 {
		t.Fatalf("视频→成片应连得上，边数=%d", len(res.Graph.Edges))
	}
	if lbl := node(t, res.Graph, res.IDMap["c"]).Label(); lbl != "成片" {
		t.Errorf("成片节点标题=%q", lbl)
	}

	for _, raw := range []string{
		// 图片接不进成片：成片只吃视频
		`[{"op":"create_node","tempId":"i","kind":"image"},{"op":"create_node","tempId":"c","kind":"compose"},{"op":"connect","source":"i","target":"c"}]`,
		// 成片不能再喂给别人
		`[{"op":"create_node","tempId":"v","kind":"video"},{"op":"create_node","tempId":"c","kind":"compose"},{"op":"connect","source":"c","target":"v"}]`,
		// 成片不调模型，不能带提示词和模型
		`[{"op":"create_node","tempId":"c","kind":"compose","prompt":"拼起来","model":"seedance"}]`,
		// composeOrder 要等连线建好再写，不能写在新建里
		`[{"op":"create_node","tempId":"c","kind":"compose","composeOrder":["x"]}]`,
		// composeOrder 不能写到别的节点上
		`[{"op":"create_node","tempId":"v","kind":"video","composeOrder":["x"]}]`,
	} {
		if _, err := canvasgraph.Apply(g, mustOps(t, raw), opts()); err == nil {
			t.Errorf("应被拒绝：%s", raw)
		}
	}
}

// TestApplyComposeOrder 段落顺序存在 data.composeOrder：只认接进来的视频，空数组清空。
func TestApplyComposeOrder(t *testing.T) {
	g := mustParse(t, composePayload)

	res := mustApply(t, g, `[{"op":"update_node","id":"c1","composeOrder":["v3","v2"]}]`)
	order, _ := node(t, res.Graph, "c1").Data()["composeOrder"].([]any)
	if len(order) != 2 || order[0] != "v3" || order[1] != "v2" {
		t.Fatalf("composeOrder=%v", order)
	}

	// 没连线接进来的节点不能排
	cur := mustParse(t, composePayload)
	cur.Nodes = append(cur.Nodes, canvasgraph.Node{"id": "v9", "type": "canvas",
		"position": map[string]any{"x": 0, "y": 2000},
		"data":     map[string]any{"kind": "video", "label": "S9"}})
	if _, err := canvasgraph.Apply(cur, mustOps(t, `[{"op":"update_node","id":"c1","composeOrder":["v9"]}]`), opts()); err == nil {
		t.Error("没连线接进成片的视频不能写进 composeOrder")
	}

	// 别的节点没有段落顺序
	if _, err := canvasgraph.Apply(g, mustOps(t, `[{"op":"update_node","id":"v1","composeOrder":["v2"]}]`), opts()); err == nil {
		t.Error("composeOrder 只能写在成片节点上")
	}

	// 空数组清空：又回到全按位置排
	cleared := mustApply(t, res.Graph, `[{"op":"update_node","id":"c1","composeOrder":[]}]`)
	if _, has := node(t, cleared.Graph, "c1").Data()["composeOrder"]; has {
		t.Error("传空数组应清掉 composeOrder")
	}
}

// TestComposeCatalogSummary 目录里成片节点的摘要是「N 段 · 有没有成片 · 顺序」，
// 顺序按 composeOrder，没写就按画布位置从上到下——模型看到它才不会把成片节点说成「素材包」。
func TestComposeCatalogSummary(t *testing.T) {
	row := func(g *canvasgraph.Graph) string {
		for _, r := range canvasgraph.BuildCatalog(g, canvasgraph.CatalogOptions{PromptRunes: 200}).Nodes {
			if r.ID == "c1" {
				return r.Prompt
			}
		}
		t.Fatal("目录里没有成片节点")
		return ""
	}

	got := row(mustParse(t, composePayload))
	if !strings.HasPrefix(got, "3 段 · 已有成片 · S1 → S2 → S3") {
		t.Errorf("摘要=%q", got)
	}

	// 成片就是成片节点的产物：hasOutput 要认 finalAssetId，否则模型看到 false 会以为还没出片
	for _, r := range canvasgraph.BuildCatalog(mustParse(t, composePayload), canvasgraph.CatalogOptions{PromptRunes: 200}).Nodes {
		if r.ID == "c1" && !r.HasOutput {
			t.Error("已有成片的节点 hasOutput 应为 true")
		}
	}

	reordered := mustApply(t, mustParse(t, composePayload), `[{"op":"update_node","id":"c1","composeOrder":["v3"]}]`)
	if got := row(reordered.Graph); !strings.HasPrefix(got, "3 段 · 已有成片 · S3 → S1 → S2") {
		t.Errorf("调过顺序的摘要=%q", got)
	}
}

// TestComposeNodeCannotGenerate 成片节点不调模型：它自己不能当生成对象。
func TestComposeNodeCannotGenerate(t *testing.T) {
	if _, err := canvasgraph.Generation(mustParse(t, composePayload), "c1"); err == nil {
		t.Error("成片节点不能发起生成")
	}
}

// TestComposePlanOfComposeNode 成片节点的合成计划：顺序按 composeOrder，取每段当前那一版素材；
// 还没出片的段进 missing，不会拿空素材去拼。
func TestComposePlanOfComposeNode(t *testing.T) {
	plan, err := canvasgraph.ComposePlanOf(mustParse(t, composePayload), "c1")
	if err != nil {
		t.Fatalf("算合成计划失败：%v", err)
	}
	if plan.Kind != "compose" || len(plan.Segments) != 3 {
		t.Fatalf("计划=%+v", plan)
	}
	if ids := plan.AssetIDs(); len(ids) != 3 || ids[0] != "1" || ids[2] != "3" {
		t.Fatalf("素材序列=%v", ids)
	}
	if len(plan.Missing) != 0 {
		t.Errorf("三段都出片了，missing=%v", plan.Missing)
	}

	// 调过顺序后按 composeOrder 取
	shifted := mustApply(t, mustParse(t, composePayload), `[{"op":"update_node","id":"c1","composeOrder":["v3","v1"]}]`)
	plan, err = canvasgraph.ComposePlanOf(shifted.Graph, "c1")
	if err != nil {
		t.Fatalf("算合成计划失败：%v", err)
	}
	if ids := plan.AssetIDs(); len(ids) != 3 || ids[0] != "3" || ids[1] != "1" {
		t.Fatalf("调过顺序的素材序列=%v", ids)
	}

	// 一段没有产物：进 missing，AssetIDs 只剩能拼的两段
	pending := mustParse(t, `{
	  "nodes": [
	    {"id":"v1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"S1","src":"http://x/1.mp4","assetId":"1"}},
	    {"id":"v2","type":"canvas","position":{"x":0,"y":400},"data":{"kind":"video","label":"S2","status":"running"}},
	    {"id":"c1","type":"canvas","position":{"x":600,"y":0},"data":{"kind":"compose","label":"成片"}}
	  ],
	  "edges": [{"id":"e1","source":"v1","target":"c1"},{"id":"e2","source":"v2","target":"c1"}]
	}`)
	plan, err = canvasgraph.ComposePlanOf(pending, "c1")
	if err != nil {
		t.Fatalf("算合成计划失败：%v", err)
	}
	if ids := plan.AssetIDs(); len(ids) != 1 || ids[0] != "1" {
		t.Fatalf("素材序列=%v", ids)
	}
	if len(plan.Missing) != 1 || plan.Missing[0] != 1 {
		t.Fatalf("missing=%v", plan.Missing)
	}
}

// TestComposePlanOfStoryboard 分镜表按镜头行序取每镜绑定的视频，不按画布位置；
// 没绑视频的镜头算缺片。节点种类不能承载成片时报错写清原因。
func TestComposePlanOfStoryboard(t *testing.T) {
	g := mustParse(t, `{
	  "nodes": [
	    {"id":"sb","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"storyboard","label":"分镜表","storyboard":{"shots":[
	      {"id":"a","scene":"推门","videoNodeId":"v2"},
	      {"id":"b","scene":"特写","videoNodeId":"v1"},
	      {"id":"c","scene":"空镜"}
	    ]}}},
	    {"id":"v1","type":"canvas","position":{"x":900,"y":0},"data":{"kind":"video","label":"S1","src":"http://x/1.mp4","assetId":"1"}},
	    {"id":"v2","type":"canvas","position":{"x":900,"y":900},"data":{"kind":"video","label":"S2","src":"http://x/2.mp4","assetId":"2"}}
	  ],
	  "edges": []
	}`)
	plan, err := canvasgraph.ComposePlanOf(g, "sb")
	if err != nil {
		t.Fatalf("算合成计划失败：%v", err)
	}
	if plan.Kind != "storyboard" || len(plan.Segments) != 3 {
		t.Fatalf("计划=%+v", plan)
	}
	// 行序说了算：第 1 镜绑 v2，第 2 镜绑 v1，第 3 镜没绑
	if ids := plan.AssetIDs(); len(ids) != 2 || ids[0] != "2" || ids[1] != "1" {
		t.Fatalf("素材序列=%v", ids)
	}
	if len(plan.Missing) != 1 || plan.Missing[0] != 2 {
		t.Fatalf("missing=%v", plan.Missing)
	}

	// 视频节点不能承载成片，报错要写清是哪个节点
	if _, err := canvasgraph.ComposePlanOf(g, "v1"); err == nil || !strings.Contains(err.Error(), "不能承载成片") {
		t.Errorf("视频节点的错误=%v", err)
	}
	if _, err := canvasgraph.ComposePlanOf(g, "nope"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Errorf("不存在的节点的错误=%v", err)
	}
}

// TestComposeSummaryMarksMissing 目录摘要要标出没出片的段：模型看到「S2(未出片)」就不会直接提交。
func TestComposeSummaryMarksMissing(t *testing.T) {
	g := mustParse(t, `{
	  "nodes": [
	    {"id":"v1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"S1","src":"http://x/1.mp4","assetId":"1"}},
	    {"id":"v2","type":"canvas","position":{"x":0,"y":400},"data":{"kind":"video","label":"S2","status":"running"}},
	    {"id":"c1","type":"canvas","position":{"x":600,"y":0},"data":{"kind":"compose","label":"成片"}}
	  ],
	  "edges": [{"id":"e1","source":"v1","target":"c1"},{"id":"e2","source":"v2","target":"c1"}]
	}`)
	for _, r := range canvasgraph.BuildCatalog(g, canvasgraph.CatalogOptions{PromptRunes: 200}).Nodes {
		if r.ID != "c1" {
			continue
		}
		if !strings.Contains(r.Prompt, "S2(未出片)") || strings.Contains(r.Prompt, "S1(未出片)") {
			t.Errorf("摘要=%q", r.Prompt)
		}
		return
	}
	t.Fatal("目录里没有成片节点")
}
