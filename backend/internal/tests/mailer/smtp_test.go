package mailer_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/mailer"
	"video-canvas/internal/model"
	"video-canvas/internal/provider/netguard"
)

// fakeSMTP 是一个最小的明文 SMTP 服务端，记录收到的信封与正文。
type fakeSMTP struct {
	ln   net.Listener
	mu   sync.Mutex
	from string
	to   []string
	data string
	auth bool // 是否收到过 AUTH 命令
}

func startFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *fakeSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeSMTP) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	w := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	w("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		s.mu.Lock()
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			s.mu.Unlock()
			w("250 fake")
			continue
		case strings.HasPrefix(cmd, "AUTH"):
			s.auth = true
			s.mu.Unlock()
			w("235 ok")
			continue
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			s.from = strings.TrimSpace(line[len("MAIL FROM:"):])
		case strings.HasPrefix(cmd, "RCPT TO:"):
			s.to = append(s.to, strings.TrimSpace(line[len("RCPT TO:"):]))
		case cmd == "DATA":
			s.mu.Unlock()
			w("354 go")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil || l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			s.mu.Lock()
			s.data = b.String()
			s.mu.Unlock()
			w("250 queued")
			continue
		case cmd == "QUIT":
			s.mu.Unlock()
			w("221 bye")
			return
		}
		s.mu.Unlock()
		w("250 ok")
	}
}

// allowAll 是测试用的守卫配置：放开回环地址，这样才能连本机的假服务端。
func allowAll() *netguard.Config {
	c := &netguard.Config{IPAllowed: func(net.IP) bool { return true }}
	c.ApplyDefaults()
	return c
}

func TestSMTPSender_Send(t *testing.T) {
	srv := startFakeSMTP(t)
	sender := mailer.NewSMTPSender(allowAll())
	cfg := model.MailConfig{Host: "127.0.0.1", Port: srv.port(), Encryption: model.SMTPEncNone, FromAddress: "noreply@x.com", FromName: "视频画布"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sender.Send(ctx, cfg, model.MailMessage{To: "user@y.com", Subject: "验证码", Body: "您的验证码是 123456"}); err != nil {
		t.Fatalf("发信失败：%v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !strings.Contains(srv.from, "noreply@x.com") || len(srv.to) != 1 || !strings.Contains(srv.to[0], "user@y.com") {
		t.Fatalf("信封不对：from=%q to=%v", srv.from, srv.to)
	}
	if srv.auth {
		t.Fatal("没配用户名不应认证")
	}
	head, body, _ := strings.Cut(srv.data, "\r\n\r\n")
	for _, want := range []string{"To: user@y.com", "MIME-Version: 1.0", "Content-Type: text/plain; charset=UTF-8", "Content-Transfer-Encoding: base64", "Subject: =?UTF-8?"} {
		if !strings.Contains(head, want) {
			t.Fatalf("邮件头缺少 %q：\n%s", want, head)
		}
	}
	dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", ""))
	if err != nil || string(dec) != "您的验证码是 123456" {
		t.Fatalf("正文解码不对：%q %v", dec, err)
	}
}

func TestSMTPSender_Rejects(t *testing.T) {
	ctx := context.Background()
	t.Run("默认守卫拒绝回环地址（防 SSRF）", func(t *testing.T) {
		srv := startFakeSMTP(t)
		sender := mailer.NewSMTPSender(nil)
		cfg := model.MailConfig{Host: "127.0.0.1", Port: srv.port(), Encryption: model.SMTPEncNone, FromAddress: "a@x.com"}
		err := sender.Send(ctx, cfg, model.MailMessage{To: "u@y.com", Subject: "s", Body: "b"})
		if !errors.Is(err, netguard.ErrBlockedAddress) {
			t.Fatalf("应返回 ErrBlockedAddress：%v", err)
		}
	})
	t.Run("头注入：收件人 / 主题 / 发件人含换行一律拒绝", func(t *testing.T) {
		srv := startFakeSMTP(t)
		sender := mailer.NewSMTPSender(allowAll())
		base := model.MailConfig{Host: "127.0.0.1", Port: srv.port(), Encryption: model.SMTPEncNone, FromAddress: "a@x.com"}
		bad := []struct {
			cfg model.MailConfig
			msg model.MailMessage
		}{
			{base, model.MailMessage{To: "u@y.com\r\nBcc: evil@z.com", Subject: "s", Body: "b"}},
			{base, model.MailMessage{To: "u@y.com", Subject: "s\nBcc: evil@z.com", Body: "b"}},
			{model.MailConfig{Host: base.Host, Port: base.Port, Encryption: base.Encryption, FromAddress: "a@x.com\r\nX: y"}, model.MailMessage{To: "u@y.com", Subject: "s", Body: "b"}},
		}
		for i, c := range bad {
			if err := sender.Send(ctx, c.cfg, c.msg); err == nil {
				t.Errorf("用例 %d 应被拒绝", i)
			}
		}
	})
	t.Run("加密方式不认识", func(t *testing.T) {
		sender := mailer.NewSMTPSender(allowAll())
		err := sender.Send(ctx, model.MailConfig{Host: "127.0.0.1", Port: 1, Encryption: "weird", FromAddress: "a@x.com"}, model.MailMessage{To: "u@y.com", Subject: "s", Body: "b"})
		if err == nil {
			t.Fatal("应报错")
		}
	})
}
