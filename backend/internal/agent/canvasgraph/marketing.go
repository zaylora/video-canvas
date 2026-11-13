package canvasgraph

import "fmt"

// MarketingMedia 是营销脚本发到画布时带上的一份已有素材。
type MarketingMedia struct {
	AssetID  string // 素材 id（十进制字符串）
	URL      string // 稳定访问地址
	FileName string
	Width    int
	Height   int
	Label    string // 节点标题，空则按种类给默认
}

// MarketingShot 是一个镜头：对应一个还没生成的视频节点。
type MarketingShot struct {
	Label  string
	Prompt string
}

// MarketingSpec 是「发到画布」的全部内容：素材节点、成片节点、每镜一个未生成的视频节点。
type MarketingSpec struct {
	Images   []MarketingMedia // 商品图与模特图（已完成的图片节点）
	RefVideo *MarketingMedia  // 参考视频（已完成的视频节点）
	Final    *MarketingMedia  // 该脚本最新成片（已完成的视频节点），没有为 nil
	Shots    []MarketingShot  // 每镜一个视频节点，提示词是该镜头单独格式化的稿
	Model    string           // 镜头视频节点选的视频模型 key，空则不写
	Params   map[string]any   // 镜头视频节点的生成参数（画幅等），不含提示词和素材
}

// 画布布局：素材一列、镜头一列、成片一列；间距按节点默认大小留出余量。
const (
	marketingColGap = 420
	marketingRowGap = 300
)

// BuildMarketing 在空画布上写入营销脚本的节点和连线：素材节点（商品图、模特图、参考视频）、成片节点、
// 每镜一个未生成的视频节点；所有素材节点连向每个镜头节点（图片和视频都能作视频节点的参考）。
// 只生成 payload，不碰数据库；不自动发起生成。
func BuildMarketing(spec MarketingSpec, opt Options) ([]byte, error) {
	if len(spec.Shots) == 0 {
		return nil, fmt.Errorf("%w：脚本没有镜头", ErrInvalid)
	}
	g, err := Parse(nil)
	if err != nil {
		return nil, err
	}
	var sources []string
	row := 0
	add := func(n Node, x, y float64) {
		n.SetPosition(Point{X: x, Y: y})
		g.Nodes = append(g.Nodes, n)
	}
	for i, m := range spec.Images {
		id := opt.newID("n_")
		data := uploadedImageData(AssetRef{ID: m.AssetID, URL: m.URL, FileName: m.FileName, Width: m.Width, Height: m.Height})
		data["label"] = mediaLabel(m.Label, fmt.Sprintf("图片 %d", i+1))
		add(Node{"id": id, "type": "canvas", "data": data}, 0, float64(row)*marketingRowGap)
		sources = append(sources, id)
		row++
	}
	if spec.RefVideo != nil {
		id := opt.newID("n_")
		add(Node{"id": id, "type": "canvas", "data": uploadedVideoData(*spec.RefVideo, "参考视频")}, 0, float64(row)*marketingRowGap)
		sources = append(sources, id)
	}
	for i, shot := range spec.Shots {
		id := opt.newID("n_")
		data := map[string]any{"kind": KindVideo, "label": mediaLabel(shot.Label, fmt.Sprintf("镜头 %d", i+1)), "status": "idle"}
		n := Node{"id": id, "type": "canvas", "data": data}
		setPrompt(n, shot.Prompt)
		params, _ := data["params"].(map[string]any)
		for k, v := range spec.Params {
			params[k] = v
		}
		params["op"] = "omni"
		if spec.Model != "" {
			data["model"] = spec.Model
		}
		add(n, marketingColGap, float64(i)*marketingRowGap)
		for _, src := range sources {
			g.Edges = append(g.Edges, Edge{"id": opt.newID("e_"), "source": src, "target": id})
		}
	}
	if spec.Final != nil {
		add(Node{"id": opt.newID("n_"), "type": "canvas", "data": uploadedVideoData(*spec.Final, "成片")}, 2*marketingColGap, 0)
	}
	return g.Marshal()
}

// mediaLabel 取节点标题，空则用默认值。
func mediaLabel(label, def string) string {
	if label == "" {
		return def
	}
	return label
}

// uploadedVideoData 是一个已有视频素材的上传型视频节点的数据（已完成、带素材地址和 id）。
func uploadedVideoData(m MarketingMedia, def string) map[string]any {
	data := map[string]any{
		"kind": KindVideo, "label": mediaLabel(m.Label, def), "status": "done",
		"src": m.URL, "assetId": m.AssetID, "mediaType": "video", "uploaded": true,
	}
	if m.FileName != "" {
		data["fileName"] = m.FileName
	}
	if m.Width > 0 && m.Height > 0 {
		data["mediaAspect"] = float64(m.Width) / float64(m.Height)
	}
	return data
}
