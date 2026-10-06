package repository_test

import (
	"context"
	"testing"

	"video-canvas/internal/canvasgraph"
	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// 编译期检查：真实仓储满足 service 声明的依赖接口。
var _ service.AgentCanvasRepo = (*AgentRepository)(nil)

// TestAgentCanvasService_EndToEndOnRealDB 在真实数据库上走一遍「应用编辑 → 用户同时保存 → 撤销」，
// 确认 service、仓储事务、乐观锁三者接得上。
func TestAgentCanvasService_EndToEndOnRealDB(t *testing.T) {
	ctx := context.Background()
	db := agentDB(t)
	r := NewAgentRepository(db)
	svc := service.NewAgentCanvasService(r, nil)

	canvas := &model.CanvasProject{UserID: 1, Title: "画布", Revision: 1,
		PayloadJSON: []byte(`{"nodes":[{"id":"n_script","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"script","label":"剧本"}}],"edges":[],"viewport":{"zoom":1}}`)}
	if err := db.Create(canvas).Error; err != nil {
		t.Fatal(err)
	}
	s := newSession(t, r, 1, canvas.ID)
	run := newRun(t, r, s, model.RunRunning)

	res, err := svc.ApplyOps(ctx, run, "tc1", []byte(`[
	  {"op":"create_group","tempId":"g","label":"镜头 01"},
	  {"op":"create_node","tempId":"a","kind":"image","label":"关键帧","prompt":"雨夜","parentGroup":"g"},
	  {"op":"create_node","tempId":"b","kind":"image","label":"备用帧","parentGroup":"g"},
	  {"op":"connect","source":"n_script","target":"a"},
	  {"op":"connect","source":"n_script","target":"b"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Revision != 2 || res.MutationID == 0 {
		t.Fatalf("res=%+v", res)
	}

	// 用户随后手动改了新节点的名字并保存（revision 2 → 3）。
	var cur model.CanvasProject
	db.First(&cur, canvas.ID)
	g, _ := canvasgraph.Parse(cur.PayloadJSON)
	g.Node(res.IDMap["a"]).Data()["label"] = "我改的名"
	b, _ := g.Marshal()
	if err := db.Model(&cur).Updates(map[string]any{"payload_json": b, "revision": 3}).Error; err != nil {
		t.Fatal(err)
	}

	// 运行结束后撤销：被用户改过的节点 a 保留（连带它的组和连线），没人动过的 b 被撤掉。
	if _, err := r.UpdateRunIf(ctx, run.ID, []string{model.RunRunning}, map[string]any{"status": model.RunSucceeded}); err != nil {
		t.Fatal(err)
	}
	undo, err := svc.Undo(ctx, 1, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 被用户改过的节点保留；它所在的组里还有节点，所以组也保留，两项都要列出。
	skipped := map[string]string{}
	for _, sk := range undo.Skipped {
		skipped[sk.NodeID] = sk.Reason
	}
	if len(skipped) != 2 || skipped[res.IDMap["a"]] == "" || skipped[res.IDMap["g"]] == "" {
		t.Fatalf("应跳过被改过的节点和它所在的组: %+v", undo.Skipped)
	}
	db.First(&cur, canvas.ID)
	if cur.Revision != 4 {
		t.Errorf("撤销应写入一次，revision 应为 4: %d", cur.Revision)
	}
	after, _ := canvasgraph.Parse(cur.PayloadJSON)
	if after.Node(res.IDMap["a"]) == nil || after.Node(res.IDMap["a"]).Label() != "我改的名" {
		t.Error("用户改过的节点必须保留")
	}
	if after.Node(res.IDMap["b"]) != nil {
		t.Error("没人动过的新节点应被撤掉")
	}
	if len(after.Edges) != 1 {
		t.Errorf("只应剩保留节点的那条连线: %d", len(after.Edges))
	}
	if after.Node(res.IDMap["g"]) == nil {
		t.Error("组里还有被保留的节点，组也应保留")
	}
	muts, _ := r.ListMutations(ctx, run.ID)
	if len(muts) != 2 || muts[0].UndoneAt == nil || muts[1].Kind != model.MutationUndo {
		t.Errorf("改动日志不对: %+v", muts)
	}

	if _, err := svc.Undo(ctx, 1, run.ID); err == nil {
		t.Error("再次撤销应返回已撤销")
	}
}
