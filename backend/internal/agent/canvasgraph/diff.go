package canvasgraph

import (
	"errors"
	"reflect"
	"strings"
)

// ErrInvalid 表示请求本身不合法（目标不存在、参数不对），调用方应把原因回给模型。
var ErrInvalid = errors.New("操作不合法")

// Change 的种类。
const (
	ChangeCreate = "create" // 新建：After 是完整对象
	ChangeUpdate = "update" // 修改：Before / After 只含变化的字段
	ChangeDelete = "delete" // 删除：Before 是完整对象
)

// Change 是一次改动里某个节点或连线的变化，是「撤销本轮」和前端三方合并的依据。
// 字段名是拍平的路径：data 下的字段写成 "data.prompt"，其余是顶层键，如 "position"、"parentId"。
type Change struct {
	Kind   string         `json:"kind"`             // node 或 edge
	Op     string         `json:"op"`               // create / update / delete
	ID     string         `json:"id"`               // 节点或连线 id
	Before map[string]any `json:"before,omitempty"` // 改动前的字段；新建时没有
	After  map[string]any `json:"after,omitempty"`  // 改动后的字段；删除时没有
	Index  int            `json:"index,omitempty"`  // 删除前在列表里的位置，撤销时用来放回原处
}

// Result 是一次编辑的结果：新图、tempId 到真实 id 的映射、以及改动差异。
type Result struct {
	Graph   *Graph            // 应用后的新图，入参不变
	IDMap   map[string]string // tempId → 真实 id
	Changes []Change          // 与入参相比的差异
}

// finish 把前后两张图的差异打包成 Result。
func finish(before, after *Graph, idMap map[string]string) *Result {
	after.groupsFirst()
	return &Result{Graph: after, IDMap: idMap, Changes: Diff(before, after)}
}

// Diff 比较两张图，列出新建、修改、删除的节点和连线。
func Diff(before, after *Graph) []Change {
	var out []Change
	beforeNodes := indexNodes(before.Nodes)
	afterNodes := indexNodes(after.Nodes)
	for _, n := range after.Nodes {
		old, ok := beforeNodes[n.ID()]
		if !ok {
			out = append(out, Change{Kind: "node", Op: ChangeCreate, ID: n.ID(), After: flatten(n)})
			continue
		}
		if b, a := diffFields(flatten(old), flatten(n)); len(a) > 0 || len(b) > 0 {
			out = append(out, Change{Kind: "node", Op: ChangeUpdate, ID: n.ID(), Before: b, After: a})
		}
	}
	for i, n := range before.Nodes {
		if _, ok := afterNodes[n.ID()]; !ok {
			out = append(out, Change{Kind: "node", Op: ChangeDelete, ID: n.ID(), Before: flatten(n), Index: i})
		}
	}
	return append(out, diffEdges(before, after)...)
}

// diffEdges 列出新建和删除的连线（连线不会被修改，只会增删）。
func diffEdges(before, after *Graph) []Change {
	var out []Change
	for _, e := range after.Edges {
		if before.edgeIndex(e.ID()) < 0 {
			out = append(out, Change{Kind: "edge", Op: ChangeCreate, ID: e.ID(), After: map[string]any(deepCopy(map[string]any(e)).(map[string]any))})
		}
	}
	for i, e := range before.Edges {
		if after.edgeIndex(e.ID()) < 0 {
			out = append(out, Change{Kind: "edge", Op: ChangeDelete, ID: e.ID(), Before: deepCopy(map[string]any(e)).(map[string]any), Index: i})
		}
	}
	return out
}

// indexNodes 按 id 建索引。
func indexNodes(ns []Node) map[string]Node {
	m := make(map[string]Node, len(ns))
	for _, n := range ns {
		m[n.ID()] = n
	}
	return m
}

// flatten 把节点拍平成 "路径 → 值"：data 展开一层（"data.prompt"），其余保留顶层键；值做深拷贝。
func flatten(n Node) map[string]any {
	out := make(map[string]any, len(n)+8)
	for k, v := range n {
		if d, ok := v.(map[string]any); ok && k == "data" {
			for dk, dv := range d {
				out["data."+dk] = deepCopy(dv)
			}
			continue
		}
		out[k] = deepCopy(v)
	}
	return out
}

// diffFields 比较两份拍平的字段，返回有变化的字段在改动前、后的取值；只存在于一侧的字段只出现在那一侧。
func diffFields(before, after map[string]any) (b, a map[string]any) {
	b, a = map[string]any{}, map[string]any{}
	for k, av := range after {
		if bv, ok := before[k]; !ok || !reflect.DeepEqual(bv, av) {
			a[k] = av
			if ok {
				b[k] = bv
			}
		}
	}
	for k, bv := range before {
		if _, ok := after[k]; !ok {
			b[k] = bv
		}
	}
	return b, a
}

// setField 按拍平路径把值写回节点；"data.x" 写进 data，其余写顶层。
func setField(n Node, path string, v any) {
	if key, ok := strings.CutPrefix(path, "data."); ok {
		n.Data()[key] = deepCopy(v)
		return
	}
	n[path] = deepCopy(v)
}

// delField 按拍平路径删除字段。
func delField(n Node, path string) {
	if key, ok := strings.CutPrefix(path, "data."); ok {
		delete(n.Data(), key)
		return
	}
	delete(n, path)
}

// getField 按拍平路径读字段，第二个返回值表示字段是否存在。
func getField(n Node, path string) (any, bool) {
	if key, ok := strings.CutPrefix(path, "data."); ok {
		v, ok := n.Data()[key]
		return v, ok
	}
	v, ok := n[path]
	return v, ok
}
