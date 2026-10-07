package canvasgraph_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// basePayload 是一张只有「剧本」节点的画布，带视口和一个未知字段，用来验证往返不丢数据。
const basePayload = `{
  "nodes": [
    {"id":"n_script","type":"canvas","position":{"x":0,"y":0},"extra":"keep-me",
     "data":{"kind":"script","label":"剧本","prompt":"雨夜便利店","params":{"prompt":"雨夜便利店"}}}
  ],
  "edges": [],
  "viewport": {"x":10,"y":20,"zoom":0.8},
  "futureField": {"a":1}
}`

// seqID 返回按序递增的 id 生成器，让测试里的 id 可预期。
func seqID() func(prefix string) string {
	n := 0
	return func(prefix string) string {
		n++
		return fmt.Sprintf("%s%d", prefix, n)
	}
}

func opts() canvasgraph.Options { return canvasgraph.Options{NewID: seqID()} }

func mustParse(t *testing.T, payload string) *canvasgraph.Graph {
	t.Helper()
	g, err := canvasgraph.Parse([]byte(payload))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return g
}

func mustOps(t *testing.T, raw string) []canvasgraph.Op {
	t.Helper()
	ops, err := canvasgraph.ParseOps([]byte(raw))
	if err != nil {
		t.Fatalf("ParseOps: %v", err)
	}
	return ops
}

func mustApply(t *testing.T, g *canvasgraph.Graph, raw string) *canvasgraph.Result {
	t.Helper()
	res, err := canvasgraph.Apply(g, mustOps(t, raw), opts())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return res
}

// marshal 返回稳定的 JSON 文本（键按字典序），用来比较两张图是否完全一致。
func marshal(t *testing.T, g *canvasgraph.Graph) string {
	t.Helper()
	b, err := g.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func node(t *testing.T, g *canvasgraph.Graph, id string) canvasgraph.Node {
	t.Helper()
	n := g.Node(id)
	if n == nil {
		t.Fatalf("节点 %s 不存在", id)
	}
	return n
}
