package storage_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	. "video-canvas/internal/storage"

	"video-canvas/internal/config"
)

// failAfterOpen 让第 1 步（读取不存在的探针对象）正常通过，之后的写入一律失败，用来验证探针停在第 2 步。
type failAfterOpen struct{ Storage }

func (f *failAfterOpen) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	return errors.New("AccessDenied: write not allowed")
}

func localCfg(t *testing.T) config.LocalStorage {
	t.Helper()
	return config.LocalStorage{Dir: t.TempDir()}
}

func TestS3Storage_StatAndRange(t *testing.T) {
	ctx := context.Background()
	f := newFakeS3(t, "vc-bucket")
	f.put("u1/a.bin", []byte("0123456789abcdef"), "video/mp4")
	s, err := NewFromSpec(f.spec())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Stat 返回大小与类型", func(t *testing.T) {
		info, err := s.Stat(ctx, "u1/a.bin")
		if err != nil {
			t.Fatal(err)
		}
		if info.Size != 16 || info.ContentType != "video/mp4" {
			t.Errorf("实际 %+v", info)
		}
	})

	t.Run("Stat 对象不存在返回 ErrNotFound", func(t *testing.T) {
		if _, err := s.Stat(ctx, "u1/none.bin"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("实际：%v", err)
		}
	})

	t.Run("OpenRange 只读取需要的字节", func(t *testing.T) {
		rc, err := s.OpenRange(ctx, "u1/a.bin", 2, 4)
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		b, _ := io.ReadAll(rc)
		if string(b) != "2345" {
			t.Errorf("实际 %q", b)
		}
	})
}

func TestProbe(t *testing.T) {
	ctx := context.Background()

	t.Run("全部通过，并清理探针对象", func(t *testing.T) {
		f := newFakeS3(t, "vc-bucket")
		s, err := NewFromSpec(f.spec())
		if err != nil {
			t.Fatal(err)
		}
		res := Probe(ctx, s, ProbeOptions{})
		if !res.OK || len(res.Steps) != 5 {
			t.Fatalf("实际 %+v", res)
		}
		for _, st := range res.Steps {
			if !st.OK {
				t.Errorf("步骤 %d（%s）应通过：%+v", st.Index, st.Name, st.Issue)
			}
		}
		if f.count() != 0 {
			t.Errorf("探针对象应被删除，桶里还剩 %d 个", f.count())
		}
	})

	t.Run("密钥无权访问：停在第 1 步并给出中文原因", func(t *testing.T) {
		f := newFakeS3(t, "vc-bucket")
		f.fail("AccessDenied", 403)
		s, _ := NewFromSpec(f.spec())
		res := Probe(ctx, s, ProbeOptions{})
		if res.OK {
			t.Fatal("不应通过")
		}
		first := res.Steps[0]
		if first.OK || first.Issue == nil || !strings.Contains(first.Issue.Title, "密钥无权") {
			t.Fatalf("第 1 步应失败并提示密钥无权，实际 %+v", first)
		}
		if !strings.Contains(first.Issue.Raw, "AccessDenied") {
			t.Errorf("应保留原始错误码，实际 %q", first.Issue.Raw)
		}
		for _, st := range res.Steps[1:] {
			if st.OK || !st.Skipped {
				t.Errorf("后续步骤应标记为跳过：%+v", st)
			}
		}
	})

	t.Run("桶不存在", func(t *testing.T) {
		f := newFakeS3(t, "vc-bucket")
		f.fail("NoSuchBucket", 404)
		s, _ := NewFromSpec(f.spec())
		res := Probe(ctx, s, ProbeOptions{})
		if res.OK || res.Steps[0].Issue == nil || !strings.Contains(res.Steps[0].Issue.Title, "桶不存在") {
			t.Fatalf("实际 %+v", res.Steps[0])
		}
	})

	t.Run("写入失败时停在第 2 步", func(t *testing.T) {
		f := newFakeS3(t, "vc-bucket")
		s, _ := NewFromSpec(f.spec())
		// 第 1 步（读取不存在的探针对象）要通过，之后才让所有请求失败
		res := Probe(ctx, &failAfterOpen{Storage: s}, ProbeOptions{})
		if res.OK || !res.Steps[0].OK || res.Steps[1].OK || res.Steps[1].Issue == nil {
			t.Fatalf("实际 %+v", res.Steps)
		}
	})

	t.Run("可以跳过签名地址检查（本地磁盘没有可访问的绝对地址）", func(t *testing.T) {
		l, err := NewLocal(localCfg(t))
		if err != nil {
			t.Fatal(err)
		}
		res := Probe(ctx, l, ProbeOptions{SkipURLCheck: true})
		if !res.OK {
			t.Fatalf("实际 %+v", res)
		}
		if !res.Steps[3].Skipped {
			t.Errorf("第 4 步应标记为跳过：%+v", res.Steps[3])
		}
	})
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"AccessDenied: Access Denied", "密钥无权"},
		{"InvalidAccessKeyId: The Access Key Id you provided does not exist", "AccessKey"},
		{"SignatureDoesNotMatch: signature mismatch", "Secret"},
		{"NoSuchBucket: The specified bucket does not exist", "桶不存在"},
		{"Please use virtual hosted style to access.", "寻址方式"},
		{"dial tcp: lookup nobody.example: no such host", "endpoint"},
		{"context deadline exceeded", "超时"},
		{"something strange", "未知错误"},
	}
	for _, tt := range tests {
		got := ClassifyError(errors.New(tt.raw))
		if !strings.Contains(got.Title+got.Hint, tt.want) {
			t.Errorf("%q 的翻译应包含 %q，实际 %+v", tt.raw, tt.want, got)
		}
		if got.Raw != tt.raw {
			t.Errorf("应保留原始错误，实际 %q", got.Raw)
		}
	}
}
