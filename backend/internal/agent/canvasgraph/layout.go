package canvasgraph

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
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

	nodeShortSide = 324.0      // 节点框的短边：任何画幅的短边都一样
	defaultAspect = 16.0 / 9.0 // 不知道画幅时节点用的宽高比
)

// aspectRe 匹配比例参数值，如 16:9、2.39:1；Auto 和别的写法都不认识。
var aspectRe = regexp.MustCompile(`^(\d+(?:\.\d+)?):(\d+(?:\.\d+)?)$`)

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

// parseAspect 把比例参数值（如「16:9」）读成宽高比；Auto、空值或写法不认识返回 0。与前端 parseAspect 一致。
func parseAspect(v any) float64 {
	s, _ := v.(string)
	m := aspectRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	w, errW := strconv.ParseFloat(m[1], 64)
	h, errH := strconv.ParseFloat(m[2], 64)
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0
	}
	return w / h
}

// nodeAspect 返回普通节点此刻的宽高比，优先级与前端 resolveNodeAspect 一致：音频固定 1:1；
// 否则成片画幅 data.mediaAspect 优先，其次比例参数（aspect_ratio / ratio），都没有按 16:9。
// 前端还会读模型能力里比例参数的默认值，这里拿不到，按 16:9 算。
func nodeAspect(n Node) float64 {
	d, _ := n["data"].(map[string]any)
	if str(d["kind"]) == KindAudio {
		return 1
	}
	if a := num(d["mediaAspect"]); a > 0 && !math.IsInf(a, 0) {
		return a
	}
	if params, ok := d["params"].(map[string]any); ok {
		for _, key := range []string{"aspect_ratio", "ratio"} {
			if a := parseAspect(params[key]); a > 0 {
				return a
			}
		}
	}
	return defaultAspect
}

// nodeFrame 返回普通节点的框：短边固定 324，横图定高、竖图定宽。规则对应前端 node-frame.ts 的 nodeFrameSize，
// 两边读同一份 testdata/canvasgraph/node-frame-cases.json 保证一致。
func nodeFrame(n Node) (w, h float64) {
	ratio := nodeAspect(n)
	if ratio >= 1 {
		return math.Round(nodeShortSide * ratio), nodeShortSide
	}
	return nodeShortSide, math.Round(nodeShortSide / ratio)
}

// nodeSize 返回节点尺寸：组用自己的宽高（不小于最小尺寸），普通节点按画幅算。
// 前端不保存普通节点的尺寸，真实尺寸是渲染后测出来的；这里按同一套规则还原。
func nodeSize(n Node) (w, h float64) {
	if n.IsGroup() {
		return math.Max(groupMinW, n.Width()), math.Max(groupMinH, n.Height())
	}
	return nodeFrame(n)
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

// placeInGroup 返回新成员在组内的相对位置：放在所有现有成员的右侧、与最后一个成员同一行（这样不会压住位置乱的老成员）；
// 组是空的就放在左上角留白处。
func (g *Graph) placeInGroup(groupID string) Point {
	members := g.members(groupID)
	if len(members) == 0 {
		return Point{X: groupPadX, Y: groupPadTop}
	}
	right := math.Inf(-1)
	for _, m := range members {
		w, _ := nodeSize(m)
		right = math.Max(right, m.Position().X+w)
	}
	return Point{X: right + defaultGapX, Y: members[len(members)-1].Position().Y}
}

// placeFreeSkipping 返回不属于任何组的新节点的位置：放在现有内容的右侧、与最上沿对齐；画布为空时放在原点。
// skip 里的节点还没定位，不算作现有内容。
func (g *Graph) placeFreeSkipping(skip map[string]bool) Point {
	found := false
	right, top := math.Inf(-1), math.Inf(1)
	for _, n := range g.Nodes {
		if n.ParentID() != "" || skip[n.ID()] {
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

// placeDeferred 依次放置还没有位置的新节点和新组（延后放置）：每个都放在已定位内容的右侧、与最上沿对齐，
// 放完就算已定位，后面的接着往右排。本批组框已贴合；后续批次扩组的碰撞由 pushGroupOverlaps 消解。
// 组连成员作为一整块放置（成员存的是相对坐标，随组移动）。
func (g *Graph) placeDeferred(ids []string) {
	waiting := make(map[string]bool, len(ids))
	for _, id := range ids {
		waiting[id] = true
	}
	for _, id := range ids {
		if n := g.Node(id); n != nil {
			n.SetPosition(g.placeFreeSkipping(waiting))
		}
		delete(waiting, id)
	}
}

// Arrange 整理布局：以所选节点的左上角为起点排成一行、一列或网格，保留原来的大致先后。
// 目标是组时，只排成员，排完后组框贴合成员；扩组会推开后序邻居，所有位置变化进入差异。
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
	work.refitGroups(parents)
	moved := work.pushGroupOverlaps(work.grownGroups(g, parents), nil)
	res := finish(g, work, nil)
	res.Warnings = work.overlapWarnings(nil, parents, moved)
	return res, nil
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
