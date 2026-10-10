package canvasgraph

import (
	"fmt"
	"math"
	"sort"
)

// 以下常量与前端 web/src/utils/canvas 的 group.ts / arrange.ts / placement.ts 保持一致。
const (
	groupPadX      = 48.0  // 组框左右留白
	groupPadTop    = 72.0  // 组框上方留白：给节点标题行（画在卡片上方）留位置
	groupPadBottom = 48.0  // 组框下方留白
	groupMinW      = 200.0 // 组框最小宽度
	groupMinH      = 140.0 // 组框最小高度
	defaultGapX    = 120.0 // 排列时的横向间距
	defaultGapY    = 80.0  // 排列时的纵向间距
)

// Layout 是排列方式。
type Layout string

// 排列方式。
const (
	LayoutRow    Layout = "row"    // 横排
	LayoutColumn Layout = "column" // 竖排
	LayoutGrid   Layout = "grid"   // 近似方形的网格
)

// Gap 是排列时节点之间的间距；不传用默认值。
type Gap struct {
	X float64 // 横向
	Y float64 // 纵向
}

// ArrangeTarget 指定要排列的对象：一个组的全部成员，或一组节点。
type ArrangeTarget struct {
	GroupID string   // 排列这个组的成员，排完后组框贴合成员
	NodeIDs []string // 排列这些普通节点
}

// rect 是画布绝对坐标下的矩形。
type rect struct{ x, y, w, h float64 }

// nodeSize 返回节点尺寸。前端不保存普通节点的尺寸，这里按默认尺寸算：所有种类都是 576×324。
func nodeSize(n Node) (w, h float64) {
	if n.IsGroup() {
		return math.Max(groupMinW, n.Width()), math.Max(groupMinH, n.Height())
	}
	return 576, 324
}

// absPos 返回节点的画布绝对坐标：成员的 position 是相对组左上角的。
func (g *Graph) absPos(n Node) Point {
	p := n.Position()
	if pid := n.ParentID(); pid != "" {
		if parent := g.Node(pid); parent != nil {
			pp := parent.Position()
			return Point{X: pp.X + p.X, Y: pp.Y + p.Y}
		}
	}
	return p
}

// absRect 返回节点的绝对矩形。
func (g *Graph) absRect(n Node) rect {
	p := g.absPos(n)
	w, h := nodeSize(n)
	return rect{p.X, p.Y, w, h}
}

// frameAround 返回包住一组矩形（含留白）的组框位置和尺寸，不小于最小尺寸。
func frameAround(rs []rect) (Point, float64, float64) {
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	for _, r := range rs {
		x0, y0 = math.Min(x0, r.x), math.Min(y0, r.y)
		x1, y1 = math.Max(x1, r.x+r.w), math.Max(y1, r.y+r.h)
	}
	return Point{X: x0 - groupPadX, Y: y0 - groupPadTop},
		math.Max(groupMinW, x1-x0+groupPadX*2),
		math.Max(groupMinH, y1-y0+groupPadTop+groupPadBottom)
}

// reparent 把节点改挂到 parent 名下（nil 表示脱离），绝对位置保持不变。
func (g *Graph) reparent(n Node, parent Node) {
	abs := g.absPos(n)
	delete(n, "parentId")
	if parent == nil {
		n.SetPosition(abs)
		return
	}
	n["parentId"] = parent.ID()
	pp := parent.Position()
	n.SetPosition(Point{X: abs.X - pp.X, Y: abs.Y - pp.Y})
}

// fitGroup 把组框贴合到成员（含留白），成员的视觉位置不变；没有成员时不动。
func (g *Graph) fitGroup(groupID string) {
	grp := g.Node(groupID)
	members := g.members(groupID)
	if grp == nil || len(members) == 0 {
		return
	}
	rs := make([]rect, len(members))
	for i, m := range members {
		rs[i] = g.absRect(m)
	}
	pos, w, h := frameAround(rs)
	for i, m := range members {
		m.SetPosition(Point{X: rs[i].x - pos.X, Y: rs[i].y - pos.Y})
	}
	grp.SetPosition(pos)
	grp["width"], grp["height"] = w, h
}

// placeInGroup 返回新成员在组内的相对位置：紧挨着最后一个成员的右侧、同一行；组是空的就放在左上角留白处。
func (g *Graph) placeInGroup(groupID string) Point {
	members := g.members(groupID)
	if len(members) == 0 {
		return Point{X: groupPadX, Y: groupPadTop}
	}
	last := members[len(members)-1]
	w, _ := nodeSize(last)
	p := last.Position()
	return Point{X: p.X + w + defaultGapX, Y: p.Y}
}

// placeFree 返回不属于任何组的新节点的位置：放在现有内容的右侧、与最上沿对齐；画布为空时放在原点。
func (g *Graph) placeFree() Point {
	found := false
	right, top := math.Inf(-1), math.Inf(1)
	for _, n := range g.Nodes {
		if n.ParentID() != "" {
			continue
		}
		r := g.absRect(n)
		right, top = math.Max(right, r.x+r.w), math.Min(top, r.y)
		found = true
	}
	if !found {
		return Point{}
	}
	return Point{X: right + defaultGapX, Y: top}
}

// Arrange 整理布局：以所选节点的左上角为起点排成一行、一列或网格，保留原来的大致先后。
// 目标是组时，只排成员，排完后组框贴合成员。返回的 Result 带位置变化的差异。
func Arrange(g *Graph, t ArrangeTarget, layout Layout, gap *Gap) (*Result, error) {
	if layout != LayoutRow && layout != LayoutColumn && layout != LayoutGrid {
		return nil, fmt.Errorf("%w: 排列方式 %q 不认识，可选 row / column / grid", ErrInvalid, layout)
	}
	work := g.Clone()
	nodes, err := arrangeTargets(work, t)
	if err != nil {
		return nil, err
	}
	gp := Gap{X: defaultGapX, Y: defaultGapY}
	if gap != nil {
		gp = *gap
	}
	abs := make(map[string]Point, len(nodes))
	for id, p := range layoutPositions(work, nodes, layout, gp) {
		abs[id] = p
	}
	parents := map[string]bool{}
	for _, n := range nodes {
		p := abs[n.ID()]
		if pid := n.ParentID(); pid != "" {
			parents[pid] = true
			pp := work.Node(pid).Position()
			p = Point{X: p.X - pp.X, Y: p.Y - pp.Y}
		}
		n.SetPosition(p)
	}
	for pid := range parents {
		work.fitGroup(pid)
	}
	return finish(g, work, nil), nil
}

// arrangeTargets 解析排列目标，返回要排列的普通节点（在 work 里的引用）。
func arrangeTargets(work *Graph, t ArrangeTarget) ([]Node, error) {
	if t.GroupID != "" {
		grp := work.Node(t.GroupID)
		if grp == nil || !grp.IsGroup() {
			return nil, fmt.Errorf("%w: 组 %q 不存在", ErrInvalid, t.GroupID)
		}
		nodes := work.members(t.GroupID)
		if len(nodes) == 0 {
			return nil, fmt.Errorf("%w: 组 %q 里没有节点", ErrInvalid, t.GroupID)
		}
		return nodes, nil
	}
	if len(t.NodeIDs) == 0 {
		return nil, fmt.Errorf("%w: 要指定 groupId 或 nodeIds", ErrInvalid)
	}
	nodes := make([]Node, 0, len(t.NodeIDs))
	for _, id := range t.NodeIDs {
		n := work.Node(id)
		if n == nil || n.IsGroup() {
			return nil, fmt.Errorf("%w: 节点 %q 不存在或是组", ErrInvalid, id)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// slot 是参与排列的一个节点：id、当前绝对位置和尺寸。
type slot struct {
	id   string
	pos  Point
	w, h float64
}

// layoutPositions 按排列方式算出每个节点的新绝对位置，算法与前端 arrangeNodes 一致：
// 起点是所选节点的左上角；横排按 x 排序，竖排和网格按 y 排序（先行后列）。
func layoutPositions(g *Graph, nodes []Node, layout Layout, gp Gap) map[string]Point {
	items := make([]slot, len(nodes))
	origin := Point{X: math.Inf(1), Y: math.Inf(1)}
	for i, n := range nodes {
		r := g.absRect(n)
		items[i] = slot{n.ID(), Point{r.x, r.y}, r.w, r.h}
		origin.X, origin.Y = math.Min(origin.X, r.x), math.Min(origin.Y, r.y)
	}
	sort.SliceStable(items, func(a, b int) bool {
		pa, pb := items[a].pos, items[b].pos
		if layout == LayoutRow {
			return pa.X < pb.X || (pa.X == pb.X && pa.Y < pb.Y)
		}
		return pa.Y < pb.Y || (pa.Y == pb.Y && pa.X < pb.X)
	})
	if layout == LayoutGrid {
		return gridPositions(items, origin, gp)
	}
	return linePositions(items, origin, gp, layout == LayoutRow)
}

// linePositions 把节点排成一行（horizontal）或一列，沿排列方向依次累加尺寸和间距。
func linePositions(items []slot, origin Point, gp Gap, horizontal bool) map[string]Point {
	out := make(map[string]Point, len(items))
	cursor := origin.Y
	if horizontal {
		cursor = origin.X
	}
	for _, it := range items {
		if horizontal {
			out[it.id] = Point{X: cursor, Y: origin.Y}
			cursor += it.w + gp.X
		} else {
			out[it.id] = Point{X: origin.X, Y: cursor}
			cursor += it.h + gp.Y
		}
	}
	return out
}

// gridPositions 排成近似方形的网格：列数取 ⌈√n⌉，每列宽取该列最宽的节点，每行高取该行最高的节点。
func gridPositions(items []slot, origin Point, gp Gap) map[string]Point {
	cols := int(math.Ceil(math.Sqrt(float64(len(items)))))
	colW, rowH := make([]float64, cols), make([]float64, (len(items)+cols-1)/cols)
	for i, it := range items {
		colW[i%cols], rowH[i/cols] = math.Max(colW[i%cols], it.w), math.Max(rowH[i/cols], it.h)
	}
	out := make(map[string]Point, len(items))
	for i, it := range items {
		p := origin
		for c := 0; c < i%cols; c++ {
			p.X += colW[c] + gp.X
		}
		for r := 0; r < i/cols; r++ {
			p.Y += rowH[r] + gp.Y
		}
		out[it.id] = p
	}
	return out
}
