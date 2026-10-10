package canvasgraph

import "fmt"

// MaxDetailNodes 是一次读取节点详情的数量上限。
const MaxDetailNodes = 50

// CatalogOptions 控制目录的大小。
type CatalogOptions struct {
	Max         int      // 最多列多少个节点，0 表示 200
	PromptRunes int      // 提示词摘要的字数，0 表示 60
	Priority    []string // 优先列出的节点 id（选中的、被 @ 的），按给定顺序排在最前
}

// CatalogNode 是目录里的一行：只够模型认出「是哪个」，不含大段内容和媒体地址。
type CatalogNode struct {
	ID        string `json:"id"`                // 节点 id
	Type      string `json:"type"`              // canvas 或 group
	Kind      string `json:"kind,omitempty"`    // script / image / video / audio，组没有
	Label     string `json:"label"`             // 标题
	Group     string `json:"group,omitempty"`   // 所属组 id
	Members   int    `json:"members,omitempty"` // 组的成员数
	Status    string `json:"status,omitempty"`  // idle / running / done / error
	HasOutput bool   `json:"hasOutput"`         // 是否已有生成产物或素材
	Prompt    string `json:"prompt,omitempty"`  // 提示词摘要
}

// Catalog 是整张画布的目录。
type Catalog struct {
	Nodes     []CatalogNode `json:"nodes"`     // 列出的节点
	Total     int           `json:"total"`     // 画布上的节点总数
	Omitted   int           `json:"omitted"`   // 因为数量上限没有列出的节点数
	EdgeCount int           `json:"edgeCount"` // 连线总数
}

// BuildCatalog 生成画布目录，作为每轮对话的上下文：模型靠它认识画布，需要细节时再调 Detail。
func BuildCatalog(g *Graph, opt CatalogOptions) Catalog {
	if opt.Max <= 0 {
		opt.Max = 200
	}
	if opt.PromptRunes <= 0 {
		opt.PromptRunes = 60
	}
	ordered := make([]Node, 0, len(g.Nodes))
	seen := map[string]bool{}
	for _, id := range opt.Priority {
		if n := g.Node(id); n != nil && !seen[id] {
			ordered, seen[id] = append(ordered, n), true
		}
	}
	for _, n := range g.Nodes {
		if !seen[n.ID()] {
			ordered = append(ordered, n)
		}
	}
	c := Catalog{Total: len(g.Nodes), EdgeCount: len(g.Edges)}
	for _, n := range ordered {
		if len(c.Nodes) >= opt.Max {
			break
		}
		c.Nodes = append(c.Nodes, catalogRow(g, n, opt.PromptRunes))
	}
	c.Omitted = c.Total - len(c.Nodes)
	return c
}

// catalogRow 生成目录的一行。
func catalogRow(g *Graph, n Node, promptRunes int) CatalogNode {
	row := CatalogNode{ID: n.ID(), Type: str(n["type"]), Label: n.Label(), Group: n.ParentID()}
	if n.IsGroup() {
		row.Members = len(g.members(n.ID()))
		return row
	}
	d := n.Data()
	row.Kind, row.Status = n.Kind(), str(d["status"])
	row.HasOutput = hasMedia(d)
	if p := str(d["prompt"]); p != "" {
		row.Prompt = truncate(p, promptRunes)
	} else if params, ok := d["params"].(map[string]any); ok {
		row.Prompt = truncate(str(params["prompt"]), promptRunes)
	}
	return row
}

// hasMedia 判断节点是不是已经有了产物或素材。
func hasMedia(d map[string]any) bool {
	if outs, _ := d["outputs"].([]any); len(outs) > 0 {
		return true
	}
	return str(d["src"]) != ""
}

// truncate 按字数截断，超出加省略号。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// DetailNode 是节点详情：媒体地址和产物列表被换成数量和标记，避免把大量 URL 塞进模型上下文。
type DetailNode struct {
	ID       string         `json:"id"`               // 节点 id
	Type     string         `json:"type"`             // canvas 或 group
	Parent   string         `json:"parent,omitempty"` // 所属组 id
	Position Point          `json:"position"`         // 位置，成员是相对组的
	Data     map[string]any `json:"data"`             // 节点数据（已去掉媒体地址）
	Size     *Point         `json:"size,omitempty"`   // 组框尺寸
}

// DetailEdge 是与所读节点相连的连线。
type DetailEdge struct {
	ID     string `json:"id"`     // 连线 id
	Source string `json:"source"` // 起点
	Target string `json:"target"` // 终点
	// Relation 为 source 表示来源线：只记派生关系（如视频截出的帧图），不是参考素材；普通连线为空
	Relation string `json:"relation,omitempty"`
}

// DetailResult 是 Detail 的结果。
type DetailResult struct {
	Nodes []DetailNode `json:"nodes"` // 请求的节点
	Edges []DetailEdge `json:"edges"` // 与它们相连的连线
}

// 详情里不给模型看的字段：媒体地址和素材 id 属于用户的私有资源，模型不需要。
var hiddenData = []string{"src", "assetId", "outputs", "paramAssets", "fileName"}

// Detail 读取一批节点的详情，最多 MaxDetailNodes 个；任何一个不存在都报错，让模型改对再读。
func Detail(g *Graph, ids []string) (*DetailResult, error) {
	if len(ids) == 0 || len(ids) > MaxDetailNodes {
		return nil, fmt.Errorf("%w: 一次要读 1 到 %d 个节点，收到 %d 个", ErrInvalid, MaxDetailNodes, len(ids))
	}
	want := map[string]bool{}
	res := &DetailResult{}
	for _, id := range ids {
		n := g.Node(id)
		if n == nil {
			return nil, fmt.Errorf("%w: 节点 %q 不存在", ErrInvalid, id)
		}
		if want[id] {
			continue
		}
		want[id] = true
		res.Nodes = append(res.Nodes, detailNode(n))
	}
	for _, e := range g.Edges {
		if want[e.Source()] || want[e.Target()] {
			res.Edges = append(res.Edges, DetailEdge{ID: e.ID(), Source: e.Source(), Target: e.Target(), Relation: e.Relation()})
		}
	}
	return res, nil
}

// detailNode 把节点转成详情：去掉媒体字段，补上产物数量和标记。
func detailNode(n Node) DetailNode {
	data := deepCopy(map[string]any(n.Data())).(map[string]any)
	outs, _ := data["outputs"].([]any)
	for _, k := range hiddenData {
		delete(data, k)
	}
	if !n.IsGroup() {
		data["outputCount"] = len(outs)
		data["hasOutput"] = hasMedia(n.Data())
		if t := str(data["text"]); t != "" {
			data["text"] = truncate(t, 4000)
		}
	}
	d := DetailNode{ID: n.ID(), Type: str(n["type"]), Parent: n.ParentID(), Position: n.Position(), Data: data}
	if n.IsGroup() {
		d.Size = &Point{X: n.Width(), Y: n.Height()}
	}
	return d
}
