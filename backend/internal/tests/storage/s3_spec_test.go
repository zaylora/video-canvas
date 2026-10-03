package storage_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	. "video-canvas/internal/storage"
)

func specOSS() Spec {
	return Spec{Provider: ProviderAliyunOSS, Region: "cn-hangzhou", Bucket: "vc-bucket", AccessKey: "ak", SecretKey: "sk"}
}

func specR2() Spec {
	return Spec{Provider: ProviderR2, AccountID: r2Account, Bucket: "vc-bucket", AccessKey: "ak", SecretKey: "sk"}
}

func specS3() Spec {
	return Spec{Provider: ProviderS3, Region: "us-east-1", Bucket: "vc-bucket", UseSSL: true, AccessKey: "ak", SecretKey: "sk"}
}

func TestNewFromSpec_Addressing(t *testing.T) {
	ctx := context.Background()

	t.Run("OSS 用虚拟主机寻址：桶名在主机名里", func(t *testing.T) {
		s, err := NewFromSpec(specOSS())
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.URL(ctx, "u1/a.png", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(got)
		if u.Host != "vc-bucket.oss-cn-hangzhou.aliyuncs.com" || u.Path != "/u1/a.png" {
			t.Errorf("实际 %s", got)
		}
	})

	t.Run("R2 用 path 寻址：桶名在路径里", func(t *testing.T) {
		s, err := NewFromSpec(specR2())
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.URL(ctx, "u1/a.png", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(got)
		if u.Host != r2Account+".r2.cloudflarestorage.com" || u.Path != "/vc-bucket/u1/a.png" {
			t.Errorf("实际 %s", got)
		}
	})

	t.Run("缺少密钥", func(t *testing.T) {
		sp := specOSS()
		sp.SecretKey = ""
		if _, err := NewFromSpec(sp); err == nil || !strings.Contains(err.Error(), "密钥") {
			t.Fatalf("期望密钥为空的错误，实际：%v", err)
		}
	})

	t.Run("配置不合法时返回 ErrInvalidSpec", func(t *testing.T) {
		sp := specOSS()
		sp.Bucket = ""
		if _, err := NewFromSpec(sp); !errors.Is(err, ErrInvalidSpec) {
			t.Fatalf("实际：%v", err)
		}
	})
}

func TestS3Storage_DirectUpload(t *testing.T) {
	ctx := context.Background()
	req := DirectUploadRequest{Key: "u1/202610/a.png", ContentType: "image/png", Size: 2048, MaxSize: 1 << 20, TTL: 15 * time.Minute}

	t.Run("POST Policy：表单字段含带前缀的 key，策略限定大小上限", func(t *testing.T) {
		sp := specS3()
		sp.PathPrefix = "prod"
		s, err := NewFromSpec(sp)
		if err != nil {
			t.Fatal(err)
		}
		if s.DirectMethod() != DirectPostPolicy {
			t.Fatalf("S3 应使用 POST Policy，实际 %s", s.DirectMethod())
		}
		d, err := s.DirectUpload(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if d.Method != "post" || d.URL == "" {
			t.Fatalf("实际 %+v", d)
		}
		if d.Fields["key"] != "prod/u1/202610/a.png" {
			t.Errorf("key 应带前缀，实际 %q", d.Fields["key"])
		}
		policy, err := base64.StdEncoding.DecodeString(d.Fields["policy"])
		if err != nil {
			t.Fatalf("policy 不是 base64：%v", err)
		}
		if !strings.Contains(string(policy), "content-length-range") || !strings.Contains(string(policy), "1048576") {
			t.Errorf("策略应限定大小上限：%s", policy)
		}
		if d.Fields["x-amz-signature"] == "" {
			t.Errorf("缺少签名字段：%v", d.Fields)
		}
	})

	t.Run("预签名 PUT（R2）：Content-Length 与 Content-Type 都签进地址", func(t *testing.T) {
		s, err := NewFromSpec(specR2())
		if err != nil {
			t.Fatal(err)
		}
		if s.DirectMethod() != DirectPresignedPut {
			t.Fatalf("R2 应使用预签名 PUT，实际 %s", s.DirectMethod())
		}
		d, err := s.DirectUpload(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if d.Method != "put" {
			t.Fatalf("实际 %+v", d)
		}
		u, err := url.Parse(d.URL)
		if err != nil {
			t.Fatal(err)
		}
		signed := u.Query().Get("X-Amz-SignedHeaders")
		if !strings.Contains(signed, "content-length") || !strings.Contains(signed, "content-type") {
			t.Errorf("Content-Length / Content-Type 应被签名，SignedHeaders=%q", signed)
		}
		if u.Query().Get("X-Amz-Expires") != "900" {
			t.Errorf("有效期应为 900 秒，实际 %q", u.Query().Get("X-Amz-Expires"))
		}
		if d.Headers["Content-Type"] != "image/png" {
			t.Errorf("应要求客户端带上同样的 Content-Type，实际 %v", d.Headers)
		}
	})

	t.Run("预签名 PUT 声明的大小超过上限直接拒签", func(t *testing.T) {
		s, _ := NewFromSpec(specR2())
		big := req
		big.Size = 2 << 20
		if _, err := s.DirectUpload(ctx, big); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("期望 ErrTooLarge，实际：%v", err)
		}
	})

	t.Run("预签名 PUT 必须声明大小", func(t *testing.T) {
		s, _ := NewFromSpec(specR2())
		noSize := req
		noSize.Size = 0
		if _, err := s.DirectUpload(ctx, noSize); err == nil {
			t.Fatal("没有声明大小时不应签发")
		}
	})

	t.Run("非法 key 被拒绝", func(t *testing.T) {
		s, _ := NewFromSpec(specS3())
		bad := req
		bad.Key = "../x"
		if _, err := s.DirectUpload(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("期望 ErrInvalidKey，实际：%v", err)
		}
	})
}
