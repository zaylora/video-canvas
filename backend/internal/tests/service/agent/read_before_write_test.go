package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service/agent"
)

const readGuardCanvas = `{"nodes":[
 {"id":"g1","type":"group","position":{"x":0,"y":0},"data":{"label":"镜头组"}},
 {"id":"n_script","type":"canvas","parentId":"g1","position":{"x":0,"y":0},"data":{"kind":"script","label":"剧本","text":"原文"}},
 {"id":"v1","type":"canvas","parentId":"g1","position":{"x":500,"y":0},"data":{"kind":"video","label":"镜头","model":"ghost","params":{"op":"t2v"}}},
 {"id":"outside","type":"canvas","position":{"x":1500,"y":0},"data":{"kind":"image","label":"组外"}}
],"edges":[]}`

func TestReadBeforeWrite_RejectsUnreadContentAtomically(t *testing.T) {
	cases := []struct{ name, op string }{
		{"正文", `{"op":"update_node","id":"n_script","prompt":"新文"}`},
		{"标题", `{"op":"update_node","id":"n_script","label":"新名"}`},
		{"模型", `{"op":"update_node","id":"v1","model":"other"}`},
		{"参数", `{"op":"update_node","id":"v1","params":{"duration":5}}`},
		{"生成方式", `{"op":"update_node","id":"v1","mode":"i2v"}`},
		{"组名", `{"op":"set_group","id":"g1","label":"新组名"}`},
		{"组背景色", `{"op":"set_group","id":"g1","color":"blue"}`},
		{"组名颜色", `{"op":"set_group","id":"g1","labelColor":"red"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBridgeEnv(t)
			b.repo.canvas.PayloadJSON = []byte(readGuardCanvas)
			rc := platformCtx(900100, "all")
			// 目录和自动注入的 selected 都只是概要，不代表读过详情。
			rc.Selection = []string{"n_script", "v1", "g1"}
			if _, err := b.br.PrepareRun(context.Background(), rc, PrepareRequest{Message: "改这个"}); err != nil {
				t.Fatal(err)
			}
			execPlatform(context.Background(), t, b, rc, "catalog", "canvas_get_state", map[string]any{})
			before, revision := string(b.repo.canvas.PayloadJSON), b.repo.canvas.Revision
			var op map[string]any
			if err := json.Unmarshal([]byte(c.op), &op); err != nil {
				t.Fatal(err)
			}
			res := execPlatform(context.Background(), t, b, rc, "write", "canvas_apply_ops", map[string]any{"ops": []any{
				map[string]any{"op": "create_node", "kind": "image"}, op,
			}})
			if !res.IsError || !strings.Contains(res.Content, op["id"].(string)) || !strings.Contains(res.Content, "canvas_get_state") || !strings.Contains(res.Content, "nodeIds") {
				t.Fatalf("未读修改必须返回含节点 id 和读取方法的错误: %+v", res)
			}
			if string(b.repo.canvas.PayloadJSON) != before || b.repo.canvas.Revision != revision || len(b.repo.muts) != 0 || len(b.bc.msgs) != 0 {
				t.Fatal("拒绝时整批不能写画布、改 revision、记改动或发 patch")
			}
		})
	}
}

func TestReadBeforeWrite_DetailAndGroupOnlyAuthorizeReturnedNodes(t *testing.T) {
	for _, group := range []bool{false, true} {
		t.Run(map[bool]string{false: "nodeIds", true: "groupId"}[group], func(t *testing.T) {
			b := newBridgeEnv(t)
			b.repo.canvas.PayloadJSON = []byte(readGuardCanvas)
			rc := platformCtx(900101, "all")
			args := map[string]any{"nodeIds": []string{"n_script", "v1"}}
			if group {
				args = map[string]any{"groupId": "g1"}
			}
			if res := execPlatform(context.Background(), t, b, rc, "read", "canvas_get_state", args); res.IsError {
				t.Fatal(res.Content)
			}
			ops := []map[string]any{{"op": "update_node", "id": "n_script", "prompt": "新文"}, {"op": "update_node", "id": "v1", "mode": "i2v"}}
			if res := execPlatform(context.Background(), t, b, rc, "write", "canvas_apply_ops", map[string]any{"ops": ops}); res.IsError {
				t.Fatal(res.Content)
			}
			before := string(b.repo.canvas.PayloadJSON)
			ops[0]["prompt"] = "不应写入"
			ops = append(ops, map[string]any{"op": "update_node", "id": "outside", "prompt": "未读"})
			res := execPlatform(context.Background(), t, b, rc, "mixed", "canvas_apply_ops", map[string]any{"ops": ops})
			if !res.IsError || !strings.Contains(res.Content, "outside") || string(b.repo.canvas.PayloadJSON) != before {
				t.Fatalf("混合批次必须整批拒绝: %+v", res)
			}
			// 详情中的连线端点、组 id 都不等于返回过其详情。
			res = execPlatform(context.Background(), t, b, rc, "group", "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "set_group", "id": "g1", "label": "改组"}}})
			if !res.IsError {
				t.Fatal("读组成员不代表读过组本身")
			}
			if res := execPlatform(context.Background(), t, b, rc, "read_group", "canvas_get_state", map[string]any{"nodeIds": []string{"g1"}}); res.IsError {
				t.Fatal(res.Content)
			}
			res = execPlatform(context.Background(), t, b, rc, "rename_group", "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "set_group", "id": "g1", "label": "改组"}}})
			if res.IsError {
				t.Fatal("读过组本身后应允许改组名：" + res.Content)
			}
		})
	}
}

func TestReadBeforeWrite_CreationAndLayoutNeedNoRead(t *testing.T) {
	b := newBridgeEnv(t)
	b.repo.canvas.PayloadJSON = []byte(readGuardCanvas)
	rc := platformCtx(900102, "all")
	res := execPlatform(context.Background(), t, b, rc, "build", "canvas_apply_ops", map[string]any{"ops": []map[string]any{
		{"op": "create_node", "tempId": "a", "kind": "image"},
		{"op": "update_node", "id": "a", "prompt": "自建"},
		{"op": "connect", "source": "n_script", "target": "a"},
		{"op": "move", "id": "v1", "position": map[string]int{"x": 800, "y": 0}},
		{"op": "create_group", "tempId": "g", "memberIds": []string{"outside"}},
		{"op": "set_group", "id": "g", "label": "自建组", "addMembers": []string{"a"}},
		{"op": "set_group", "id": "g1", "removeMembers": []string{"v1"}},
	}})
	if res.IsError {
		t.Fatal(res.Content)
	}
	var out struct {
		IDMap map[string]string `json:"id_map"`
	}
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatal(err)
	}
	for _, op := range []map[string]any{{"op": "update_node", "id": out.IDMap["a"], "prompt": "再次改"}, {"op": "set_group", "id": out.IDMap["g"], "color": "red"}} {
		if res := execPlatform(context.Background(), t, b, rc, "edit", "canvas_apply_ops", map[string]any{"ops": []any{op}}); res.IsError {
			t.Fatal(res.Content)
		}
	}
	if res := execPlatform(context.Background(), t, b, rc, "layout", "canvas_arrange", map[string]any{"nodeIds": []string{"n_script", "v1"}, "layout": "row"}); res.IsError {
		t.Fatal(res.Content)
	}
}

func TestReadBeforeWrite_SurvivesContinuationAndNewBridge(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(900103, "all")
	execPlatform(context.Background(), t, b, rc, "read", "canvas_get_state", map[string]any{"nodeIds": []string{"n_script"}})
	// 审批后续跑、继续和插话保留 run id；EndPlatformRun/闲置清理以及新进程对象都不能丢已读记录。
	b.br.EndPlatformRun(rc.RunID)
	*b.now = b.now.Add(PlatformRunIdleTTL + time.Hour)
	b.br = NewAgentBridge(BridgeDeps{Repo: b.repo, Canvas: NewAgentCanvasService(b.repo, b.bc), Agent: b.svc, Registry: b.reg, Secrets: fakeSecrets{"channel:ch1": "sk-test"}})
	if _, err := b.br.PrepareRun(context.Background(), rc, PrepareRequest{}); err != nil {
		t.Fatal(err)
	}
	if res := execPlatform(context.Background(), t, b, rc, "resume/edit", "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "update_node", "id": "n_script", "prompt": "续跑"}}}); res.IsError {
		t.Fatal(res.Content)
	}
}

func TestReadBeforeWrite_ScopeAndFailedRead(t *testing.T) {
	for _, field := range []string{"run", "session", "canvas", "user", "failed_read"} {
		t.Run(field, func(t *testing.T) {
			b := newBridgeEnv(t)
			rc := platformCtx(900104, "all")
			ids := []string{"n_script"}
			if field == "failed_read" {
				ids = append(ids, "missing")
			}
			execPlatform(context.Background(), t, b, rc, "read", "canvas_get_state", map[string]any{"nodeIds": ids})
			switch field {
			case "run":
				rc.RunID++
			case "session":
				rc.SessionID++
			case "canvas":
				rc.CanvasID++
				b.repo.canvas.ID = rc.CanvasID
			case "user":
				rc.UserID++
				b.repo.canvas.UserID = rc.UserID
			}
			res := execPlatform(context.Background(), t, b, rc, "write", "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "update_node", "id": "n_script", "prompt": "不可改"}}})
			if !res.IsError {
				t.Fatalf("已读不能跨作用域或从失败的读取获得: %s", field)
			}
		})
	}
}

func TestReadBeforeWrite_CapturedFrameIsAlreadyRead(t *testing.T) {
	e := newVideoEnv(t, model.AgentModeAll, true)
	res := e.tool(t, e.tok, "canvas_capture_frame", map[string]any{"nodeId": "v1", "frames": []map[string]any{{"at": "last"}}})
	if res.IsError {
		t.Fatal(res.Content)
	}
	var out struct {
		Frames []struct {
			NodeID string `json:"node_id"`
		} `json:"frames"`
	}
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatal(err)
	}
	res = e.tool(t, e.tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "update_node", "id": out.Frames[0].NodeID, "label": "尾帧"}}})
	if res.IsError {
		t.Fatal(res.Content)
	}
}

func TestReadBeforeWrite_StorageFailuresDoNotGrantOrBypassRead(t *testing.T) {
	e := newAgentCanvasEnv(t)
	ctx := context.Background()
	storageErr := errors.New("读取凭证失败，不应透传")
	e.repo.recordReadErr = storageErr
	if _, err := e.svc.ReadDetail(ctx, e.run, []string{"n_script"}); !errors.Is(err, storageErr) {
		t.Fatalf("登记失败应返回基础设施错误: %v", err)
	}
	e.repo.recordReadErr = nil
	_, err := e.svc.ApplyOps(ctx, e.run, "write", []byte(`[{"op":"update_node","id":"n_script","prompt":"不可改"}]`))
	wantAgentCode(t, err, errcode.ErrAgentNodeUnread)
	e.repo.readErr = storageErr
	_, err = e.svc.ApplyOps(ctx, e.run, "write", []byte(`[{"op":"update_node","id":"n_script","prompt":"不可改"}]`))
	if !errors.Is(err, storageErr) || e.repo.commits != 0 {
		t.Fatalf("资格查询失败不能跳过检查: %v", err)
	}
}

func TestReadBeforeWrite_DetailsDoNotAuthorizeConnectedNodes(t *testing.T) {
	b := newBridgeEnv(t)
	b.repo.canvas.PayloadJSON = []byte(readGuardCanvas)
	rc := platformCtx(900105, "all")
	execPlatform(context.Background(), t, b, rc, "connect", "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "connect", "source": "n_script", "target": "outside"}}})
	execPlatform(context.Background(), t, b, rc, "read", "canvas_get_state", map[string]any{"nodeIds": []string{"n_script"}})
	res := execPlatform(context.Background(), t, b, rc, "script/edit", "canvas_apply_ops", map[string]any{"ops": []map[string]any{
		{"op": "update_node", "id": "outside", "prompt": "未读"},
		{"op": "update_node", "id": "v1", "prompt": "未读"},
		{"op": "update_node", "id": "outside", "label": "重复 id"},
	}})
	if !res.IsError || strings.Count(res.Content, "outside") != 1 || !strings.Contains(res.Content, "v1") {
		t.Fatalf("要列全并去重，连线中的另一端也需读详情: %+v", res)
	}
}
