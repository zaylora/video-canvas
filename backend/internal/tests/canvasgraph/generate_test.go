package canvasgraph_test

import (
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/canvasgraph"
)

// genPayload：剧本 → 角色图 → 镜头视频；另有一个还没出图的参考图和一个已经出图的节点。
const genPayload = `{
  "nodes": [
    {"id":"n_text","type":"canvas","data":{"kind":"script","label":"剧本","text":"男主走进便利店","status":"done"}},
    {"id":"n_empty","type":"canvas","data":{"kind":"script","label":"空文本","text":""}},
    {"id":"n_ref","type":"canvas","data":{"kind":"image","label":"角色","assetId":"42","src":"/files/42","status":"done"}},
    {"id":"n_wait","type":"canvas","data":{"kind":"image","label":"场景"}},
    {"id":"n_vid","type":"canvas","data":{"kind":"video","label":"镜头1","model":"seedance","params":{"prompt":"推镜","duration":5,"op":"i2v"}}},
    {"id":"n_done","type":"canvas","data":{"kind":"image","label":"成品","src":"/files/9","status":"done"}},
    {"id":"g1","type":"group","data":{"label":"组"}}
  ],
  "edges": [
    {"id":"e1","source":"n_text","target":"n_vid"},
    {"id":"e2","source":"n_empty","target":"n_vid"},
    {"id":"e3","source":"n_ref","target":"n_vid"},
    {"id":"e4","source":"n_wait","target":"n_vid"}
  ]
}`

func TestGeneration(t *testing.T) {
	g := mustParse(t, genPayload)

	t.Run("读出模型、提示词、参数和上游", func(t *testing.T) {
		src, err := canvasgraph.Generation(g, "n_vid")
		if err != nil {
			t.Fatal(err)
		}
		if src.Model != "seedance" || src.Prompt != "推镜" || src.Kind != "video" {
			t.Errorf("基本信息不对: %+v", src)
		}
		if _, has := src.Params["prompt"]; has || src.Params["op"] != "i2v" {
			t.Errorf("params 不含 prompt、保留其它字段: %v", src.Params)
		}
		if len(src.Texts) != 1 || src.Texts[0] != "男主走进便利店" {
			t.Errorf("空文本不收: %v", src.Texts)
		}
		if len(src.Assets) != 1 || src.Assets[0] != (canvasgraph.GenAsset{Kind: "image", AssetID: "42"}) {
			t.Errorf("素材: %v", src.Assets)
		}
		if src.Pending != 1 {
			t.Errorf("没出图的上游计数: %d", src.Pending)
		}
	})

	t.Run("产物状态", func(t *testing.T) {
		done, _ := canvasgraph.Generation(g, "n_done")
		fresh, _ := canvasgraph.Generation(g, "n_vid")
		if !done.HasOutput || fresh.HasOutput {
			t.Errorf("HasOutput done=%v fresh=%v", done.HasOutput, fresh.HasOutput)
		}
	})

	t.Run("data.prompt 兜底（旧节点）", func(t *testing.T) {
		g2 := mustParse(t, `{"nodes":[{"id":"a","type":"canvas","data":{"kind":"image","label":"x","prompt":"旧提示词"}}],"edges":[]}`)
		src, err := canvasgraph.Generation(g2, "a")
		if err != nil || src.Prompt != "旧提示词" {
			t.Errorf("src=%+v err=%v", src, err)
		}
	})

	t.Run("不存在和组都不能生成", func(t *testing.T) {
		for _, id := range []string{"ghost", "g1"} {
			if _, err := canvasgraph.Generation(g, id); !errors.Is(err, canvasgraph.ErrInvalid) {
				t.Errorf("%s 应返回 ErrInvalid: %v", id, err)
			}
		}
	})
}

func TestBindTasks(t *testing.T) {
	g := mustParse(t, genPayload)
	before := marshal(t, g)
	res := canvasgraph.BindTasks(g, map[string]string{"n_vid": "123", "ghost": "9", "g1": "8"})
	if marshal(t, g) != before {
		t.Fatal("入参图不能被修改")
	}
	d := node(t, res.Graph, "n_vid").Data()
	if d["taskId"] != "123" || d["status"] != "running" {
		t.Errorf("应写入 taskId 和 running: %v", d)
	}
	if len(res.Changes) != 1 || res.Changes[0].ID != "n_vid" {
		t.Errorf("不存在的节点和组被忽略，只有一项改动: %+v", res.Changes)
	}
	if d["params"].(map[string]any)["prompt"] != "推镜" {
		t.Error("其它字段不变")
	}
}

func TestImageRefs(t *testing.T) {
	g := mustParse(t, genPayload)
	refs, err := canvasgraph.ImageRefs(g, []string{"n_ref", "n_ref"})
	if err != nil || len(refs) != 2 || refs[0] != (canvasgraph.ImageRef{NodeID: "n_ref", Label: "角色", AssetID: "42"}) {
		t.Fatalf("refs=%+v err=%v", refs, err)
	}
	for id, want := range map[string]string{"ghost": "不存在", "g1": "不是图片节点", "n_vid": "不是图片节点", "n_text": "不是图片节点", "n_wait": "还没有图片内容"} {
		_, err := canvasgraph.ImageRefs(g, []string{id})
		if !errors.Is(err, canvasgraph.ErrInvalid) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s：应返回含 %q 的 ErrInvalid: %v", id, want, err)
		}
	}
}
