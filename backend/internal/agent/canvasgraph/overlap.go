package canvasgraph

import (
	"fmt"
	"sort"
)

// maxOverlapWarnings 是一次写入最多给出的重叠提醒条数，免得刷屏。
const maxOverlapWarnings = 5

// intersects 判断两个矩形是否有重叠（只是挨着不算）。
func (r rect) intersects(o rect) bool {
	return r.x < o.x+o.w && o.x < r.x+r.w && r.y < o.y+o.h && o.y < r.y+r.h
}

// overlapWarnings 只提醒本次定位、贴合或推动涉及的剩余碰撞；显式位置和上游原布局不会自动改动。
// 跨组碰撞以所属组为对象，同组内部仍以成员为对象，避免组与外组每个成员各报一遍。
func (g *Graph) overlapWarnings(explicit, refit, moved map[string]bool) []string {
	pairs := g.overlapPairs(explicit, refit, moved)
	var out []string
	used := map[string]int{}
	for len(pairs) > 0 && len(out) < maxOverlapWarnings {
		// 优先覆盖尚未出现的对象；同分沿 id 排序，避免一个组刷满前五条。
		best, score := 0, used[pairs[0][0]]+used[pairs[0][1]]
		for i, p := range pairs[1:] {
			if s := used[p[0]] + used[p[1]]; s < score {
				best, score = i+1, s
			}
		}
		p := pairs[best]
		a, b := g.Node(p[0]), g.Node(p[1])
		out = append(out, fmt.Sprintf("「%s」（%s）与「%s」（%s）重叠；节点可用 move 或 canvas_arrange 调整；组与组之间重叠要对组逐个 move（canvas_apply_ops），canvas_arrange 只重排组内成员，不会移动组本身", a.Label(), a.ID(), b.Label(), b.ID()))
		used[p[0]]++
		used[p[1]]++
		pairs = append(pairs[:best], pairs[best+1:]...)
	}
	return out
}

// overlapPairs 收集规范化后的唯一对象对，再排序以隔离 map 和画布节点排列顺序的影响。
func (g *Graph) overlapPairs(explicit, refit, moved map[string]bool) [][2]string {
	seen := map[[2]string]bool{}
	for i, a := range g.Nodes {
		for _, b := range g.Nodes[i+1:] {
			if !overlapTouched(a, explicit, refit, moved) && !overlapTouched(b, explicit, refit, moved) {
				continue
			}
			if a.ID() == b.ParentID() || b.ID() == a.ParentID() || !g.absRect(a).intersects(g.absRect(b)) {
				continue
			}
			seen[g.overlapPair(a, b)] = true
		}
	}
	pairs := make([][2]string, 0, len(seen))
	for p := range seen {
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i][0] < pairs[j][0] || (pairs[i][0] == pairs[j][0] && pairs[i][1] < pairs[j][1])
	})
	return pairs
}

// overlapPair 跨组时折叠到组，再把 id 小的对象放前面，两个方向及外组成员共享同一键。
func (g *Graph) overlapPair(a, b Node) [2]string {
	x, y := a.ID(), b.ID()
	if a.ParentID() != b.ParentID() {
		x, y = g.overlapObject(a), g.overlapObject(b)
	}
	if x > y {
		x, y = y, x
	}
	return [2]string{x, y}
}

// overlapObject 只有有效的所属组才能折叠，兼容尚未被前端 normalize 的旧存档孤立成员。
func (g *Graph) overlapObject(n Node) string {
	if parent := g.Node(n.ParentID()); parent != nil && parent.IsGroup() {
		return parent.ID()
	}
	return n.ID()
}

// overlapTouched 组整体移动时，成员的跨组碰撞也属于本次改动。
func overlapTouched(n Node, explicit, refit, moved map[string]bool) bool {
	return explicit[n.ID()] || refit[n.ID()] || moved[n.ID()] || moved[n.ParentID()]
}
