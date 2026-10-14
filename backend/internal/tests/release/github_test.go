package release_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/release"
)

func newServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestGitHubClient_Latest(t *testing.T) {
	ctx := context.Background()

	t.Run("解析最新正式版本，去掉 v 前缀", func(t *testing.T) {
		var gotPath, gotUA, gotAccept string
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath, gotUA, gotAccept = r.URL.Path, r.Header.Get("User-Agent"), r.Header.Get("Accept")
			_, _ = w.Write([]byte(`{"tag_name":"v0.4.0","body":"## [0.4.0]\n- 新功能","html_url":"https://github.com/o/r/releases/tag/v0.4.0","published_at":"2026-10-20T08:00:00Z","draft":false,"prerelease":false}`))
		})
		c := release.NewGitHubClient("o/r", release.Options{BaseURL: srv.URL, UserAgent: "video-canvas/0.3.2"})
		rel, err := c.Latest(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if gotPath != "/repos/o/r/releases/latest" || gotUA != "video-canvas/0.3.2" || !strings.Contains(gotAccept, "github") {
			t.Fatalf("请求不对：path=%s ua=%s accept=%s", gotPath, gotUA, gotAccept)
		}
		if rel.Version != "0.4.0" || !strings.Contains(rel.Notes, "新功能") || rel.URL == "" || !rel.PublishedAt.Equal(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)) {
			t.Fatalf("%+v", rel)
		}
	})

	t.Run("错误状态码翻译成看得懂的原因", func(t *testing.T) {
		cases := []struct {
			status int
			want   string
		}{
			{http.StatusNotFound, "还没有正式发布"},
			{http.StatusForbidden, "限流"},
			{http.StatusTooManyRequests, "限流"},
			{http.StatusBadGateway, "502"},
		}
		for _, c := range cases {
			srv := newServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(c.status) })
			_, err := release.NewGitHubClient("o/r", release.Options{BaseURL: srv.URL}).Latest(ctx)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("状态码 %d 应报「%s」，得到 %v", c.status, c.want, err)
			}
		}
	})

	t.Run("返回内容不是合法版本时报错", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"tag_name":""}`)) })
		if _, err := release.NewGitHubClient("o/r", release.Options{BaseURL: srv.URL}).Latest(ctx); err == nil {
			t.Fatal("没有 tag_name 应报错")
		}
	})

	t.Run("超时", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) })
		_, err := release.NewGitHubClient("o/r", release.Options{BaseURL: srv.URL, Timeout: 50 * time.Millisecond}).Latest(ctx)
		if err == nil {
			t.Fatal("应超时")
		}
	})

	t.Run("仓库名不合法时不发请求", func(t *testing.T) {
		called := false
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) { called = true })
		for _, repo := range []string{"", "o", "o/r/x", "../x/y", "o/r?x=1"} {
			if _, err := release.NewGitHubClient(repo, release.Options{BaseURL: srv.URL}).Latest(ctx); err == nil {
				t.Fatalf("仓库名 %q 应报错", repo)
			}
		}
		if called {
			t.Fatal("仓库名不合法时不应发请求")
		}
	})
}
