package service_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/service"
)

func TestAssetServiceOpenCanvasPoster(t *testing.T) {
	cover := []byte{0xff, 0xd8, 0xff, 0xd9}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/poster":
			_, _ = w.Write(cover)
		case "/redirect":
			http.Redirect(w, req, "/poster", http.StatusFound)
		case "/oversized":
			w.Header().Set("Content-Length", "9000000")
			w.WriteHeader(http.StatusOK)
		default:
			_, _ = w.Write([]byte("not an image"))
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		name, path, kind string
		user             uint64
		calls            int
	}{
		{"正确封面", "/poster", "video", 1, 1},
		{"无处理服务", "", "video", 1, 1},
		{"拒绝非图片", "/invalid", "video", 1, 1},
		{"拒绝跳转", "/redirect", "video", 1, 1},
		{"限制封面大小", "/oversized", "video", 1, 1},
		{"拒绝非视频", "/poster", "image", 1, 0},
		{"拒绝无归属", "/poster", "video", 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _ := newAssetTestService(config.Storage{})
			repo.rows[5] = &model.Asset{ID: 5, UserID: 1, Kind: tc.kind, StorageID: 1, StorageKey: "u1/video.mp4"}
			resolver := &fakeVariantResolver{targets: map[string]service.VariantTarget{}}
			if tc.path != "" {
				resolver.targets["poster"] = service.VariantTarget{URL: server.URL + tc.path, Signed: true}
			}
			svc.SetVariantResolver(resolver)
			file, err := svc.OpenCanvasPoster(context.Background(), tc.user, 5)
			if file != nil {
				defer file.Body.Close()
			}
			if resolver.calls != tc.calls {
				t.Fatalf("处理服务调用次数: %d，不是 %d", resolver.calls, tc.calls)
			}
			if tc.name != "正确封面" {
				if !errors.Is(err, provider.ErrAssetNotFound) {
					t.Fatalf("应回退占位: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(file.Body)
			if err != nil || !bytes.Equal(body, cover) || file.Asset.MimeType != "image/jpeg" || file.URL != "" {
				t.Fatalf("封面不符: %v, %v", file.Asset.MimeType, err)
			}
		})
	}
}
