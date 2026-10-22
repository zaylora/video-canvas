package canvasgraph

import "fmt"

// MaxDetailNodes 是一次读取节点详情的数量上限。
const MaxDetailNodes = 50

// CatalogOptions 控制目录的大小。
type CatalogOptions struct {
	Max         int      // 最多列多少个节点，0 表示 200
	PromptRunes int      // 提示词摘要的字数，0 表示 60
	Priority    []string // 优先列出的节点 id（选中的、被 @ 的），按给定顺序排在最前
	Selected    []string // 用户发消息时选中的节点和组：明确列进目录的 selected，画布上没有的丢掉，重复的只留一个
}

// maxSelectedMembers 是选中的组最多列出多少个成员 id；更多的只说还有多少个，模型要看再用 canvas_get_state 读。
const maxSelectedMembers = 50

// CatalogSelected 是用户选中的一项：节点或组。选中的组带成员，让模型知道「这一组」里有什么。
type CatalogSelected struct {
	ID          string   `json:"id"`                    // 节点或组的 id
	Type        string   `json:"type"`                  // canvas 或 group
	Kind        string   `json:"kind,omitempty"`        // script / image / video / audio，组没有
	Label       string   `json:"label"`                 // 标题（组是组名）
	MemberIDs   []string `json:"memberIds,omitempty"`   // 组的成员 id，最多 maxSelectedMembers 个
	MoreMembers int      `json:"moreMembers,omitempty"` // 没有列出来的成员数
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
	// Selected 是用户发消息时在画布上选中的节点和组；没选就没有这个字段。用户说「这个」「这组」「选中的」指的就是它们
	Selected []CatalogSelected `json:"selected,omitempty"`
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
	c.Selected = selectedRows(g, opt.Selected)
	return c
}

// selectedRows 把选中的 id 变成目录里的 selected：按给定顺序，去重，画布上没有的丢掉；选中的组带上成员。
func selectedRows(g *Graph, ids []string) []CatalogSelected {
	var out []CatalogSelected
	seen := map[string]bool{}
	for _, id := range ids {
		n := g.Node(id)
		if n == nil || seen[id] {
			continue
		}
		seen[id] = true
		row := CatalogSelected{ID: id, Type: str(n["type"]), Label: n.Label()}
		if n.IsGroup() {
			members := g.members(id)
			for i, m := range members {
				if i >= maxSelectedMembers {
					row.MoreMembers = len(members) - maxSelectedMembers
					break
				}
				row.MemberIDs = append(row.MemberIDs, m.ID())
			}
		} else {
			row.Kind = n.Kind()
		}
		out = append(out, row)
	}
	return out
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
	if t := str(d["text"]); n.Kind() == KindScript && t != "" {
		// 文本节点的内容在正文里，没有提示词；没有正文的旧节点回退到下面的提示词
		row.Prompt = truncate(t, promptRunes)
	} else if p := str(d["prompt"]); p != "" {
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
}

// DetailResult 是 Detail 的结果。
type DetailResult struct {
	Nodes []DetailNode `json:"nodes"` // 请求的节点
	Edges []DetailEdge `json:"edges"` // 与它们相连的连线
}

// 详情里不给模型看的字段：媒体地址和素材 id 属于用户的私有资源，模型不需要。
var hiddenData = []string{"src", "assetId", "outputs", "pendingTaskIds", "paramAssets", "fileName"}

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
			res.Edges = append(res.Edges, DetailEdge{ID: e.ID(), Source: e.Source(), Target: e.Target()})
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
