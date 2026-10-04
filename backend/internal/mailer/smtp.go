// Package mailer 是发信的具体实现（标准库 net/smtp），业务层只依赖 service.Mailer 接口。
package mailer

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider/netguard"
)

// sendTimeout 是一次发信（含连接、握手、认证、投递）的总时限，防止对端不响应把请求拖死。
const sendTimeout = 20 * time.Second

// SMTPSender 用标准库 net/smtp 发信，支持 none / starttls / tls 三种加密方式。
// 所有连接都经 netguard 拨号：先解析 DNS 并校验不是内网 / 回环 / 链路本地，再直连校验过的 IP（防 SSRF 与 DNS rebinding）。
type SMTPSender struct {
	guard *netguard.Config
}

// NewSMTPSender 创建发信器；guard 为 nil 时用默认守卫（拒绝内网地址），测试里可注入放开回环的守卫。
func NewSMTPSender(guard *netguard.Config) *SMTPSender {
	if guard == nil {
		guard = &netguard.Config{}
	}
	guard.ApplyDefaults()
	return &SMTPSender{guard: guard}
}

// Send 发送一封纯文本邮件。失败返回的错误可能含服务端回应与目标主机名，但不含密码；调用方对外展示前仍应脱敏。
func (s *SMTPSender) Send(ctx context.Context, cfg model.MailConfig, msg model.MailMessage) error {
	// 1. 先校验所有会进邮件头 / 信封的字段，杜绝换行注入（Bcc 注入、伪造头）
	for _, v := range []string{msg.To, msg.Subject, cfg.FromAddress, cfg.FromName} {
		if strings.ContainsAny(v, "\r\n") {
			return errors.New("邮件头字段不能包含换行")
		}
	}
	from, err := mail.ParseAddress(cfg.FromAddress)
	if err != nil {
		return fmt.Errorf("发件地址不合法：%w", err)
	}
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("收件地址不合法：%w", err)
	}
	if cfg.Encryption != model.SMTPEncNone && cfg.Encryption != model.SMTPEncStartTLS && cfg.Encryption != model.SMTPEncTLS {
		return fmt.Errorf("不支持的加密方式 %q", cfg.Encryption)
	}

	// 2. 建立连接并完成握手：整个过程共用一个总时限
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	c, closeConn, err := s.connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeConn()

	// 3. 认证（有用户名才认证）。net/smtp 的 PlainAuth 只在 TLS 或本机连接上发送密码，明文连接会直接报错，这是想要的行为
	if cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("SMTP 认证失败：%w", err)
		}
	}

	// 4. 投递
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("MAIL FROM 被拒绝：%w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("RCPT TO 被拒绝：%w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA 被拒绝：%w", err)
	}
	if _, err := w.Write(buildMessage(cfg, to.Address, msg)); err != nil {
		return fmt.Errorf("写入邮件内容失败：%w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("邮件投递失败：%w", err)
	}
	_ = c.Quit() // 邮件已被接收，QUIT 失败不影响结果
	return nil
}

// connect 按加密方式连接并返回 SMTP 客户端与关闭函数。
func (s *SMTPSender) connect(ctx context.Context, cfg model.MailConfig) (*smtp.Client, func(), error) {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	conn, err := s.guard.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("连接 %s 失败：%w", addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl) // 读写也受总时限约束
	}
	tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}
	if cfg.Encryption == model.SMTPEncTLS {
		conn = tls.Client(conn, tlsCfg)
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("SMTP 握手失败：%w", err)
	}
	closeConn := func() { _ = c.Close() }
	if cfg.Encryption == model.SMTPEncStartTLS {
		if err := c.StartTLS(tlsCfg); err != nil {
			closeConn()
			return nil, nil, fmt.Errorf("STARTTLS 失败：%w", err)
		}
	}
	return c, closeConn, nil
}

// buildMessage 组装 RFC 5322 邮件：主题与发件人名按 RFC 2047 编码，正文用 base64（避免非 ASCII 与行长问题）。
func buildMessage(cfg model.MailConfig, to string, msg model.MailMessage) []byte {
	from := mail.Address{Name: cfg.FromName, Address: cfg.FromAddress}
	var b strings.Builder
	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", msg.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	enc := base64.StdEncoding.EncodeToString([]byte(msg.Body))
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return []byte(b.String())
}
