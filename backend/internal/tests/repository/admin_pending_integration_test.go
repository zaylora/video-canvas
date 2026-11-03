package repository_test

import (
	"context"
	"encoding/json"
	"testing"

	"video-canvas/internal/model"
	agentsvc "video-canvas/internal/service/agent"
)

func TestAdminPendingDraftApprovalRealDB(t *testing.T) {
	e := newPluginDBEnv(t)
	ctx := context.Background()
	checked, err := e.bridge.ExecutePlatformTool(ctx, e.rc, "check-pending", "admin_plugin_check", json.RawMessage(`{"code":`+string(mustJSON(t, integrationDraft))+`}`))
	if err != nil || checked.IsError {
		t.Fatalf("检查失败：%+v %v", checked, err)
	}
	body := draftModelInput("p2-platform", "p2-channel")
	body["pricing"] = map[string]any{"billing": "per_call", "pending": true}
	request := agentsvc.AdminChangeRequest{Title: "创建测试草稿", Reason: "先测试协议，售价待确认", Items: []agentsvc.AdminChangeInput{
		{Target: "model_create", Key: "pending-video", Patch: body, DependsOn: []int{1, 2}},
		{Target: "plugin", Key: "p2-platform", Patch: map[string]any{"code": integrationDraft}},
		{Target: "channel", Key: "p2-channel", Patch: map[string]any{"name": "测试渠道", "base_url": "https://example.invalid"}},
	}}
	raw := mustJSON(t, request)
	proposal, err := e.bridge.ProposeApproval(ctx, e.rc, "admin_change", raw)
	if err != nil || proposal.IsError {
		t.Fatalf("草稿提案失败：%+v %v", proposal, err)
	}
	outcome, err := e.bridge.ResolveApproval(ctx, e.rc, "admin_change", proposal.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := e.bridge.ExecuteApproval(ctx, e.rc, "admin_change", 811, "pending", *outcome)
	if err != nil || applied.Status != agentsvc.ExecExecuted {
		t.Fatalf("审批执行失败：%+v %v", applied, err)
	}
	saved, _, _, err := e.admin.ReadModel(ctx, "pending-video")
	if err != nil {
		t.Fatal(err)
	}
	pricing, _ := saved["pricing"].(map[string]any)
	if pricing["pending"] != true || pricing["unit"] != nil {
		t.Fatalf("草稿编造了售价：%s", mustJSON(t, saved))
	}
	var pointer model.AIModel
	if err := e.db.Where("key = ?", "pending-video").First(&pointer).Error; err != nil || pointer.Enabled {
		t.Fatalf("草稿已上架：%+v %v", pointer, err)
	}
	trial, err := e.bridge.ProposeApproval(ctx, e.rc, "admin_test_run", json.RawMessage(`{"model_key":"pending-video","input":{"prompt":"测试片段","op":"t2v"},"reason":"验证协议"}`))
	if err != nil || trial.IsError || !trial.Terminate {
		t.Fatalf("未定价草稿无法申请试跑：%+v %v", trial, err)
	}
}

// mustJSON 用于固定测试数据，序列化失败直接终止用例。
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
