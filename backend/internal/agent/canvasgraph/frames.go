package canvasgraph

import (
	"fmt"
	"strings"
)

// MaxFrames 是一次最多往画布上放的帧图数。
const MaxFrames = 6

// 帧图摆放：在视频节点右侧、同一列向下依次避让，与前端 placeBeside 的规则一致。
const (
	frameGapX     = 80.0 // 帧图与来源节点的横向间距
	frameGapY     = 40.0 // 帧图之间、帧图与其他节点之间的最小纵向间距
	frameMaxTries = 60   // 避让最多下移几次
)

// VideoSource 是一个可以「看」或截帧的视频节点：节点标题和它当前产物的素材 id。
type VideoSource struct {
	NodeID  string
	Label   string
	AssetID string // 素材 id（十进制字符串）
}

// VideoRef 取一个视频节点当前的视频素材。节点必须存在、是视频节点并且已经有产物，
// 否则返回 ErrInvalid 并写明是哪个节点、为什么，让模型改正。
func VideoRef(g *Graph, id string) (VideoSource, error) {
	n := g.Node(id)
	switch {
	case n == nil:
		return VideoSource{}, fmt.Errorf("%w：节点 %q 不存在", ErrInvalid, id)
	case n.IsGroup() || n.Kind() != KindVideo:
		return VideoSource{}, fmt.Errorf("%w：节点 %q（%s）不是视频节点，只能从视频节点取画面", ErrInvalid, id, n.Label())
	}
	asset := str(n.Data()["assetId"])
	if asset == "" {
		return VideoSource{}, fmt.Errorf("%w：节点 %q（%s）还没有视频内容", ErrInvalid, id, n.Label())
	}
	return VideoSource{NodeID: id, Label: n.Label(), AssetID: asset}, nil
}

// FrameSpec 描述一张要放上画布的帧图：素材已经存好，这里只管节点和连线。
type FrameSpec struct {
	Label     string   // 节点标题
	AssetID   string   // 帧图素材 id（十进制字符串）
	URL       string   // 素材的稳定访问地址
	FileName  string   // 文件名，可空
	Width     int      // 像素，可为 0（不写 mediaAspect）
	Height    int      // 像素，可为 0
	ConnectTo []string // 作为参考连到这些节点（帧图 → 目标）；空表示只放在画布上不连线
}

// AddFrameNodes 在视频节点右侧新建上传型图片节点，并连上来源线（视频 → 帧图）。
// 只有写了 ConnectTo 的帧才再连到对应节点作参考。
//
// 来源线是 video → image，不符合生成依赖规则（视频的下游只能是视频），所以不能走 connect 操作，
// 只能由这个受信任的函数建；帧图是 uploaded 节点，不出生成面板，这根线不会被它自己当成参考素材。
// 入参 g 不会被修改，结果在 Result.Graph 里；任何一处不合法都整批不生效。
// Result.IDMap 里 "frame0"、"frame1"…… 对应各帧新节点的 id。
func AddFrameNodes(g *Graph, videoID string, frames []FrameSpec, opt Options) (*Result, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("%w：至少需要 1 帧", ErrInvalid)
	}
	if len(frames) > MaxFrames {
		return nil, fmt.Errorf("%w：一次最多放 %d 帧，传了 %d 帧", ErrInvalid, MaxFrames, len(frames))
	}
	work := g.Clone()
	if _, err := VideoRef(work, videoID); err != nil {
		return nil, err
	}
	video := work.Node(videoID)
	idMap := make(map[string]string, len(frames))
	for i, f := range frames {
		n := frameNode(opt.newID("n_"), f)
		n.SetPosition(work.placeBeside(video))
		work.Nodes = append(work.Nodes, n)
		idMap[fmt.Sprintf("frame%d", i)] = n.ID()
		work.Edges = append(work.Edges, Edge{"id": opt.newID("e_"), "source": video.ID(), "target": n.ID()})
		for _, to := range f.ConnectTo {
			if err := work.linkFrame(n, to, opt); err != nil {
				return nil, err
			}
		}
	}
	return finish(g, work, idMap), nil
}

// frameNode 构造一个已有素材的上传型图片节点，字段与前端从素材创建节点时一致。
func frameNode(id string, f FrameSpec) Node {
	label := strings.Join(strings.Fields(f.Label), " ")
	if r := []rune(label); len(r) > maxLabelRunes {
		label = string(r[:maxLabelRunes])
	}
	if label == "" {
		label = "视频帧"
	}
	data := uploadedImageData(AssetRef{ID: f.AssetID, URL: f.URL, FileName: f.FileName, Width: f.Width, Height: f.Height})
	data["label"] = label
	return Node{"id": id, "type": "canvas", "data": data}
}

// uploadedImageData 是一个已有素材的上传型图片节点的数据（已完成、带素材地址和 id），不含标题。
func uploadedImageData(r AssetRef) map[string]any {
	data := map[string]any{
		"kind": KindImage, "status": "done",
		"src": r.URL, "assetId": r.ID, "mediaType": "image", "uploaded": true,
	}
	if r.FileName != "" {
		data["fileName"] = r.FileName
	}
	if r.Width > 0 && r.Height > 0 {
		data["mediaAspect"] = float64(r.Width) / float64(r.Height)
	}
	return data
}

// linkFrame 把帧图连到目标节点作参考。目标要存在、不是组，并且图片能连到它（图片的下游是图片和视频）。
func (g *Graph) linkFrame(frame Node, target string, opt Options) error {
	dst := g.Node(target)
	switch {
	case dst == nil:
		return fmt.Errorf("%w：连线目标 %q 不存在", ErrInvalid, target)
	case dst.IsGroup():
		return fmt.Errorf("%w：组不能连线（%q）", ErrInvalid, target)
	case !canLink(KindImage, dst.Kind()):
		return fmt.Errorf("%w：图片节点不能连到 %s 节点（%q），图片的下游只能是 %s",
			ErrInvalid, dst.Kind(), target, strings.Join(downstream[KindImage], "、"))
	case g.hasEdge(frame.ID(), dst.ID()):
		return nil
	}
	g.Edges = append(g.Edges, Edge{"id": opt.newID("e_"), "source": frame.ID(), "target": dst.ID()})
	return nil
}

// placeBeside 返回新的普通节点放在 src 右侧的位置：垂直居中，与现有节点重叠就整体下移，直到不重叠。
func (g *Graph) placeBeside(src Node) Point {
	sr := g.absRect(src)
	w, h := nodeSize(Node{"type": "canvas"})
	p := Point{X: sr.x + sr.w + frameGapX, Y: sr.y + (sr.h-h)/2}
	for try := 0; try < frameMaxTries && g.collides(rect{p.X, p.Y, w, h}); try++ {
		p.Y += h + frameGapY
	}
	return p
}

// collides 判断矩形（含 frameGapY 的留白）是否压住了画布上的任何节点。
func (g *Graph) collides(r rect) bool {
	for _, n := range g.Nodes {
		o := g.absRect(n)
		if r.x-frameGapY < o.x+o.w && r.x+r.w+frameGapY > o.x && r.y-frameGapY < o.y+o.h && r.y+r.h+frameGapY > o.y {
			return true
		}
	}
	return false
}
