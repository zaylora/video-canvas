package plugin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginrunner"
)

func pullExecutor(ipAllowed func() bool) *plugin.Executor {
	opts := plugin.Options{Runner: plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{}).Handler())}
	if ipAllowed != nil {
		opts.IPAllowed = allowLoopback
	}
	return plugin.New(opts)
}

// 拉取上游模型：GET {base_url}/v1/models，渠道 API Key 作 Bearer，读 OpenAI 形状 data[].id，去重保序。
func TestListUpstreamModels_成功(t *testing.T) {
	var mu sync.Mutex
	var gotPath, gotAuth, gotBiz, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath, gotAuth, gotBiz, gotUA = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Biz"), r.Header.Get("User-Agent")
		mu.Unlock()
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-image-2"},{"id":"sd2.0-c1"},{"id":"gpt-image-2"},{"id":""},{"object":"x"}]}`))
	}))
	defer srv.Close()

	ch := provider.ChannelSnapshot{Key: "c1", BaseURL: srv.URL, Headers: map[string]string{"X-Biz": "canvas"}}
	got, err := pullExecutor(func() bool { return true }).ListUpstreamModels(context.Background(), ch, "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"gpt-image-2", "sd2.0-c1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("模型 = %v，期望 %v（去重、去掉空 id 与没有 id 的项）", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/v1/models" || gotAuth != "Bearer sk-test" || gotBiz != "canvas" || gotUA == "" {
		t.Fatalf("上游请求不对：path=%q auth=%q biz=%q ua=%q", gotPath, gotAuth, gotBiz, gotUA)
	}
}

func TestListUpstreamModels_baseURL已带v1不重复拼接(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	got, err := pullExecutor(func() bool { return true }).ListUpstreamModels(context.Background(), provider.ChannelSnapshot{Key: "c1", BaseURL: srv.URL + "/v1/"}, "k")
	if err != nil || len(got) != 0 {
		t.Fatalf("空列表不是错误：%v %v", got, err)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("路径 = %q，期望 /v1/models", gotPath)
	}
}

func TestListUpstreamModels_失败分类(t *testing.T) {
	cases := []struct {
		name   string
		handle http.HandlerFunc
		reason provider.PullReason
	}{
		{"401", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"bad key sk-secret"}`))
		}, provider.PullUnauthorized},
		{"403", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }, provider.PullUnauthorized},
		{"404", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }, provider.PullStatus},
		{"500", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }, provider.PullStatus},
		{"不是 JSON", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`<html>hi</html>`)) }, provider.PullFormat},
		{"没有 data 数组", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"models":["a"]}`)) }, provider.PullFormat},
		{"响应过大", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"id":"` + strings.Repeat("a", provider.MaxPullBodyBytes+10) + `"}]}`))
		}, provider.PullTooLarge},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handle)
			defer srv.Close()
			_, err := pullExecutor(func() bool { return true }).ListUpstreamModels(context.Background(), provider.ChannelSnapshot{Key: "c1", BaseURL: srv.URL}, "sk-secret")
			var pe *provider.PullError
			if !errors.As(err, &pe) || pe.Reason != tt.reason {
				t.Fatalf("错误 = %v，期望原因 %q", err, tt.reason)
			}
			if strings.Contains(err.Error(), "sk-secret") {
				t.Fatalf("错误信息里不能出现 Key 明文：%v", err)
			}
		})
	}
}

func TestListUpstreamModels_模型数超限(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"data":[`)
	for i := 0; i <= provider.MaxPullModels; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`{"id":"m`)
		sb.WriteString(strings.Repeat("x", 1))
		sb.WriteString(string(rune('a' + i%26)))
		sb.WriteString(strings.Repeat("y", i/26))
		sb.WriteString(`"}`)
	}
	sb.WriteString(`]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(sb.String())) }))
	defer srv.Close()
	_, err := pullExecutor(func() bool { return true }).ListUpstreamModels(context.Background(), provider.ChannelSnapshot{Key: "c1", BaseURL: srv.URL}, "k")
	var pe *provider.PullError
	if !errors.As(err, &pe) || pe.Reason != provider.PullTooLarge {
		t.Fatalf("超过 %d 个应按过大处理：%v", provider.MaxPullModels, err)
	}
}

// 与其他出站请求一样受 SSRF 防护：默认拒绝回环 / 内网地址。
func TestListUpstreamModels_拦截内网地址(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":[]}`)) }))
	defer srv.Close()
	_, err := pullExecutor(nil).ListUpstreamModels(context.Background(), provider.ChannelSnapshot{Key: "c1", BaseURL: srv.URL}, "k")
	var pe *provider.PullError
	if !errors.As(err, &pe) || pe.Reason != provider.PullUnreachable {
		t.Fatalf("内网地址应被拦截并报不可达：%v", err)
	}
}

// 格式不符时，错误信息要带上响应开头（脱敏），方便判断上游返回的是 HTML 还是别的 JSON。
func TestListUpstreamModels_格式不符带响应片段(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><title>console sk-secret</title></html>"))
	}))
	defer srv.Close()
	_, err := pullExecutor(func() bool { return true }).ListUpstreamModels(context.Background(), provider.ChannelSnapshot{Key: "c1", BaseURL: srv.URL}, "sk-secret")
	var pe *provider.PullError
	if !errors.As(err, &pe) || pe.Reason != provider.PullFormat {
		t.Fatalf("错误 = %v，期望格式错误", err)
	}
	if !strings.Contains(err.Error(), "<html><title>console") || !strings.Contains(err.Error(), "text/html") {
		t.Fatalf("错误信息缺少响应片段 / Content-Type：%v", err)
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("错误信息里不能出现 Key 明文：%v", err)
	}
}
