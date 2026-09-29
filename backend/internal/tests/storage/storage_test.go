package storage_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	. "video-canvas/internal/storage"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/config"
)

func newTestLocal(t *testing.T, baseURL string) *LocalStorage {
	t.Helper()
	l, err := NewLocal(config.LocalStorage{Dir: filepath.Join(t.TempDir(), "assets"), BaseURL: baseURL})
	if err != nil {
		t.Fatalf("创建本地存储失败：%v", err)
	}
	return l
}

func TestValidateKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		ok   bool
	}{
		{"正常多级 key", "u1/202609/abc-123_x.png", true},
		{"单层 key", "a.mp4", true},
		{"空 key", "", false},
		{"绝对路径", "/etc/passwd", false},
		{"父目录穿越", "u1/../../etc/passwd", false},
		{"开头就是 ..", "../x", false},
		{"当前目录段", "u1/./x", false},
		{"反斜杠", `u1\x.png`, false},
		{"盘符", "C:/x.png", false},
		{"空路径段", "u1//x.png", false},
		{"末尾斜杠", "u1/x/", false},
		{"隐藏文件", "u1/.hidden", false},
		{"临时文件前缀", ".tmp-abc", false},
		{"空格", "u1/a b.png", false},
		{"中文", "u1/图片.png", false},
		{"百分号编码", "u1/%2e%2e/x", false},
		{"NUL 字节", "u1/a\x00.png", false},
		{"过长", strings.Repeat("a", 513), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKey(tt.key)
			if tt.ok && err != nil {
				t.Fatalf("期望通过，实际：%v", err)
			}
			if !tt.ok && !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("期望 ErrInvalidKey，实际：%v", err)
			}
		})
	}
}

func TestLocalStorage_PutOpenDelete(t *testing.T) {
	l := newTestLocal(t, "")
	ctx := context.Background()

	// 1. 写入后能读回，多级目录自动创建
	if err := l.Put(ctx, "u1/202609/a.txt", strings.NewReader("hello"), 5, "text/plain"); err != nil {
		t.Fatalf("Put 失败：%v", err)
	}
	rc, err := l.Open(ctx, "u1/202609/a.txt")
	if err != nil {
		t.Fatalf("Open 失败：%v", err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "hello" {
		t.Fatalf("读回内容不一致：%q", b)
	}

	// 2. 未知大小（-1）也能写入，且覆盖已有对象
	if err := l.Put(ctx, "u1/202609/a.txt", strings.NewReader("world!"), -1, ""); err != nil {
		t.Fatalf("覆盖写入失败：%v", err)
	}
	rc, _ = l.Open(ctx, "u1/202609/a.txt")
	b, _ = io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "world!" {
		t.Fatalf("覆盖后内容不一致：%q", b)
	}

	// 3. 删除后再读返回 ErrNotFound，重复删除不报错
	if err := l.Delete(ctx, "u1/202609/a.txt"); err != nil {
		t.Fatalf("Delete 失败：%v", err)
	}
	if _, err := l.Open(ctx, "u1/202609/a.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际：%v", err)
	}
	if err := l.Delete(ctx, "u1/202609/a.txt"); err != nil {
		t.Fatalf("重复删除不应报错：%v", err)
	}
}

func TestLocalStorage_PathTraversal(t *testing.T) {
	l := newTestLocal(t, "")
	ctx := context.Background()
	outside := filepath.Join(filepath.Dir(l.Dir()), "evil.txt")

	for _, key := range []string{"../evil.txt", "/abs/evil.txt", `..\evil.txt`, "u1/../../evil.txt"} {
		if err := l.Put(ctx, key, strings.NewReader("x"), 1, ""); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Put(%q) 期望 ErrInvalidKey，实际：%v", key, err)
		}
		if _, err := l.Open(ctx, key); !errors.Is(err, ErrNotFound) {
			t.Errorf("Open(%q) 期望 ErrNotFound，实际：%v", key, err)
		}
		if err := l.Delete(ctx, key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Delete(%q) 期望 ErrInvalidKey，实际：%v", key, err)
		}
		if _, err := l.URL(ctx, key, 0); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("URL(%q) 期望 ErrInvalidKey，实际：%v", key, err)
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("存储目录之外出现了文件")
	}
}

// errAfterReader 读到一半返回错误，模拟上传中断。
type errAfterReader struct {
	data []byte
	err  error
}

func (r *errAfterReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestLocalStorage_PutAtomic(t *testing.T) {
	ctx := context.Background()

	t.Run("写入中途失败不留下目标文件和临时文件", func(t *testing.T) {
		l := newTestLocal(t, "")
		boom := errors.New("boom")
		err := l.Put(ctx, "u1/a.bin", &errAfterReader{data: []byte("partial"), err: boom}, -1, "")
		if !errors.Is(err, boom) {
			t.Fatalf("期望透传读取错误，实际：%v", err)
		}
		if _, err := l.Open(ctx, "u1/a.bin"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("目标文件不应存在：%v", err)
		}
		assertNoTmpFiles(t, l.Dir())
	})

	t.Run("失败的覆盖写入保留旧内容", func(t *testing.T) {
		l := newTestLocal(t, "")
		if err := l.Put(ctx, "u1/a.bin", strings.NewReader("old"), 3, ""); err != nil {
			t.Fatal(err)
		}
		_ = l.Put(ctx, "u1/a.bin", &errAfterReader{data: []byte("new"), err: errors.New("boom")}, -1, "")
		rc, err := l.Open(ctx, "u1/a.bin")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		if string(b) != "old" {
			t.Fatalf("旧内容被破坏：%q", b)
		}
		assertNoTmpFiles(t, l.Dir())
	})

	t.Run("声明长度与实际不符失败并清理", func(t *testing.T) {
		l := newTestLocal(t, "")
		err := l.Put(ctx, "u1/a.bin", strings.NewReader("abc"), 10, "")
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("期望 ErrUnexpectedEOF，实际：%v", err)
		}
		if _, err := l.Open(ctx, "u1/a.bin"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("目标文件不应存在：%v", err)
		}
		assertNoTmpFiles(t, l.Dir())
	})

	t.Run("ctx 已取消时不写入", func(t *testing.T) {
		l := newTestLocal(t, "")
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := l.Put(cctx, "u1/a.bin", strings.NewReader("abc"), 3, "")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("期望 context.Canceled，实际：%v", err)
		}
		assertNoTmpFiles(t, l.Dir())
	})
}

func assertNoTmpFiles(t *testing.T, dir string) {
	t.Helper()
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), ".tmp-") {
			t.Errorf("残留临时文件：%s", p)
		}
		return nil
	})
}

func TestLocalStorage_URL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"BaseURL 为空返回相对路径", "", "/files/u1/a.png"},
		{"带 BaseURL", "http://localhost:8080", "http://localhost:8080/files/u1/a.png"},
		{"BaseURL 末尾斜杠被去掉", "https://cdn.example.com/", "https://cdn.example.com/files/u1/a.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLocal(t, tt.baseURL)
			got, err := l.URL(context.Background(), "u1/a.png", time.Hour)
			if err != nil || got != tt.want {
				t.Fatalf("期望 %q，实际 %q（err=%v）", tt.want, got, err)
			}
		})
	}
}

func TestFileServer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	l := newTestLocal(t, "")
	content := "0123456789"
	if err := l.Put(context.Background(), "u1/202609/v.mp4", strings.NewReader(content), int64(len(content)), "video/mp4"); err != nil {
		t.Fatal(err)
	}
	// 存储目录同级放一个不该被访问到的文件
	secret := filepath.Join(filepath.Dir(l.Dir()), "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.GET("/files/*filepath", FileServer(l))
	r.HEAD("/files/*filepath", FileServer(l))

	do := func(method, path string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("正常读取带缓存头和类型", func(t *testing.T) {
		w := do(http.MethodGet, "/files/u1/202609/v.mp4", nil)
		if w.Code != http.StatusOK || w.Body.String() != content {
			t.Fatalf("状态码 %d，内容 %q", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Header().Get("Cache-Control"), "max-age") {
			t.Errorf("缺少 Cache-Control：%q", w.Header().Get("Cache-Control"))
		}
		if w.Header().Get("Content-Type") != "video/mp4" {
			t.Errorf("Content-Type 不对：%q", w.Header().Get("Content-Type"))
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("缺少 nosniff")
		}
		if w.Header().Get("Accept-Ranges") != "bytes" {
			t.Errorf("应声明支持 Range")
		}
	})
	t.Run("Range 请求返回 206", func(t *testing.T) {
		w := do(http.MethodGet, "/files/u1/202609/v.mp4", map[string]string{"Range": "bytes=2-5"})
		if w.Code != http.StatusPartialContent || w.Body.String() != "2345" {
			t.Fatalf("状态码 %d，内容 %q", w.Code, w.Body.String())
		}
		if got := w.Header().Get("Content-Range"); got != "bytes 2-5/10" {
			t.Errorf("Content-Range 不对：%q", got)
		}
	})
	t.Run("HEAD 请求无 body", func(t *testing.T) {
		w := do(http.MethodHead, "/files/u1/202609/v.mp4", nil)
		if w.Code != http.StatusOK || w.Body.Len() != 0 {
			t.Fatalf("状态码 %d，body 长度 %d", w.Code, w.Body.Len())
		}
	})
	t.Run("文件不存在 404", func(t *testing.T) {
		if w := do(http.MethodGet, "/files/u1/none.mp4", nil); w.Code != http.StatusNotFound {
			t.Fatalf("期望 404，实际 %d", w.Code)
		}
	})
	t.Run("目录 404", func(t *testing.T) {
		if w := do(http.MethodGet, "/files/u1/202609", nil); w.Code != http.StatusNotFound {
			t.Fatalf("期望 404，实际 %d", w.Code)
		}
	})
	t.Run("路径穿越 404 且不泄露文件", func(t *testing.T) {
		for _, p := range []string{"/files/../secret.txt", "/files/u1/../../secret.txt", "/files/..%2Fsecret.txt", "/files/%2e%2e/secret.txt", `/files/..%5Csecret.txt`} {
			w := do(http.MethodGet, p, nil)
			if w.Code == http.StatusOK || strings.Contains(w.Body.String(), "secret") {
				t.Errorf("%s 泄露了文件：%d %q", p, w.Code, w.Body.String())
			}
		}
	})
}

func TestNew(t *testing.T) {
	t.Run("local 与空 driver 返回 LocalStorage", func(t *testing.T) {
		for _, driver := range []string{"", "local"} {
			st, err := New(config.Storage{Driver: driver, Local: config.LocalStorage{Dir: filepath.Join(t.TempDir(), "d")}})
			if err != nil {
				t.Fatalf("driver=%q 失败：%v", driver, err)
			}
			if _, ok := st.(*LocalStorage); !ok {
				t.Fatalf("driver=%q 应返回 *LocalStorage，实际 %T", driver, st)
			}
		}
	})
	t.Run("local 缺目录报错", func(t *testing.T) {
		if _, err := New(config.Storage{Driver: "local"}); err == nil {
			t.Fatal("期望报错")
		}
	})
	t.Run("s3 返回 S3Storage", func(t *testing.T) {
		st, err := New(config.Storage{Driver: "s3", S3: testS3Config()})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := st.(*S3Storage); !ok {
			t.Fatalf("应返回 *S3Storage，实际 %T", st)
		}
	})
	t.Run("s3 缺配置报错", func(t *testing.T) {
		bad := []config.S3Storage{
			{Bucket: "b", AccessKey: "a", SecretKey: "s"},
			{Endpoint: "e.example.com", AccessKey: "a", SecretKey: "s"},
			{Endpoint: "e.example.com", Bucket: "b"},
		}
		for i, c := range bad {
			if _, err := New(config.Storage{Driver: "s3", S3: c}); err == nil {
				t.Errorf("用例 %d 期望报错", i)
			}
		}
	})
	t.Run("未知 driver 报错", func(t *testing.T) {
		if _, err := New(config.Storage{Driver: "ftp"}); err == nil {
			t.Fatal("期望报错")
		}
	})
}
