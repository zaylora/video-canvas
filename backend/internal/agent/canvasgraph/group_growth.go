package canvasgraph

import (
	"maps"
	"sort"
)

// refitGroups 统一收尾成员变动；按画布顺序贴合，避免 map 遍历影响后续布局。
func (g *Graph) refitGroups(groups map[string]bool) {
	for _, n := range g.Nodes {
		if groups[n.ID()] && n.IsGroup() {
			g.fitGroup(n.ID())
		}
	}
}

// grownGroups 只激活 refit 后向外扩展的组，缩小或原框未变时不碰原有布局。
func (g *Graph) grownGroups(before *Graph, refit map[string]bool) map[string]bool {
	grown := map[string]bool{}
	for _, n := range g.Nodes {
		if !n.IsGroup() || !refit[n.ID()] {
			continue
		}
		old := before.Node(n.ID())
		if old == nil {
			grown[n.ID()] = true
			continue
		}
		r, prev := g.absRect(n), before.absRect(old)
		if r.x < prev.x || r.y < prev.y || r.x+r.w > prev.x+prev.w || r.y+r.h > prev.y+prev.h {
			grown[n.ID()] = true
		}
	}
	return grown
}

// pushGroupOverlaps 沿固定的 x、y、id 顺序向后传播扩组碰撞；不整理无关的原有重叠。
// 前序对象先定型，后序对象只在被已激活对象压住时移动；显式定位的对象始终固定。
// 因此单个撑大组保持锚点，多组同批撑大相撞时由前序组保留锚点，后序组整体避让。
// 左侧（或同 x 的上方）对象不被反向推动；无法消解的碰撞交给 overlapWarnings。
// 这是 Agent 写入时的避让策略，前端 group.ts / arrange.ts / placement.ts 尚无此步骤；
// 尺寸与间距仍使用 layout.go 中与前端一致的常量，结果通过 diff 下发，无需前端重算。
func (g *Graph) pushGroupOverlaps(grown, explicit map[string]bool) map[string]bool {
	moved := map[string]bool{}
	if len(grown) == 0 {
		return moved
	}
	active := maps.Clone(grown)
	objects := filter(g.Nodes, func(n Node) bool { return n.ParentID() == "" })
	sort.Slice(objects, func(i, j int) bool {
		a, b := objects[i].Position(), objects[j].Position()
		if a.X != b.X {
			return a.X < b.X
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return objects[i].ID() < objects[j].ID()
	})
	for i, n := range objects {
		if explicit[n.ID()] || !g.overlapsActive(n, objects[:i], active) {
			continue
		}
		// 显式定位的后序对象也作为固定障碍：被推对象可以跨过它，但不能把它推走。
		blockers := filter(objects, func(o Node) bool { return o.ID() != n.ID() && explicit[o.ID()] })
		for _, o := range objects[:i] {
			if !explicit[o.ID()] {
				blockers = append(blockers, o)
			}
		}
		g.pushPastBlockers(n, blockers)
		moved[n.ID()], active[n.ID()] = true, true
	}
	return moved
}

// pushPastBlockers 每次跨过一个已定型对象的右边或下边，并留出默认间距。
// x、y 单调不减，跨过的边永远不会再碰到；每个障碍最多触发一次推动。
// 循环上限为障碍数（至多 N-1），全图推动次数至多 N*(N-1)/2，保证收敛并避免死循环。
func (g *Graph) pushPastBlockers(n Node, blockers []Node) {
	for range len(blockers) {
		blocker := g.spacingBlocker(n, blockers)
		if blocker == nil {
			break
		}
		r, b := g.absRect(n), g.absRect(blocker)
		p := n.Position()
		if r.x == b.x && r.y > b.y {
			p.Y = b.y + b.h + defaultGapY // 同列下方保留列关系，其余优先向右
		} else {
			p.X = b.x + b.w + defaultGapX
		}
		n.SetPosition(p)
	}
}

// overlapsActive 只用前序扩组/级联对象的真实相交激活推动，不处理仅间距较小的原布局。
func (g *Graph) overlapsActive(n Node, prev []Node, active map[string]bool) bool {
	r := g.absRect(n)
	for _, o := range prev {
		if active[o.ID()] && r.intersects(g.absRect(o)) {
			return true
		}
	}
	return false
}

// spacingBlocker 避让过程中检查所有已定型障碍及默认间距，避免制造新的碰撞或紧贴。
func (g *Graph) spacingBlocker(n Node, blockers []Node) Node {
	r := g.absRect(n)
	for _, o := range blockers {
		b := g.absRect(o)
		if r.x < b.x+b.w+defaultGapX && b.x < r.x+r.w+defaultGapX &&
			r.y < b.y+b.h+defaultGapY && b.y < r.y+r.h+defaultGapY {
			return o
		}
	}
	return nil
}
