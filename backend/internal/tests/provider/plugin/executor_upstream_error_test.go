package plugin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
)

// 上游非 2xx 时，宿主要把上游返回的错误原文（遮掉渠道 Key）带进 provider.Error.Upstream，最终给用户看；
// 插件写了 classifyError 时用它给的 message，没写时宿主自己从响应体里取。
func TestExecutorCarriesUpstreamErrorText(t *testing.T) {
	const plain = `
module.exports = {
  buildSubmitRequest: function(ctx) { return {method: "POST", path: "/run", json: {x: 1}}; },
  parseSubmitResponse: function(ctx, resp) { return {providerTaskId: "id"}; }
};`
	const classified = `
module.exports = {
  buildSubmitRequest: function(ctx) { return {method: "POST", path: "/run", json: {x: 1}}; },
  parseSubmitResponse: function(ctx, resp) { return {providerTaskId: "id"}; },
  classifyError: function(ctx, resp) { return {class: "moderation", message: "图片涉嫌违规 key=" + resp.body.key}; }
};`
	cases := []struct {
		name     string
		code     string
		status   int
		body     string
		class    provider.ErrorClass
		upstream string
	}{
		{"error 对象里的 message", plain, 400, `{"error":{"code":"","type":"new_api_error","message":"Unsupported input: prompt (request id: abc)"}}`,
			provider.ClassTerminal, "Unsupported input: prompt (request id: abc)"},
		{"顶层 message", plain, 400, `{"code":"bad","message":"invalid ratio"}`, provider.ClassTerminal, "invalid ratio"},
		{"error 是字符串", plain, 400, `{"error":"model not found"}`, provider.ClassTerminal, "model not found"},
		{"纯文本响应体", plain, 400, "  bad request: size\n", provider.ClassTerminal, "bad request: size"},
		{"取不到原文时说明 HTTP 状态", plain, 400, ``, provider.ClassTerminal, "上游返回 HTTP 400"},
		{"HTML 错误页不原样给用户", plain, 502, `<html><body>502 Bad Gateway</body></html>`, provider.ClassRetryable, "上游返回 HTTP 502"},
		{"5xx 也带原文", plain, 503, `{"error":{"message":"upstream overloaded"}}`, provider.ClassRetryable, "upstream overloaded"},
		{"余额不足也带原文", plain, 402, `{"error":{"message":"insufficient balance 0.00"}}`, provider.ClassProviderBalance, "insufficient balance 0.00"},
		{"原文里的渠道 Key 要遮掉", plain, 401, `{"error":{"message":"invalid key secret-value"}}`, provider.ClassTerminal, "invalid key ***"},
		{"插件 classifyError 给的原文", classified, 400, `{"key":"secret-value"}`, provider.ClassModeration, "图片涉嫌违规 key=***"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()
			exec := testExecutor(t, tc.code, "secret-value")
			snap := testSnapshot(upstream.URL, tc.code, modelcfg.KindImage, pluginmeta.Auth{Type: pluginmeta.AuthBearer})
			snap.Plugin.Meta.Endpoints = map[string]pluginmeta.Endpoint{modelcfg.KindImage: {Mode: pluginmeta.ModeAsync}}
			_, err := exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"}})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
			if got := provider.UpstreamOf(err); got != tc.upstream {
				t.Fatalf("上游原文应是 %q，实际 %q（%v）", tc.upstream, got, err)
			}
		})
	}
}
