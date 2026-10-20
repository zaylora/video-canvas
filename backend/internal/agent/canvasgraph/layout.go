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
	groupRowMax    = 4     // 组内一行最多放几个成员，放满就换行
	settleMargin   = 40.0  // 新建对象之间、新建对象与已有对象之间至少留的间距
	maxSettleSteps = 500   // 避让时最多往下挪几次，防止异常数据下死循环
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
	NodeIDs []string // 排列这些普通节点，或这些顶层的组（组当作一个整体，成员跟着走；组不能和组内成员混排）
}

// rect 是画布绝对坐标下的矩形。
type rect struct{ x, y, w, h float64 }

// sizeOf 返回节点尺寸。前端不保存普通节点的尺寸，这里按前端同一套规则算（见 NodeSize）。
func (g *Graph) sizeOf(n Node) (w, h float64) { return NodeSize(n, g.hint) }

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
	w, h := g.sizeOf(n)
	return rect{p.X, p.Y, w, h}
}

// frameRect 返回对象在批末贴合之后的绝对矩形：有成员的组按成员的包围盒加留白算，其余同 absRect。
// 一批操作里，后建的对象要躲开前面已经长大的组，不能按组建出来时的最小尺寸算。
func (g *Graph) frameRect(n Node) rect {
	if n.IsGroup() {
		if members := g.members(n.ID()); len(members) > 0 {
			rs := make([]rect, len(members))
			for i, m := range members {
				rs[i] = g.absRect(m)
			}
			pos, w, h := frameAround(rs)
			return rect{pos.X, pos.Y, w, h}
		}
	}
	return g.absRect(n)
}

// hits 判断两个矩形是否相交，margin 是要求保留的最小间距。
func (r rect) hits(o rect, margin float64) bool {
	return r.x < o.x+o.w+margin && o.x < r.x+r.w+margin && r.y < o.y+o.h+margin && o.y < r.y+r.h+margin
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

// placeInGroup 返回新成员在组内的相对位置：紧挨着最后一个成员的右侧、同一行；一行放满 groupRowMax 个
// 就换到最下面一行的下方、回到最左一列；组是空的就放在左上角留白处。
func (g *Graph) placeInGroup(groupID string) Point {
	members := g.members(groupID)
	if len(members) == 0 {
		return Point{X: groupPadX, Y: groupPadTop}
	}
	if len(members)%groupRowMax != 0 {
		last := members[len(members)-1]
		w, _ := g.sizeOf(last)
		p := last.Position()
		return Point{X: p.X + w + defaultGapX, Y: p.Y}
	}
	left, bottom := math.Inf(1), math.Inf(-1)
	for _, m := range members {
		_, h := g.sizeOf(m)
		p := m.Position()
		left, bottom = math.Min(left, p.X), math.Max(bottom, p.Y+h)
	}
	return Point{X: left, Y: bottom + defaultGapY}
}

// placeFree 返回不属于任何组的新对象的位置：放在现有内容的左下方、与最左沿对齐，往下一行行排；画布为空时放在原点。
// 分镜是一镜一组竖着往下排的，往下放比往右放更符合阅读顺序，也不会把画布越撑越宽。
// 已经有成员的组按贴合后的范围算，免得后建的对象落在它马上要长成的范围里。
func (g *Graph) placeFree() Point {
	found := false
	left, bottom := math.Inf(1), math.Inf(-1)
	for _, n := range g.Nodes {
		if n.ParentID() != "" {
			continue
		}
		r := g.frameRect(n)
		left, bottom = math.Min(left, r.x), math.Max(bottom, r.y+r.h)
		found = true
	}
	if !found {
		return Point{}
	}
	return Point{X: left, Y: bottom + defaultGapY}
}

// settle 让本批新建的顶层对象（组和不在任何组里的节点）避开其他顶层对象：按创建顺序逐个检查，
// 与已有对象或者前面已经落定的新对象相交（含 settleMargin 的间距），就整体往下挪到它们的下方。
// 已有对象一律不动：用户没提到的东西，Agent 不该碰。必须在组框贴合之后调用。
func (g *Graph) settle(created []string) {
	isNew := make(map[string]bool, len(created))
	for _, id := range created {
		isNew[id] = true
	}
	var fixed []rect
	for _, n := range g.Nodes {
		if n.ParentID() == "" && !isNew[n.ID()] {
			fixed = append(fixed, g.absRect(n))
		}
	}
	for _, id := range created {
		n := g.Node(id)
		if n == nil || n.ParentID() != "" { // 后来被打进了别的组，位置由那个组管
			continue
		}
		r := g.absRect(n)
		moved := false
		for i := 0; i < maxSettleSteps; i++ {
			bottom, hit := 0.0, false
			for _, f := range fixed {
				if r.hits(f, settleMargin) {
					bottom, hit = math.Max(bottom, f.y+f.h), true
				}
			}
			if !hit {
				break
			}
			r.y, moved = bottom+defaultGapY, true
		}
		if moved {
			n.SetPosition(Point{X: r.x, Y: r.y})
		}
		fixed = append(fixed, r)
	}
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
	hasGroup := false
	for _, id := range t.NodeIDs {
		n := work.Node(id)
		if n == nil {
			return nil, fmt.Errorf("%w: 节点或组 %q 不存在", ErrInvalid, id)
		}
		hasGroup = hasGroup || n.IsGroup()
		nodes = append(nodes, n)
	}
	// 组可以和别的顶层对象（组、散落的节点）一起排，组被当作一个整体；但不能和组内的成员混排：
	// 成员的坐标是相对组的，两种坐标系混在一起排没有意义。
	if hasGroup {
		for _, n := range nodes {
			if n.ParentID() != "" {
				return nil, fmt.Errorf("%w: 组和组内成员不能一起排列，%q 在组 %q 里；要排成员请指定 groupId", ErrInvalid, n.ID(), n.ParentID())
			}
		}
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
