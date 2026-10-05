package service_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/netguard"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// ---------- 注册设置 ----------

func TestSettingsService_RegisterSettings(t *testing.T) {
	ctx := context.Background()

	t.Run("库里没有值时回落代码常量（并发 4、初始积分 50），注册开关默认开", func(t *testing.T) {
		svc := NewSettingsService(newFakeSettingsRepo(), &fakeAudit{})
		v, err := svc.RegisterSettings(ctx)
		if err != nil || !v.RegisterEnabled || v.InitialCredits != DefaultInitialCredits || v.DefaultMaxActiveTasks != DefaultMaxActiveTasks {
			t.Fatalf("%+v %v", v, err)
		}
		if v.InitialCredits != 50 || v.DefaultMaxActiveTasks != 4 {
			t.Fatalf("常量应为初始积分 50、并发 4：%+v", v)
		}
	})
	t.Run("库值优先；非法的库值被忽略", func(t *testing.T) {
		repo := newFakeSettingsRepo()
		repo.kv = map[string]string{model.SettingRegisterEnabled: "false", model.SettingInitialCredits: "8", model.SettingDefaultMaxActiveTasks: "abc"}
		svc := NewSettingsService(repo, &fakeAudit{})
		v, _ := svc.RegisterSettings(ctx)
		if v.RegisterEnabled || v.InitialCredits != 8 || v.DefaultMaxActiveTasks != DefaultMaxActiveTasks {
			t.Fatalf("%+v", v)
		}
	})
	t.Run("读取失败透传", func(t *testing.T) {
		repo := newFakeSettingsRepo()
		repo.getErr = errBoom
		if _, err := NewSettingsService(repo, &fakeAudit{}).RegisterSettings(ctx); !errors.Is(err, errBoom) {
			t.Fatalf("%v", err)
		}
	})
}

func TestSettingsService_UpdateRegisterSettings(t *testing.T) {
	ctx := context.Background()

	t.Run("成功：写库并写审计（含前后值）", func(t *testing.T) {
		repo, audit := newFakeSettingsRepo(), &fakeAudit{}
		svc := NewSettingsService(repo, audit)
		v, err := svc.UpdateRegisterSettings(ctx, 7, &model.RegisterSettingsView{RegisterEnabled: false, InitialCredits: 0, DefaultMaxActiveTasks: 5})
		if err != nil || v.RegisterEnabled || v.DefaultMaxActiveTasks != 5 {
			t.Fatalf("%+v %v", v, err)
		}
		if repo.kv[model.SettingRegisterEnabled] != "false" || repo.kv[model.SettingInitialCredits] != "0" || repo.kv[model.SettingDefaultMaxActiveTasks] != "5" || repo.by != 7 {
			t.Fatalf("库值不对：%v by=%d", repo.kv, repo.by)
		}
		if got := audit.actions(); len(got) != 1 || got[0] != model.AdminAuditSettingsReg || audit.logs[0].ActorID != 7 {
			t.Fatalf("审计不对：%+v", audit.logs)
		}
	})
	t.Run("参数越界返回 ErrInvalidParams，不写库不写审计", func(t *testing.T) {
		for _, req := range []*model.RegisterSettingsView{
			{InitialCredits: -1, DefaultMaxActiveTasks: 2},
			{InitialCredits: 1, DefaultMaxActiveTasks: 0},
			{InitialCredits: 1, DefaultMaxActiveTasks: 65},
		} {
			repo, audit := newFakeSettingsRepo(), &fakeAudit{}
			if _, err := NewSettingsService(repo, audit).UpdateRegisterSettings(ctx, 1, req); codeOf(err) != errcode.ErrInvalidParams.Code {
				t.Fatalf("%+v 应 10001：%v", req, err)
			}
			if len(repo.kv) != 0 || len(audit.logs) != 0 {
				t.Fatal("不应有写入")
			}
		}
	})
	t.Run("写库失败透传", func(t *testing.T) {
		repo := newFakeSettingsRepo()
		repo.setErr = errBoom
		_, err := NewSettingsService(repo, &fakeAudit{}).UpdateRegisterSettings(ctx, 1, &model.RegisterSettingsView{InitialCredits: 1, DefaultMaxActiveTasks: 2})
		if !errors.Is(err, errBoom) {
			t.Fatalf("%v", err)
		}
	})
}

// ---------- SMTP ----------

type fakeSMTPRepo struct {
	row      *model.SMTPSetting
	checkOK  *bool
	checkMsg string
	getErr   error
}

func (r *fakeSMTPRepo) Get(context.Context) (*model.SMTPSetting, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if r.row == nil {
		return nil, repository.ErrNotFound
	}
	cp := *r.row
	return &cp, nil
}

func (r *fakeSMTPRepo) SaveConfig(_ context.Context, s *model.SMTPSetting) error {
	if r.row == nil {
		r.row = &model.SMTPSetting{}
	}
	r.row.Host, r.row.Port, r.row.Encryption, r.row.Username = s.Host, s.Port, s.Encryption, s.Username
	r.row.FromAddress, r.row.FromName, r.row.Enabled, r.row.UpdatedBy = s.FromAddress, s.FromName, s.Enabled, s.UpdatedBy
	return nil
}

func (r *fakeSMTPRepo) SetPassword(_ context.Context, enc, nonce []byte, by uint64) error {
	if r.row == nil {
		r.row = &model.SMTPSetting{}
	}
	r.row.PasswordEnc, r.row.PasswordNonce, r.row.UpdatedBy = enc, nonce, by
	return nil
}

func (r *fakeSMTPRepo) RecordCheck(_ context.Context, ok bool, msg string, at time.Time) error {
	r.checkOK, r.checkMsg = &ok, msg
	if r.row != nil {
		r.row.LastCheckOK, r.row.LastCheckError, r.row.LastCheckAt = &ok, msg, &at
	}
	return nil
}

type fakeMailer struct {
	err  error
	cfgs []model.MailConfig
	msgs []model.MailMessage
}

func (m *fakeMailer) Send(_ context.Context, cfg model.MailConfig, msg model.MailMessage) error {
	m.cfgs, m.msgs = append(m.cfgs, cfg), append(m.msgs, msg)
	return m.err
}

// publicHostChecker 模拟公网解析：只拒绝内网字面量与名为 internal.* 的域名，其余放行；真实的 IP 判断由 netguard 的测试覆盖。
func publicHostChecker(_ context.Context, host string) error {
	if ip := net.ParseIP(host); ip != nil && netguard.IsBlockedIP(ip) {
		return netguard.ErrBlockedAddress
	}
	if strings.HasPrefix(host, "internal.") {
		return netguard.ErrBlockedAddress
	}
	if strings.HasPrefix(host, "nx.") {
		return errors.New("no such host")
	}
	return nil
}

const testSecretKey = "unit-test-ai-secret-key"

func newSMTPSvc(repo *fakeSMTPRepo, mailer *fakeMailer, audit *fakeAudit, secretKey string) *SMTPService {
	return NewSMTPService(repo, mailer, audit, secretKey, publicHostChecker)
}

func validSMTPReq() *model.UpdateSMTPSettingsReq {
	return &model.UpdateSMTPSettingsReq{Host: "smtp.example.com", Port: 587, Encryption: "starttls", Username: "mailer", FromAddress: "noreply@example.com", FromName: "视频画布", Enabled: true}
}

func TestSMTPService_Get(t *testing.T) {
	ctx := context.Background()
	t.Run("未配置：返回空视图（has_password=false）", func(t *testing.T) {
		v, err := newSMTPSvc(&fakeSMTPRepo{}, &fakeMailer{}, &fakeAudit{}, testSecretKey).Get(ctx)
		if err != nil || v.HasPassword || v.Enabled || v.Encryption != model.SMTPEncStartTLS {
			t.Fatalf("%+v %v", v, err)
		}
	})
	t.Run("已配置：只给 has_password，不含任何密码字段", func(t *testing.T) {
		repo := &fakeSMTPRepo{row: &model.SMTPSetting{Host: "h", Port: 25, PasswordEnc: []byte("secret-ct"), PasswordNonce: []byte("n"), Encryption: "none"}}
		v, err := newSMTPSvc(repo, &fakeMailer{}, &fakeAudit{}, testSecretKey).Get(ctx)
		if err != nil || !v.HasPassword || v.Host != "h" {
			t.Fatalf("%+v %v", v, err)
		}
	})
	t.Run("读取失败透传", func(t *testing.T) {
		if _, err := newSMTPSvc(&fakeSMTPRepo{getErr: errBoom}, &fakeMailer{}, &fakeAudit{}, testSecretKey).Get(ctx); !errors.Is(err, errBoom) {
			t.Fatalf("%v", err)
		}
	})
}

func TestSMTPService_Update(t *testing.T) {
	ctx := context.Background()
	t.Run("成功：保存并写审计（审计里没有密码）", func(t *testing.T) {
		repo, audit := &fakeSMTPRepo{}, &fakeAudit{}
		svc := newSMTPSvc(repo, &fakeMailer{}, audit, testSecretKey)
		v, err := svc.Update(ctx, 9, validSMTPReq())
		if err != nil || v.Host != "smtp.example.com" || !v.Enabled {
			t.Fatalf("%+v %v", v, err)
		}
		if repo.row.UpdatedBy != 9 {
			t.Fatalf("updated_by 不对：%+v", repo.row)
		}
		if got := audit.actions(); len(got) != 1 || got[0] != model.AdminAuditSettingsSMTP {
			t.Fatalf("审计不对：%v", got)
		}
		if strings.Contains(strings.ToLower(string(audit.logs[0].DetailJSON)), "password") {
			t.Fatalf("审计不应含密码字段：%s", audit.logs[0].DetailJSON)
		}
	})

	tests := []struct {
		name   string
		mutate func(*model.UpdateSMTPSettingsReq)
		want   string
	}{
		{"回环 IP 被拒", func(r *model.UpdateSMTPSettingsReq) { r.Host = "127.0.0.1" }, "内网"},
		{"私网 IP 被拒", func(r *model.UpdateSMTPSettingsReq) { r.Host = "10.0.0.8" }, "内网"},
		{"链路本地被拒（云元数据地址）", func(r *model.UpdateSMTPSettingsReq) { r.Host = "169.254.169.254" }, "内网"},
		{"域名解析到内网被拒", func(r *model.UpdateSMTPSettingsReq) { r.Host = "internal.corp.example" }, "内网"},
		{"域名解析失败被拒", func(r *model.UpdateSMTPSettingsReq) { r.Host = "nx.example.com" }, "解析"},
		{"主机带协议 / 路径被拒", func(r *model.UpdateSMTPSettingsReq) { r.Host = "smtp://smtp.example.com/x" }, "主机"},
		{"启用时缺主机", func(r *model.UpdateSMTPSettingsReq) { r.Host = "" }, "主机"},
		{"启用时端口为 0", func(r *model.UpdateSMTPSettingsReq) { r.Port = 0 }, "端口"},
		{"启用时发件地址不合法", func(r *model.UpdateSMTPSettingsReq) { r.FromAddress = "not-an-email" }, "发件地址"},
		{"加密方式不认识", func(r *model.UpdateSMTPSettingsReq) { r.Encryption = "ssl3" }, "加密"},
		{"用户名含换行", func(r *model.UpdateSMTPSettingsReq) { r.Username = "a\nb" }, "换行"},
		{"发件人名称含换行", func(r *model.UpdateSMTPSettingsReq) { r.FromName = "a\r\nBcc: x" }, "换行"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, audit := &fakeSMTPRepo{}, &fakeAudit{}
			req := validSMTPReq()
			tt.mutate(req)
			_, err := newSMTPSvc(repo, &fakeMailer{}, audit, testSecretKey).Update(ctx, 1, req)
			var ec *errcode.Error
			if !errors.As(err, &ec) || ec.Code != errcode.ErrSMTPInvalid.Code || !strings.Contains(ec.Msg, tt.want) {
				t.Fatalf("期望 53006 且文案含 %q：%v", tt.want, err)
			}
			if repo.row != nil || len(audit.logs) != 0 {
				t.Fatal("被拒绝时不应保存也不应写审计")
			}
		})
	}

	t.Run("未启用时允许先存一半（不强求主机 / 端口），但填了主机仍要过内网校验", func(t *testing.T) {
		repo := &fakeSMTPRepo{}
		svc := newSMTPSvc(repo, &fakeMailer{}, &fakeAudit{}, testSecretKey)
		if _, err := svc.Update(ctx, 1, &model.UpdateSMTPSettingsReq{Encryption: "tls"}); err != nil {
			t.Fatalf("空配置未启用应可保存：%v", err)
		}
		if _, err := svc.Update(ctx, 1, &model.UpdateSMTPSettingsReq{Encryption: "tls", Host: "192.168.1.1"}); codeOf(err) != errcode.ErrSMTPInvalid.Code {
			t.Fatalf("未启用也要拒绝内网：%v", err)
		}
	})

	t.Run("保存配置不会清掉已设置的密码", func(t *testing.T) {
		repo := &fakeSMTPRepo{row: &model.SMTPSetting{PasswordEnc: []byte("ct"), PasswordNonce: []byte("n")}}
		if _, err := newSMTPSvc(repo, &fakeMailer{}, &fakeAudit{}, testSecretKey).Update(ctx, 1, validSMTPReq()); err != nil {
			t.Fatal(err)
		}
		if string(repo.row.PasswordEnc) != "ct" {
			t.Fatal("密码被清掉了")
		}
	})
}

func TestSMTPService_SetPassword(t *testing.T) {
	ctx := context.Background()
	t.Run("成功：密码加密落库（密文里没有明文），审计不含密码，之后任何读取接口都读不回", func(t *testing.T) {
		repo, audit := &fakeSMTPRepo{}, &fakeAudit{}
		svc := newSMTPSvc(repo, &fakeMailer{}, audit, testSecretKey)
		if err := svc.SetPassword(ctx, 5, "  p@ss-w0rd  "); err != nil {
			t.Fatal(err)
		}
		if len(repo.row.PasswordEnc) == 0 || len(repo.row.PasswordNonce) == 0 || strings.Contains(string(repo.row.PasswordEnc), "p@ss-w0rd") {
			t.Fatalf("应存密文：%+v", repo.row)
		}
		if strings.Contains(string(audit.logs[0].DetailJSON), "p@ss") {
			t.Fatalf("审计含明文密码：%s", audit.logs[0].DetailJSON)
		}
		v, _ := svc.Get(ctx)
		if !v.HasPassword {
			t.Fatal("has_password 应为 true")
		}
	})
	t.Run("缺 APP_AI_SECRET_KEY：53006，文案提示配置密钥", func(t *testing.T) {
		err := newSMTPSvc(&fakeSMTPRepo{}, &fakeMailer{}, &fakeAudit{}, "").SetPassword(ctx, 1, "x")
		var ec *errcode.Error
		if !errors.As(err, &ec) || ec.Code != errcode.ErrSMTPInvalid.Code || !strings.Contains(ec.Msg, "APP_AI_SECRET_KEY") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("空密码 / 过长密码返回 ErrInvalidParams", func(t *testing.T) {
		svc := newSMTPSvc(&fakeSMTPRepo{}, &fakeMailer{}, &fakeAudit{}, testSecretKey)
		for _, pw := range []string{"", "   ", strings.Repeat("a", 2000)} {
			if err := svc.SetPassword(ctx, 1, pw); codeOf(err) != errcode.ErrInvalidParams.Code {
				t.Fatalf("%q 应 10001：%v", pw, err)
			}
		}
	})
}

func TestSMTPService_TestSend(t *testing.T) {
	ctx := context.Background()
	setup := func(sendErr error) (*SMTPService, *fakeSMTPRepo, *fakeMailer) {
		repo, mailer := &fakeSMTPRepo{}, &fakeMailer{err: sendErr}
		svc := newSMTPSvc(repo, mailer, &fakeAudit{}, testSecretKey)
		if _, err := svc.Update(ctx, 1, validSMTPReq()); err != nil {
			t.Fatal(err)
		}
		if err := svc.SetPassword(ctx, 1, "p@ss-w0rd"); err != nil {
			t.Fatal(err)
		}
		return svc, repo, mailer
	}

	t.Run("成功：用解密后的密码发信，记录检查成功", func(t *testing.T) {
		svc, repo, mailer := setup(nil)
		if err := svc.Test(ctx, "to@x.com"); err != nil {
			t.Fatal(err)
		}
		if len(mailer.cfgs) != 1 || mailer.cfgs[0].Password != "p@ss-w0rd" || mailer.cfgs[0].Host != "smtp.example.com" || mailer.msgs[0].To != "to@x.com" {
			t.Fatalf("发信参数不对：%+v %+v", mailer.cfgs, mailer.msgs)
		}
		if repo.checkOK == nil || !*repo.checkOK {
			t.Fatal("应记录检查成功")
		}
	})
	t.Run("失败：返回 53007，文案已脱敏（不含密码），记录失败原因", func(t *testing.T) {
		svc, repo, _ := setup(errors.New("535 auth failed for mailer:p@ss-w0rd"))
		err := svc.Test(ctx, "to@x.com")
		var ec *errcode.Error
		if !errors.As(err, &ec) || ec.Code != errcode.ErrSMTPSendFailed.Code {
			t.Fatalf("%v", err)
		}
		if strings.Contains(ec.Msg, "p@ss-w0rd") || strings.Contains(repo.checkMsg, "p@ss-w0rd") || !strings.Contains(ec.Msg, "535") {
			t.Fatalf("文案应保留原因但脱敏密码：msg=%q record=%q", ec.Msg, repo.checkMsg)
		}
		if repo.checkOK == nil || *repo.checkOK {
			t.Fatal("应记录检查失败")
		}
	})
	t.Run("过长的错误被截断", func(t *testing.T) {
		svc, _, _ := setup(errors.New(strings.Repeat("x", 1000)))
		err := svc.Test(ctx, "to@x.com")
		var ec *errcode.Error
		if !errors.As(err, &ec) || len([]rune(ec.Msg)) > 300 {
			t.Fatalf("文案应被截断：%d", len([]rune(ec.Msg)))
		}
	})
	t.Run("没配置主机：53008", func(t *testing.T) {
		svc := newSMTPSvc(&fakeSMTPRepo{}, &fakeMailer{}, &fakeAudit{}, testSecretKey)
		if err := svc.Test(ctx, "to@x.com"); codeOf(err) != errcode.ErrSMTPNotConfigured.Code {
			t.Fatalf("%v", err)
		}
	})
	t.Run("配了密码但密钥缺失无法解密：53006", func(t *testing.T) {
		repo := &fakeSMTPRepo{row: &model.SMTPSetting{Host: "smtp.example.com", Port: 25, Encryption: "none", FromAddress: "a@x.com", PasswordEnc: []byte("c"), PasswordNonce: []byte("n")}}
		if err := newSMTPSvc(repo, &fakeMailer{}, &fakeAudit{}, "").Test(ctx, "to@x.com"); codeOf(err) != errcode.ErrSMTPInvalid.Code {
			t.Fatalf("%v", err)
		}
	})
	t.Run("发信前再次校验主机（DNS 可能已被改成内网）", func(t *testing.T) {
		repo := &fakeSMTPRepo{row: &model.SMTPSetting{Host: "internal.corp.example", Port: 25, Encryption: "none", FromAddress: "a@x.com"}}
		mailer := &fakeMailer{}
		if err := newSMTPSvc(repo, mailer, &fakeAudit{}, testSecretKey).Test(ctx, "to@x.com"); codeOf(err) != errcode.ErrSMTPInvalid.Code || len(mailer.cfgs) != 0 {
			t.Fatalf("应拒绝且不发信：%v", err)
		}
	})
}

func TestSMTPService_RegisterMailAdapter(t *testing.T) {
	ctx := context.Background()
	t.Run("Enabled：未配置 / 未启用为 false，启用且有主机为 true", func(t *testing.T) {
		repo := &fakeSMTPRepo{}
		svc := newSMTPSvc(repo, &fakeMailer{}, &fakeAudit{}, testSecretKey)
		if on, err := svc.Enabled(ctx); on || err != nil {
			t.Fatalf("%v %v", on, err)
		}
		repo.row = &model.SMTPSetting{Host: "h", Enabled: false}
		if on, _ := svc.Enabled(ctx); on {
			t.Fatal("未启用应为 false")
		}
		repo.row.Enabled = true
		if on, _ := svc.Enabled(ctx); !on {
			t.Fatal("启用应为 true")
		}
	})
	t.Run("SendVerifyCode：启用后发信，正文含验证码；未启用返回 53008", func(t *testing.T) {
		repo, mailer := &fakeSMTPRepo{}, &fakeMailer{}
		svc := newSMTPSvc(repo, mailer, &fakeAudit{}, testSecretKey)
		if err := svc.SendVerifyCode(ctx, "a@x.com", "123456"); codeOf(err) != errcode.ErrSMTPNotConfigured.Code {
			t.Fatalf("%v", err)
		}
		_, _ = svc.Update(ctx, 1, validSMTPReq())
		if err := svc.SendVerifyCode(ctx, "a@x.com", "123456"); err != nil {
			t.Fatal(err)
		}
		if len(mailer.msgs) != 1 || !strings.Contains(mailer.msgs[0].Body, "123456") || mailer.msgs[0].To != "a@x.com" {
			t.Fatalf("%+v", mailer.msgs)
		}
	})
	t.Run("SendVerifyCode 发信失败：53007 且不含密码", func(t *testing.T) {
		repo, mailer := &fakeSMTPRepo{}, &fakeMailer{err: errors.New("dial tcp: i/o timeout")}
		svc := newSMTPSvc(repo, mailer, &fakeAudit{}, testSecretKey)
		_, _ = svc.Update(ctx, 1, validSMTPReq())
		if err := svc.SendVerifyCode(ctx, "a@x.com", "123456"); codeOf(err) != errcode.ErrSMTPSendFailed.Code {
			t.Fatalf("%v", err)
		}
	})
}
