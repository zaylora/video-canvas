package plugin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
)

// 契约 §3：准备阶段的结果放进 buildSubmitRequest 的 ctx.prepared；
// 重试时 Task.Prepared 已有值，宿主跳过准备阶段，仍要把它交给插件。
func TestExecutorSubmitPassesPreparedToBuildSubmitRequest(t *testing.T) {
	const code = `
module.exports = {
  buildPrepareRequests: function(ctx) { return [{method: "GET", path: "/prep"}]; },
  parsePrepareResponses: function(ctx, resps) { return {token: resps[0].body.token}; },
  buildSubmitRequest: function(ctx) {
    return {method: "POST", path: "/run", json: {token: ctx.prepared ? ctx.prepared.token : "none"}};
  },
  parseSubmitResponse: function(ctx, resp) {
    return {immediate: {status: "succeeded", outputs: [{type: "text", text: "ok"}]}};
  }
};`

	var mu sync.Mutex
	var prepCalls int
	var gotToken string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/prep" {
			prepCalls++
			_, _ = w.Write([]byte(`{"token":"fresh"}`))
			return
		}
		body := map[string]string{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotToken = body["token"]
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	exec := testExecutor(t, code, "")
	snap := testSnapshot(upstream.URL, code, modelcfg.KindText, pluginmeta.Auth{Type: pluginmeta.AuthNone})
	in := provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "hi"}}

	if _, err := exec.Submit(context.Background(), snap, in); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if prepCalls != 1 || gotToken != "fresh" {
		t.Fatalf("首次提交应先准备再带上结果：prepCalls=%d token=%q", prepCalls, gotToken)
	}
	mu.Unlock()

	in.Task.Prepared = json.RawMessage(`{"token":"persisted"}`)
	if _, err := exec.Submit(context.Background(), snap, in); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if prepCalls != 1 || gotToken != "persisted" {
		t.Fatalf("重试应跳过准备并使用已持久化的结果：prepCalls=%d token=%q", prepCalls, gotToken)
	}
}
