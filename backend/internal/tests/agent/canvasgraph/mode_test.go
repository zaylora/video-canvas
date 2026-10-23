package canvasgraph_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// 测试用的模型能力：veo 是视频模型（三种方式都支持），kling 只支持文生视频，flux 是图片模型（文生图 + 图生图，自动判断）。
func testModelOps(model string) ([]string, bool) {
	switch model {
	case "veo":
		return []string{"t2v", "i2v", "omni"}, true
	case "kling":
		return []string{"t2v"}, true
	case "flux":
		return []string{"t2i", "i2i"}, true
	}
	return nil, false
}

func modeOpts() canvasgraph.Options {
	return canvasgraph.Options{NewID: seqID(), ModelOps: testModelOps}
}

func applyMode(t *testing.T, g *canvasgraph.Graph, raw string) (*canvasgraph.Result, error) {
	t.Helper()
	return canvasgraph.Apply(g, mustOps(t, raw), modeOpts())
}

func paramOp(n canvasgraph.Node) any {
	params, _ := n.Data()["params"].(map[string]any)
	return params["op"]
}

func TestApplyMode_SetsParamsOp(t *testing.T) {
	g := mustParse(t, `{}`)
	res, err := applyMode(t, g, `[{"op":"create_node","tempId":"v","kind":"video","label":"镜头","model":"veo","mode":"i2v"}]`)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := paramOp(node(t, res.Graph, res.IDMap["v"])); got != "i2v" {
		t.Errorf("params.op = %v，想要 i2v", got)
	}
}

func TestApplyMode_UpdateNodeOnlyMode(t *testing.T) {
	g := mustParse(t, `{"nodes":[{"id":"v1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"镜头","model":"veo"}}],"edges":[]}`)
	res, err := applyMode(t, g, `[{"op":"update_node","id":"v1","mode":"omni"}]`)
	if err != nil {
		t.Fatalf("只改 mode 也是有效修改: %v", err)
	}
	if got := paramOp(node(t, res.Graph, "v1")); got != "omni" {
		t.Errorf("params.op = %v，想要 omni", got)
	}
}

func TestApplyMode_Rejections(t *testing.T) {
	g := mustParse(t, `{"nodes":[
	  {"id":"v1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"无模型"}},
	  {"id":"s1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"script","label":"剧本"}}
	],"edges":[]}`)
	cases := []struct{ name, ops, want string }{
		{"模型不支持这种方式", `[{"op":"create_node","kind":"video","model":"kling","mode":"omni"}]`, "不支持"},
		{"自动判断的图片模型不用设", `[{"op":"create_node","kind":"image","model":"flux","mode":"i2i"}]`, "自动"},
		{"不认识的方式", `[{"op":"create_node","kind":"video","model":"veo","mode":"foo"}]`, "不认识"},
		{"视频节点不能用图片的方式", `[{"op":"create_node","kind":"video","model":"veo","mode":"t2i"}]`, "视频节点"},
		{"文本节点没有生成方式", `[{"op":"update_node","id":"s1","mode":"t2v"}]`, "图片、视频"},
		{"还没选模型", `[{"op":"update_node","id":"v1","mode":"t2v"}]`, "先"},
		{"不能再从 params 里直接写 op", `[{"op":"create_node","kind":"video","model":"veo","params":{"op":"i2v"}}]`, "mode"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := applyMode(t, g, c.ops)
			var ve *canvasgraph.ValidationError
			if !errors.As(err, &ve) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v，想要 ValidationError 且含 %q", err, c.want)
			}
		})
	}
}

func TestApplyMode_UnknownModelIsNotChecked(t *testing.T) {
	// 模型不在可用清单里（可能已下架）：校验交给生成时，这里只检查方式本身合不合节点种类
	g := mustParse(t, `{}`)
	if _, err := applyMode(t, g, `[{"op":"create_node","kind":"video","model":"ghost","mode":"i2v"}]`); err != nil {
		t.Fatalf("未知模型不该因 mode 被拒: %v", err)
	}
}

func TestApplyMode_NoHookStillValidatesKind(t *testing.T) {
	g := mustParse(t, `{}`)
	_, err := canvasgraph.Apply(g, mustOps(t, `[{"op":"create_node","kind":"video","model":"veo","mode":"t2i"}]`), opts())
	if err == nil || !strings.Contains(err.Error(), "视频节点") {
		t.Fatalf("没有模型清单时也该检查种类: %v", err)
	}
}

const modeGraph = `{"nodes":[
 {"id":"a","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"已选图生","model":"veo","params":{"op":"i2v"}}},
 {"id":"b","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"没选方式","model":"veo"}},
 {"id":"c","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"自动图","model":"flux"}},
 {"id":"d","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"没模型"}},
 {"id":"e","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"script","label":"剧本"}},
 {"id":"f","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"存了失效的方式","model":"kling","params":{"op":"omni"}}}
],"edges":[]}`

func TestDetail_ShowsMode(t *testing.T) {
	g := mustParse(t, modeGraph)
	res, err := canvasgraph.Detail(g, []string{"a", "b", "c", "d", "e", "f"})
	if err != nil {
		t.Fatal(err)
	}
	canvasgraph.AnnotateModes(res, testModelOps)
	byID := map[string]canvasgraph.DetailNode{}
	for _, n := range res.Nodes {
		byID[n.ID] = n
	}
	if m := byID["a"].Mode; m == nil || m.Op != "i2v" || !slices.Equal(m.Options, []string{"t2v", "i2v", "omni"}) || m.Auto {
		t.Errorf("a = %+v", m)
	}
	if m := byID["b"].Mode; m == nil || m.Op != "" || len(m.Options) != 3 {
		t.Errorf("没选方式的节点 op 应为空、仍给出可选项: %+v", m)
	}
	if m := byID["c"].Mode; m == nil || !m.Auto || len(m.Options) != 0 || m.Op != "" {
		t.Errorf("自动判断的图片模型: %+v", m)
	}
	if m := byID["f"].Mode; m == nil || m.Op != "" {
		t.Errorf("已保存的方式不被模型支持时按没选算: %+v", m)
	}
	for _, id := range []string{"d", "e"} {
		if byID[id].Mode != nil {
			t.Errorf("%s 没有模型或不是图片/视频节点，不该有 mode", id)
		}
	}
}

func TestPickOp(t *testing.T) {
	ops := []string{"t2v", "i2v", "omni"}
	cases := []struct {
		name     string
		ops      []string
		chosen   any
		hasImage bool
		want     string
	}{
		{"选了就用选的", ops, "omni", false, "omni"},
		{"选的不被支持：有图走图生", ops, "t2i", true, "i2v"},
		{"没选有图走图生", ops, nil, true, "i2v"},
		{"没选没图取第一个", ops, nil, false, "t2v"},
		{"图片自动：有图图生图", []string{"t2i", "i2i"}, "t2i", true, "i2i"},
		{"图片自动：无图文生图", []string{"t2i", "i2i"}, "i2i", false, "t2i"},
		{"没有方式的模型", nil, "t2v", true, ""},
	}
	for _, c := range cases {
		if got := canvasgraph.PickOp(c.ops, c.chosen, c.hasImage); got != c.want {
			t.Errorf("%s: PickOp = %q，想要 %q", c.name, got, c.want)
		}
	}
}

func warnGraph(op string, upstream ...string) string {
	nodes := `{"id":"v","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"镜头","model":"veo"` + op + `}}`
	edges := ""
	for i, k := range upstream {
		id := "u" + string(rune('0'+i))
		nodes += `,{"id":"` + id + `","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"` + k + `","label":"素材` + id + `"}}`
		if edges != "" {
			edges += ","
		}
		edges += `{"id":"e` + id + `","source":"` + id + `","target":"v"}`
	}
	return `{"nodes":[` + nodes + `],"edges":[` + edges + `]}`
}

func TestModeWarnings(t *testing.T) {
	t2v := `,"params":{"op":"t2v"}`
	i2v := `,"params":{"op":"i2v"}`
	omni := `,"params":{"op":"omni"}`
	cases := []struct {
		name     string
		graph    string
		wantSub  string // 为空表示不该有警告
		wantNone bool
	}{
		{"文生视频却连了参考图", warnGraph(t2v, "image"), "文生视频", false},
		{"没选方式、只连了视频参考：落到文生视频", warnGraph("", "video"), "文生视频", false},
		{"图生视频却连了视频", warnGraph(i2v, "image", "video"), "只引用图片", false},
		{"图生视频却连了音频", warnGraph(i2v, "audio"), "只引用图片", false},
		{"全能参考连什么都行", warnGraph(omni, "image", "video", "audio"), "", true},
		{"文生视频没连参考", warnGraph(t2v), "", true},
		{"没选方式连了图：生成时按图生走", warnGraph("", "image"), "", true},
		{"图生视频只连图", warnGraph(i2v, "image"), "", true},
		{"图生视频还没连参考：上游可能稍后再连，不警告", warnGraph(i2v), "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := mustParse(t, c.graph)
			got := canvasgraph.ModeWarnings(g, []string{"v"}, testModelOps)
			if c.wantNone {
				if len(got) != 0 {
					t.Fatalf("不该有警告: %v", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0], c.wantSub) || !strings.Contains(got[0], "镜头") {
				t.Fatalf("警告 = %v，想要一条含 %q 和节点标题", got, c.wantSub)
			}
		})
	}
}

func TestModeWarnings_OnlyCheckedNodes_AndKnownModels(t *testing.T) {
	g := mustParse(t, warnGraph(`,"params":{"op":"t2v"}`, "image"))
	if got := canvasgraph.ModeWarnings(g, []string{"u0"}, testModelOps); len(got) != 0 {
		t.Errorf("只检查传入的节点: %v", got)
	}
	none := func(string) ([]string, bool) { return nil, false }
	if got := canvasgraph.ModeWarnings(g, []string{"v"}, none); len(got) != 0 {
		t.Errorf("模型未知时不警告: %v", got)
	}
	if got := canvasgraph.ModeWarnings(g, []string{"v", "v", "zzz"}, testModelOps); len(got) != 1 {
		t.Errorf("重复和不存在的 id 要忽略: %v", got)
	}
}

func TestModeWarnings_OffersOnlySupportedOptions(t *testing.T) {
	// kling 只支持文生视频：连了参考也没有别的方式可换，警告里不该建议不存在的方式
	g := mustParse(t, `{"nodes":[
	  {"id":"v","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"镜头","model":"kling"}},
	  {"id":"u","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"图"}}
	],"edges":[{"id":"e","source":"u","target":"v"}]}`)
	got := canvasgraph.ModeWarnings(g, []string{"v"}, testModelOps)
	if len(got) != 1 || strings.Contains(got[0], "omni") || strings.Contains(got[0], "i2v") {
		t.Fatalf("警告 = %v，不该建议该模型不支持的方式", got)
	}
}
