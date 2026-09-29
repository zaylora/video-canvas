package storage_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
	. "video-canvas/internal/storage"

	"video-canvas/internal/config"
)

// testS3Config 返回一份不需要真实连接的配置；显式给 region，避免签名时去查询 bucket location。
func testS3Config() config.S3Storage {
	return config.S3Storage{
		Endpoint:  "oss-cn-hangzhou.aliyuncs.com",
		Region:    "cn-hangzhou",
		Bucket:    "vc-bucket",
		AccessKey: "ak",
		SecretKey: "sk",
		UseSSL:    true,
	}
}

func TestNormalizePrefix(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"/", ""},
		{"assets", "assets/"},
		{"/assets/", "assets/"},
		{"a/b", "a/b/"},
	}
	for _, tt := range tests {
		if got := NormalizePrefix(tt.in); got != tt.want {
			t.Errorf("normalizePrefix(%q) = %q，期望 %q", tt.in, got, tt.want)
		}
	}
}

func TestS3Storage_ObjectKey(t *testing.T) {
	t.Run("带前缀", func(t *testing.T) {
		cfg := testS3Config()
		cfg.PathPrefix = "/prod/"
		s, err := NewS3(cfg)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.ObjectKey("u1/a.png")
		if err != nil || got != "prod/u1/a.png" {
			t.Fatalf("实际 %q，err=%v", got, err)
		}
	})
	t.Run("无前缀", func(t *testing.T) {
		s, _ := NewS3(testS3Config())
		got, err := s.ObjectKey("u1/a.png")
		if err != nil || got != "u1/a.png" {
			t.Fatalf("实际 %q，err=%v", got, err)
		}
	})
	t.Run("非法 key 被拒绝", func(t *testing.T) {
		s, _ := NewS3(testS3Config())
		if _, err := s.ObjectKey("../x"); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("期望 ErrInvalidKey，实际：%v", err)
		}
	})
}

func TestS3Storage_URL(t *testing.T) {
	ctx := context.Background()

	t.Run("配置公开地址时返回公开 URL 并带前缀", func(t *testing.T) {
		cfg := testS3Config()
		cfg.PublicBaseURL = "https://cdn.example.com/"
		cfg.PathPrefix = "prod"
		s, err := NewS3(cfg)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.URL(ctx, "u1/202609/a.png", time.Hour)
		if err != nil || got != "https://cdn.example.com/prod/u1/202609/a.png" {
			t.Fatalf("实际 %q，err=%v", got, err)
		}
	})

	t.Run("未配置公开地址时返回带过期时间的签名 URL", func(t *testing.T) {
		cfg := testS3Config()
		cfg.PathPrefix = "prod"
		s, err := NewS3(cfg)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.URL(ctx, "u1/a.png", 90*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(got)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(u.Host, "aliyuncs.com") || !strings.HasSuffix(u.Path, "/prod/u1/a.png") {
			t.Errorf("URL 主机或路径不对：%s", got)
		}
		q := u.Query()
		if q.Get("X-Amz-Expires") != "90" {
			t.Errorf("过期时间应为 90 秒，实际 %q", q.Get("X-Amz-Expires"))
		}
		if q.Get("X-Amz-Signature") == "" {
			t.Errorf("缺少签名：%s", got)
		}
	})

	t.Run("ttl 为 0 使用默认 1 小时", func(t *testing.T) {
		s, _ := NewS3(testS3Config())
		got, err := s.URL(ctx, "u1/a.png", 0)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(got)
		if u.Query().Get("X-Amz-Expires") != "3600" {
			t.Errorf("默认过期应为 3600 秒，实际 %q", u.Query().Get("X-Amz-Expires"))
		}
	})

	t.Run("非法 key 报错", func(t *testing.T) {
		s, _ := NewS3(testS3Config())
		if _, err := s.URL(ctx, "/etc/passwd", time.Hour); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("期望 ErrInvalidKey，实际：%v", err)
		}
	})

	t.Run("endpoint 带协议头也能创建", func(t *testing.T) {
		cfg := testS3Config()
		cfg.Endpoint = "https://oss-cn-hangzhou.aliyuncs.com"
		if _, err := NewS3(cfg); err != nil {
			t.Fatalf("期望成功：%v", err)
		}
	})
}

func TestEscapePath(t *testing.T) {
	if got := EscapePath("a b/c#d/e.png"); got != "a%20b/c%23d/e.png" {
		t.Fatalf("实际 %q", got)
	}
}
