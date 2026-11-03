package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/configbody"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	. "video-canvas/internal/service/agent"
)

type adminFake struct {
	bodies    map[string]json.RawMessage
	live      map[string]bool
	user      model.User
	audits    []repository.AssistantAuditRecord
	writes    int
	enableErr error
	settings  map[string]string
}

func newAdminFake() *adminFake {
	f := &adminFake{bodies: map[string]json.RawMessage{}, live: map[string]bool{}, user: model.User{Username: "运营", Role: model.RoleAdmin}, settings: map[string]string{model.SettingPricingCreditsPerYuan: "7.5", model.SettingPricingFXOverride: "6.6529"}}
	for _, key := range []string{"a", "b", "c"} {
		f.bodies[key] = json.RawMessage(fmt.Sprintf(`{"key":%q,"kind":"video","label":"测试模型","channels":[{"channel":"main","upstream_model":"test","protocol":{"plugin":"test","version":"1.0.0"}}],"capabilities":{"ops":["t2v"],"prompt":{"max_length":2000}},"pricing":{"billing":"per_call","unit":2,"cost":{"on":true,"unit":1}}}`, key))
	}
	return f
}
func (f *adminFake) LoadModels(context.Context) ([]repository.PublishedModel, error) {
	out := []repository.PublishedModel{}
	for _, key := range []string{"a", "b", "c"} {
		out = append(out, repository.PublishedModel{Key: key, Kind: "video", Enabled: f.live[key], Body: model.JSONText(f.bodies[key])})
	}
	return out, nil
}
func (f *adminFake) ListSecrets(context.Context) ([]model.AISecret, error) { return nil, nil }
func (f *adminFake) RecentModelUsage(context.Context, time.Time) (map[string]int64, error) {
	return map[string]int64{}, nil
}
func (f *adminFake) GetModelConfig(_ context.Context, key string) (*model.AIConfigRevision, error) {
	b, ok := f.bodies[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &model.AIConfigRevision{BodyJSON: model.JSONText(b)}, nil
}
func (f *adminFake) GetModelPointer(_ context.Context, key string) (*model.AIModel, error) {
	return &model.AIModel{Key: key, Enabled: f.live[key]}, nil
}
func (f *adminFake) GetByID(context.Context, uint64) (*model.User, error) {
	u := f.user
	return &u, nil
}
func (f *adminFake) ListChannels(context.Context) ([]model.AIChannel, error) {
	return []model.AIChannel{{Key: "main", HeadersJSON: model.JSONText(`{"Authorization":"SECRET"}`)}}, nil
}
func (f *adminFake) ChangeAudits(_ context.Context, order string) ([]repository.AssistantAuditRecord, error) {
	out := []repository.AssistantAuditRecord{}
	for _, row := range f.audits {
		if strings.Contains(string(row.Detail), order) {
			out = append(out, row)
		}
	}
	return out, nil
}
func (f *adminFake) CheckEnable(context.Context, string, []byte) error { return f.enableErr }
func (f *adminFake) SaveModel(_ context.Context, in service.ModelSaveInput) (*service.SaveModelResult, error) {
	if configbody.ModelSHA256(f.bodies[in.Key], f.live[in.Key]) != in.BaseSHA256 {
		return nil, repository.ErrRevisionConflict
	}
	if f.live[in.Key] && f.enableErr != nil {
		return nil, f.enableErr
	}
	f.bodies[in.Key] = in.Body
	f.live[in.Key] = *in.Enabled
	f.writes++
	in.Audit.ID = uint64(f.writes)
	f.audits = append(f.audits, repository.AssistantAuditRecord{ID: in.Audit.ID, Target: "model", Key: in.Key, Detail: in.Audit.DetailJSON})
	return &service.SaveModelResult{Key: in.Key}, nil
}
func (f *adminFake) GetAll(context.Context) (map[string]string, error) { return f.settings, nil }
func (f *adminFake) WithPricingTx(context.Context, func(repository.PricingTx) error) error {
	return fmt.Errorf("测试未使用定价写入")
}

func adminEnv(f *adminFake) (*AdminAssistantService, *AgentBridge, RunContext) {
	svc := NewAdminAssistantService(AdminAssistantDeps{Models: f, Writer: f, Users: f, Channels: f, Audit: f, Settings: service.NewPricingSettingsService(f, nil)})
	bridge := NewAgentBridge(BridgeDeps{Admin: svc, DramaApprovalKey: []byte("stable-test-service-key")})
	return svc, bridge, RunContext{RunID: 1, UserID: 1, SessionID: 1, Mode: "admin"}
}
func relativeInput(f *adminFake, key string) AdminChangeInput {
	return AdminChangeInput{Target: "model", Key: key, BaseSHA256: configbody.ModelSHA256(f.bodies[key], f.live[key]), Patch: map[string]any{"pricing": map[string]any{"unit": float64(3)}}, PricingBasis: []PricingBasis{{Path: "pricing.unit", Mode: "relative", From: 2, Pct: 10, Price: 3}}}
}
func proposeAdmin(t *testing.T, b *AgentBridge, rc RunContext, inputs ...AdminChangeInput) (*ApprovalProposal, AdminChangePayload) {
	t.Helper()
	raw, _ := json.Marshal(AdminChangeRequest{Title: "视频模型涨价", Reason: "按用户要求涨价", Items: inputs})
	p, err := b.ProposeApproval(context.Background(), rc, "admin_change", raw)
	if err != nil || p.IsError {
		t.Fatalf("提案失败：%+v %v", p, err)
	}
	var body AdminChangePayload
	if err := json.Unmarshal(p.Payload, &body); err != nil {
		t.Fatal(err)
	}
	return p, body
}

func TestAdminChangeStatusAndPricing(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		change       func(*adminFake, *AdminChangeInput)
	}{
		{"正常相对调价", "ok", func(_ *adminFake, _ *AdminChangeInput) {}},
		{"快照过期", "stale", func(_ *adminFake, in *AdminChangeInput) { in.BaseSHA256 = "old" }},
		{"禁止改渠道", "invalid", func(_ *adminFake, in *AdminChangeInput) { in.Patch["channels"] = nil }},
		{"schema 校验失败", "invalid", func(_ *adminFake, in *AdminChangeInput) { in.Patch["label"] = float64(1) }},
		{"已启用模型检查失败", "invalid", func(f *adminFake, in *AdminChangeInput) {
			f.live["a"] = true
			in.BaseSHA256 = configbody.ModelSHA256(f.bodies["a"], true)
			f.enableErr = errcode.ErrChannelDisabled
		}},
		{"价格复算不符", "mismatch", func(_ *adminFake, in *AdminChangeInput) { in.PricingBasis[0].Price = 4 }},
		{"缺少依据", "invalid", func(_ *adminFake, in *AdminChangeInput) { in.PricingBasis = nil }},
		{"删除售价也必须有依据", "invalid", func(_ *adminFake, in *AdminChangeInput) {
			in.Patch = map[string]any{"pricing": map[string]any{"unit": nil}}
			in.PricingBasis = nil
		}},
		{"成本换算精度", "ok", func(_ *adminFake, in *AdminChangeInput) {
			in.Patch = map[string]any{"pricing": map[string]any{"unit": float64(10), "cost": map[string]any{"on": true, "unit": float64(7)}}}
			in.PricingBasis = []PricingBasis{{Path: "pricing.unit", Mode: "cost", Upstream: &PricingUpstream{Amount: 0.14, Currency: "USD", Unit: "次", SourceURL: "https://official.example/pricing"}, MarkupPct: 30, Price: 10}}
		}},
		{"美元汇率缺失", "invalid", func(f *adminFake, in *AdminChangeInput) {
			f.settings[model.SettingPricingFXOverride] = ""
			in.PricingBasis = []PricingBasis{{Path: "pricing.unit", Mode: "cost", Upstream: &PricingUpstream{Amount: 0.14, Currency: "USD", Unit: "次", SourceURL: "https://official.example"}, Price: 3}}
		}},
		{"整数边界先六位舍入", "ok", func(_ *adminFake, in *AdminChangeInput) { in.PricingBasis[0].Pct = 50; in.PricingBasis[0].Price = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdminFake()
			in := relativeInput(f, "a")
			tc.change(f, &in)
			s, _, _ := adminEnv(f)
			p, err := s.BuildChanges(context.Background(), AdminChangeRequest{Title: "改价", Reason: "要求", Items: []AdminChangeInput{in}})
			if err != nil {
				t.Fatal(err)
			}
			if p.Items[0].Status != tc.status {
				t.Fatalf("want %s got %+v", tc.status, p.Items[0])
			}
			if tc.name == "成本换算精度" && p.Items[0].Basis[0].Cost != 7 {
				t.Fatal(p.Items[0].Basis)
			}
		})
	}
}

func TestAdminChangeApprovePartialConflictRoleAudit(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		decision   model.DecideAgentApprovalReq
		mutate     func(*adminFake)
	}{
		{"全部应用", "executed", model.DecideAgentApprovalReq{Decision: "approve"}, func(*adminFake) {}},
		{"部分批准", "partially_approved", model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: true}, {Index: 1, Approve: false}}}, func(*adminFake) {}},
		{"执行时冲突其他照常应用", "partially_approved", model.DecideAgentApprovalReq{Decision: "approve"}, func(f *adminFake) {
			f.bodies["a"] = json.RawMessage(strings.Replace(string(f.bodies["a"]), "测试模型", "已被修改", 1))
		}},
		{"降权全部跳过", "failed", model.DecideAgentApprovalReq{Decision: "approve"}, func(f *adminFake) { f.user.Role = model.RoleUser }},
		{"整单拒绝", "rejected", model.DecideAgentApprovalReq{Decision: "reject"}, func(*adminFake) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdminFake()
			_, b, rc := adminEnv(f)
			p, _ := proposeAdmin(t, b, rc, relativeInput(f, "a"), relativeInput(f, "b"))
			o, err := b.ResolveApproval(context.Background(), rc, "admin_change", p.Payload, tc.decision)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(f)
			res, err := b.ExecuteApproval(context.Background(), rc, "admin_change", 5, "tool", *o)
			if err != nil || res.Status != tc.want {
				t.Fatalf("%+v %v", res, err)
			}
			var result AdminChangeResult
			_ = json.Unmarshal(res.Result, &result)
			if result.AppliedBy != "运营" || len(result.Items) != 2 || len(result.AuditIDs) != f.writes {
				t.Fatalf("%+v", result)
			}
			if tc.name == "降权全部跳过" && result.Items[0].Status != "skipped" {
				t.Fatal(result)
			}
			if tc.name == "执行时冲突其他照常应用" && (result.Items[0].Status != "failed" || !strings.Contains(result.Items[0].Message, "已被他人修改")) {
				t.Fatal(result)
			}
			for _, audit := range f.audits {
				if !strings.Contains(string(audit.Detail), `"via":"assistant"`) || !strings.Contains(string(audit.Detail), `"approval_id":"5"`) || !strings.Contains(string(audit.Detail), `"before"`) {
					t.Fatal(string(audit.Detail))
				}
			}
			writes := f.writes
			if _, err := b.ExecuteApproval(context.Background(), rc, "admin_change", 5, "tool", *o); err != nil || f.writes != writes {
				t.Fatalf("重放不应重复写 %v", err)
			}
		})
	}
}

func TestAdminChangeSignatureAndRollback(t *testing.T) {
	f := newAdminFake()
	_, b, rc := adminEnv(f)
	p, body := proposeAdmin(t, b, rc, relativeInput(f, "a"))
	bad := body
	bad.Title = "伪造"
	raw, _ := json.Marshal(bad)
	if _, err := b.ResolveApproval(context.Background(), rc, "admin_change", raw, model.DecideAgentApprovalReq{Decision: "approve"}); err == nil {
		t.Fatal("篡改提案应拒绝")
	}
	o, err := b.ResolveApproval(context.Background(), rc, "admin_change", p.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	changed := *o
	changed.Status = "rejected"
	if _, err := b.ExecuteApproval(context.Background(), rc, "admin_change", 8, "tool", changed); err == nil {
		t.Fatal("篡改决定应拒绝")
	}
	if _, err := b.ExecuteApproval(context.Background(), rc, "admin_change", 8, "tool", *o); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]any{"revert_of": body.OrderNo})
	reverse, err := b.ProposeApproval(context.Background(), rc, "admin_change", raw)
	if err != nil || reverse.IsError {
		t.Fatalf("%+v %v", reverse, err)
	}
	var inverse AdminChangePayload
	_ = json.Unmarshal(reverse.Payload, &inverse)
	if inverse.RevertOf != body.OrderNo || len(inverse.Items[0].Diff) != 1 {
		t.Fatalf("%+v", inverse)
	}
	undo, err := b.ResolveApproval(context.Background(), rc, "admin_change", reverse.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.ExecuteApproval(context.Background(), rc, "admin_change", 9, "tool", *undo); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(f.bodies["a"]), `"unit":2`) {
		t.Fatal(string(f.bodies["a"]))
	}
}

func TestAdminConfigDiff(t *testing.T) {
	before := map[string]any{"pricing": map[string]any{"tiers": []any{map[string]any{"unit": float64(2)}}}, "hint": "旧"}
	after := map[string]any{"pricing": map[string]any{"tiers": []any{map[string]any{"unit": float64(3)}}}, "label": "新"}
	diff := AdminConfigDiff(before, after)
	if len(diff) != 3 || diff[0].Path != "hint" || diff[0].After != nil || diff[2].Path != "pricing.tiers[0].unit" {
		t.Fatalf("%+v", diff)
	}
}

func TestAdminReadToolsAndPrepare(t *testing.T) {
	f := newAdminFake()
	svc, _, rc := adminEnv(f)
	e := newBridgeEnv(t)
	b := NewAgentBridge(BridgeDeps{Admin: svc, Agent: e.svc, Registry: e.reg, Secrets: fakeSecrets{"channel:ch1": "test-key"}})
	rc.ModelKey = "claude"
	setup, err := b.PrepareRun(context.Background(), rc, PrepareRequest{Message: "调整价格", PageContext: json.RawMessage(`{"route":"/admin/ai/channels"}`)})
	if err != nil || !strings.Contains(setup.FirstMessage, "/admin/ai/channels") || !strings.Contains(setup.FirstMessage, "7.5") || !strings.Contains(setup.SystemPrompt, "小镜") || strings.Contains(setup.FirstMessage, "画布目录") {
		t.Fatalf("%+v %v", setup, err)
	}
	for _, tc := range []struct{ name, args string }{
		{"admin_list_models", `{"kind":"video","enabled":false,"channel":"main","q":"a"}`},
		{"admin_get_model", `{"key":"a"}`},
		{"admin_list_channels", `{}`},
		{"admin_get_pricing_settings", `{}`},
		{"admin_quote_preview", `{"key":"a","input":{"prompt":"x","op":"t2v"}}`},
	} {
		res, err := b.ExecutePlatformTool(context.Background(), rc, "tool", tc.name, json.RawMessage(tc.args))
		if err != nil || res.IsError || strings.Contains(res.Content, "SECRET") {
			t.Fatalf("%s %+v %v", tc.name, res, err)
		}
		if tc.name == "admin_quote_preview" && !strings.Contains(res.Content, `"quote_credits":2`) {
			t.Fatal(res.Content)
		}
		if tc.name == "admin_get_model" && !strings.Contains(res.Content, "body_sha256") {
			t.Fatal(res.Content)
		}
	}
	denied, err := b.ExecutePlatformTool(context.Background(), rc, "tool", "canvas_get_state", json.RawMessage(`{}`))
	if err != nil || !denied.IsError {
		t.Fatalf("%+v %v", denied, err)
	}
	f.user.Role = "user"
	if _, err := b.ExecutePlatformTool(context.Background(), rc, "tool", "admin_list_models", json.RawMessage(`{}`)); err == nil {
		t.Fatal("降权后读工具必须拒绝")
	}
}

func TestAdminBillingExemption(t *testing.T) {
	f := &fakeBillingRepo{noAccount: true}
	b := NewAgentBilling(f)
	run := billingRun(49)
	run.Mode = "admin"
	if err := b.Preflight(context.Background(), run, billingPricing, billingCaps, 1000000); err != nil {
		t.Fatal(err)
	}
	call, err := b.Begin(context.Background(), run, "model")
	if err != nil {
		t.Fatal(err)
	}
	// UsageMissing 路径仍记录输入输出用量，只把收费归零。
	out := CallOutcome{InputChars: 100, Result: nil, Meta: CallMeta{Mode: "admin"}}
	out.Result = &llmgateway.Result{UsageMissing: true, Text: "回答"}
	res, err := b.Settle(context.Background(), run, call, billingPricing, out)
	if err != nil || res.Charged != 0 || len(f.settled) != 1 || f.settled[0].InputTokens != 100 || f.settled[0].OutputTokens != 2 {
		t.Fatalf("%+v %+v %v", res, f.settled, err)
	}
}

func TestAdminSceneValidation(t *testing.T) {
	e := newObserveEnv(t)
	for _, scene := range []string{"", "canvas", "admin"} {
		if _, err := e.svc.ListRuns(context.Background(), ObserveActor{UserID: 1}, &RunListFilter{Scene: scene}); err != nil {
			t.Fatal(err)
		}
		if e.repo.listQuery.Scene != scene {
			t.Fatal(e.repo.listQuery)
		}
	}
	if _, err := e.svc.ListRuns(context.Background(), ObserveActor{}, &RunListFilter{Scene: "typo"}); err == nil {
		t.Fatal("非法场景应拒绝")
	}
}
