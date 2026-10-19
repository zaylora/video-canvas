package canvasgraph_test

import (
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// directorPayload 是一张带导演台节点的画布：导演台带着 3D 场景，连着一个导出的视频节点。
const directorPayload = `{
  "nodes": [
    {"id":"n_dir","type":"canvas","position":{"x":0,"y":0},
     "data":{"kind":"director","label":"导演台1","scene":{"version":1,"duration":5,"activeCameraId":"c1","characters":[],"props":[],"cameras":[]}}},
    {"id":"n_vid","type":"canvas","position":{"x":700,"y":0},
     "data":{"kind":"video","label":"导演台1·机位1","uploaded":true}}
  ],
  "edges": [{"id":"e_1","source":"n_dir","target":"n_vid"}]
}`

// TestDirectorNodeSurvivesRoundTrip 导演台节点读得进来、写回去时场景原样保留。
func TestDirectorNodeSurvivesRoundTrip(t *testing.T) {
	g := mustParse(t, directorPayload)
	res := mustApply(t, g, `[{"op":"create_node","tempId":"a","kind":"script"}]`)
	out, err := res.Graph.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(out), `"activeCameraId":"c1"`) {
		t.Errorf("导演台的场景丢了：%s", out)
	}
}

// TestAgentCannotCreateDirector Agent 不能新建导演台节点，导演台只在画布上手动建。
func TestAgentCannotCreateDirector(t *testing.T) {
	_, err := canvasgraph.Apply(mustParse(t, `{}`), mustOps(t, `[{"op":"create_node","tempId":"a","kind":"director"}]`), opts())
	if err == nil || !strings.Contains(err.Error(), "导演台") {
		t.Fatalf("应拒绝新建导演台，err=%v", err)
	}
}

// TestAgentCannotUpdateDirector Agent 不能改导演台节点。
func TestAgentCannotUpdateDirector(t *testing.T) {
	_, err := canvasgraph.Apply(mustParse(t, directorPayload), mustOps(t, `[{"op":"update_node","id":"n_dir","label":"新名字"}]`), opts())
	if err == nil || !strings.Contains(err.Error(), "导演台") {
		t.Fatalf("应拒绝修改导演台，err=%v", err)
	}
}

// TestDirectorCannotBeLinked 导演台没有下游，也不能当别的节点的下游。
func TestDirectorCannotBeLinked(t *testing.T) {
	g := mustParse(t, directorPayload)
	for _, ops := range []string{
		`[{"op":"create_node","tempId":"v","kind":"video"},{"op":"connect","source":"n_dir","target":"v"}]`,
		`[{"op":"create_node","tempId":"s","kind":"script"},{"op":"connect","source":"s","target":"n_dir"}]`,
	} {
		if _, err := canvasgraph.Apply(g, mustOps(t, ops), opts()); err == nil {
			t.Errorf("应拒绝连线：%s", ops)
		}
	}
}

// TestDirectorNotGeneratable 导演台不调模型，Agent 不能对它发起生成。
func TestDirectorNotGeneratable(t *testing.T) {
	_, err := canvasgraph.Generation(mustParse(t, directorPayload), "n_dir")
	if err == nil {
		t.Fatal("导演台应不能生成")
	}
}

// TestDirectorSourceLineIgnored 导出节点连着导演台的线只表示出处，不算待产出的上游。
func TestDirectorSourceLineIgnored(t *testing.T) {
	src, err := canvasgraph.Generation(mustParse(t, directorPayload), "n_vid")
	if err != nil {
		t.Fatalf("Generation: %v", err)
	}
	if src.Pending != 0 || len(src.Assets) != 0 {
		t.Errorf("来源线不该算上游：pending=%d assets=%d", src.Pending, len(src.Assets))
	}
}
