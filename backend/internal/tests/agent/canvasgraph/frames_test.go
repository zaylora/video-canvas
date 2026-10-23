package canvasgraph_test

import (
	"errors"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// framesPayload：镜头 1（有视频产物）、镜头 2（空视频节点，远在右边）、一个文本节点、一个还没产物的视频。
const framesPayload = `{
  "nodes": [
    {"id":"n_v1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"镜头 1","status":"done","assetId":"11"}},
    {"id":"n_v2","type":"canvas","position":{"x":3000,"y":0},"data":{"kind":"video","label":"镜头 2"}},
    {"id":"n_txt","type":"canvas","position":{"x":0,"y":900},"data":{"kind":"script","label":"剧本"}},
    {"id":"n_empty","type":"canvas","position":{"x":0,"y":1800},"data":{"kind":"video","label":"没生成的"}}
  ],
  "edges": []
}`

func frame(label string, connect ...string) canvasgraph.FrameSpec {
	return canvasgraph.FrameSpec{
		Label: label, AssetID: "900", URL: "/files/frame.png", FileName: "frame.png",
		Width: 1280, Height: 720, ConnectTo: connect,
	}
}

func TestVideoRef(t *testing.T) {
	g := mustParse(t, framesPayload)
	ref, err := canvasgraph.VideoRef(g, "n_v1")
	if err != nil {
		t.Fatalf("VideoRef: %v", err)
	}
	if ref.NodeID != "n_v1" || ref.Label != "镜头 1" || ref.AssetID != "11" {
		t.Fatalf("ref = %+v", ref)
	}
	for _, c := range []struct{ id, want string }{
		{"n_nope", "不存在"},
		{"n_txt", "不是视频节点"},
		{"n_empty", "还没有视频内容"},
	} {
		_, err := canvasgraph.VideoRef(g, c.id)
		if !errors.Is(err, canvasgraph.ErrInvalid) || !contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v，想要 ErrInvalid 且含 %q", c.id, err, c.want)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestAddFrameNodes_BuildsUploadedImageNodes(t *testing.T) {
	g := mustParse(t, framesPayload)
	res, err := canvasgraph.AddFrameNodes(g, "n_v1", []canvasgraph.FrameSpec{frame("镜头 1 · 尾帧")}, opts())
	if err != nil {
		t.Fatalf("AddFrameNodes: %v", err)
	}
	id := res.IDMap["frame0"]
	if id == "" {
		t.Fatalf("IDMap = %v，应含 frame0", res.IDMap)
	}
	n := node(t, res.Graph, id)
	d := n.Data()
	want := map[string]any{
		"kind": "image", "label": "镜头 1 · 尾帧", "status": "done", "src": "/files/frame.png",
		"assetId": "900", "mediaType": "image", "uploaded": true, "fileName": "frame.png",
	}
	for k, v := range want {
		if d[k] != v {
			t.Errorf("data.%s = %v，想要 %v", k, d[k], v)
		}
	}
	if a, _ := d["mediaAspect"].(float64); a < 1.77 || a > 1.78 {
		t.Errorf("mediaAspect = %v，想要 1280/720", d["mediaAspect"])
	}
	// 来源线：视频 → 帧图，不带 connectTo 就只有这一根线
	if len(res.Graph.Edges) != 1 || res.Graph.Edges[0].Source() != "n_v1" || res.Graph.Edges[0].Target() != id {
		t.Fatalf("edges = %v", res.Graph.Edges)
	}
	// 改动差异：1 个节点 + 1 根线
	var nodes, edges int
	for _, c := range res.Changes {
		if c.Op != canvasgraph.ChangeCreate {
			t.Errorf("意外的改动 %+v", c)
		}
		if c.Kind == "node" {
			nodes++
		} else {
			edges++
		}
	}
	if nodes != 1 || edges != 1 {
		t.Errorf("改动 = %d 个节点 %d 根线，想要 1/1", nodes, edges)
	}
}

func TestAddFrameNodes_ConnectOnlyWhenAsked(t *testing.T) {
	g := mustParse(t, framesPayload)
	res, err := canvasgraph.AddFrameNodes(g, "n_v1", []canvasgraph.FrameSpec{
		frame("参考帧", "n_v2"), // 作为下一镜参考：连线
		frame("备用帧"),         // 不连
	}, opts())
	if err != nil {
		t.Fatalf("AddFrameNodes: %v", err)
	}
	f0, f1 := res.IDMap["frame0"], res.IDMap["frame1"]
	has := func(src, dst string) bool {
		for _, e := range res.Graph.Edges {
			if e.Source() == src && e.Target() == dst {
				return true
			}
		}
		return false
	}
	if !has("n_v1", f0) || !has("n_v1", f1) {
		t.Error("每个帧图都应有来自视频的来源线")
	}
	if !has(f0, "n_v2") {
		t.Error("写了 connectTo 的帧应连到下一镜")
	}
	if has(f1, "n_v2") {
		t.Error("没写 connectTo 的帧不该连到下一镜")
	}
	if len(res.Graph.Edges) != 3 {
		t.Errorf("连线数 = %d，想要 3", len(res.Graph.Edges))
	}
}

func TestAddFrameNodes_DuplicateConnectIsOneEdge(t *testing.T) {
	g := mustParse(t, framesPayload)
	res, err := canvasgraph.AddFrameNodes(g, "n_v1", []canvasgraph.FrameSpec{frame("帧", "n_v2", "n_v2")}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Graph.Edges) != 2 {
		t.Fatalf("连线数 = %d，想要 2（来源线 + 一根连到下一镜）", len(res.Graph.Edges))
	}
}

func TestAddFrameNodes_Errors(t *testing.T) {
	cases := []struct {
		name   string
		video  string
		frames []canvasgraph.FrameSpec
		want   string
	}{
		{"没有帧", "n_v1", nil, "至少"},
		{"超过 6 帧", "n_v1", []canvasgraph.FrameSpec{frame("1"), frame("2"), frame("3"), frame("4"), frame("5"), frame("6"), frame("7")}, "最多"},
		{"视频节点不存在", "n_nope", []canvasgraph.FrameSpec{frame("x")}, "不存在"},
		{"不是视频节点", "n_txt", []canvasgraph.FrameSpec{frame("x")}, "不是视频节点"},
		{"连线目标不存在", "n_v1", []canvasgraph.FrameSpec{frame("x", "n_nope")}, "不存在"},
		{"图片不能连到文本", "n_v1", []canvasgraph.FrameSpec{frame("x", "n_txt")}, "不能连到"},
		{"目标是自己的视频也不能连回", "n_v1", []canvasgraph.FrameSpec{frame("x", "n_v1")}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := mustParse(t, framesPayload)
			before := marshal(t, g)
			_, err := canvasgraph.AddFrameNodes(g, c.video, c.frames, opts())
			if c.name == "目标是自己的视频也不能连回" {
				// 视频节点可以作为帧图的下游（image → video 合法），这是允许的，不是错误
				if err != nil {
					t.Fatalf("image → video 合法，不该报错: %v", err)
				}
				return
			}
			if !errors.Is(err, canvasgraph.ErrInvalid) || !contains(err.Error(), c.want) {
				t.Fatalf("err = %v，想要 ErrInvalid 且含 %q", err, c.want)
			}
			if marshal(t, g) != before {
				t.Error("失败时入参不该被修改")
			}
		})
	}
}

func TestAddFrameNodes_DoesNotMutateInput(t *testing.T) {
	g := mustParse(t, framesPayload)
	before := marshal(t, g)
	if _, err := canvasgraph.AddFrameNodes(g, "n_v1", []canvasgraph.FrameSpec{frame("帧", "n_v2")}, opts()); err != nil {
		t.Fatal(err)
	}
	if marshal(t, g) != before {
		t.Error("成功时入参也不该被修改，结果在 Result.Graph 里")
	}
}

func TestAddFrameNodes_PlacedRightOfVideoThenStackedDown(t *testing.T) {
	g := mustParse(t, framesPayload)
	res, err := canvasgraph.AddFrameNodes(g, "n_v1", []canvasgraph.FrameSpec{frame("a"), frame("b"), frame("c")}, opts())
	if err != nil {
		t.Fatal(err)
	}
	// 普通节点 576×324：视频在 (0,0)，帧图在其右侧 80 处；同列向下依次避让（高 324 + 间距 40）
	want := []canvasgraph.Point{{X: 656, Y: 0}, {X: 656, Y: 364}, {X: 656, Y: 728}}
	for i, w := range want {
		got := node(t, res.Graph, res.IDMap["frame"+string(rune('0'+i))]).Position()
		if got != w {
			t.Errorf("第 %d 帧位置 = %+v，想要 %+v", i, got, w)
		}
	}
}

func TestAddFrameNodes_AvoidsExistingNodes(t *testing.T) {
	// 镜头 2 紧贴在视频右边（x=700），帧图默认位置 (656,0) 会压住它，要往下挪到不重叠
	g := mustParse(t, `{"nodes":[
	  {"id":"n_v1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"镜头 1","assetId":"11"}},
	  {"id":"n_v2","type":"canvas","position":{"x":700,"y":0},"data":{"kind":"video","label":"镜头 2"}}
	],"edges":[]}`)
	res, err := canvasgraph.AddFrameNodes(g, "n_v1", []canvasgraph.FrameSpec{frame("a")}, opts())
	if err != nil {
		t.Fatal(err)
	}
	p := node(t, res.Graph, res.IDMap["frame0"]).Position()
	// 镜头 2 占 y∈[0,324]，加 40 预留，帧图 y 至少是 364
	if p.X != 656 || p.Y < 364 {
		t.Errorf("位置 = %+v，应避开镜头 2（y ≥ 364）", p)
	}
}
