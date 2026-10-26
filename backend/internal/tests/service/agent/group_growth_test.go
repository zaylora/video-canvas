package agent_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gorm.io/datatypes"

	"video-canvas/internal/agent/canvasgraph"
	. "video-canvas/internal/service/agent"
)

func TestAgentGroupGrowth_PushIsPersistedPatchedAndUndoable(t *testing.T) {
	e := newAgentCanvasEnv(t)
	e.repo.canvas.PayloadJSON = datatypes.JSON(`{"nodes":[
 {"id":"g","type":"group","position":{"x":0,"y":0},"width":200,"height":140},
 {"id":"n","type":"canvas","position":{"x":320,"y":0},"data":{"kind":"image"}}]}`)
	before := e.payload(t)
	r, err := e.svc.ApplyOps(context.Background(), e.run, "grow", []byte(`[{"op":"create_node","kind":"image","parentGroup":"g"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 0 || !reflect.DeepEqual(r.Updated, []string{"g", "n"}) {
		t.Fatalf("推动应和扩组一起报告为 updated：%+v", r)
	}
	if p := e.payload(t).Node("n").Position(); p.X != 792 || p.Y != 0 {
		t.Fatalf("推动应落库：%+v", p)
	}
	raw, err := json.Marshal(e.bc.msgs[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	var patch struct {
		Changes []canvasgraph.Change `json:"changes"`
	}
	if err := json.Unmarshal(raw, &patch); err != nil {
		t.Fatal(err)
	}
	var saved []canvasgraph.Change
	if err := json.Unmarshal(e.repo.muts[0].ChangesJSON, &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, patch.Changes) {
		t.Fatal("patch 与撤销记录应包含相同 diff")
	}
	found := false
	for _, c := range patch.Changes {
		if c.ID == "n" && c.Before["position"] != nil && c.After["position"] != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("patch 缺少被推节点的位置差异")
	}
	if _, err := e.svc.UndoRun(context.Background(), e.run); err != nil {
		t.Fatal(err)
	}
	if diff := canvasgraph.Diff(before, e.payload(t)); len(diff) != 0 {
		t.Fatalf("撤销未还原：%+v", diff)
	}
}

func TestAgentArrange_GroupOverlapWarningsSurviveReplay(t *testing.T) {
	e := newAgentCanvasEnv(t)
	e.repo.canvas.PayloadJSON = datatypes.JSON(`{"nodes":[
 {"id":"g","type":"group","position":{"x":0,"y":0},"width":672,"height":848},
 {"id":"upstream","type":"group","position":{"x":-100,"y":0},"width":200,"height":140},
 {"id":"a","type":"canvas","parentId":"g","position":{"x":48,"y":72},"data":{"kind":"image"}},
 {"id":"b","type":"canvas","parentId":"g","position":{"x":48,"y":476},"data":{"kind":"image"}}]}`)
	ctx := WithIdempotencyKey(context.Background(), "arrange-overlap")
	target := canvasgraph.ArrangeTarget{GroupID: "g"}
	first, err := e.svc.Arrange(ctx, e.run, "arrange", target, canvasgraph.LayoutRow)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Warnings) != 1 || !strings.Contains(first.Warnings[0], "逐个 move") {
		t.Fatalf("无法自动消解的上游组重叠应返回提醒：%+v", first)
	}
	again, err := e.svc.Arrange(ctx, e.run, "arrange", target, canvasgraph.LayoutRow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Warnings, again.Warnings) || e.repo.commits != 1 {
		t.Fatal("幂等重放应保留提醒且不重复写入")
	}
}

func TestAgentArrange_ToolReturnsGroupWarnings(t *testing.T) {
	b := newBridgeEnv(t)
	b.repo.canvas.PayloadJSON = []byte(`{"nodes":[
 {"id":"g","type":"group","position":{"x":0,"y":0},"width":672,"height":848},
 {"id":"upstream","type":"group","position":{"x":-100,"y":0},"width":200,"height":140},
 {"id":"a","type":"canvas","parentId":"g","position":{"x":48,"y":72},"data":{"kind":"image"}},
 {"id":"b","type":"canvas","parentId":"g","position":{"x":48,"y":476},"data":{"kind":"image"}}]}`)
	r := execPlatform(context.Background(), t, b, platformCtx(999700, "all"), "arrange", "canvas_arrange", map[string]any{"groupId": "g", "layout": "row"})
	var out struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(r.Content), &out); err != nil {
		t.Fatal(err)
	}
	if r.IsError || len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "逐个 move") {
		t.Fatalf("工具应将未消解组重叠返回给 Agent：%+v", r)
	}
}
