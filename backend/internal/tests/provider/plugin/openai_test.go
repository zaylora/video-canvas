package plugin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/plugins"
)

// openaiEnv 用真实 runner 预检并装载内置 openai.js，元数据取自预检结果。
type openaiEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newOpenAIEnv(t *testing.T, mod func(*plugin.Options)) *openaiEnv {
	t.Helper()
	src, err := plugins.Source("openai")
	if err != nil {
		t.Fatal(err)
	}
	code := string(src)
	runner := plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{}).Handler())
	pre, err := runner.Precheck(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	if !pre.OK {
		t.Fatalf("内置 openai.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	opts := plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "sk-openai"}, IPAllowed: allowLoopback}
	if mod != nil {
		mod(&opts)
	}
	return &openaiEnv{exec: plugin.New(opts), meta: meta, sha: pre.SHA256}
}

// snapshot 的 api 为空表示渠道没填设置（走默认 Responses）。
func (e *openaiEnv) snapshot(baseURL, api string, caps modelcfg.Capabilities) *provider.Snapshot {
	var settings map[string]any
	if api != "" {
		settings = map[string]any{"api": api}
	}
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: "text", UpstreamModel: "gpt-test", Capabilities: caps},
		Channel: provider.ChannelSnapshot{
			Key: "openai-main", PluginKey: "openai", PluginVersionID: 1, BaseURL: baseURL, Settings: settings,
		},
		Plugin:          provider.PluginSnapshot{Key: "openai", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

func (e *openaiEnv) runtime(baseURL string) *provider.ChannelRuntime {
	return e.snapshot(baseURL, "", modelcfg.Capabilities{}).Runtime()
}

func openaiVisionCaps() modelcfg.Capabilities {
	return modelcfg.Capabilities{
		Prompt: modelcfg.PromptSpec{MaxLength: 10000},
		Refs:   modelcfg.Refs{Image: modelcfg.RefSpec{On: true, Max: 1, MaxMB: 10}},
	}
}

func openaiUpstream(status int, body string) (*fakeNewAPI, *httptest.Server) {
	up := &fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	}}
	return up, httptest.NewServer(up)
}

func openaiSubmit(t *testing.T, env *openaiEnv, snap *provider.Snapshot, input map[string]any) (*provider.SubmitResult, error) {
	t.Helper()
	return env.exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1, UserID: 9}, Input: input})
}

func TestOpenAIPrecheckAndMeta(t *testing.T) {
	env := newOpenAIEnv(t, nil)
	if env.meta.Key != "openai" || env.meta.Name != "OpenAI" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthBearer {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("text"); !ok || ep.Mode != pluginmeta.ModeSync || len(env.meta.Endpoints) != 1 {
		t.Fatalf("只应声明 text sync：%+v", env.meta.Endpoints)
	}
	api, ok := env.meta.ChannelSettings.Get("api")
	if !ok || api.Type != "enum" || api.Default != "responses" || len(api.Options) != 2 ||
		api.Options[0] != "responses" || api.Options[1] != "chat_completions" || api.Label == "" {
		t.Fatalf("渠道设置 api 不符：%+v", api)
	}
}

// 默认走 Responses：路径、Bearer、instructions、max_output_tokens、store=false、字符串 input。
func TestOpenAIResponsesDefaultRequest(t *testing.T) {
	up, srv := openaiUpstream(0, `{"status":"completed","output":[{"type":"reasoning","summary":[]},{"type":"message","content":[{"type":"output_text","text":"你好，"},{"type":"output_text","text":"世界"}]}],"usage":{"input_tokens":12,"output_tokens":34}}`)
	defer srv.Close()
	env := newOpenAIEnv(t, nil)
	snap := env.snapshot(srv.URL, "", promptCaps)
	snap.Model.Params = map[string]any{"system": "你是助手", "max_tokens": float64(256), "extra": map[string]any{"reasoning": map[string]any{"effort": "low"}, "store": true}}
	res, err := openaiSubmit(t, env, snap, map[string]any{"prompt": "打个招呼", "temperature": 0.3, "max_tokens": float64(1000)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Immediate == nil || res.Immediate.Status != provider.StatusSucceeded || len(res.Immediate.Outputs) != 1 || res.Immediate.Outputs[0].Text != "你好，世界" {
		t.Fatalf("文本结果不符：%+v", res)
	}
	if u := res.Immediate.Outputs[0].Usage; u == nil || u.InputTokens != 12 || u.OutputTokens != 34 {
		t.Fatalf("应回传 Token 用量：%+v", u)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/v1/responses" || got.Auth != "Bearer sk-openai" {
		t.Fatalf("上游请求不符：%+v", got)
	}
	b := got.Body
	if b["model"] != "gpt-test" || b["input"] != "打个招呼" || b["instructions"] != "你是助手" || b["stream"] != false ||
		b["store"] != false || b["max_output_tokens"] != float64(256) || b["temperature"] != 0.3 {
		t.Fatalf("请求体不符：%v", b)
	}
	if _, has := b["max_tokens"]; has {
		t.Fatalf("Responses 不应带 max_tokens：%v", b)
	}
	if r, _ := b["reasoning"].(map[string]any); r["effort"] != "low" {
		t.Fatalf("extra 应合并进顶层：%v", b)
	}
}

// 用户输入的 system / max_tokens 优先；没给 temperature 就不发；没有任何上限就不发 max_output_tokens。
func TestOpenAIResponsesInputOverrides(t *testing.T) {
	up, srv := openaiUpstream(0, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`)
	defer srv.Close()
	env := newOpenAIEnv(t, nil)
	res, err := openaiSubmit(t, env, env.snapshot(srv.URL, "responses", promptCaps), map[string]any{"prompt": "x", "system": "S", "max_tokens": float64(50)})
	if err != nil {
		t.Fatal(err)
	}
	if u := res.Immediate.Outputs[0].Usage; u != nil {
		t.Fatalf("上游没有 usage 就不应带：%+v", u)
	}
	b := up.last().Body
	if b["instructions"] != "S" || b["max_output_tokens"] != float64(50) {
		t.Fatalf("请求体不符：%v", b)
	}
	if _, has := b["temperature"]; has {
		t.Fatalf("没填 temperature 不应发送：%v", b)
	}
	if _, err := openaiSubmit(t, env, env.snapshot(srv.URL, "responses", promptCaps), map[string]any{"prompt": "x"}); err != nil {
		t.Fatal(err)
	}
	if _, has := up.last().Body["max_output_tokens"]; has {
		t.Fatalf("没有上限不应发送 max_output_tokens：%v", up.last().Body)
	}
	// 用户值超过 params 上限时封顶
	snap := env.snapshot(srv.URL, "responses", promptCaps)
	snap.Model.Params = map[string]any{"max_tokens": float64(100)}
	if _, err := openaiSubmit(t, env, snap, map[string]any{"prompt": "x", "max_tokens": float64(999)}); err != nil {
		t.Fatal(err)
	}
	if up.last().Body["max_output_tokens"] != float64(100) {
		t.Fatalf("应按 params 上限封顶：%v", up.last().Body)
	}
}

// Responses 视觉：input 变成消息数组，图片是 input_image 的签名 URL 字符串。
func TestOpenAIResponsesImage(t *testing.T) {
	up, srv := openaiUpstream(0, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"一只猫"}]}]}`)
	defer srv.Close()
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("PNG")}}
	env := newOpenAIEnv(t, func(o *plugin.Options) { o.Assets = assets })
	if _, err := openaiSubmit(t, env, env.snapshot(srv.URL, "responses", openaiVisionCaps()), map[string]any{"prompt": "图里有什么", "images": []uint64{5}}); err != nil {
		t.Fatal(err)
	}
	input, _ := up.last().Body["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("input 应是一条消息：%v", up.last().Body["input"])
	}
	msg := input[0].(map[string]any)
	content, _ := msg["content"].([]any)
	if msg["role"] != "user" || len(content) != 2 {
		t.Fatalf("消息不符：%v", msg)
	}
	text, img := content[0].(map[string]any), content[1].(map[string]any)
	if text["type"] != "input_text" || text["text"] != "图里有什么" || img["type"] != "input_image" || img["image_url"] != "https://cdn.example.com/a.png" {
		t.Fatalf("内容块不符：%v", content)
	}
}

// Responses 响应的失败形态。
func TestOpenAIResponsesBadResponses(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		class provider.ErrorClass
	}{
		{"没有文本", `{"status":"completed","output":[{"type":"reasoning"}]}`, provider.ClassTerminal},
		{"空输出", `{"status":"completed","output":[]}`, provider.ClassTerminal},
		{"截断且无文本", `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"reasoning"}]}`, provider.ClassTerminal},
		{"内容过滤截断", `{"status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[]}`, provider.ClassModeration},
		{"拒答", `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"I can't help with that"}]}]}`, provider.ClassModeration},
		{"失败状态带 error", `{"status":"failed","error":{"code":"server_error","message":"boom"},"output":[]}`, provider.ClassTerminal},
		{"error 对象", `{"error":{"message":"content policy violated"}}`, provider.ClassModeration},
		{"失败状态余额", `{"status":"failed","error":{"code":"insufficient_quota","message":"You exceeded your current quota"},"output":[]}`, provider.ClassProviderBalance},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv := openaiUpstream(0, tc.body)
			defer srv.Close()
			env := newOpenAIEnv(t, nil)
			res, err := openaiSubmit(t, env, env.snapshot(srv.URL, "", promptCaps), map[string]any{"prompt": "x"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Immediate == nil || res.Immediate.Status != provider.StatusFailed || res.Immediate.ErrorClass != tc.class || res.Immediate.ErrorMessage == "" {
				t.Fatalf("应失败为 %s：%+v", tc.class, res.Immediate)
			}
		})
	}
}

// 截断但已有文本：仍算成功，文本照常返回。
func TestOpenAIResponsesIncompleteWithText(t *testing.T) {
	_, srv := openaiUpstream(0, `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","content":[{"type":"output_text","text":"半截"}]}],"usage":{"input_tokens":1,"output_tokens":2}}`)
	defer srv.Close()
	env := newOpenAIEnv(t, nil)
	res, err := openaiSubmit(t, env, env.snapshot(srv.URL, "", promptCaps), map[string]any{"prompt": "x"})
	if err != nil || res.Immediate.Status != provider.StatusSucceeded || res.Immediate.Outputs[0].Text != "半截" {
		t.Fatalf("已有文本应成功：%+v %v", res, err)
	}
}

// 切到 Chat Completions：路径、max_completion_tokens（不是 max_tokens）、system + user、图片 image_url。
func TestOpenAIChatCompletions(t *testing.T) {
	up, srv := openaiUpstream(0, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"你好"}}],"usage":{"prompt_tokens":7,"completion_tokens":9}}`)
	defer srv.Close()
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("PNG")}}
	env := newOpenAIEnv(t, func(o *plugin.Options) { o.Assets = assets })
	snap := env.snapshot(srv.URL, "chat_completions", openaiVisionCaps())
	snap.Model.Params = map[string]any{"system": "你是助手", "max_tokens": float64(128), "extra": map[string]any{"reasoning_effort": "low"}}
	res, err := openaiSubmit(t, env, snap, map[string]any{"prompt": "看图", "images": []uint64{5}, "temperature": 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if res.Immediate.Status != provider.StatusSucceeded || res.Immediate.Outputs[0].Text != "你好" {
		t.Fatalf("结果不符：%+v", res.Immediate)
	}
	if u := res.Immediate.Outputs[0].Usage; u == nil || u.InputTokens != 7 || u.OutputTokens != 9 {
		t.Fatalf("用量不符：%+v", u)
	}
	got := up.last()
	if got.Path != "/v1/chat/completions" || got.Auth != "Bearer sk-openai" {
		t.Fatalf("上游请求不符：%+v", got)
	}
	b := got.Body
	if b["model"] != "gpt-test" || b["max_completion_tokens"] != float64(128) || b["stream"] != false || b["temperature"] != 0.5 || b["reasoning_effort"] != "low" {
		t.Fatalf("请求体不符：%v", b)
	}
	if _, has := b["max_tokens"]; has {
		t.Fatalf("不应使用已弃用的 max_tokens：%v", b)
	}
	msgs, _ := b["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" || msgs[0].(map[string]any)["content"] != "你是助手" {
		t.Fatalf("应有 system + user：%v", b["messages"])
	}
	parts, _ := msgs[1].(map[string]any)["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("user 内容应含文本与图片：%v", msgs[1])
	}
	img := parts[1].(map[string]any)
	if parts[0].(map[string]any)["type"] != "text" || img["type"] != "image_url" || img["image_url"].(map[string]any)["url"] != "https://cdn.example.com/a.png" {
		t.Fatalf("内容块不符：%v", parts)
	}
}

func TestOpenAIChatBadResponses(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		class provider.ErrorClass
	}{
		{"空内容", `{"choices":[{"finish_reason":"stop","message":{"content":""}}]}`, provider.ClassTerminal},
		{"没有 choices", `{"choices":[]}`, provider.ClassTerminal},
		{"截断无文本", `{"choices":[{"finish_reason":"length","message":{"content":null}}]}`, provider.ClassTerminal},
		{"内容过滤", `{"choices":[{"finish_reason":"content_filter","message":{"content":null}}]}`, provider.ClassModeration},
		{"拒答", `{"choices":[{"finish_reason":"stop","message":{"content":null,"refusal":"I'm sorry, I can't"}}]}`, provider.ClassModeration},
		{"error 体", `{"error":{"message":"The server had an error"}}`, provider.ClassTerminal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv := openaiUpstream(0, tc.body)
			defer srv.Close()
			env := newOpenAIEnv(t, nil)
			res, err := openaiSubmit(t, env, env.snapshot(srv.URL, "chat_completions", promptCaps), map[string]any{"prompt": "x"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Immediate == nil || res.Immediate.Status != provider.StatusFailed || res.Immediate.ErrorClass != tc.class || res.Immediate.ErrorMessage == "" {
				t.Fatalf("应失败为 %s：%+v", tc.class, res.Immediate)
			}
		})
	}
}

// 截断但有文本：成功。
func TestOpenAIChatLengthWithText(t *testing.T) {
	_, srv := openaiUpstream(0, `{"choices":[{"finish_reason":"length","message":{"content":"半截"}}]}`)
	defer srv.Close()
	env := newOpenAIEnv(t, nil)
	res, err := openaiSubmit(t, env, env.snapshot(srv.URL, "chat_completions", promptCaps), map[string]any{"prompt": "x"})
	if err != nil || res.Immediate.Status != provider.StatusSucceeded || res.Immediate.Outputs[0].Text != "半截" {
		t.Fatalf("已有文本应成功：%+v %v", res, err)
	}
}

// HTTP 非 2xx：两种接口共用同一套分类。
func TestOpenAIClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"额度用尽", 429, `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota","code":"insufficient_quota"}}`, provider.ClassProviderBalance},
		{"账单硬上限", 400, `{"error":{"message":"Billing hard limit has been reached","type":"invalid_request_error","code":"billing_hard_limit_reached"}}`, provider.ClassProviderBalance},
		{"普通限流", 429, `{"error":{"message":"Rate limit reached for gpt in organization on requests per min.","type":"requests","code":"rate_limit_exceeded"}}`, provider.ClassRetryable},
		{"内容策略", 400, `{"error":{"message":"Your request was rejected","type":"invalid_request_error","code":"content_policy_violation"}}`, provider.ClassModeration},
		{"安全系统", 400, `{"error":{"message":"Your request was rejected as a result of our safety system.","type":"invalid_request_error","code":null}}`, provider.ClassModeration},
		{"服务端 500", 500, `{"error":{"message":"The server had an error","type":"server_error"}}`, provider.ClassRetryable},
		{"过载 503", 503, `bad gateway`, provider.ClassRetryable},
		{"Key 无效", 401, `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`, provider.ClassTerminal},
		{"模型不存在", 404, `{"error":{"message":"The model does not exist","type":"invalid_request_error","code":"model_not_found"}}`, provider.ClassTerminal},
		{"参数错误", 400, `{"error":{"message":"Unsupported parameter: temperature","type":"invalid_request_error"}}`, provider.ClassTerminal},
	}
	for _, api := range []string{"responses", "chat_completions"} {
		for _, tc := range cases {
			t.Run(api+"/"+tc.name, func(t *testing.T) {
				_, srv := openaiUpstream(tc.status, tc.body)
				defer srv.Close()
				env := newOpenAIEnv(t, nil)
				_, err := openaiSubmit(t, env, env.snapshot(srv.URL, api, promptCaps), map[string]any{"prompt": "x"})
				if err == nil || provider.ClassOf(err) != tc.class {
					t.Fatalf("分类应是 %s：%v", tc.class, err)
				}
			})
		}
	}
}

// 连通性检查：GET /v1/models，Bearer 注入；401 失败。
func TestOpenAICheck(t *testing.T) {
	env := newOpenAIEnv(t, nil)
	good, goodSrv := openaiUpstream(0, `{"object":"list","data":[]}`)
	defer goodSrv.Close()
	res, err := env.exec.Check(context.Background(), env.runtime(goodSrv.URL))
	if err != nil || res == nil || !res.OK {
		t.Fatalf("检查应成功：%+v %v", res, err)
	}
	if got := good.last(); got.Method != "GET" || got.Path != "/v1/models" || got.Auth != "Bearer sk-openai" {
		t.Fatalf("检查请求不符：%+v", got)
	}
	_, badSrv := openaiUpstream(401, `{"error":{"message":"Incorrect API key provided"}}`)
	defer badSrv.Close()
	res, err = env.exec.Check(context.Background(), env.runtime(badSrv.URL))
	if err == nil || (res != nil && res.OK) {
		t.Fatalf("401 应检查失败：%+v %v", res, err)
	}
}

// 导入：只导文本类（gpt-* / o 系列 / chatgpt-*），排除向量、语音、图片、审核、实时、转写等。
func TestOpenAIImport(t *testing.T) {
	_, srv := openaiUpstream(0, `{"object":"list","data":[
		{"id":"gpt-4o"},{"id":"gpt-4.1-mini"},{"id":"o3"},{"id":"o4-mini"},{"id":"chatgpt-4o-latest"},{"id":"gpt-5"},
		{"id":"text-embedding-3-small"},{"id":"tts-1"},{"id":"whisper-1"},{"id":"dall-e-3"},{"id":"gpt-image-1"},
		{"id":"omni-moderation-latest"},{"id":"gpt-4o-realtime-preview"},{"id":"gpt-4o-transcribe"},{"id":"gpt-4o-audio-preview"},
		{"id":"gpt-4o-mini-tts"},{"id":"gpt-3.5-turbo-instruct"},{"id":"davinci-002"},{"id":""},{"object":"model"}
	]}`)
	defer srv.Close()
	env := newOpenAIEnv(t, nil)
	drafts, err := env.exec.Import(context.Background(), env.runtime(srv.URL), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]provider.ModelDraft{}
	for _, d := range drafts {
		got[d.UpstreamModel] = d
	}
	want := []string{"gpt-4o", "gpt-4.1-mini", "o3", "o4-mini", "chatgpt-4o-latest", "gpt-5"}
	if len(drafts) != len(want) {
		t.Fatalf("应只导入 %d 个文本模型：%+v", len(want), drafts)
	}
	for _, id := range want {
		if d, ok := got[id]; !ok || d.Kind != "text" || d.Label != id {
			t.Fatalf("缺少或不符的草稿 %s：%+v", id, d)
		}
	}
}
