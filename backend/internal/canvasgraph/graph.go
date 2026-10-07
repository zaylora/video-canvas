// Package canvasgraph 是画布 payload_json 的结构化读写：解析、校验并应用 Agent 的编辑操作、
// 计算改动差异、按差异撤销。
//
// 它与前端 web/src/utils/canvas 下的连线规则、打组、排列算法保持一致（见 internal/tests/canvasgraph），
// 但不依赖任何内部包，也不碰数据库：调用方负责读写 payload 和 revision 乐观锁。
package canvasgraph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrPayload 表示 payload 不是合法的画布结构。
var ErrPayload = errors.New("画布内容不合法")

// Node 是画布上的一个节点（普通节点 type=canvas，组 type=group）。
// 用 map 而不是结构体，是为了让前端新增的字段原样往返、不被这里丢掉。
type Node map[string]any

// Edge 是两个节点之间的连线。
type Edge map[string]any

// Point 是画布上的一个坐标。
type Point struct {
	X float64 `json:"x"` // 横坐标
	Y float64 `json:"y"` // 纵坐标
}

// Graph 是一张画布：节点、连线，以及视口等其他原样保留的顶层字段。
type Graph struct {
	Nodes []Node // 节点，组排在成员之前
	Edges []Edge // 连线
	rest  map[string]json.RawMessage
}

// Parse 解析 payload_json。空内容和 null 视为空画布；不是 JSON 对象或 nodes/edges 类型不对返回 ErrPayload。
func Parse(payload []byte) (*Graph, error) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(payload, &top); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPayload, err)
	}
	if top == nil {
		top = map[string]json.RawMessage{}
	}
	g := &Graph{rest: top}
	if raw, ok := top["nodes"]; ok {
		if err := json.Unmarshal(raw, &g.Nodes); err != nil {
			return nil, fmt.Errorf("%w: nodes: %w", ErrPayload, err)
		}
		delete(top, "nodes")
	}
	if raw, ok := top["edges"]; ok {
		if err := json.Unmarshal(raw, &g.Edges); err != nil {
			return nil, fmt.Errorf("%w: edges: %w", ErrPayload, err)
		}
		delete(top, "edges")
	}
	return g, nil
}

// Marshal 把图写回 payload_json，视口等其他顶层字段原样带回。
func (g *Graph) Marshal() ([]byte, error) {
	out := make(map[string]json.RawMessage, len(g.rest)+2)
	for k, v := range g.rest {
		out[k] = v
	}
	nodes, edges := g.Nodes, g.Edges
	if nodes == nil {
		nodes = []Node{}
	}
	if edges == nil {
		edges = []Edge{}
	}
	var err error
	if out["nodes"], err = json.Marshal(nodes); err != nil {
		return nil, err
	}
	if out["edges"], err = json.Marshal(edges); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// Clone 返回深拷贝，对副本的任何修改都不影响原图。
func (g *Graph) Clone() *Graph {
	c := &Graph{rest: g.rest, Nodes: make([]Node, len(g.Nodes)), Edges: make([]Edge, len(g.Edges))}
	for i, n := range g.Nodes {
		c.Nodes[i] = deepCopy(map[string]any(n)).(map[string]any)
	}
	for i, e := range g.Edges {
		c.Edges[i] = deepCopy(map[string]any(e)).(map[string]any)
	}
	return c
}

// Node 按 id 查节点，不存在返回 nil。
func (g *Graph) Node(id string) Node {
	for _, n := range g.Nodes {
		if n.ID() == id {
			return n
		}
	}
	return nil
}

// edge 按 id 查连线的下标，不存在返回 -1。
func (g *Graph) edgeIndex(id string) int {
	for i, e := range g.Edges {
		if e.ID() == id {
			return i
		}
	}
	return -1
}

// hasEdge 判断两个节点之间是否已有同向连线。
func (g *Graph) hasEdge(source, target string) bool {
	for _, e := range g.Edges {
		if e.Source() == source && e.Target() == target {
			return true
		}
	}
	return false
}

// members 返回某个组的全部成员。
func (g *Graph) members(groupID string) []Node {
	var out []Node
	for _, n := range g.Nodes {
		if n.ParentID() == groupID {
			out = append(out, n)
		}
	}
	return out
}

// groupsFirst 把组排到成员前面（xyflow 要求父节点在前），其余保持相对顺序。
func (g *Graph) groupsFirst() {
	out := make([]Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		if n.IsGroup() {
			out = append(out, n)
		}
	}
	for _, n := range g.Nodes {
		if !n.IsGroup() {
			out = append(out, n)
		}
	}
	g.Nodes = out
}

// ID 返回节点 id。
func (n Node) ID() string { return str(n["id"]) }

// IsGroup 判断是不是组节点。
func (n Node) IsGroup() bool { return str(n["type"]) == "group" }

// ParentID 返回所属组的 id，没有返回空串。
func (n Node) ParentID() string { return str(n["parentId"]) }

// Kind 返回普通节点的种类（script/image/video/audio），组返回空串。
func (n Node) Kind() string { return str(n.Data()["kind"]) }

// Label 返回节点标题。
func (n Node) Label() string { return str(n.Data()["label"]) }

// Data 返回 data 字段；不存在时创建并挂上，调用方可以直接写入。
func (n Node) Data() map[string]any {
	if d, ok := n["data"].(map[string]any); ok {
		return d
	}
	d := map[string]any{}
	n["data"] = d
	return d
}

// Position 返回节点位置：成员是相对组左上角的，其余是画布绝对坐标。
func (n Node) Position() Point {
	p, _ := n["position"].(map[string]any)
	return Point{X: num(p["x"]), Y: num(p["y"])}
}

// SetPosition 写入节点位置。
func (n Node) SetPosition(p Point) { n["position"] = map[string]any{"x": p.X, "y": p.Y} }

// Width 返回组框宽度，普通节点返回 0。
func (n Node) Width() float64 { return num(n["width"]) }

// Height 返回组框高度，普通节点返回 0。
func (n Node) Height() float64 { return num(n["height"]) }

// ID 返回连线 id。
func (e Edge) ID() string { return str(e["id"]) }

// Source 返回起点节点 id。
func (e Edge) Source() string { return str(e["source"]) }

// Target 返回终点节点 id。
func (e Edge) Target() string { return str(e["target"]) }

// str 把任意值当字符串读，不是字符串返回空串。
func str(v any) string {
	s, _ := v.(string)
	return s
}

// num 把任意值当数字读，不是数字返回 0。
func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

// deepCopy 递归拷贝 JSON 形状的值（map、切片和标量）。
func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		c := make(map[string]any, len(t))
		for k, x := range t {
			c[k] = deepCopy(x)
		}
		return c
	case []any:
		c := make([]any, len(t))
		for i, x := range t {
			c[i] = deepCopy(x)
		}
		return c
	default:
		return v
	}
}
