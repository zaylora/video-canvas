package canvasgraph

import (
	"reflect"
	"sort"
)

// Skip 是撤销时没有处理的一项：用户后来改过，或者已经有了生成产物。
type Skip struct {
	Kind   string // node 或 edge
	NodeID string // 节点或连线 id
	Field  string // 字段路径，整个对象被保留时为空
	Reason string // 中文原因
}

// RevertResult 是撤销的结果。
type RevertResult struct {
	Graph    *Graph // 撤销后的图，入参不变
	Reverted int    // 恢复或删除了多少项
	Skipped  []Skip // 没处理的项和原因
}

// Revert 按改动差异逆向撤销，作用在「当前最新的图」上。原则是不覆盖用户之后的修改：
//   - 字段只有当前值仍等于 Agent 写入的值才恢复，否则跳过并列出；
//   - Agent 新建的节点，已经有生成产物或被用户改过就保留（连线也留着），否则删除；
//   - Agent 删掉的节点和连线，放回原来的位置。
func Revert(g *Graph, changes []Change) *RevertResult {
	r := &reverter{g: g.Clone(), kept: map[string]bool{}, res: &RevertResult{}}
	r.restoreDeleted(changes)
	r.revertUpdates(changes)
	r.removeCreatedNodes(changes)
	r.removeCreatedEdges(changes)
	r.g.groupsFirst()
	r.res.Graph = r.g
	return r.res
}

// reverter 保存撤销过程中的状态。
type reverter struct {
	g    *Graph
	kept map[string]bool // 被保留下来的 Agent 新建节点
	res  *RevertResult
}

// restoreDeleted 把被删除的节点和连线放回原位：先按原下标从小到大插回节点，再插回两端都在的连线。
func (r *reverter) restoreDeleted(changes []Change) {
	var nodes, edges []Change
	for _, c := range changes {
		switch {
		case c.Op == ChangeDelete && c.Kind == "node":
			nodes = append(nodes, c)
		case c.Op == ChangeDelete && c.Kind == "edge":
			edges = append(edges, c)
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Index < nodes[j].Index })
	sort.SliceStable(edges, func(i, j int) bool { return edges[i].Index < edges[j].Index })
	for _, c := range nodes {
		if r.g.Node(c.ID) != nil {
			continue
		}
		n := unflatten(c.Before)
		r.g.Nodes = insertAt(r.g.Nodes, c.Index, n)
		r.res.Reverted++
	}
	for _, c := range edges {
		e := Edge(deepCopy(c.Before).(map[string]any))
		if r.g.edgeIndex(c.ID) >= 0 || r.g.Node(e.Source()) == nil || r.g.Node(e.Target()) == nil {
			continue
		}
		r.g.Edges = insertAt(r.g.Edges, c.Index, e)
		r.res.Reverted++
	}
}

// revertUpdates 逆序恢复被修改的字段：同一字段被连续改过两次时，要先撤后一次才能撤前一次。
func (r *reverter) revertUpdates(changes []Change) {
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		if c.Op != ChangeUpdate || c.Kind != "node" {
			continue
		}
		n := r.g.Node(c.ID)
		if n == nil {
			r.res.Skipped = append(r.res.Skipped, Skip{Kind: "node", NodeID: c.ID, Reason: "节点已不存在"})
			continue
		}
		restored := false
		for _, path := range fieldPaths(c) {
			if r.revertField(n, c, path) {
				restored = true
			}
		}
		if restored {
			r.res.Reverted++
		}
	}
}

// revertField 恢复一个字段，当前值不等于 Agent 写入的值时跳过。返回是否恢复了。
func (r *reverter) revertField(n Node, c Change, path string) bool {
	cur, curHas := getField(n, path)
	after, afterHas := c.After[path]
	if curHas != afterHas || (curHas && !reflect.DeepEqual(cur, after)) {
		r.res.Skipped = append(r.res.Skipped, Skip{Kind: "node", NodeID: c.ID, Field: path, Reason: "你已修改"})
		return false
	}
	if before, ok := c.Before[path]; ok {
		setField(n, path, before)
	} else {
		delField(n, path)
	}
	return true
}

// fieldPaths 返回一次修改涉及的全部字段路径，按字典序，保证结果稳定。
func fieldPaths(c Change) []string {
	set := map[string]bool{}
	for k := range c.Before {
		set[k] = true
	}
	for k := range c.After {
		set[k] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// removeCreatedNodes 删除 Agent 新建且没人动过的节点：先删普通节点，再删已经空了的组。
func (r *reverter) removeCreatedNodes(changes []Change) {
	for _, pass := range []bool{false, true} {
		for i := len(changes) - 1; i >= 0; i-- {
			c := changes[i]
			if c.Op != ChangeCreate || c.Kind != "node" {
				continue
			}
			n := r.g.Node(c.ID)
			if n == nil || n.IsGroup() != pass {
				continue
			}
			if reason := r.keepReason(n, c); reason != "" {
				r.kept[c.ID] = true
				r.res.Skipped = append(r.res.Skipped, Skip{Kind: "node", NodeID: c.ID, Reason: reason})
				continue
			}
			r.removeNode(c.ID)
			r.res.Reverted++
		}
	}
}

// keepReason 判断新建的节点该不该保留，返回保留原因，空串表示可以删除。
func (r *reverter) keepReason(n Node, c Change) string {
	if n.IsGroup() {
		if len(r.g.members(n.ID())) > 0 {
			return "组里还有节点，保留"
		}
		return ""
	}
	d := n.Data()
	if s, _ := d["taskId"].(string); s != "" {
		return "已有生成任务，保留"
	}
	if outs, _ := d["outputs"].([]any); len(outs) > 0 {
		return "已有产物，保留"
	}
	cur := flatten(n)
	delete(cur, "data.status")
	want := make(map[string]any, len(c.After))
	for k, v := range c.After {
		want[k] = v
	}
	delete(want, "data.status")
	if !reflect.DeepEqual(cur, want) {
		return "你已修改，保留"
	}
	return ""
}

// removeNode 删除节点和与它相连的所有连线。
func (r *reverter) removeNode(id string) {
	nodes := r.g.Nodes[:0:0]
	for _, n := range r.g.Nodes {
		if n.ID() != id {
			nodes = append(nodes, n)
		}
	}
	r.g.Nodes = nodes
	edges := r.g.Edges[:0:0]
	for _, e := range r.g.Edges {
		if e.Source() != id && e.Target() != id {
			edges = append(edges, e)
		}
	}
	r.g.Edges = edges
}

// removeCreatedEdges 删除 Agent 新建的连线；只要有一端是被保留下来的节点，连线就留着，
// 免得用户手里的节点凭空断开。
func (r *reverter) removeCreatedEdges(changes []Change) {
	for _, c := range changes {
		if c.Op != ChangeCreate || c.Kind != "edge" {
			continue
		}
		i := r.g.edgeIndex(c.ID)
		if i < 0 {
			continue
		}
		e := r.g.Edges[i]
		if r.kept[e.Source()] || r.kept[e.Target()] {
			continue
		}
		r.g.Edges = append(r.g.Edges[:i], r.g.Edges[i+1:]...)
		r.res.Reverted++
	}
}

// unflatten 把拍平的字段还原成节点：data.* 收回 data 下。
func unflatten(flat map[string]any) Node {
	n := Node{}
	for k, v := range flat {
		setField(n, k, v)
	}
	return n
}

// insertAt 把元素插到下标 i，越界时放到末尾。
func insertAt[T any](s []T, i int, v T) []T {
	if i < 0 || i > len(s) {
		i = len(s)
	}
	s = append(s, v)
	copy(s[i+1:], s[i:])
	s[i] = v
	return s
}
