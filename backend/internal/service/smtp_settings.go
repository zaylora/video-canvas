package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider/netguard"
	"video-canvas/internal/repository"
)

// SMTPSettingRepo 是 SMTP 配置的数据访问接口（真实实现是 repository.SMTPSettingRepository）。
type SMTPSettingRepo interface {
	// Get 读取配置，还没有保存过返回 repository.ErrNotFound。
	Get(ctx context.Context) (*model.SMTPSetting, error)
	// SaveConfig 保存除密码与检查结果外的配置字段，不会清掉已设置的密码。
	SaveConfig(ctx context.Context, s *model.SMTPSetting) error
	// SetPassword 保存密码密文与随机数。
	SetPassword(ctx context.Context, enc, nonce []byte, updatedBy uint64) error
	// RecordCheck 记录最近一次测试发信的结果。
	RecordCheck(ctx context.Context, ok bool, errMsg string, at time.Time) error
}

// Mailer 是发信的依赖（真实实现是 mailer.SMTPSender），测试里换成 fake。
type Mailer interface {
	// Send 用给定的 SMTP 参数发送一封纯文本邮件。
	Send(ctx context.Context, cfg model.MailConfig, msg model.MailMessage) error
}

// HostChecker 判断一个 SMTP 主机能不能连：字面量 IP 与 DNS 解析后的 IP 都不能是内网 / 回环 / 链路本地。
// 真实实现是 netguard.Config.CheckHost。
type HostChecker func(ctx context.Context, host string) error

// NewNetguardHostChecker 返回基于 netguard 默认守卫的 HostChecker。
func NewNetguardHostChecker() HostChecker {
	cfg := &netguard.Config{}
	cfg.ApplyDefaults()
	return cfg.CheckHost
}

// smtpPasswordName 是 SMTP 密码加密时的附加认证数据，防止把别处的密文拷进这一行被当成合法密码解出。
const smtpPasswordName = "smtp:password"

const (
	smtpPasswordMaxLen = 1024
	smtpErrMaxRunes    = 200 // 对外展示的发信错误最大长度
)

// smtpHostRe 限制主机名只含字母数字、点和短横线（IPv4 字面量也满足）。IPv6 字面量不支持：邮件服务商都有域名。
var smtpHostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.\-]{0,253}[A-Za-z0-9])?$`)

// SMTPService 管理邮件服务配置并负责发信。同时实现 RegisterMail。
type SMTPService struct {
	repo    SMTPSettingRepo
	mailer  Mailer
	audit   AdminAuditWriter
	cipher  *aiSecretCipher // 复用 ai_secrets 的 AES-256-GCM；主密钥为空时为 nil
	checker HostChecker
}

// NewSMTPService 创建 SMTP 服务；secretKey 是 APP_AI_SECRET_KEY（可为空，此时不能设置密码）。
func NewSMTPService(repo SMTPSettingRepo, mailer Mailer, audit AdminAuditWriter, secretKey string, checker HostChecker) *SMTPService {
	return &SMTPService{repo: repo, mailer: mailer, audit: audit, cipher: newAISecretCipher(secretKey), checker: checker}
}

// Get 返回 SMTP 配置视图。密码永远不返回，只告诉前端有没有设置。
func (s *SMTPService) Get(ctx context.Context) (*model.SMTPSettingsView, error) {
	row, err := s.repo.Get(ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return &model.SMTPSettingsView{Encryption: model.SMTPEncStartTLS}, nil
	}
	if err != nil {
		return nil, err
	}
	return &model.SMTPSettingsView{
		Host: row.Host, Port: row.Port, Encryption: row.Encryption, Username: row.Username,
		HasPassword: len(row.PasswordEnc) > 0, FromAddress: row.FromAddress, FromName: row.FromName, Enabled: row.Enabled,
		LastCheckAt: row.LastCheckAt, LastCheckOK: row.LastCheckOK, LastCheckError: row.LastCheckError,
	}, nil
}

// Update 保存 SMTP 配置（不含密码），写审计并返回最新视图。
func (s *SMTPService) Update(ctx context.Context, actorID uint64, req *model.UpdateSMTPSettingsReq) (*model.SMTPSettingsView, error) {
	// 1. 校验字段；主机必须通过内网 / 回环 / 链路本地检查（含 DNS 解析后的 IP），防止把服务器当跳板扫内网
	req.Host = strings.TrimSpace(req.Host)
	if err := s.validateConfig(ctx, req); err != nil {
		return nil, err
	}

	// 2. 保存。密码不在这里改（单独的只写接口），所以不会被覆盖
	err := s.repo.SaveConfig(ctx, &model.SMTPSetting{
		Host: req.Host, Port: req.Port, Encryption: req.Encryption, Username: strings.TrimSpace(req.Username),
		FromAddress: strings.TrimSpace(req.FromAddress), FromName: strings.TrimSpace(req.FromName), Enabled: req.Enabled, UpdatedBy: actorID,
	})
	if err != nil {
		return nil, err
	}

	// 3. 审计：记配置字段本身，不含密码
	adminAudit(ctx, s.audit, actorID, model.AdminAuditSettingsSMTP, model.AdminAuditTargetSettings, 0, map[string]any{
		"host": req.Host, "port": req.Port, "encryption": req.Encryption, "username": req.Username,
		"from_address": req.FromAddress, "enabled": req.Enabled,
	})
	return s.Get(ctx)
}

// validateConfig 校验 SMTP 配置。启用时主机、端口、发件地址必填；未启用时允许先存一半，但填了主机仍要过内网检查。
func (s *SMTPService) validateConfig(ctx context.Context, req *model.UpdateSMTPSettingsReq) error {
	invalid := func(format string, a ...any) error { return errcode.ErrSMTPInvalid.WithMsg(fmt.Sprintf(format, a...)) }
	switch req.Encryption {
	case model.SMTPEncNone, model.SMTPEncStartTLS, model.SMTPEncTLS:
	default:
		return invalid("加密方式只能是 none、starttls 或 tls")
	}
	// 邮件头相关字段不允许换行（Bcc 注入）；发信层也会再拦一次
	for _, v := range []string{req.Username, req.FromName, req.FromAddress} {
		if strings.ContainsAny(v, "\r\n") {
			return invalid("用户名、发件人名称和发件地址不能包含换行")
		}
	}
	if req.Enabled {
		switch {
		case req.Host == "":
			return invalid("启用邮件服务需要填写主机")
		case req.Port < 1 || req.Port > 65535:
			return invalid("启用邮件服务需要填写有效端口（1-65535）")
		}
	}
	if req.FromAddress != "" || req.Enabled {
		if _, err := mail.ParseAddress(req.FromAddress); err != nil {
			return invalid("发件地址不是有效的邮箱")
		}
	}
	if req.Host != "" {
		if !smtpHostRe.MatchString(req.Host) {
			return invalid("主机只能是域名或 IPv4 地址，不要带协议或路径")
		}
		if err := s.checker(ctx, req.Host); err != nil {
			return hostRejection(err)
		}
	}
	return nil
}

// hostRejection 把主机校验失败转成对用户友好的 53006。
func hostRejection(err error) error {
	if errors.Is(err, netguard.ErrBlockedAddress) {
		return errcode.ErrSMTPInvalid.WithMsg("主机不能指向内网、回环或链路本地地址")
	}
	return errcode.ErrSMTPInvalid.WithMsg("无法解析主机名，请检查拼写")
}

// SetPassword 设置（覆盖）SMTP 密码：AES-256-GCM 加密入库，只写不读，之后任何接口都读不回明文。
func (s *SMTPService) SetPassword(ctx context.Context, actorID uint64, password string) error {
	// 1. 没有主密钥就无法加密：明确报错，绝不悄悄存明文
	if s.cipher == nil {
		return errcode.ErrSMTPInvalid.WithMsg("服务端未配置 APP_AI_SECRET_KEY，无法保存密码，请先配置密钥")
	}
	// 2. 粘贴密码时常带首尾空白 / 换行，先去掉
	password = strings.TrimSpace(password)
	if password == "" || len(password) > smtpPasswordMaxLen {
		return errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("密码不能为空，且不超过 %d 字节", smtpPasswordMaxLen))
	}
	// 3. 加密并入库；审计只记“改了密码”，不记值
	ct, nonce, err := s.cipher.seal(smtpPasswordName, password)
	if err != nil {
		return fmt.Errorf("加密 SMTP 密码失败：%w", err)
	}
	if err := s.repo.SetPassword(ctx, ct, nonce, actorID); err != nil {
		return err
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditSettingsSMTP, model.AdminAuditTargetSettings, 0, map[string]any{"password_changed": true})
	return nil
}

// mailConfig 读取并解密出完整的发信参数；未配置返回 ErrSMTPNotConfigured。requireEnabled 为 true 时还要求已启用。
func (s *SMTPService) mailConfig(ctx context.Context, requireEnabled bool) (*model.MailConfig, error) {
	row, err := s.repo.Get(ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrSMTPNotConfigured
	}
	if err != nil {
		return nil, err
	}
	if row.Host == "" || (requireEnabled && !row.Enabled) {
		return nil, errcode.ErrSMTPNotConfigured
	}
	cfg := &model.MailConfig{
		Host: row.Host, Port: row.Port, Encryption: row.Encryption, Username: row.Username,
		FromAddress: row.FromAddress, FromName: row.FromName,
	}
	if len(row.PasswordEnc) > 0 {
		if s.cipher == nil {
			return nil, errcode.ErrSMTPInvalid.WithMsg("服务端未配置 APP_AI_SECRET_KEY，无法解密已保存的密码")
		}
		if cfg.Password, err = s.cipher.open(smtpPasswordName, row.PasswordEnc, row.PasswordNonce); err != nil {
			return nil, errcode.ErrSMTPInvalid.WithMsg("SMTP 密码解密失败，请确认主密钥未变更并重新设置密码")
		}
	}
	return cfg, nil
}

// send 发信并把失败转成已脱敏的 53007。发信前再校验一次主机：保存之后 DNS 可能被改成内网。
func (s *SMTPService) send(ctx context.Context, cfg *model.MailConfig, msg model.MailMessage) error {
	if err := s.checker(ctx, cfg.Host); err != nil {
		return hostRejection(err)
	}
	if err := s.mailer.Send(ctx, *cfg, msg); err != nil {
		return errcode.ErrSMTPSendFailed.WithMsg(sanitizeMailError(err, cfg.Password))
	}
	return nil
}

// sanitizeMailError 生成对外展示的发信失败原因：抹掉密码，并截断过长内容。
func sanitizeMailError(err error, password string) string {
	msg := err.Error()
	if password != "" {
		msg = strings.ReplaceAll(msg, password, "***")
	}
	if r := []rune(msg); len(r) > smtpErrMaxRunes {
		msg = string(r[:smtpErrMaxRunes]) + "…"
	}
	return msg
}

// Test 向 to 发一封测试邮件并记录结果（POST /admin/settings/smtp/test）。
func (s *SMTPService) Test(ctx context.Context, to string) error {
	// 1. 取配置（测试不要求已启用：就是为了启用之前先验证）
	cfg, err := s.mailConfig(ctx, false)
	if err != nil {
		return err
	}
	// 2. 发信并记录检查结果；记录失败只记日志，不覆盖发信结果
	sendErr := s.send(ctx, cfg, model.MailMessage{
		To: to, Subject: "视频画布邮件服务测试", Body: "这是一封测试邮件。收到它说明邮件服务配置正确。",
	})
	msg := ""
	if sendErr != nil {
		var ec *errcode.Error
		if errors.As(sendErr, &ec) {
			msg = ec.Msg
		} else {
			msg = sendErr.Error()
		}
	}
	if err := s.repo.RecordCheck(ctx, sendErr == nil, msg, time.Now()); err != nil {
		logger.Warn("记录 SMTP 检查结果失败", zap.Error(err))
	}
	return sendErr
}

// Enabled 实现 RegisterMail：SMTP 是否已配置并启用。
func (s *SMTPService) Enabled(ctx context.Context) (bool, error) {
	row, err := s.repo.Get(ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return row.Enabled && row.Host != "", nil
}

// SendVerifyCode 实现 RegisterMail：发送注册验证码邮件。
func (s *SMTPService) SendVerifyCode(ctx context.Context, to, code string) error {
	cfg, err := s.mailConfig(ctx, true)
	if err != nil {
		return err
	}
	return s.send(ctx, cfg, model.MailMessage{
		To:      to,
		Subject: "视频画布注册验证码",
		Body:    fmt.Sprintf("您的注册验证码是 %s，10 分钟内有效。如果不是您本人操作，请忽略这封邮件。", code),
	})
}
