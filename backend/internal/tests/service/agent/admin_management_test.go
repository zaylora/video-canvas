package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"

	. "video-canvas/internal/service/agent"
)

type managementSettings struct {
	values map[string]string
	writes int
}

func (f *managementSettings) GetAll(context.Context) (map[string]string, error) { return f.values, nil }
func (f *managementSettings) SetMany(_ context.Context, v map[string]string, _ uint64) error {
	f.writes++
	for k, value := range v {
		f.values[k] = value
	}
	return nil
}

type managementSMTP struct {
	row    model.SMTPSetting
	writes int
}

func (f *managementSMTP) Get(context.Context) (*model.SMTPSetting, error) {
	row := f.row
	return &row, nil
}
func (f *managementSMTP) SaveConfig(_ context.Context, row *model.SMTPSetting) error {
	f.writes++
	f.row = *row
	return nil
}
func (f *managementSMTP) SetPassword(context.Context, []byte, []byte, uint64) error {
	panic("助手不得设置密码")
}
func (f *managementSMTP) RecordCheck(context.Context, bool, string, time.Time) error { return nil }

type managementMail struct {
	calls int
	fail  bool
}

func (f *managementMail) Send(context.Context, model.MailConfig, model.MailMessage) error {
	f.calls++
	if f.fail {
		return errors.New("发送失败")
	}
	return nil
}

type managementOperations struct {
	f     *adminFake
	calls int
	seen  map[string]bool
}

func (m *managementOperations) ExecuteAdminOperation(_ context.Context, audit *model.AIAuditLog, run func() error) (uint64, error) {
	var detail map[string]any
	_ = json.Unmarshal(audit.DetailJSON, &detail)
	key := fmt.Sprint(detail["approval_id"], ":", detail["index"])
	if m.seen[key] {
		return 0, repository.ErrAdminOperationUncertain
	}
	m.seen[key] = true
	m.calls++
	if err := run(); err != nil {
		return 0, err
	}
	detail["state"] = "applied"
	raw, _ := json.Marshal(detail)
	id := uint64(m.calls)
	m.f.audits = append(m.f.audits, repository.AssistantAuditRecord{ID: id, Target: audit.TargetType, Key: audit.TargetKey, Detail: model.JSONText(raw)})
	return id, nil
}

type managementEnv struct {
	d        *AdminManagementDeps
	f        *adminFake
	settings *managementSettings
	smtp     *managementSMTP
	mail     *managementMail
	ops      *managementOperations
	b        *AgentBridge
	rc       RunContext
}

func newManagementEnv() managementEnv {
	f := newAdminFake()
	f.user.Role = model.RoleSuperAdmin
	settings := &managementSettings{values: map[string]string{}}
	smtp := &managementSMTP{row: model.SMTPSetting{Host: "smtp.example.com", Port: 587, Encryption: "starttls", FromAddress: "admin@example.com", PasswordEnc: []byte("DO_NOT_EXPOSE"), PasswordNonce: []byte("PRIVATE")}}
	mail := &managementMail{}
	ops := &managementOperations{f: f, seen: map[string]bool{}}
	d := &AdminManagementDeps{Register: service.NewSettingsService(settings, nil), SMTP: service.NewSMTPService(smtp, mail, nil, "", func(context.Context, string) error { return nil }), Operations: ops}
	svc := NewAdminAssistantService(AdminAssistantDeps{Users: f, Audit: f, Settings: service.NewPricingSettingsService(f, nil), Management: d})
	b := NewAgentBridge(BridgeDeps{Admin: svc, DramaApprovalKey: []byte("management-test-signature")})
	return managementEnv{d, f, settings, smtp, mail, ops, b, RunContext{RunID: 1, SessionID: 1, UserID: 1, Mode: "admin"}}
}
func (e managementEnv) read(t *testing.T, target, key string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"target": target, "key": key})
	res, err := e.b.ExecutePlatformTool(context.Background(), e.rc, "read", "admin_get_resource", raw)
	if err != nil || res.IsError {
		t.Fatalf("read: %+v %v", res, err)
	}
	if strings.Contains(res.Content, "DO_NOT_EXPOSE") || strings.Contains(res.Content, "PRIVATE") {
		t.Fatal("泄露凭证")
	}
	var v struct {
		SHA string `json:"body_sha256"`
	}
	_ = json.Unmarshal([]byte(res.Content), &v)
	return v.SHA
}
func (e managementEnv) propose(t *testing.T, target, key string, patch map[string]any) *ApprovalProposal {
	t.Helper()
	sha := e.read(t, target, key)
	raw, _ := json.Marshal(AdminChangeRequest{Title: "调整后台", Reason: "用户要求", Items: []AdminChangeInput{{Target: target, Key: key, BaseSHA256: sha, Patch: patch}}})
	p, err := e.b.ProposeApproval(context.Background(), e.rc, "admin_change", raw)
	if err != nil || p.IsError {
		t.Fatalf("propose: %+v %v", p, err)
	}
	return p
}
func (e managementEnv) execute(t *testing.T, p *ApprovalProposal, decision string) *ApprovalExecution {
	t.Helper()
	out, err := e.b.ResolveApproval(context.Background(), e.rc, "admin_change", p.Payload, model.DecideAgentApprovalReq{Decision: decision})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.b.ExecuteApproval(context.Background(), e.rc, "admin_change", 77, "tool", *out)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAdminManagementApprovalAndReplay(t *testing.T) {
	for _, scenario := range []string{"批准", "拒绝", "降权", "审核中改值", "重复执行", "签名篡改"} {
		t.Run(scenario, func(t *testing.T) {
			e := newManagementEnv()
			p := e.propose(t, "register_settings", "register", map[string]any{"initial_credits": float64(80)})
			if e.settings.writes != 0 || e.ops.calls != 0 {
				t.Fatal("提案前产生副作用")
			}
			if scenario == "签名篡改" {
				p.Payload = json.RawMessage(strings.Replace(string(p.Payload), `"initial_credits":80`, `"initial_credits":999`, 1))
				_, err := e.b.ResolveApproval(context.Background(), e.rc, "admin_change", p.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
				if err == nil {
					t.Fatal("篡改审批被接受")
				}
				return
			}
			if scenario == "降权" {
				e.f.user.Role = model.RoleAdmin
			}
			if scenario == "审核中改值" {
				e.settings.values[model.SettingInitialCredits] = "60"
			}
			decision := "approve"
			if scenario == "拒绝" {
				decision = "reject"
			}
			res := e.execute(t, p, decision)
			wantWrite := scenario == "批准" || scenario == "重复执行"
			if (e.settings.writes == 1) != wantWrite {
				t.Fatalf("writes=%d result=%+v", e.settings.writes, res)
			}
			if wantWrite && res.Status != "executed" {
				t.Fatalf("%+v", res)
			}
			if scenario == "重复执行" {
				e.execute(t, p, "approve")
				if e.settings.writes != 1 || e.ops.calls != 1 {
					t.Fatal("重复写入")
				}
			}
			if scenario == "审核中改值" && !strings.Contains(string(res.Result), "已被他人修改") {
				t.Fatalf("%s", res.Result)
			}
		})
	}
}

func TestAdminManagementPreflight(t *testing.T) {
	for _, tc := range []struct {
		target, key string
		patch       map[string]any
	}{
		{"register_settings", "register", map[string]any{"initial_credits": -1}},
		{"register_settings", "register", map[string]any{"default_max_active_tasks": 100}},
		{"register_settings", "register", map[string]any{"register_enabled": "false"}},
		{"register_settings", "register", map[string]any{"password": "DO_NOT_EXPOSE"}},
		{"smtp_settings", "smtp", map[string]any{"action": "test", "to": "not-email"}},
		{"smtp_settings", "smtp", map[string]any{"port": 70000}},
		{"smtp_settings", "smtp", map[string]any{"password": "DO_NOT_EXPOSE"}},
		{"smtp_settings", "smtp", map[string]any{"action": "delete"}},
	} {
		t.Run(tc.target+fmt.Sprint(tc.patch), func(t *testing.T) {
			e := newManagementEnv()
			sha := e.read(t, tc.target, tc.key)
			raw, _ := json.Marshal(AdminChangeRequest{Title: "调整", Reason: "用户要求", Items: []AdminChangeInput{{Target: tc.target, Key: tc.key, BaseSHA256: sha, Patch: tc.patch}}})
			p, err := e.b.ProposeApproval(context.Background(), e.rc, "admin_change", raw)
			if err != nil || !p.IsError || len(p.Payload) != 0 {
				t.Fatalf("非法提案未被阻止：%+v %v", p, err)
			}
			if strings.Contains(p.Content, "DO_NOT_EXPOSE") {
				t.Fatal("错误返回泄露凭证值")
			}
			if e.settings.writes != 0 || e.smtp.writes != 0 || e.mail.calls != 0 {
				t.Fatal("预检产生副作用")
			}
		})
	}
}

func TestAdminManagementSMTPUpdateAndTest(t *testing.T) {
	for _, action := range []string{"update", "test"} {
		t.Run(action, func(t *testing.T) {
			e := newManagementEnv()
			if action == "test" {
				e.smtp.row.PasswordEnc = nil
				e.smtp.row.PasswordNonce = nil
			}
			patch := map[string]any{"from_name": "连镜"}
			if action == "test" {
				patch = map[string]any{"action": "test", "to": "receiver@example.com"}
			}
			p := e.propose(t, "smtp_settings", "smtp", patch)
			if e.mail.calls != 0 || e.smtp.writes != 0 {
				t.Fatal("未审批就发送或保存")
			}
			res := e.execute(t, p, "approve")
			if res.Status != "executed" {
				t.Fatalf("%s", res.Result)
			}
			if action == "update" && (e.smtp.writes != 1 || e.smtp.row.FromName != "连镜") {
				t.Fatal("未保存配置")
			}
			if action == "test" && e.mail.calls != 1 {
				t.Fatal("未发送测试邮件")
			}
			e.execute(t, p, "approve")
			if e.mail.calls > 1 || e.smtp.writes > 1 {
				t.Fatal("重放产生重复副作用")
			}
		})
	}
}

func TestAdminManagementPermissionAndBadInput(t *testing.T) {
	for _, tc := range []struct{ name, args string }{{"admin_query", `{"resource":"queue","page_size":101}`}, {"admin_query", `{"resource":"payment"}`}, {"admin_query", `[]`}, {"admin_get_resource", `{"target":"storage","key":"invalid"}`}, {"admin_get_resource", `{"target":"register_settings","key":"wrong"}`}} {
		e := newManagementEnv()
		res, err := e.b.ExecutePlatformTool(context.Background(), e.rc, "bad", tc.name, []byte(tc.args))
		if err != nil || !res.IsError {
			t.Fatalf("非法参数: %+v %v", res, err)
		}
	}
	e := newManagementEnv()
	e.f.user.Role = model.RoleUser
	_, err := e.b.ExecutePlatformTool(context.Background(), e.rc, "read", "admin_get_resource", []byte(`{"target":"register_settings","key":"register"}`))
	var business *errcode.Error
	if !errors.As(err, &business) || business.Code != errcode.ErrForbidden.Code {
		t.Fatalf("非管理员未被拒绝：%v", err)
	}
}

func TestAdminManagementToolsScope(t *testing.T) {
	for _, mode := range []string{"admin", "all", "drama"} {
		decls := map[string]bool{}
		for _, d := range AgentToolDecls(mode, false, false) {
			decls[d.Name] = true
		}
		for _, name := range []string{"admin_query", "admin_get_resource"} {
			if decls[name] != (mode == "admin") {
				t.Errorf("mode=%s tool=%s available=%v", mode, name, decls[name])
			}
		}
	}
}
