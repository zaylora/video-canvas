package plugin_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
)

type codeStore map[string]string

func (s codeStore) Code(context.Context, uint64, string) (string, error) {
	for _, code := range s {
		return code, nil
	}
	return "", fmt.Errorf("插件代码不存在")
}

type secretResolver struct {
	value string
}

func (s secretResolver) Get(context.Context, string) (string, error) {
	return s.value, nil
}

func testExecutor(t *testing.T, code, secret string) *plugin.Executor {
	t.Helper()
	runner := plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{}).Handler())
	return plugin.New(plugin.Options{
		Runner:  runner,
		Codes:   codeStore{"code": code},
		Secrets: secretResolver{value: secret},
	})
}

func testSnapshot(baseURL, code, kind string, auth pluginmeta.Auth) *provider.Snapshot {
	sum := sha256.Sum256([]byte(code))
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{
			Key: "test-model", Kind: kind, UpstreamModel: "upstream",
			Capabilities: modelcfg.Capabilities{Prompt: modelcfg.PromptSpec{MaxLength: 10000}},
		},
		Channel: provider.ChannelSnapshot{
			Key: "test-channel", PluginKey: "test-plugin", PluginVersionID: 1,
			BaseURL: baseURL, TrustedInternal: true,
		},
		Plugin: provider.PluginSnapshot{
			Key: "test-plugin", Version: "1.0.0", SHA256: hex.EncodeToString(sum[:]),
			Meta: pluginmeta.Meta{
				APIVersion: 1, Key: "test-plugin", Name: "测试插件", Version: "1.0.0",
				Auth: auth, Endpoints: map[string]pluginmeta.Endpoint{kind: {Mode: pluginmeta.ModeSync}},
			},
		},
		ModelRevisionID: 1,
	}
}

func TestExecutorSubmitInjectsBearerAndParsesImmediate(t *testing.T) {
	const code = `
module.exports = {
  buildSubmitRequest: function(ctx) {
    return {method: "POST", path: "/run", headers: {"X-Test": "ok"}, json: {prompt: ctx.input.prompt}};
  },
  parseSubmitResponse: function(ctx, resp) {
    return {immediate: {status: "succeeded", outputs: [{type: "text", text: resp.body.result}]}};
  }
};`

	var mu sync.Mutex
	var gotAuth, gotPrompt string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]string{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		gotAuth, gotPrompt = r.Header.Get("Authorization"), body["prompt"]
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"done"}`))
	}))
	defer upstream.Close()

	exec := testExecutor(t, code, "secret-value")
	snap := testSnapshot(upstream.URL, code, modelcfg.KindText, pluginmeta.Auth{Type: pluginmeta.AuthBearer})
	result, err := exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 7, UserID: 9}, Input: map[string]any{"prompt": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Immediate == nil || result.Immediate.Status != provider.StatusSucceeded ||
		result.Immediate.Outputs[0].Text != "done" {
		t.Fatalf("即时结果不符合预期：%+v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotAuth != "Bearer secret-value" || gotPrompt != "hello" {
		t.Fatalf("上游请求不符合预期：auth=%q prompt=%q", gotAuth, gotPrompt)
	}
}

func TestExecutorAsyncSubmitAndQuery(t *testing.T) {
	const code = `
module.exports = {
  buildSubmitRequest: function(ctx) { return {method: "POST", path: "/submit", json: {x: 1}}; },
  parseSubmitResponse: function(ctx, resp) { return {providerTaskId: "provider-123"}; },
  buildQueryRequest: function(ctx) { return {method: "GET", path: "/tasks/" + ctx.task.providerTaskId}; },
  parseQueryResponse: function(ctx, resp) {
    return {status: "succeeded", outputs: [{type: "url", url: ctx.channel.baseUrl + "/result.mp4", mediaType: "video"}]};
  }
};`

	var queried bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tasks/provider-123" {
			queried = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	exec := testExecutor(t, code, "")
	snap := testSnapshot(upstream.URL, code, "video", pluginmeta.Auth{Type: pluginmeta.AuthNone})
	snap.Plugin.Meta.Endpoints["video"] = pluginmeta.Endpoint{Mode: pluginmeta.ModeAsync}
	submitted, err := exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "hi"}})
	if err != nil || submitted.ProviderTaskID != "provider-123" || submitted.Immediate != nil {
		t.Fatalf("异步提交结果不符合预期：%+v %v", submitted, err)
	}
	result, err := exec.Query(context.Background(), snap, provider.TaskRef{ID: 1, ProviderTaskID: submitted.ProviderTaskID})
	if err != nil {
		t.Fatal(err)
	}
	if !queried || result.Status != provider.StatusSucceeded || len(result.Outputs) != 1 {
		t.Fatalf("异步查询结果不符合预期：queried=%v result=%+v", queried, result)
	}
}

func TestExecutorRejectsForbiddenHeaderBeforeSending(t *testing.T) {
	const code = `
module.exports = {
  buildSubmitRequest: function(ctx) {
    return {method: "POST", path: "/run", headers: {"Authorization": "fake"}};
  },
  parseSubmitResponse: function() { return {immediate: {status: "succeeded", outputs: [{type: "text", text: "x"}]}}; }
};`
	var called bool
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer upstream.Close()

	exec := testExecutor(t, code, "secret")
	snap := testSnapshot(upstream.URL, code, modelcfg.KindText, pluginmeta.Auth{Type: pluginmeta.AuthBearer})
	_, err := exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "hi"}})
	if err == nil || provider.CodeOf(err) != provider.CodePluginError || !strings.Contains(err.Error(), "请求头") {
		t.Fatalf("应拒绝插件伪造 Authorization：%v", err)
	}
	if called {
		t.Fatal("非法请求描述不应发送到上游")
	}
}

func TestExecutorRequestTimeoutIsApplied(t *testing.T) {
	const code = `
module.exports = {
  buildSubmitRequest: function(ctx) { return {method: "GET", path: "/slow", timeout: 0.01}; },
  parseSubmitResponse: function() { return {immediate: {status: "succeeded", outputs: [{type: "text", text: "x"}]}}; }
};`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	exec := testExecutor(t, code, "")
	snap := testSnapshot(upstream.URL, code, modelcfg.KindText, pluginmeta.Auth{Type: pluginmeta.AuthNone})
	_, err := exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "hi"}})
	if err == nil || provider.ClassOf(err) != provider.ClassSubmitUnknown {
		t.Fatalf("提交超时应归类为 submit_unknown：%v", err)
	}
}

var _ = model.Asset{}
