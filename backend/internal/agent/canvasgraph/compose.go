package canvasgraph

import (
	"fmt"
	"slices"
	"strings"
)

// composeOrderIDs 读成片节点的段落顺序（data.composeOrder），兼容图从 JSON 解析出来的 []any
// 和同一次调用里刚写进去的 []any / []string。
func composeOrderIDs(n Node) []string {
	var raw []any
	switch v := n.Data()["composeOrder"].(type) {
	case []any:
		raw = v
	case []string:
		raw = make([]any, len(v))
		for i, s := range v {
			raw[i] = s
		}
	default:
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s := str(v); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// composeVideos 返回接进成片节点的视频，按合成的实际顺序排好。
func composeVideos(g *Graph, n Node) []Node {
	ups := make([]Node, 0, 8)
	for _, e := range g.Edges {
		if e.Target() != n.ID() {
			continue
		}
		if src := g.Node(e.Source()); src != nil && src.Kind() == KindVideo {
			ups = append(ups, src)
		}
	}
	sortComposeVideos(ups, composeOrderIDs(n))
	return ups
}

// sortComposeVideos 按前端 utils/canvas/compose-input.ts 的同一规则排序：先按 composeOrder 里的先后，
// 没列到的按画布位置（上→下、同高左→右）排在后面，位置也一样时按 id 定死，保证两次读到的一致。
func sortComposeVideos(ups []Node, order []string) {
	rank := make(map[string]int, len(order))
	for i, id := range order {
		if _, dup := rank[id]; !dup {
			rank[id] = i
		}
	}
	slices.SortStableFunc(ups, func(a, b Node) int {
		ra, oka := rank[a.ID()]
		rb, okb := rank[b.ID()]
		switch {
		case oka && okb && ra != rb:
			return ra - rb
		case oka && !okb:
			return -1
		case !oka && okb:
			return 1
		}
		pa, pb := a.Position(), b.Position()
		if pa.Y != pb.Y {
			if pa.Y < pb.Y {
				return -1
			}
			return 1
		}
		if pa.X != pb.X {
			if pa.X < pb.X {
				return -1
			}
			return 1
		}
		switch {
		case a.ID() < b.ID():
			return -1
		case a.ID() > b.ID():
			return 1
		}
		return 0
	})
}

// activeAssetID 取节点当前那一版产物的素材 id（同前端 activeOutputIdOf + readOutputs）：
// 老节点只有 src + assetId、没有 outputs，裸读 activeOutputId 会静默漏段。
func activeAssetID(d map[string]any) string {
	outs := readOutputs(d)
	if active := str(d["activeOutputId"]); active != "" {
		for _, out := range outs {
			if str(out["id"]) == active {
				if id := str(out["assetId"]); id != "" {
					return id
				}
			}
		}
	}
	if len(outs) > 0 {
		if id := str(outs[len(outs)-1]["assetId"]); id != "" {
			return id
		}
	}
	return str(d["assetId"])
}

// ComposeSegment 是成片里的一段：一个视频节点和它当前那一版素材。
type ComposeSegment struct {
	NodeID  string `json:"node_id"`
	Label   string `json:"label"`
	Seq     int    `json:"seq"` // 段号，从 0 起，就是成片里的先后
	AssetID string `json:"-"`   // 素材 id 属于用户的私有资源，不给模型看
}

// ComposePlan 是一次合成的输入：承载成片的节点、按成片顺序排好的段、还没出片的段号。
type ComposePlan struct {
	NodeID   string           `json:"node_id"`
	Kind     string           `json:"kind"`
	Segments []ComposeSegment `json:"segments"`
	Missing  []int            `json:"missing"` // 连进来了/排在表里但还没有视频的段号
}

// AssetIDs 返回已经出片的段按成片顺序组成的素材序列；没有一段出片时返回空。
func (p *ComposePlan) AssetIDs() []string {
	out := make([]string, 0, len(p.Segments))
	for _, s := range p.Segments {
		if s.AssetID != "" {
			out = append(out, s.AssetID)
		}
	}
	return out
}

// ComposePlanOf 算一个节点现在要拼什么。两种入口的顺序规则与界面完全一致：
// 成片节点按连进来的视频（composeOrder 优先，其余按位置），分镜表按镜头行序取每镜绑定的视频。
// 节点不存在、或种类不能承载成片时返回 ErrInvalid，写清是哪个节点、为什么。
func ComposePlanOf(g *Graph, nodeID string) (*ComposePlan, error) {
	n := g.Node(nodeID)
	switch {
	case n == nil:
		return nil, fmt.Errorf("%w：节点 %q 不存在", ErrInvalid, nodeID)
	case n.Kind() != KindCompose && n.Kind() != KindStoryboard:
		return nil, fmt.Errorf("%w：节点 %q（%s）不能承载成片，只能用在成片合成节点或分镜表上", ErrInvalid, nodeID, n.Label())
	}
	plan := &ComposePlan{NodeID: n.ID(), Kind: n.Kind(), Segments: []ComposeSegment{}, Missing: []int{}}
	if n.Kind() == KindCompose {
		for _, up := range composeVideos(g, n) {
			seg := ComposeSegment{NodeID: up.ID(), Label: up.Label(), Seq: len(plan.Segments), AssetID: activeAssetID(up.Data())}
			if seg.AssetID == "" {
				plan.Missing = append(plan.Missing, seg.Seq)
			}
			plan.Segments = append(plan.Segments, seg)
		}
		return plan, nil
	}
	for _, shot := range existingShots(n) {
		seq := len(plan.Segments)
		up := g.Node(str(shot["videoNodeId"]))
		seg := ComposeSegment{Seq: seq, Label: fmt.Sprintf("第 %d 镜", seq+1)}
		if up != nil && up.Kind() == KindVideo {
			seg.NodeID, seg.Label, seg.AssetID = up.ID(), up.Label(), activeAssetID(up.Data())
		}
		if seg.AssetID == "" {
			plan.Missing = append(plan.Missing, seq)
		}
		plan.Segments = append(plan.Segments, seg)
	}
	return plan, nil
}

// composeSummary 是目录里成片节点的摘要：「N 段 · 有没有成片 · 顺序」。
// 成片的顺序在画布上就是这个节点唯一的正经信息，模型看不到它就只能瞎猜。
func composeSummary(g *Graph, n Node, runes int) string {
	ups := composeVideos(g, n)
	if len(ups) == 0 {
		return "还没有接进视频"
	}
	labels := make([]string, 0, len(ups))
	for _, u := range ups {
		// 还没出片的段标出来：模型在目录里就能看到缺哪段，不用先提交一次合成再被拒
		label := u.Label()
		if activeAssetID(u.Data()) == "" {
			label += "(未出片)"
		}
		labels = append(labels, label)
	}
	state := "未合成"
	if str(n.Data()["finalAssetId"]) != "" {
		state = "已有成片"
	}
	return truncate(fmt.Sprintf("%d 段 · %s · %s", len(ups), state, strings.Join(labels, " → ")), runes)
}
