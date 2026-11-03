package repository_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/configbody"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	agentsvc "video-canvas/internal/service/agent"
)

func TestAdminAssistantRealDBTransactionsAuditConflictAndReplay(t *testing.T) {
	for _, scenario := range []string{"批准三项", "审核期间冲突", "批准后角色降级", "并发重放"} {
		t.Run(scenario, func(t *testing.T) {
			db := isolatedDB(t, &model.AIModel{}, &model.AIConfigRevision{}, &model.AIAuditLog{}, &model.AdminAuditLog{}, &model.SystemSetting{}, &model.User{}, &model.AIChannel{}, &model.AIPlugin{}, &model.AIPluginVersion{}, &model.AISecret{})
			ctx := context.Background()
			models := repository.NewAIConfigRepository(db)
			channels := repository.NewAIChannelRepository(db)
			writer := service.NewAIConfigService(models, channels, repository.NewAIPluginRepository(db), "")
			settingsRepo := repository.NewSystemSettingRepository(db)
			settings := service.NewPricingSettingsService(settingsRepo, nil)
			if err := db.Create(&model.User{Username: "admin-assistant", Password: "test", Role: model.RoleAdmin}).Error; err != nil {
				t.Fatal(err)
			}
			var user model.User
			if err := db.First(&user).Error; err != nil {
				t.Fatal(err)
			}
			if err := settingsRepo.SetMany(ctx, map[string]string{model.SettingPricingCreditsPerYuan: "7.5", model.SettingPricingMarkup: "30"}, user.ID); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"a", "b", "c"} {
				body := json.RawMessage(fmt.Sprintf(`{"key":%q,"kind":"agent","label":"测试模型","channels":[{"channel":"main","upstream_model":"test"}],"capabilities":{"prompt":{"max_length":2000},"context":{"window":128000,"output":4096}},"pricing":{"billing":"token","token":{"in":2,"out":5}}}`, key))
				if _, err := writer.SaveModel(ctx, service.ModelSaveInput{Key: key, Create: true, Body: body, AdminID: user.ID}); err != nil {
					t.Fatal(err)
				}
			}
			auditRepo := repository.NewAdminAssistantRepository(db)
			admin := agentsvc.NewAdminAssistantService(agentsvc.AdminAssistantDeps{Models: models, Writer: writer, Users: repository.NewUserRepository(db), Channels: channels, Settings: settings, Audit: auditRepo})
			bridge := agentsvc.NewAgentBridge(agentsvc.BridgeDeps{Admin: admin, DramaApprovalKey: []byte("persistent-test-key")})
			rc := agentsvc.RunContext{RunID: 123, UserID: user.ID, SessionID: 456, Mode: "admin"}
			inputs := []agentsvc.AdminChangeInput{}
			for _, key := range []string{"a", "b", "c"} {
				_, sha, _, err := admin.ReadModel(ctx, key)
				if err != nil {
					t.Fatal(err)
				}
				inputs = append(inputs, agentsvc.AdminChangeInput{Target: "model", Key: key, BaseSHA256: sha, Patch: map[string]any{"label": "修改后的名称"}})
			}
			view, err := settings.Get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			inputs = append(inputs, agentsvc.AdminChangeInput{Target: "pricing_settings", Key: "pricing", BaseSHA256: configbody.SHA256(service.PricingSettingsBody(view)), Patch: map[string]any{"default_markup_pct": 35}})
			raw, _ := json.Marshal(agentsvc.AdminChangeRequest{Title: "调整模型标签和默认加价率", Reason: "测试审批事务", Items: inputs})
			proposal, err := bridge.ProposeApproval(ctx, rc, "admin_change", raw)
			if err != nil || proposal.IsError {
				t.Fatalf("%+v %v", proposal, err)
			}
			decision := model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: true}, {Index: 1, Approve: true}, {Index: 3, Approve: true}}}
			outcome, err := bridge.ResolveApproval(ctx, rc, "admin_change", proposal.Payload, decision)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "审核期间冲突" {
				body, _, _, _ := admin.ReadModel(ctx, "b")
				body["label"] = "人工修改"
				raw, _ := json.Marshal(body)
				if _, err := writer.SaveModel(ctx, service.ModelSaveInput{Key: "b", Body: raw, AdminID: user.ID}); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "批准后角色降级" {
				if err := db.Model(&user).Update("role", model.RoleUser).Error; err != nil {
					t.Fatal(err)
				}
			}
			run := func() (*agentsvc.ApprovalExecution, error) {
				return bridge.ExecuteApproval(ctx, rc, "admin_change", 999, "tool", *outcome)
			}
			res, err := run()
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "并发重放" {
				var wg sync.WaitGroup
				for range 2 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						if _, err := run(); err != nil {
							t.Error(err)
						}
					}()
				}
				wg.Wait()
			}
			var result agentsvc.AdminChangeResult
			if err := json.Unmarshal(res.Result, &result); err != nil {
				t.Fatal(err)
			}
			want := 3
			if scenario == "审核期间冲突" {
				want = 2
			}
			if scenario == "批准后角色降级" {
				want = 0
			}
			if len(result.AuditIDs) != want || result.AppliedBy != "admin-assistant" {
				t.Fatalf("%+v", result)
			}
			logs, err := auditRepo.ChangeAudits(ctx, outcome.AdminChange.OrderNo)
			if err != nil || len(logs) != want {
				t.Fatalf("%+v %v", logs, err)
			}
			for _, log := range logs {
				if !strings.Contains(string(log.Detail), `"via":"assistant"`) || !strings.Contains(string(log.Detail), `"approval_id":"999"`) || !strings.Contains(string(log.Detail), `"before"`) {
					t.Fatal(string(log.Detail))
				}
			}
			current, _, _, err := admin.ReadModel(ctx, "c")
			if err != nil || current["label"] != "测试模型" {
				t.Fatalf("未批准项目被修改 %+v %v", current, err)
			}
			if scenario == "审核期间冲突" {
				if result.Items[1].Status != "failed" {
					t.Fatal(result)
				}
				current, _, _, _ := admin.ReadModel(ctx, "b")
				if current["label"] != "人工修改" {
					t.Fatal(current)
				}
			}
		})
	}
}

func TestAdminAssistantRealDBAuditRollback(t *testing.T) {
	db := isolatedDB(t, &model.AIModel{}, &model.AIConfigRevision{}, &model.AIAuditLog{})
	repo := repository.NewAIConfigRepository(db)
	ctx := context.Background()
	input := repository.SaveModelInput{Pointer: repository.ConfigPointer{Target: "model", Key: "m", Kind: "video"}, Body: []byte(`{"key":"m","kind":"video","label":"before"}`)}
	if _, err := repo.SaveModel(ctx, input); err != nil {
		t.Fatal(err)
	}
	// 模拟审计表拒绝写入，配置必须跟随事务一起回滚。
	if err := db.Exec("ALTER TABLE ai_audit_logs ADD CONSTRAINT reject_assistant CHECK (action <> 'model.update')").Error; err != nil {
		t.Fatal(err)
	}
	input.BaseSHA256 = configbody.ModelSHA256(input.Body, false)
	input.Body = []byte(`{"key":"m","kind":"video","label":"after"}`)
	input.Audit = &model.AIAuditLog{Action: model.AuditModelUpdate, TargetType: "model", TargetKey: "m", DetailJSON: model.JSONText(`{"approval_id":"1","index":0}`)}
	if _, err := repo.SaveModel(ctx, input); err == nil {
		t.Fatal("审计失败必须失败")
	}
	rev, err := repo.GetModelConfig(ctx, "m")
	if err != nil || !strings.Contains(string(rev.BodyJSON), "before") {
		t.Fatalf("%s %v", rev.BodyJSON, err)
	}
}

func TestAgentRunsSceneRealDB(t *testing.T) {
	db := isolatedDB(t, &model.AgentRunDigest{})
	rows := []model.AgentRunDigest{{RunID: 1, Mode: "all"}, {RunID: 2, Mode: "admin"}, {RunID: 3, Mode: "drama"}}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewAgentObserveRepository(db)
	for _, tc := range []struct {
		scene string
		want  int
	}{{"", 2}, {"canvas", 2}, {"admin", 1}} {
		rows, _, err := repo.ListDigests(context.Background(), model.DigestQuery{Scene: tc.scene})
		if err != nil || len(rows) != tc.want {
			t.Fatalf("%s %+v %v", tc.scene, rows, err)
		}
		n, err := repo.CountDigests(context.Background(), model.DigestQuery{Scene: tc.scene})
		if err != nil || n != int64(tc.want) {
			t.Fatalf("count %d %v", n, err)
		}
	}
}

func TestAdminRecentUsageRealDB(t *testing.T) {
	db := isolatedDB(t, &model.GenerationTask{}, &model.AgentModelCall{})
	if err := db.Create(&model.AgentModelCall{ModelKey: "a", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	calls, err := repository.NewAdminAssistantRepository(db).RecentModelUsage(context.Background(), time.Now().Add(-7*24*time.Hour))
	if err != nil || calls["a"] != 1 {
		t.Fatalf("%+v %v", calls, err)
	}
}

func TestAdminBillingRealDBNoLedgerCharge(t *testing.T) {
	db := billingDB(t)
	openAccount(t, db, 7, 100, 0)
	ctx := context.Background()
	repo := repository.NewAgentRepository(db)
	billing := agentsvc.NewAgentBilling(repo)
	run := &model.AgentRun{ID: 77, UserID: 7, Mode: "admin", BudgetCredits: 1}
	pricing := modelcfg.Pricing{Billing: "token", Token: &modelcfg.TokenPrice{In: 1000, Out: 1000}}
	if err := billing.Preflight(ctx, run, pricing, modelcfg.Capabilities{}, 1_000_000); err != nil {
		t.Fatal(err)
	}
	call, err := billing.Begin(ctx, run, "agent-model")
	if err != nil {
		t.Fatal(err)
	}
	out := agentsvc.CallOutcome{Meta: agentsvc.CallMeta{Mode: "admin"}, Result: &llmgateway.Result{Usage: llmgateway.Usage{InputTokens: 1_000_000, OutputTokens: 100_000}}}
	if res, err := billing.SettleExternal(ctx, call, pricing, out); err != nil || res.Charged != 0 || res.Credits != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	var account model.UserCredit
	if err := db.First(&account, "user_id = ?", 7).Error; err != nil || account.Balance != 100 {
		t.Fatalf("%+v %v", account, err)
	}
	var row model.AgentModelCall
	if err := db.First(&row, call.ID).Error; err != nil || row.InputTokens != 1_000_000 || row.OutputTokens != 100_000 || row.Charged != 0 {
		t.Fatalf("%+v %v", row, err)
	}
	var n int64
	if err := db.Model(&model.CreditLedger{}).Where("agent_call_id = ?", call.ID).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("不应写扣费流水 %d %v", n, err)
	}
}

func TestAdminPricingRollbackUnsetRealDB(t *testing.T) {
	db := isolatedDB(t, &model.User{}, &model.SystemSetting{}, &model.AdminAuditLog{}, &model.AIAuditLog{})
	ctx := context.Background()
	user := &model.User{Username: "pricing-admin", Password: "test", Role: model.RoleAdmin}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	settings := service.NewPricingSettingsService(repository.NewSystemSettingRepository(db), nil)
	admin := agentsvc.NewAdminAssistantService(agentsvc.AdminAssistantDeps{Users: repository.NewUserRepository(db), Settings: settings, Audit: repository.NewAdminAssistantRepository(db)})
	b := agentsvc.NewAgentBridge(agentsvc.BridgeDeps{Admin: admin, DramaApprovalKey: []byte("persistent-test-key")})
	rc := agentsvc.RunContext{RunID: 1, UserID: user.ID, SessionID: 1, Mode: "admin"}
	view, _ := settings.Get(ctx)
	raw, _ := json.Marshal(agentsvc.AdminChangeRequest{Title: "设置定价", Reason: "用户要求", Items: []agentsvc.AdminChangeInput{{Target: "pricing_settings", Key: "pricing", BaseSHA256: configbody.SHA256(service.PricingSettingsBody(view)), Patch: map[string]any{"credits_per_yuan": 7.5, "default_markup_pct": 35}}}})
	p, err := b.ProposeApproval(ctx, rc, "admin_change", raw)
	if err != nil || p.IsError {
		t.Fatalf("%+v %v", p, err)
	}
	o, err := b.ResolveApproval(ctx, rc, "admin_change", p.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := b.ExecuteApproval(ctx, rc, "admin_change", 11, "tool", *o); err != nil || res.Status != "executed" {
		t.Fatalf("%+v %v", res, err)
	}
	raw, _ = json.Marshal(map[string]any{"revert_of": o.AdminChange.OrderNo})
	p, err = b.ProposeApproval(ctx, rc, "admin_change", raw)
	if err != nil || p.IsError {
		t.Fatalf("%+v %v", p, err)
	}
	o, err = b.ResolveApproval(ctx, rc, "admin_change", p.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := b.ExecuteApproval(ctx, rc, "admin_change", 12, "tool", *o); err != nil || res.Status != "executed" {
		t.Fatalf("%+v %v", res, err)
	}
	view, err = settings.Get(ctx)
	if err != nil || view.CreditsPerYuan != nil || view.DefaultMarkupPct != 30 {
		t.Fatalf("%+v %v", view, err)
	}
}
