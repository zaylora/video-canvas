package repository_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/configbody"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	agentsvc "video-canvas/internal/service/agent"
)

func TestAdminAssistantShortOrderAndLegacyRollbackRealDB(t *testing.T) {
	db := isolatedDB(t, &model.User{}, &model.SystemSetting{}, &model.AIAuditLog{}, &model.AdminAuditLog{})
	ctx := context.Background()
	user := &model.User{Username: "legacy-order-admin", Password: "test", Role: model.RoleAdmin}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	settingsRepo := repository.NewSystemSettingRepository(db)
	if err := settingsRepo.SetMany(ctx, map[string]string{model.SettingPricingCreditsPerYuan: "7.5", model.SettingPricingMarkup: "35"}, user.ID); err != nil {
		t.Fatal(err)
	}
	settings := service.NewPricingSettingsService(settingsRepo, nil)
	audit := repository.NewAdminAssistantRepository(db)
	admin := agentsvc.NewAdminAssistantService(agentsvc.AdminAssistantDeps{Users: repository.NewUserRepository(db), Settings: settings, Audit: audit, Now: func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }})
	// 两类审计都能占用短号；历史长号仍原样用于审计查询和反向变更。
	if err := db.Create(&model.AIAuditLog{Action: model.AuditModelUpdate, TargetKey: "m", DetailJSON: model.JSONText(`{"via":"assistant","order_no":"CO-261102-ABCDEF"}`)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AdminAuditLog{Action: model.AdminAuditSettingsPricing, DetailJSON: model.JSONText(`{"via":"assistant","order_no":"CO-261102-123456"}`)}).Error; err != nil {
		t.Fatal(err)
	}
	view, err := settings.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	after := service.PricingSettingsBody(view)
	var before map[string]any
	_ = json.Unmarshal(after, &before)
	before["default_markup_pct"] = 30
	legacy := "CO-261102-25C9F1F7D639983DD4DE611406655215"
	detail, _ := json.Marshal(map[string]any{"via": "assistant", "order_no": legacy, "before": before, "after": json.RawMessage(after), "diff": []agentsvc.AdminChangeDiff{{Path: "default_markup_pct", Before: 30, After: 35}}})
	if err := db.Create(&model.AdminAuditLog{Action: model.AdminAuditSettingsPricing, DetailJSON: detail}).Error; err != nil {
		t.Fatal(err)
	}
	agentsvc.SetAdminOrderRandomForTest(admin, bytes.NewReader([]byte{0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0, 0, 1, 0, 0, 2}))
	p, err := admin.BuildChanges(ctx, agentsvc.AdminChangeRequest{Title: "改加价率", Reason: "用户要求", Items: []agentsvc.AdminChangeInput{{Target: "pricing_settings", Key: "pricing", BaseSHA256: configbody.SHA256(after), Patch: map[string]any{"default_markup_pct": float64(36)}}}})
	if err != nil || p.OrderNo != "CO-261102-000001" || p.Items[0].Status != "ok" {
		t.Fatalf("未避开两类审计已有短号：%+v %v", p, err)
	}
	bridge := agentsvc.NewAgentBridge(agentsvc.BridgeDeps{Admin: admin, DramaApprovalKey: []byte("legacy-order-test-key")})
	rc := agentsvc.RunContext{RunID: 1, UserID: user.ID, SessionID: 1, Mode: "admin"}
	args, _ := json.Marshal(map[string]any{"revert_of": legacy})
	proposal, err := bridge.ProposeApproval(ctx, rc, "admin_change", args)
	if err != nil || proposal.IsError {
		t.Fatalf("历史长号应能提出回滚：%+v %v", proposal, err)
	}
	outcome, err := bridge.ResolveApproval(ctx, rc, "admin_change", proposal.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.AdminChange.RevertOf != legacy || outcome.AdminChange.OrderNo != "CO-261102-000002" {
		t.Fatalf("回滚关联长号或新短号错误：%+v", outcome.AdminChange)
	}
	result, err := bridge.ExecuteApproval(ctx, rc, "admin_change", 123, "rollback", *outcome)
	if err != nil || result.Status != "executed" {
		t.Fatalf("历史长号回滚未执行：%+v %v", result, err)
	}
	view, err = settings.Get(ctx)
	if err != nil || view.DefaultMarkupPct != 30 {
		t.Fatalf("历史配置未恢复：%+v %v", view, err)
	}
	rows, err := audit.ChangeAudits(ctx, legacy)
	if err != nil || len(rows) != 1 {
		t.Fatalf("历史长号审计不可用：%+v %v", rows, err)
	}
}
