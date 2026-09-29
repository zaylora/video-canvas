package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	. "video-canvas/internal/service"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/service/aiconfigfake"
)

func TestDeriveAISecretKey(t *testing.T) {
	raw32 := bytes.Repeat([]byte{7}, 32)
	sum := sha256.Sum256([]byte("my passphrase"))
	tests := []struct {
		name string
		in   string
		want []byte
	}{
		{"64 位十六进制直接使用", hex.EncodeToString(raw32), raw32},
		{"标准 base64（32 字节）直接使用", base64.StdEncoding.EncodeToString(raw32), raw32},
		{"无填充 base64 也接受", base64.RawStdEncoding.EncodeToString(raw32), raw32},
		{"URL 安全 base64 也接受", base64.URLEncoding.EncodeToString(raw32), raw32},
		{"其他文本用 SHA-256 派生", "my passphrase", sum[:]},
		{"长度不是 32 字节的 base64 按普通文本派生", base64.StdEncoding.EncodeToString([]byte("short")), func() []byte {
			s := sha256.Sum256([]byte(base64.StdEncoding.EncodeToString([]byte("short"))))
			return s[:]
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveAISecretKey(tt.in)
			if len(got) != 32 || !bytes.Equal(got, tt.want) {
				t.Fatalf("派生结果不符合预期：%x", got)
			}
		})
	}
}

func TestAIConfigService_Secret_RoundTrip(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := aicNewSvc()

	if err := svc.SetSecret(ctx, "rh_key", "  sk-very-secret-123\n", 5); err != nil {
		t.Fatal(err)
	}

	t.Run("往返一致且首尾空白被去掉", func(t *testing.T) {
		got, err := svc.Get(ctx, "rh_key")
		if err != nil || got != "sk-very-secret-123" {
			t.Fatalf("解密结果不符合预期：%q %v", got, err)
		}
	})
	t.Run("库里存的是密文，不含明文", func(t *testing.T) {
		row := repo.Secrets["rh_key"]
		if bytes.Contains(row.Ciphertext, []byte("sk-very-secret-123")) || len(row.Nonce) != 12 || row.KeyVersion != 1 || row.UpdatedBy != 5 {
			t.Fatalf("入库数据不符合预期：%+v", row)
		}
	})
	t.Run("每次加密 nonce 不同", func(t *testing.T) {
		_ = svc.SetSecret(ctx, "rh_key2", "same", 1)
		_ = svc.SetSecret(ctx, "rh_key3", "same", 1)
		if bytes.Equal(repo.Secrets["rh_key2"].Nonce, repo.Secrets["rh_key3"].Nonce) {
			t.Fatal("nonce 不应重复")
		}
	})
	t.Run("覆盖后新值立即生效（缓存被清除）", func(t *testing.T) {
		if err := svc.SetSecret(ctx, "rh_key", "sk-new", 6); err != nil {
			t.Fatal(err)
		}
		got, _ := svc.Get(ctx, "rh_key")
		if got != "sk-new" {
			t.Fatalf("应读到新值，实际 %q", got)
		}
	})
	t.Run("ListSecrets 永远不含明文和密文", func(t *testing.T) {
		list, err := svc.ListSecrets(ctx)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(list)
		text := string(b)
		for _, bad := range []string{"sk-new", "sk-very-secret", "ciphertext", "nonce", base64.StdEncoding.EncodeToString(repo.Secrets["rh_key"].Ciphertext)} {
			if strings.Contains(text, bad) {
				t.Fatalf("列表泄露了敏感内容 %q：%s", bad, text)
			}
		}
		var found *SecretStatus
		for i := range list {
			if list[i].Name == "rh_key" {
				found = &list[i]
			}
		}
		if found == nil || !found.IsSet || found.UpdatedBy != 6 || found.UpdatedAt == nil {
			t.Fatalf("rh_key 状态不符合预期：%+v", found)
		}
	})
}

func TestAIConfigService_Secret_GetCache(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := aicNewSvc()
	clock := time.Now()
	svc.SetNow(func() time.Time { return clock })
	_ = svc.SetSecret(ctx, "k1", "v1", 1)
	if _, err := svc.Get(ctx, "k1"); err != nil {
		t.Fatal(err)
	}
	// 直接把库里的行删掉：缓存有效期内仍能读到，过期后才发现凭证已不存在
	delete(repo.Secrets, "k1")
	if got, err := svc.Get(ctx, "k1"); err != nil || got != "v1" {
		t.Fatalf("缓存有效期内应命中：%q %v", got, err)
	}
	clock = clock.Add(AISecretCacheTTL + time.Second)
	if _, err := svc.Get(ctx, "k1"); !errors.Is(err, ErrAISecretNotSet) {
		t.Fatalf("缓存过期后应查库并返回未设置：%v", err)
	}
}

func TestAIConfigService_Secret_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("没有主密钥：启动不失败，设置与读取都返回明确错误", func(t *testing.T) {
		repo := aiconfigfake.NewMemRepo()
		svc := NewAIConfigService(repo, &aiconfigfake.Validator{}, "")
		err := svc.SetSecret(ctx, "k1", "v", 1)
		var e *errcode.Error
		if !errors.As(err, &e) || e.Code != errcode.ErrInternal.Code || !strings.Contains(e.Msg, "APP_AI_SECRET_KEY") {
			t.Fatalf("设置凭证应返回带说明的服务端错误：%v", err)
		}
		if len(repo.Secrets) != 0 {
			t.Fatal("没有主密钥时不应落库")
		}
		if _, err := svc.Get(ctx, "k1"); !errors.Is(err, ErrAISecretKeyMissing) {
			t.Fatalf("读取应返回 ErrAISecretKeyMissing：%v", err)
		}
		// 其他功能不受影响
		if _, err := svc.ListModels(ctx, ""); err != nil {
			t.Fatalf("没有主密钥不影响模型清单：%v", err)
		}
		if _, err := svc.ListSecrets(ctx); err != nil {
			t.Fatalf("没有主密钥不影响列出凭证状态：%v", err)
		}
	})

	t.Run("凭证未设置", func(t *testing.T) {
		svc, _, _ := aicNewSvc()
		if _, err := svc.Get(ctx, "none"); !errors.Is(err, ErrAISecretNotSet) {
			t.Fatalf("期望 ErrAISecretNotSet：%v", err)
		}
	})

	t.Run("更换主密钥后无法解密", func(t *testing.T) {
		repo := aiconfigfake.NewMemRepo()
		a := NewAIConfigService(repo, &aiconfigfake.Validator{}, "key-A")
		if err := a.SetSecret(ctx, "k1", "v", 1); err != nil {
			t.Fatal(err)
		}
		b := NewAIConfigService(repo, &aiconfigfake.Validator{}, "key-B")
		if _, err := b.Get(ctx, "k1"); err == nil {
			t.Fatalf("应解密失败：%v", err)
		}
	})

	t.Run("密文被拷到另一个凭证名下无法解密（名字绑定在认证数据里）", func(t *testing.T) {
		svc, repo, _ := aicNewSvc()
		_ = svc.SetSecret(ctx, "a", "value-a", 1)
		cp := *repo.Secrets["a"]
		cp.Name = "b"
		repo.Secrets["b"] = &cp
		if _, err := svc.Get(ctx, "b"); err == nil {
			t.Fatal("换名后的密文不应解开")
		}
	})

	t.Run("不认识的密钥版本", func(t *testing.T) {
		svc, repo, _ := aicNewSvc()
		_ = svc.SetSecret(ctx, "a", "value-a", 1)
		repo.Secrets["a"].KeyVersion = 2
		if _, err := svc.Get(ctx, "a"); err == nil {
			t.Fatal("未知 key_version 应报错")
		}
	})

	tests := []struct {
		name, secName, value string
	}{
		{"凭证名非法", "bad name!", "v"},
		{"凭证名为空", "", "v"},
		{"凭证值为空", "k", "   "},
		{"凭证值过长", "k", strings.Repeat("x", AISecretMaxLen+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := aicNewSvc()
			aicWantCode(t, svc.SetSecret(ctx, tt.secName, tt.value, 1), errcode.ErrInvalidParams.Code)
		})
	}
}

func TestAIConfigService_ListSecrets_ReferencedUnset(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := aicNewSvc()
	// 平台引用了 rh_key，但还没设置
	if _, err := svc.SaveDraft(ctx, model.ConfigTargetProvider, "", true, aicProviderBody("p1", "rh_key", ""), "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveDraft(ctx, model.ConfigTargetProvider, "", true, aicProviderBody("p2", "rh_key", ""), "", 1); err != nil {
		t.Fatal(err)
	}
	_ = svc.SetSecret(ctx, "other", "x", 1)
	list, err := svc.ListSecrets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "other" || list[1].Name != "rh_key" {
		t.Fatalf("应按名字排序并包含被引用未设置的凭证：%+v", list)
	}
	rh := list[1]
	if rh.IsSet || rh.UpdatedAt != nil || len(rh.ReferencedBy) != 2 || rh.ReferencedBy[0] != "p1" || rh.ReferencedBy[1] != "p2" {
		t.Fatalf("rh_key 状态不符合预期：%+v", rh)
	}
}
