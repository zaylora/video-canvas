package plugin_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
)

// mapSecrets 按凭证名返回值，没有的名字报错（模拟“Secret Key 没设置”）。
type mapSecrets map[string]string

func (m mapSecrets) Get(_ context.Context, name string) (string, error) {
	if v, ok := m[name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("凭证 %s 不存在", name)
}

func channelTestExecutor(code string, secrets mapSecrets) *plugin.Executor {
	return plugin.New(plugin.Options{
		Runner:    plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{}).Handler()),
		Codes:     codeStore{"code": code},
		Secrets:   secrets,
		IPAllowed: allowLoopback,
	})
}

func submitText(t *testing.T, exec *plugin.Executor, snap *provider.Snapshot) {
	t.Helper()
	if _, err := exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 1}, Input: map[string]any{"prompt": "hi"},
	}); err != nil {
		t.Fatalf("提交失败：%v", err)
	}
}

// 渠道自定义请求头：只加到发往 base_url 同源的请求；优先级 默认 UA < 渠道头 < 插件请求描述里的头 < 宿主注入的鉴权。
func TestExecutor_渠道自定义请求头与优先级(t *testing.T) {
	const code = `
module.exports = {
  buildSubmitRequest: function(ctx) { return {method: "POST", path: "/run", headers: {"X-Test": "from-plugin"}, json: {}}; },
  parseSubmitResponse: function(ctx, resp) { return {immediate: {status: "succeeded", outputs: [{type: "text", text: "ok"}]}}; }
};`
	var mu sync.Mutex
	var got http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	snap := testSnapshot(upstream.URL, code, modelcfg.KindText, pluginmeta.Auth{Type: pluginmeta.AuthBearer})
	snap.Channel.Headers = map[string]string{"X-Biz": "canvas", "X-Test": "from-channel", "User-Agent": "Custom/1.0"}
	submitText(t, channelTestExecutor(code, mapSecrets{"channel:test-channel": "KEY"}), snap)

	mu.Lock()
	defer mu.Unlock()
	if got.Get("X-Biz") != "canvas" {
		t.Errorf("渠道自定义头应发往同源请求：%v", got)
	}
	if got.Get("User-Agent") != "Custom/1.0" {
		t.Errorf("渠道设置的 User-Agent 应覆盖默认值：%q", got.Get("User-Agent"))
	}
	if got.Get("X-Test") != "from-plugin" {
		t.Errorf("插件请求描述里的头应覆盖渠道头：%q", got.Get("X-Test"))
	}
	if got.Get("Authorization") != "Bearer KEY" {
		t.Errorf("鉴权头由宿主注入：%q", got.Get("Authorization"))
	}
}

func TestExecutor_渠道自定义请求头不发往其他域名(t *testing.T) {
	var mu sync.Mutex
	var other http.Header
	otherSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		other = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer otherSrv.Close()
	code := fmt.Sprintf(`
module.exports = {
  buildSubmitRequest: function(ctx) { return {method: "POST", url: %q, json: {}}; },
  parseSubmitResponse: function(ctx, resp) { return {immediate: {status: "succeeded", outputs: [{type: "text", text: "ok"}]}}; }
};`, otherSrv.URL+"/x")
	base := httptest.NewServer(http.NotFoundHandler()) // 端口与 otherSrv 不同，不同源
	defer base.Close()

	snap := testSnapshot(base.URL, code, modelcfg.KindText, pluginmeta.Auth{Type: pluginmeta.AuthBearer})
	snap.Channel.Headers = map[string]string{"X-Biz": "canvas"}
	submitText(t, channelTestExecutor(code, mapSecrets{"channel:test-channel": "KEY"}), snap)

	mu.Lock()
	defer mu.Unlock()
	if other.Get("X-Biz") != "" || other.Get("Authorization") != "" {
		t.Errorf("发往其他域名的请求不能带渠道头和 Key：%v", other)
	}
}

// auth: custom 的插件不再需要渠道开关：宿主直接把 API Key 放进 ctx.credentials.apiKey，
// 渠道设置了 Secret Key 时同时放进 ctx.credentials.secretKey；没设置时 secretKey 为空，不报错。
func TestExecutor_custom鉴权直接提供凭证(t *testing.T) {
	const code = `
module.exports = {
  buildSubmitRequest: function(ctx) {
    var c = ctx.credentials || {};
    return {method: "POST", path: "/run", headers: {"X-Sig": (c.apiKey || "none") + "|" + (c.secretKey || "")}, json: {}};
  },
  parseSubmitResponse: function(ctx, resp) { return {immediate: {status: "succeeded", outputs: [{type: "text", text: "ok"}]}}; }
};`
	cases := []struct {
		name    string
		secrets mapSecrets
		want    string
	}{
		{"有 Key 和 Secret Key", mapSecrets{"channel:test-channel": "AK", "channel:test-channel:secret": "SK"}, "AK|SK"},
		{"只有 Key", mapSecrets{"channel:test-channel": "AK"}, "AK|"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var sig string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				sig = r.Header.Get("X-Sig")
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer upstream.Close()
			snap := testSnapshot(upstream.URL, code, modelcfg.KindText, pluginmeta.Auth{Type: pluginmeta.AuthCustom})
			submitText(t, channelTestExecutor(code, tt.secrets), snap)
			mu.Lock()
			defer mu.Unlock()
			if sig != tt.want {
				t.Fatalf("插件拿到的凭证 = %q，期望 %q", sig, tt.want)
			}
		})
	}
}
