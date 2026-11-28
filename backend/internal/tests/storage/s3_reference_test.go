package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"video-canvas/internal/storage"
)

func TestS3ReferenceConditionalPutAndMetadata(t *testing.T) {
	fake := newFakeS3(t, "reference-test")
	st, err := storage.NewFromSpec(fake.spec())
	if err != nil {
		t.Fatal(err)
	}
	const key = "ref-cache/v1/test"
	if _, err := st.StatReference(t.Context(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("明确的 404 应判为缺失：%v", err)
	}
	if err := st.PutReference(t.Context(), key, strings.NewReader("first"), 5, "image/png", "digest"); err != nil {
		t.Fatal(err)
	}
	if err := st.PutReference(t.Context(), key, strings.NewReader("overwrite"), 9, "image/png", "other"); !errors.Is(err, storage.ErrObjectExists) {
		t.Fatalf("条件上传不能覆盖已存在的对象：%v", err)
	}
	got, err := st.StatReference(t.Context(), key)
	if err != nil || got.Size != 5 || got.SHA256 != "digest" {
		t.Fatalf("HEAD 元数据不匹配：got=%+v err=%v", got, err)
	}
	fake.fail("AccessDenied", http.StatusForbidden)
	if _, err := st.StatReference(t.Context(), key); !errors.Is(err, storage.ErrReferenceAccessDenied) {
		t.Fatalf("403 应归类为权限失败：%v", err)
	}
}

// TestRealBucketReferenceCacheR2 在隔离测试前缀验证 R2 的条件 PUT、元数据和签名 GET。
func TestRealBucketReferenceCacheR2(t *testing.T) {
	spec := realBucketSpec(t)
	if spec.Provider != storage.ProviderR2 || spec.PublicBaseURL != "" {
		t.Skip("需要私有 R2 测试存储")
	}
	spec.PathPrefix += "/reference-" + uuid.NewString()
	bucket, err := storage.NewFromSpec(spec)
	if err != nil {
		t.Fatal("无法建立 R2 测试客户端")
	}
	ctx := t.Context()
	data := tinyPNG(t)
	const key = "ref-cache/v1/reference-test/image.png"
	const digest = "reference-test-sha256"
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := bucket.Delete(cleanup, key); err != nil {
			t.Error("测试前缀对象清理失败")
		}
	})
	if err := bucket.PutReference(ctx, key, bytes.NewReader(data), int64(len(data)), "image/png", digest); err != nil {
		t.Fatal("私有 R2 条件上传失败")
	}
	if err := bucket.PutReference(ctx, key, bytes.NewReader(data), int64(len(data)), "image/png", digest); !errors.Is(err, storage.ErrObjectExists) {
		t.Fatal("R2 重复条件上传未返回已存在")
	}
	info, err := bucket.StatReference(ctx, key)
	if err != nil || info.Size != int64(len(data)) || info.SHA256 != digest {
		t.Fatal("R2 HEAD 未返回正确的大小与摘要")
	}
	assertReferenceSignedGET(t, bucket, key, data)
}

func assertReferenceSignedGET(t *testing.T, bucket storage.ReferenceStorage, key string, want []byte) {
	t.Helper()
	signed, err := bucket.URL(t.Context(), key, time.Minute)
	if err != nil || !strings.HasPrefix(signed, "https://") {
		t.Fatal("签发 R2 HTTPS GET 失败")
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, signed, nil)
	if err != nil {
		t.Fatal("无法建立签名 GET 请求")
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatal("签名 GET 请求失败")
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(io.LimitReader(resp.Body, int64(len(want))+1))
	if err != nil || resp.StatusCode != http.StatusOK || !bytes.Equal(got, want) {
		t.Fatalf("签名 GET 未读到参考素材：HTTP %d，%d 字节", resp.StatusCode, len(got))
	}
}
