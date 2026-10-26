package repository_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	. "video-canvas/internal/repository"
	agentsvc "video-canvas/internal/service/agent"
)

func TestAgentCanvasReads_PersistAcrossServicesWithoutGoRunRecord(t *testing.T) {
	ctx := context.Background()
	db := agentDB(t)
	canvas := &model.CanvasProject{UserID: 1, Title: "画布", Revision: 1, PayloadJSON: []byte(`{"nodes":[{"id":"n1","type":"canvas","data":{"kind":"script","label":"剧本","text":"原文"}}],"edges":[]}`)}
	if err := db.Create(canvas).Error; err != nil {
		t.Fatal(err)
	}
	// 独立 Agent 服务拥有运行记录：Go 只收到上下文，不能要求 agent_runs 里存在这一行。
	run := &model.AgentRun{ID: 900123, UserID: 1, CanvasID: canvas.ID, SessionID: 55}
	svc := agentsvc.NewAgentCanvasService(NewAgentRepository(db), nil)
	if _, err := svc.ReadDetail(ctx, run, []string{"n1", "n1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadDetail(ctx, run, []string{"n1"}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.AgentCanvasRead{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("重复读取应幂等，实际 %d 条", count)
	}
	// 模拟审批/继续后的新实例，仅共享数据库；详情已读和新建节点都必须恢复资格。
	svc = agentsvc.NewAgentCanvasService(NewAgentRepository(db), nil)
	res, err := svc.ApplyOps(ctx, run, "write", []byte(`[{"op":"update_node","id":"n1","prompt":"续写"},{"op":"create_node","kind":"image","tempId":"a"}]`))
	if err != nil {
		t.Fatal(err)
	}
	svc = agentsvc.NewAgentCanvasService(NewAgentRepository(db), nil)
	if _, err := svc.ApplyOps(ctx, run, "created", []byte(`[{"op":"update_node","id":"`+res.IDMap["a"]+`","prompt":"自建后再改"}]`)); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"run", "session", "user", "canvas"} {
		t.Run(field, func(t *testing.T) {
			other := *run
			switch field {
			case "run":
				other.ID++
			case "session":
				other.SessionID++
			case "user":
				other.UserID++
			case "canvas":
				other.CanvasID++
			}
			ids, err := NewAgentRepository(db).ListCanvasReads(ctx, &other)
			if err != nil {
				t.Fatal(err)
			}
			if len(ids) != 0 {
				t.Fatalf("不能跨 %s 读取资格：%v", field, ids)
			}
		})
	}
	other := *run
	other.ID++
	_, err = svc.ApplyOps(ctx, &other, "other", []byte(`[{"op":"update_node","id":"n1","prompt":"不可改"}]`))
	var ec *errcode.Error
	if !errors.As(err, &ec) || ec.Code != errcode.ErrAgentNodeUnread.Code {
		t.Fatalf("另一运行必须重新读：%v", err)
	}
}

func TestAgentCanvasReads_CreationAndReadRecordCommitAtomically(t *testing.T) {
	ctx := context.Background()
	db := agentDB(t)
	canvas := newCanvas(t, db, 1)
	run := &model.AgentRun{ID: 900124, UserID: 1, CanvasID: canvas.ID, SessionID: 55}
	// 让自建节点的已读记录写入失败，验证画布和改动日志也会回滚。
	if err := db.Exec("ALTER TABLE agent_canvas_reads ADD CONSTRAINT reject_created CHECK (node_id = 'forbidden')").Error; err != nil {
		t.Fatal(err)
	}
	svc := agentsvc.NewAgentCanvasService(NewAgentRepository(db), nil)
	if _, err := svc.ApplyOps(ctx, run, "write", []byte(`[{"op":"create_node","kind":"image"}]`)); err == nil {
		t.Fatal("记录失败必须返回错误")
	}
	var after model.CanvasProject
	if err := db.First(&after, canvas.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Revision != canvas.Revision || !jsonEqual(after.PayloadJSON, string(canvas.PayloadJSON)) {
		t.Fatal("已读记录失败不能留下画布改动")
	}
	for _, table := range []any{&model.AgentCanvasRead{}, &model.AgentMutation{}} {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("事务回滚后不应有记录: %T = %d", table, count)
		}
	}
}

func TestE2EDurable_ReadBeforeWriteSurvivesApproval(t *testing.T) {
	e := newDurableE2E(t,
		llmTurn{tools: []llmTool{{id: "read", name: "canvas_get_state", args: map[string]any{"nodeIds": []string{"n_script"}}}}},
		// 制造仍在输出、尚未进入审批的窗口，让用户插话走真实 API 的 running 状态检查。
		llmTurn{text: "稍等", slow: 500 * time.Millisecond, tools: []llmTool{{id: "delete", name: "canvas_delete", args: map[string]any{"nodeIds": []string{"n_old"}, "reason": "废稿"}}}},
		llmTurn{tools: []llmTool{{id: "edit", name: "canvas_apply_ops", args: map[string]any{"ops": []map[string]any{{"op": "update_node", "id": "n_script", "prompt": "审批后续写"}}}}}},
		llmTurn{text: "完成。"},
	)
	sid := e.newSession(t)
	runID := e.startRun(t, sid, "先读剧本，删除废稿，再续写")
	e.waitEvent(t, sid, "读取完成", func(ev map[string]any) bool {
		return ev["type"] == "tool.end" && ev["data"].(map[string]any)["name"] == "canvas_get_state"
	})
	// 插话进入同一 run，不能重置先前读过的节点。
	e.data(t, "POST", "/agent/runs/"+runID+"/interject", `{"message":"保留原有剧本风格"}`, http.StatusOK)
	ap := e.waitEvent(t, sid, "等待审批", func(ev map[string]any) bool { return ev["type"] == "approval.created" })
	aid := ap["data"].(map[string]any)["id"].(string)
	e.data(t, "POST", "/agent/approvals/"+aid+"/decision", `{"decision":"approve"}`, http.StatusOK)
	e.waitEvent(t, sid, "审批后完成", runStatus("succeeded"))
	e.waitEvent(t, sid, "插话已被读到", func(ev map[string]any) bool { return ev["type"] == "steer.read" })
	if n := e.graph(t).Node("n_script"); n.Data()["text"] != "审批后续写" {
		t.Fatalf("审批后应沿用已读资格: %v", n)
	}
	id, ok := idcodec.Decode(runID)
	if !ok {
		t.Fatal("运行 id 无法解码")
	}
	muts, err := e.repo.ListMutations(context.Background(), id)
	if err != nil || len(muts) != 2 {
		t.Fatalf("同一 run 应有删除和续写两次改动: %+v %v", muts, err)
	}
}

func TestE2EDurable_ReadBeforeWriteSurvivesContinue(t *testing.T) {
	e := newDurableE2E(t,
		llmTurn{tools: []llmTool{{id: "read", name: "canvas_get_state", args: map[string]any{"nodeIds": []string{"n_script"}}}}},
		llmTurn{tools: []llmTool{{id: "edit", name: "canvas_apply_ops", args: map[string]any{"ops": []map[string]any{{"op": "update_node", "id": "n_script", "prompt": "继续后续写"}}}}}},
		llmTurn{text: "完成。"},
	)
	sid := e.newSession(t)
	run := e.data(t, "POST", "/agent/sessions/"+sid+"/runs", `{"message":"读取后续写","budget_credits":5}`, http.StatusAccepted).(map[string]any)
	runID := run["id"].(string)
	e.waitEvent(t, sid, "读取后预算用尽", runStatus("budget_exhausted"))
	e.data(t, "POST", "/agent/runs/"+runID+"/resume", `{"add_budget":50}`, http.StatusOK)
	e.waitEvent(t, sid, "继续后完成", runStatus("succeeded"))
	if n := e.graph(t).Node("n_script"); n.Data()["text"] != "继续后续写" {
		t.Fatalf("继续后应沿用已读资格: %v", n)
	}
}

func TestE2EDurable_CodemodeEnforcesReadBeforeWrite(t *testing.T) {
	e := newDurableE2E(t,
		llmTurn{tools: []llmTool{{id: "unread_script", name: "codemode", args: map[string]any{"code": `return await tools.canvas_apply_ops({ops:[{op:"update_node",id:"n_script",prompt:"不应写入"}]});`}}}},
		llmTurn{tools: []llmTool{{id: "read_script", name: "codemode", args: map[string]any{"code": `await tools.canvas_get_state({nodeIds:["n_script"]}); return await tools.canvas_apply_ops({ops:[{op:"update_node",id:"n_script",prompt:"脚本读后续写"}]});`}}}},
		llmTurn{text: "完成。"},
	)
	sid := e.newSession(t)
	e.startRun(t, sid, "批量读后修改")
	e.waitEvent(t, sid, "脚本完成", runStatus("succeeded"))
	if n := e.graph(t).Node("n_script"); n.Data()["text"] != "脚本读后续写" {
		t.Fatalf("脚本读后应可改: %v", n)
	}
	var muts []model.AgentMutation
	if err := e.db.Find(&muts).Error; err != nil {
		t.Fatal(err)
	}
	if len(muts) != 1 {
		t.Fatalf("未读脚本不能留下改动: %+v", muts)
	}
	var calls []model.AgentToolCall
	if err := e.db.Where("name = ?", "canvas_apply_ops").Order("id").Find(&calls).Error; err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !calls[0].IsError || calls[1].IsError {
		t.Fatalf("脚本调用也必须先拒绝未读，再允许已读: %+v", calls)
	}
}
