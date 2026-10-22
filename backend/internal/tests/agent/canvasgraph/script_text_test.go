package canvasgraph_test

import (
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// 文本节点没有提示词，Agent 用 prompt 字段写的内容就是节点正文（data.text）：
// 前端按 Markdown 渲染它，下游节点引用的也是它。

func TestCreateScriptNodeWritesBodyText(t *testing.T) {
	g := mustParse(t, `{}`)
	res := mustApply(t, g, `[{"op":"create_node","tempId":"s","kind":"script","label":"剧本","prompt":"## 第一集\n\n**类型：** 都市"}]`)
	d := node(t, res.Graph, res.IDMap["s"]).Data()

	if d["text"] != "## 第一集\n\n**类型：** 都市" {
		t.Errorf("正文应写进 data.text: %v", d)
	}
	if d["status"] != "done" {
		t.Errorf("有内容的文本节点应是 done，否则前端只显示占位: %v", d["status"])
	}
	if _, ok := d["prompt"]; ok {
		t.Errorf("文本节点不应再写 data.prompt: %v", d["prompt"])
	}
	if params, _ := d["params"].(map[string]any); params != nil {
		if _, ok := params["prompt"]; ok {
			t.Errorf("文本节点不应再写 params.prompt: %v", params)
		}
	}
}

func TestCreateScriptNodeWithoutContentStaysEmpty(t *testing.T) {
	g := mustParse(t, `{}`)
	res := mustApply(t, g, `[{"op":"create_node","tempId":"s","kind":"script","label":"空文本"}]`)
	d := node(t, res.Graph, res.IDMap["s"]).Data()
	if _, ok := d["text"]; ok {
		t.Errorf("没给内容不该有 text: %v", d)
	}
	if _, ok := d["status"]; ok {
		t.Errorf("没给内容不该有 status: %v", d)
	}
}

func TestUpdateScriptNodeReplacesBodyText(t *testing.T) {
	g := mustParse(t, `{"nodes":[{"id":"n1","type":"canvas","position":{"x":0,"y":0},
		"data":{"kind":"script","label":"剧本","text":"旧内容","status":"done"}}]}`)

	res := mustApply(t, g, `[{"op":"update_node","id":"n1","prompt":"新内容"}]`)
	d := node(t, res.Graph, "n1").Data()
	if d["text"] != "新内容" || d["status"] != "done" {
		t.Errorf("应替换正文: %v", d)
	}
	if _, ok := d["prompt"]; ok {
		t.Errorf("不应写 data.prompt: %v", d["prompt"])
	}

	cleared := mustApply(t, res.Graph, `[{"op":"update_node","id":"n1","prompt":""}]`)
	d = node(t, cleared.Graph, "n1").Data()
	if d["text"] != "" || d["status"] != "idle" {
		t.Errorf("清空后回到没内容: %v", d)
	}
}

func TestUpdateScriptNodeClearsStaleError(t *testing.T) {
	g := mustParse(t, `{"nodes":[{"id":"n1","type":"canvas","position":{"x":0,"y":0},
		"data":{"kind":"script","label":"剧本","status":"error","error":"失败了","errorTaskRef":"t1"}}]}`)
	res := mustApply(t, g, `[{"op":"update_node","id":"n1","prompt":"补写的内容"}]`)
	d := node(t, res.Graph, "n1").Data()
	if d["status"] != "done" {
		t.Errorf("status=%v", d["status"])
	}
	if _, ok := d["error"]; ok {
		t.Errorf("写入内容后旧的失败信息要清掉: %v", d)
	}
	if _, ok := d["errorTaskRef"]; ok {
		t.Errorf("errorTaskRef 也要清掉: %v", d)
	}
}

func TestNonScriptNodeKeepsPrompt(t *testing.T) {
	g := mustParse(t, `{}`)
	res := mustApply(t, g, `[{"op":"create_node","tempId":"x","kind":"image","label":"角色","prompt":"短发少女"}]`)
	d := node(t, res.Graph, res.IDMap["x"]).Data()
	if d["prompt"] != "短发少女" || d["params"].(map[string]any)["prompt"] != "短发少女" {
		t.Errorf("图片节点仍写提示词: %v", d)
	}
	if _, ok := d["text"]; ok {
		t.Errorf("图片节点不该有 text: %v", d)
	}
}

func TestAgentWrittenScriptFeedsDownstream(t *testing.T) {
	g := mustParse(t, `{}`)
	res := mustApply(t, g, `[
		{"op":"create_node","tempId":"s","kind":"script","label":"镜头文本","prompt":"近景，林夏抬头"},
		{"op":"create_node","tempId":"i","kind":"image","label":"关键帧"},
		{"op":"connect","source":"s","target":"i"}
	]`)
	src, err := canvasgraph.Generation(res.Graph, res.IDMap["i"])
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Texts) != 1 || src.Texts[0] != "近景，林夏抬头" {
		t.Errorf("Agent 写的文本节点应作为上游文字带进生成: %v", src.Texts)
	}
}

func TestCatalogShowsScriptBodyText(t *testing.T) {
	g := mustParse(t, `{"nodes":[
		{"id":"n1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"script","label":"剧本","text":"夜里一点，修车厂","status":"done"}},
		{"id":"n2","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"script","label":"旧节点","prompt":"旧版写在提示词里"}}
	]}`)
	cat := canvasgraph.BuildCatalog(g, canvasgraph.CatalogOptions{})
	got := map[string]string{}
	for _, row := range cat.Nodes {
		got[row.ID] = row.Prompt
	}
	if got["n1"] != "夜里一点，修车厂" {
		t.Errorf("目录里文本节点应显示正文摘要: %q", got["n1"])
	}
	if got["n2"] != "旧版写在提示词里" {
		t.Errorf("没有正文的旧节点仍回退到提示词: %q", got["n2"])
	}
}
