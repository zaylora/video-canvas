package canvasgraph

import "fmt"

// maxOverlapWarnings 是一次写入最多给出的重叠提醒条数，免得刷屏。
const maxOverlapWarnings = 5

// intersects 判断两个矩形是否有重叠（只是挨着不算）。
func (r rect) intersects(o rect) bool {
	return r.x < o.x+o.w && o.x < r.x+r.w && r.y < o.y+o.h && o.y < r.y+r.h
}

// overlapWarnings 检查这批 ops 里显式指定位置的节点、成员有变动的组，有没有压住别的节点。
// 显式指定的位置会原样保留，不自动挪动，所以只提醒，让模型下一步用 move 或 arrange 修正；
// 组和它自己的成员之间的包含关系不算重叠。
func (a *applier) overlapWarnings() []string {
	var out []string
	seen := map[[2]string]bool{}
	check := func(n Node, what string) {
		for _, o := range a.g.Nodes {
			if len(out) >= maxOverlapWarnings {
				return
			}
			if o.ID() == n.ID() || o.ParentID() == n.ID() || n.ParentID() == o.ID() {
				continue
			}
			key := [2]string{n.ID(), o.ID()}
			if n.ID() > o.ID() {
				key = [2]string{o.ID(), n.ID()}
			}
			if seen[key] || !a.g.absRect(n).intersects(a.g.absRect(o)) {
				continue
			}
			seen[key] = true
			out = append(out, fmt.Sprintf("%s「%s」（%s）与「%s」（%s）重叠，需要的话请用 move 或 arrange 调整", what, n.Label(), n.ID(), o.Label(), o.ID()))
		}
	}
	for _, n := range a.g.Nodes { // 按画布顺序遍历，提醒的先后稳定
		switch {
		case a.explicit[n.ID()]:
			check(n, "指定的位置：节点")
		case a.refit[n.ID()] && n.IsGroup():
			check(n, "撑大后的组")
		}
	}
	return out
}
