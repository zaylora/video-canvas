package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "video-canvas/internal/handler"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/config"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

// fakeFileResolver 实现 handler.FileResolver：按 key 返回预置的结果。
type fakeFileResolver struct {
	targets map[string]*service.FileTarget
	err     error
	gotKey  string
}

func (f *fakeFileResolver) ResolveFile(ctx context.Context, key string) (*service.FileTarget, error) {
	f.gotKey = key
	if f.err != nil {
		return nil, f.err
	}
	t, ok := f.targets[key]
	if !ok {
		return nil, errcode.ErrAssetNotFound
	}
	return t, nil
}

func newFilesRouter(res *fakeFileResolver) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewFilesHandler(res)
	r.GET("/files/*filepath", h)
	r.HEAD("/files/*filepath", h)
	return r
}

func TestFilesHandler(t *testing.T) {
	t.Run("本地存储：直接提供文件，支持 Range", func(t *testing.T) {
		dir := t.TempDir()
		local, err := storage.NewLocal(config.LocalStorage{Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		_ = os.MkdirAll(filepath.Join(dir, "u1"), 0o755)
		_ = os.WriteFile(filepath.Join(dir, "u1", "a.png"), []byte("0123456789"), 0o644)
		res := &fakeFileResolver{targets: map[string]*service.FileTarget{"u1/a.png": {Local: local}}}
		r := newFilesRouter(res)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/files/u1/a.png", nil)
		req.Header.Set("Range", "bytes=2-4")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusPartialContent || w.Body.String() != "234" || w.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("实际 %d %q %q", w.Code, w.Body.String(), w.Header().Get("Content-Type"))
		}
		if res.gotKey != "u1/a.png" {
			t.Errorf("应把去掉前导斜杠的 key 交给 service，实际 %q", res.gotKey)
		}
	})

	t.Run("对象存储：302 跳转，限制缓存时长与来源信息", func(t *testing.T) {
		res := &fakeFileResolver{targets: map[string]*service.FileTarget{
			"u1/a.mp4": {RedirectURL: "https://bucket.oss-cn-hangzhou.aliyuncs.com/u1/a.mp4?X-Amz-Signature=abc", MaxAge: 30 * time.Minute},
		}}
		r := newFilesRouter(res)
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(method, "/files/u1/a.mp4", nil))
			if w.Code != http.StatusFound || w.Header().Get("Location") != "https://bucket.oss-cn-hangzhou.aliyuncs.com/u1/a.mp4?X-Amz-Signature=abc" {
				t.Fatalf("%s：%d %q", method, w.Code, w.Header().Get("Location"))
			}
			if w.Header().Get("Cache-Control") != "private, max-age=1800" || w.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Errorf("%s：缓存与来源头不对：%v", method, w.Header())
			}
		}
	})

	t.Run("错误映射：不存在 404，存储不可用 502，未知错误 500", func(t *testing.T) {
		tests := []struct {
			name string
			res  *fakeFileResolver
			want int
		}{
			{"素材不存在", &fakeFileResolver{}, http.StatusNotFound},
			{"存储不可用", &fakeFileResolver{err: errcode.ErrStorageUnavailable}, http.StatusBadGateway},
			{"未知错误", &fakeFileResolver{err: errors.New("db down")}, http.StatusInternalServerError},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				newFilesRouter(tt.res).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/files/u1/none.png", nil))
				if w.Code != tt.want {
					t.Fatalf("实际 %d，期望 %d", w.Code, tt.want)
				}
				if w.Header().Get("Location") != "" {
					t.Error("错误响应不能带跳转")
				}
			})
		}
	})
}
