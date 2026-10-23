package agent_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/videoframe"
	. "video-canvas/internal/service/agent"
)

// fakeFrames 是假的抽帧器：不跑 ffmpeg，按调用返回可预期的字节，并记下收到的参数。
type fakeFrames struct {
	unavailable bool
	dur         float64
	err         error
	sampleN     int
	sampleFmt   videoframe.Format
	frameAts    []float64
	srcs        []string
}

func (f *fakeFrames) Available() bool { return !f.unavailable }

func (f *fakeFrames) Duration(_ context.Context, src string) (float64, error) {
	f.srcs = append(f.srcs, src)
	return f.dur, f.err
}

func (f *fakeFrames) Sample(_ context.Context, src string, n int, o videoframe.FrameOpts) ([]videoframe.Frame, float64, error) {
	f.srcs = append(f.srcs, src)
	f.sampleN, f.sampleFmt = n, o.Format
	if f.err != nil {
		return nil, 0, f.err
	}
	out := make([]videoframe.Frame, n)
	for i := range out {
		out[i] = videoframe.Frame{At: float64(i), Data: []byte{0xff, 0xd8, byte(i)}}
	}
	return out, f.dur, nil
}

func (f *fakeFrames) Frame(_ context.Context, src string, at float64, o videoframe.FrameOpts) ([]byte, error) {
	f.srcs = append(f.srcs, src)
	if f.err != nil {
		return nil, f.err
	}
	f.frameAts = append(f.frameAts, at)
	return []byte(fmt.Sprintf("PNG@%.2f/%s", at, o.Format)), nil
}

// fakeFrameStore 是假的帧图存储：记下存了什么，返回递增的素材 id。
type fakeFrameStore struct {
	saved []string
	err   error
}

func (s *fakeFrameStore) SaveFrame(_ context.Context, _ uint64, fileName string, data []byte) (*model.AssetView, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.saved = append(s.saved, fileName+"|"+string(data))
	id := uint64(500 + len(s.saved))
	return &model.AssetView{ID: id, Kind: "image", URL: fmt.Sprintf("/files/f%d.png", id), Width: 1280, Height: 720, FileName: fileName}, nil
}

const videoCanvas = `{"nodes":[
 {"id":"v1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"镜头 1","assetId":"100","src":"/f/100"}},
 {"id":"v2","type":"canvas","position":{"x":3000,"y":0},"data":{"kind":"video","label":"镜头 2"}},
 {"id":"vbad","type":"canvas","position":{"x":0,"y":900},"data":{"kind":"video","label":"坏素材","assetId":"101"}},
 {"id":"vgone","type":"canvas","position":{"x":0,"y":1800},"data":{"kind":"video","label":"没了","assetId":"999"}},
 {"id":"vempty","type":"canvas","position":{"x":0,"y":2700},"data":{"kind":"video","label":"还没生成"}},
 {"id":"txt","type":"canvas","position":{"x":0,"y":3600},"data":{"kind":"script","label":"剧本"}}
],"edges":[]}`

type videoEnv struct {
	*bridgeEnv
	frames *fakeFrames
	store  *fakeFrameStore
	tok    string
}

func newVideoEnv(t *testing.T, mode string, vision bool, remote ...bool) *videoEnv {
	t.Helper()
	b := newBridgeEnv(t)
	b.repo.canvas.PayloadJSON = []byte(videoCanvas)
	b.reg.snap.Model.Capabilities.Vision = vision
	assets := &fakeAssets{owner: 1, remote: len(remote) > 0 && remote[0], files: map[uint64]*model.Asset{
		100: {ID: 100, Kind: "video", MimeType: "video/mp4", ByteSize: 5},
		101: {ID: 101, Kind: "image", MimeType: "image/png", ByteSize: 3}, // 节点说是视频，素材其实是图片
	}, data: map[uint64][]byte{100: []byte("VIDEO"), 101: []byte("PNG")}}
	frames := &fakeFrames{dur: 10}
	store := &fakeFrameStore{}
	b.br = NewAgentBridge(BridgeDeps{
		Repo: b.repo, Canvas: NewAgentCanvasService(b.repo, b.bc), Agent: b.svc, Billing: NewAgentBilling(b.bill),
		Registry: b.reg, Secrets: fakeSecrets{"channel:ch1": "sk-test"}, Streamer: b.str, Assets: assets,
		Frames: frames, FrameStore: store,
	})
	tok, _ := b.token(t, mode)
	return &videoEnv{bridgeEnv: b, frames: frames, store: store, tok: tok}
}

func (e *videoEnv) graph(t *testing.T) *canvasgraph.Graph {
	t.Helper()
	g, err := canvasgraph.Parse(e.repo.canvas.PayloadJSON)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestAgentBridge_InspectVideo(t *testing.T) {
	t.Run("默认抽 6 帧，按顺序交回 JPEG 图片，文字里写明时长和每帧时间", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModePrompt, true)
		res := e.tool(t, e.tok, "canvas_inspect_video", map[string]any{"nodeId": "v1"})
		if res.IsError || len(res.Images) != 6 {
			t.Fatalf("res=%+v", res)
		}
		if e.frames.sampleN != 6 || e.frames.sampleFmt != videoframe.JPEG {
			t.Errorf("应抽 6 帧 JPEG: n=%d fmt=%s", e.frames.sampleN, e.frames.sampleFmt)
		}
		if res.Images[0].MimeType != "image/jpeg" || res.Images[0].Data == "" {
			t.Errorf("图片 = %+v", res.Images[0])
		}
		for _, want := range []string{"镜头 1", "10.0 秒", "第 1 帧"} {
			if !strings.Contains(res.Content, want) {
				t.Errorf("文字里应含 %q: %s", want, res.Content)
			}
		}
		if !hasTool(AgentToolsForMode("prompt"), "canvas_inspect_video") {
			t.Error("看视频是只读工具，各模式都能用")
		}
	})

	t.Run("count 由模型决定，范围 1 到 6", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModeAll, true)
		res := e.tool(t, e.tok, "canvas_inspect_video", map[string]any{"nodeId": "v1", "count": 3})
		if res.IsError || len(res.Images) != 3 || e.frames.sampleN != 3 {
			t.Fatalf("res=%+v n=%d", res, e.frames.sampleN)
		}
		for _, n := range []int{0, 7, -1} {
			bad := e.tool(t, e.tok, "canvas_inspect_video", map[string]any{"nodeId": "v1", "count": n})
			if !bad.IsError || !strings.Contains(bad.Content, "1 到 6") {
				t.Errorf("count=%d 应报范围错误: %+v", n, bad)
			}
		}
	})

	t.Run("素材在对象存储里：把地址交给 ffmpeg，不下载内容", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModeAll, true, true)
		res := e.tool(t, e.tok, "canvas_inspect_video", map[string]any{"nodeId": "v1"})
		if res.IsError {
			t.Fatalf("res=%+v", res)
		}
		if len(e.frames.srcs) == 0 || e.frames.srcs[0] != "https://cdn.example.com/100?sig=x" {
			t.Errorf("应直接传签名地址: %v", e.frames.srcs)
		}
	})

	t.Run("素材在本地磁盘：落成临时文件交给 ffmpeg", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModeAll, true)
		e.tool(t, e.tok, "canvas_inspect_video", map[string]any{"nodeId": "v1"})
		if len(e.frames.srcs) == 0 || strings.HasPrefix(e.frames.srcs[0], "http") || e.frames.srcs[0] == "" {
			t.Errorf("应是本地临时文件路径: %v", e.frames.srcs)
		}
	})

	t.Run("模型不能看图：不下发这个工具，直接调用也被拒绝", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModeAll, false)
		if hasTool(AgentToolsFor(model.AgentModeAll, false), "canvas_inspect_video") {
			t.Error("不能看图的模型不该拿到看视频工具")
		}
		res := e.tool(t, e.tok, "canvas_inspect_video", map[string]any{"nodeId": "v1"})
		if !res.IsError || !strings.Contains(res.Content, "不能看图") {
			t.Errorf("res=%+v", res)
		}
	})

	t.Run("节点和素材的各种问题都给出人话原因，不返回图片", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModeAll, true)
		for _, c := range []struct{ id, want string }{
			{"nope", "不存在"},
			{"txt", "不是视频节点"},
			{"vempty", "还没有视频内容"},
			{"vgone", "不存在或已删除"},
			{"vbad", "不是视频"},
		} {
			res := e.tool(t, e.tok, "canvas_inspect_video", map[string]any{"nodeId": c.id})
			if !res.IsError || !strings.Contains(res.Content, c.want) || len(res.Images) != 0 {
				t.Errorf("%s: want %q, got %+v", c.id, c.want, res)
			}
		}
	})

	t.Run("环境没有 ffmpeg：说明原因并引导手动截取帧", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModeAll, true)
		e.frames.unavailable = true
		res := e.tool(t, e.tok, "canvas_inspect_video", map[string]any{"nodeId": "v1"})
		if !res.IsError || !strings.Contains(res.Content, "不能处理视频") || !strings.Contains(res.Content, "截取帧") {
			t.Errorf("res=%+v", res)
		}
	})

	t.Run("抽帧超时和解码失败：说明原因", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModeAll, true)
		e.frames.err = videoframe.ErrTimeout
		res := e.tool(t, e.tok, "canvas_inspect_video", map[string]any{"nodeId": "v1"})
		if !res.IsError || !strings.Contains(res.Content, "超时") {
			t.Errorf("超时: %+v", res)
		}
		e.frames.err = fmt.Errorf("%w: moov atom not found", videoframe.ErrDecode)
		res = e.tool(t, e.tok, "canvas_inspect_video", map[string]any{"nodeId": "v1"})
		if !res.IsError || !strings.Contains(res.Content, "无法解码") || strings.Contains(res.Content, "moov") {
			t.Errorf("解码失败不该把 ffmpeg 原始输出透给模型: %+v", res)
		}
	})
}

func TestAgentBridge_CaptureFrame(t *testing.T) {
	type frameArg = map[string]any

	t.Run("截尾帧并连到下一镜：存素材、建图片节点、连来源线和参考线", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModeAll, true)
		res := e.tool(t, e.tok, "canvas_capture_frame", map[string]any{
			"nodeId": "v1",
			"frames": []frameArg{{"at": "last", "connectTo": []string{"v2"}}},
		})
		if res.IsError {
			t.Fatalf("res=%+v", res)
		}
		if len(e.store.saved) != 1 || !strings.Contains(e.store.saved[0], "PNG@9.90") {
			t.Fatalf("应存一张尾帧（10 秒视频的 9.9 秒处）: %v", e.store.saved)
		}
		g := e.graph(t)
		var frameID string
		for _, n := range g.Nodes {
			if n.Kind() == "image" {
				frameID = n.ID()
				d := n.Data()
				if d["uploaded"] != true || d["assetId"] != "501" || d["src"] != "/files/f501.png" || !strings.Contains(n.Label(), "镜头 1") {
					t.Errorf("帧图节点 = %+v", d)
				}
			}
		}
		if frameID == "" {
			t.Fatal("画布上应出现帧图节点")
		}
		var fromVideo, toNext bool
		for _, ed := range g.Edges {
			fromVideo = fromVideo || (ed.Source() == "v1" && ed.Target() == frameID)
			toNext = toNext || (ed.Source() == frameID && ed.Target() == "v2")
		}
		if !fromVideo || !toNext {
			t.Errorf("应有来源线 v1→帧图 和参考线 帧图→v2: %v", g.Edges)
		}
		var out struct {
			OK     bool `json:"ok"`
			Frames []struct {
				NodeID      string   `json:"node_id"`
				At          float64  `json:"at"`
				ConnectedTo []string `json:"connected_to"`
			} `json:"frames"`
		}
		if err := json.Unmarshal([]byte(res.Content), &out); err != nil || !out.OK || len(out.Frames) != 1 || out.Frames[0].NodeID != frameID || out.Frames[0].ConnectedTo[0] != "v2" {
			t.Errorf("结果 JSON = %s (%v)", res.Content, err)
		}
	})

	t.Run("不写 connectTo 的帧只放在画布上，不连到下一镜", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModeAll, true)
		res := e.tool(t, e.tok, "canvas_capture_frame", map[string]any{
			"nodeId": "v1", "frames": []frameArg{{"at": "first"}, {"at": 4.5}, {"at": "last", "connectTo": []string{"v2"}}},
		})
		if res.IsError {
			t.Fatalf("res=%+v", res)
		}
		g := e.graph(t)
		toNext := 0
		for _, ed := range g.Edges {
			if ed.Target() == "v2" {
				toNext++
			}
		}
		if toNext != 1 || len(e.store.saved) != 3 || len(g.Edges) != 4 {
			t.Errorf("只有尾帧连到下一镜: toNext=%d saved=%d edges=%d", toNext, len(e.store.saved), len(g.Edges))
		}
		if !strings.Contains(strings.Join(e.store.saved, ","), "PNG@0.00") || !strings.Contains(strings.Join(e.store.saved, ","), "PNG@4.50") {
			t.Errorf("时间点没按要求取: %v", e.store.saved)
		}
	})

	t.Run("同一时间点重复请求只截一次，连线目标合并", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModeAll, true)
		res := e.tool(t, e.tok, "canvas_capture_frame", map[string]any{
			"nodeId": "v1", "frames": []frameArg{{"at": 2}, {"at": 2.0, "connectTo": []string{"v2"}}},
		})
		if res.IsError || len(e.store.saved) != 1 {
			t.Fatalf("res=%+v saved=%v", res, e.store.saved)
		}
		g := e.graph(t)
		linked := false
		for _, ed := range g.Edges {
			linked = linked || ed.Target() == "v2"
		}
		if !linked {
			t.Error("合并后应保留 connectTo")
		}
	})

	t.Run("参数不合法：整批不执行，也不会存素材", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModeAll, true)
		seven := make([]frameArg, 7)
		for i := range seven {
			seven[i] = frameArg{"at": i}
		}
		for _, c := range []struct {
			name   string
			args   map[string]any
			want   string
			errsub string
		}{
			{"没有帧", map[string]any{"nodeId": "v1", "frames": []frameArg{}}, "1 到 6", ""},
			{"超过 6 帧", map[string]any{"nodeId": "v1", "frames": seven}, "最多", ""},
			{"at 不认识", map[string]any{"nodeId": "v1", "frames": []frameArg{{"at": "middle"}}}, "first", ""},
			{"at 为负", map[string]any{"nodeId": "v1", "frames": []frameArg{{"at": -1}}}, "不能为负", ""},
			{"at 超出时长", map[string]any{"nodeId": "v1", "frames": []frameArg{{"at": 11}}}, "10.0 秒", ""},
			{"连线目标不存在", map[string]any{"nodeId": "v1", "frames": []frameArg{{"at": 1, "connectTo": []string{"zzz"}}}}, "不存在", ""},
			{"图片不能连到文本", map[string]any{"nodeId": "v1", "frames": []frameArg{{"at": 1, "connectTo": []string{"txt"}}}}, "不能连到", ""},
			{"不是视频节点", map[string]any{"nodeId": "txt", "frames": []frameArg{{"at": 1}}}, "不是视频节点", ""},
		} {
			res := e.tool(t, e.tok, "canvas_capture_frame", c.args)
			if !res.IsError || !strings.Contains(res.Content, c.want) {
				t.Errorf("%s: want %q, got %+v", c.name, c.want, res)
			}
		}
		if len(e.store.saved) != 0 {
			t.Errorf("校验失败不该存任何素材: %v", e.store.saved)
		}
		if len(e.graph(t).Nodes) != 6 {
			t.Error("校验失败不该改画布")
		}
	})

	t.Run("只有能改画布的任务模式才有这个工具", func(t *testing.T) {
		if !hasTool(AgentToolsForMode(model.AgentModeAll), "canvas_capture_frame") || !hasTool(AgentToolsForMode(model.AgentModeStoryboard), "canvas_capture_frame") {
			t.Error("全能创作和分镜搭建应有")
		}
		for _, m := range []string{model.AgentModeScript, model.AgentModePrompt} {
			if hasTool(AgentToolsForMode(m), "canvas_capture_frame") {
				t.Errorf("%s 模式不该有", m)
			}
			e := newVideoEnv(t, m, true)
			res := e.tool(t, e.tok, "canvas_capture_frame", map[string]any{"nodeId": "v1", "frames": []frameArg{{"at": 1}}})
			if !res.IsError || len(e.store.saved) != 0 {
				t.Errorf("%s 模式直接调用应被拒绝: %+v", m, res)
			}
		}
	})

	t.Run("不看图的模型也能截帧", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModeAll, false)
		if !hasTool(AgentToolsFor(model.AgentModeAll, false), "canvas_capture_frame") {
			t.Error("截帧不需要模型能看图")
		}
		res := e.tool(t, e.tok, "canvas_capture_frame", map[string]any{"nodeId": "v1", "frames": []frameArg{{"at": "last"}}})
		if res.IsError {
			t.Errorf("res=%+v", res)
		}
	})

	t.Run("环境问题给出人话原因", func(t *testing.T) {
		e := newVideoEnv(t, model.AgentModeAll, true)
		e.frames.unavailable = true
		res := e.tool(t, e.tok, "canvas_capture_frame", map[string]any{"nodeId": "v1", "frames": []frameArg{{"at": 1}}})
		if !res.IsError || !strings.Contains(res.Content, "不能处理视频") {
			t.Errorf("没有 ffmpeg: %+v", res)
		}
		e.frames.unavailable = false
		e.frames.err = videoframe.ErrNoFrame
		res = e.tool(t, e.tok, "canvas_capture_frame", map[string]any{"nodeId": "v1", "frames": []frameArg{{"at": 1}}})
		if !res.IsError || !strings.Contains(res.Content, "没有画面") {
			t.Errorf("取不到帧: %+v", res)
		}
	})
}

// 用真实的 ffmpeg 走一遍：本地素材落临时文件 → 抽帧 → 存 PNG → 建节点。本机没有 ffmpeg 时跳过。
func TestAgentBridge_VideoWithRealFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("本机没有 ffmpeg，跳过")
	}
	path := filepath.Join(t.TempDir(), "in.mp4")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=d=3:s=320x240:r=10", "-pix_fmt", "yuv420p", "-y", path).CombinedOutput(); err != nil {
		t.Fatalf("合成测试视频失败: %v\n%s", err, out)
	}
	video, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b := newBridgeEnv(t)
	b.repo.canvas.PayloadJSON = []byte(videoCanvas)
	assets := &fakeAssets{owner: 1, files: map[uint64]*model.Asset{
		100: {ID: 100, Kind: "video", MimeType: "video/mp4", ByteSize: int64(len(video))},
	}, data: map[uint64][]byte{100: video}}
	store := &fakeFrameStore{}
	b.br = NewAgentBridge(BridgeDeps{
		Repo: b.repo, Canvas: NewAgentCanvasService(b.repo, b.bc), Agent: b.svc, Billing: NewAgentBilling(b.bill),
		Registry: b.reg, Secrets: fakeSecrets{"channel:ch1": "sk-test"}, Streamer: b.str, Assets: assets,
		Frames: videoframe.New(videoframe.Options{}), FrameStore: store,
	})
	tok, _ := b.token(t, model.AgentModeAll)

	res := b.tool(t, tok, "canvas_inspect_video", map[string]any{"nodeId": "v1", "count": 3})
	if res.IsError || len(res.Images) != 3 {
		t.Fatalf("看视频: %+v", res)
	}
	for i, img := range res.Images {
		raw, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := jpeg.Decode(bytes.NewReader(raw)); err != nil {
			t.Errorf("第 %d 帧不是合法 JPEG: %v", i+1, err)
		}
	}
	if !strings.Contains(res.Content, "3.0 秒") {
		t.Errorf("时长应约 3.0 秒: %s", res.Content)
	}

	res = b.tool(t, tok, "canvas_capture_frame", map[string]any{"nodeId": "v1", "frames": []map[string]any{{"at": "last", "connectTo": []string{"v2"}}}})
	if res.IsError || len(store.saved) != 1 {
		t.Fatalf("截帧: %+v saved=%d", res, len(store.saved))
	}
	_, data, _ := strings.Cut(store.saved[0], "|")
	cfg, err := png.DecodeConfig(strings.NewReader(data))
	if err != nil || cfg.Width != 320 || cfg.Height != 240 {
		t.Errorf("尾帧应是 320x240 的 PNG: %+v err=%v", cfg, err)
	}
}
