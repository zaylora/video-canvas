package plugin_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/plugins"
)

// anthropicEnv 用真实 runner 预检并装载内置 anthropic.js，元数据取自预检结果。
type anthropicEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newAnthropicEnv(t *testing.T, mod func(*plugin.Options)) *anthropicEnv {
	t.Helper()
	src, err := plugins.Source("anthropic")
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
		t.Fatalf("内置 anthropic.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	opts := plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "sk-ant"}, IPAllowed: allowLoopback}
	if mod != nil {
		mod(&opts)
	}
	return &anthropicEnv{exec: plugin.New(opts), meta: meta, sha: pre.SHA256}
}

func (e *anthropicEnv) snapshot(baseURL string, caps modelcfg.Capabilities) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: "text", UpstreamModel: "claude-up", Capabilities: caps},
		Channel: provider.ChannelSnapshot{
			Key: "anthropic-main", PluginKey: "anthropic", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "anthropic", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

func (e *anthropicEnv) runtime(baseURL string) *provider.ChannelRuntime {
	return e.snapshot(baseURL, promptCaps).Runtime()
}

type anthropicRecorded struct {
	Method, Path, Key, Version string
	Body                       map[string]any
}

type fakeAnthropic struct {
	mu       sync.Mutex
	requests []anthropicRecorded
	handler  func(w http.ResponseWriter, r *http.Request)
}

func (f *fakeAnthropic) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.requests = append(f.requests, anthropicRecorded{
		Method: r.Method, Path: r.URL.Path, Key: r.Header.Get("x-api-key"), Version: r.Header.Get("anthropic-version"), Body: body,
	})
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	f.handler(w, r)
}

func (f *fakeAnthropic) last() anthropicRecorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

// anthropicSubmit 起一个返回固定响应的假上游并提交一次文本任务。
func anthropicSubmit(t *testing.T, status int, resp string, params map[string]any, input map[string]any) (*provider.SubmitResult, *fakeAnthropic, error) {
	t.Helper()
	up := &fakeAnthropic{handler: func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	env := newAnthropicEnv(t, func(o *plugin.Options) { o.Assets = &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("PNG")}} })
	snap := env.snapshot(srv.URL, modelcfg.Capabilities{
		Prompt: modelcfg.PromptSpec{MaxLength: 10000},
		Refs:   modelcfg.Refs{Image: modelcfg.RefSpec{On: true, Max: 1, MaxMB: 10}},
	})
	snap.Model.Params = params
	if input["prompt"] == nil {
		input["prompt"] = "你好"
	}
	res, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1, UserID: 9}, Input: input})
	return res, up, err
}

func TestAnthropicPrecheckAndMeta(t *testing.T) {
	env := newAnthropicEnv(t, nil)
	m := env.meta
	if m.Key != "anthropic" || m.Name != "Anthropic" || m.Version != "1.0.0" {
		t.Fatalf("meta 不符合预期：%+v", m)
	}
	if m.Auth.Type != pluginmeta.AuthHeader || m.Auth.Name != "x-api-key" {
		t.Fatalf("鉴权应是 x-api-key 头：%+v", m.Auth)
	}
	if ep, ok := m.Endpoint("text"); !ok || ep.Mode != pluginmeta.ModeSync {
		t.Fatalf("text 应是 sync：%+v", ep)
	}
	if len(m.Endpoints) != 1 {
		t.Fatalf("只应声明 text：%+v", m.Endpoints)
	}
}

func TestAnthropicRequestShape(t *testing.T) {
	res, up, err := anthropicSubmit(t, 200,
		`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"你好，世界"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":34}}`,
		map[string]any{"system": "你是助手", "max_tokens": float64(256), "extra": map[string]any{"top_p": 0.9, "model": "evil"}},
		map[string]any{"prompt": "打个招呼", "temperature": 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if res.Immediate == nil || res.Immediate.Status != provider.StatusSucceeded || len(res.Immediate.Outputs) != 1 || res.Immediate.Outputs[0].Text != "你好，世界" {
		t.Fatalf("结果不符：%+v", res)
	}
	if u := res.Immediate.Outputs[0].Usage; u == nil || u.InputTokens != 12 || u.OutputTokens != 34 {
		t.Fatalf("usage 不符：%+v", u)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/v1/messages" || got.Key != "sk-ant" || got.Version != "2023-06-01" {
		t.Fatalf("请求头/路径不符：%+v", got)
	}
	b := got.Body
	if b["model"] != "claude-up" || b["max_tokens"] != float64(256) || b["stream"] != false || b["temperature"] != 0.5 || b["system"] != "你是助手" || b["top_p"] != 0.9 {
		t.Fatalf("请求体不符：%v", b)
	}
	msgs, _ := b["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("system 不应进 messages：%v", msgs)
	}
	if m := msgs[0].(map[string]any); m["role"] != "user" || m["content"] != "打个招呼" {
		t.Fatalf("user 消息不符：%v", m)
	}
}

func TestAnthropicMaxTokens(t *testing.T) {
	ok := `{"content":[{"type":"text","text":"x"}],"stop_reason":"end_turn"}`
	cases := []struct {
		name   string
		params map[string]any
		input  map[string]any
		want   float64
	}{
		{"都没给用默认", map[string]any{}, map[string]any{}, 4096},
		{"只有上限", map[string]any{"max_tokens": float64(1000)}, map[string]any{}, 1000},
		{"用户值不超过上限", map[string]any{"max_tokens": float64(1000)}, map[string]any{"max_tokens": float64(300)}, 300},
		{"用户值超过上限被压低", map[string]any{"max_tokens": float64(1000)}, map[string]any{"max_tokens": float64(5000)}, 1000},
		{"没有上限时用用户值", map[string]any{}, map[string]any{"max_tokens": float64(777)}, 777},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, up, err := anthropicSubmit(t, 200, ok, tc.params, tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if got := up.last().Body["max_tokens"]; got != tc.want {
				t.Fatalf("max_tokens 应是 %v：%v", tc.want, got)
			}
		})
	}
}

func TestAnthropicSystemFromInputWins(t *testing.T) {
	_, up, err := anthropicSubmit(t, 200, `{"content":[{"type":"text","text":"x"}]}`,
		map[string]any{"system": "固定"}, map[string]any{"system": "输入"})
	if err != nil {
		t.Fatal(err)
	}
	if up.last().Body["system"] != "输入" {
		t.Fatalf("输入的 system 应优先：%v", up.last().Body)
	}
}

func TestAnthropicImageURL(t *testing.T) {
	_, up, err := anthropicSubmit(t, 200, `{"content":[{"type":"text","text":"一只猫"}]}`,
		map[string]any{}, map[string]any{"prompt": "图里是什么", "images": []uint64{5}})
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := up.last().Body["messages"].([]any)
	content, _ := msgs[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("应有图片与文字两个块：%v", content)
	}
	img := content[0].(map[string]any)
	src, _ := img["source"].(map[string]any)
	if img["type"] != "image" || src["type"] != "url" || src["url"] != "https://cdn.example.com/a.png" {
		t.Fatalf("图片块不符：%v", img)
	}
	if txt := content[1].(map[string]any); txt["type"] != "text" || txt["text"] != "图里是什么" {
		t.Fatalf("文字块不符：%v", txt)
	}
}

func TestAnthropicOnlyTextBlocks(t *testing.T) {
	res, _, err := anthropicSubmit(t, 200,
		`{"content":[{"type":"thinking","thinking":"想一想","signature":"s"},{"type":"text","text":"甲"},{"type":"tool_use","id":"t","name":"n","input":{}},{"type":"text","text":"乙"},{"type":"redacted_thinking","data":"x"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":2}}`,
		map[string]any{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Immediate.Status != provider.StatusSucceeded || res.Immediate.Outputs[0].Text != "甲乙" {
		t.Fatalf("应只拼接文本块：%+v", res.Immediate)
	}
}

func TestAnthropicUsageCountsCache(t *testing.T) {
	res, _, err := anthropicSubmit(t, 200,
		`{"content":[{"type":"text","text":"x"}],"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":20,"cache_read_input_tokens":30}}`,
		map[string]any{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if u := res.Immediate.Outputs[0].Usage; u == nil || u.InputTokens != 60 || u.OutputTokens != 5 {
		t.Fatalf("输入应含缓存 Token：%+v", u)
	}
}

func TestAnthropicNoUsageWhenMissing(t *testing.T) {
	res, _, err := anthropicSubmit(t, 200, `{"content":[{"type":"text","text":"x"}]}`, map[string]any{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Immediate.Outputs[0].Usage != nil {
		t.Fatalf("没有 usage 时不应带：%+v", res.Immediate.Outputs[0].Usage)
	}
}

func TestAnthropicFailedResults(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		class provider.ErrorClass
		msg   string
	}{
		{"空内容", `{"content":[],"stop_reason":"end_turn"}`, provider.ClassTerminal, "空内容"},
		{"被思考占满", `{"content":[{"type":"thinking","thinking":"..."}],"stop_reason":"max_tokens"}`, provider.ClassTerminal, "max_tokens"},
		{"截断无文本", `{"content":[],"stop_reason":"max_tokens"}`, provider.ClassTerminal, "max_tokens"},
		{"拒答", `{"content":[{"type":"text","text":"抱歉"}],"stop_reason":"refusal"}`, provider.ClassModeration, ""},
		{"拒答无文本", `{"content":[],"stop_reason":"refusal"}`, provider.ClassModeration, ""},
		{"错误体", `{"type":"error","error":{"type":"api_error","message":"boom"}}`, provider.ClassTerminal, "boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _, err := anthropicSubmit(t, 200, tc.body, map[string]any{}, map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			r := res.Immediate
			if r == nil || r.Status != provider.StatusFailed || r.ErrorClass != tc.class {
				t.Fatalf("应失败且分类为 %s：%+v", tc.class, r)
			}
			if tc.msg != "" && !strings.Contains(r.ErrorMessage, tc.msg) {
				t.Fatalf("原因应含 %q：%q", tc.msg, r.ErrorMessage)
			}
		})
	}
}

func TestAnthropicClassifyHTTPErrors(t *testing.T) {
	e := func(typ, msg string) string {
		return `{"type":"error","error":{"type":"` + typ + `","message":"` + msg + `"},"request_id":"req_1"}`
	}
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"鉴权失败", 401, e("authentication_error", "invalid x-api-key"), provider.ClassTerminal},
		{"计费错误", 402, e("billing_error", "payment issue"), provider.ClassProviderBalance},
		{"余额不足 400", 400, e("invalid_request_error", "Your credit balance is too low"), provider.ClassProviderBalance},
		{"花费上限 400", 400, e("invalid_request_error", "You have reached your workspace spend limit"), provider.ClassProviderBalance},
		{"参数错误", 400, e("invalid_request_error", "messages: field required"), provider.ClassTerminal},
		{"无权限", 403, e("permission_error", "not allowed"), provider.ClassTerminal},
		{"模型不存在", 404, e("not_found_error", "model: x"), provider.ClassTerminal},
		{"请求过大", 413, e("request_too_large", "too large"), provider.ClassTerminal},
		{"限流", 429, e("rate_limit_error", "Number of request tokens has exceeded your per-minute rate limit"), provider.ClassRetryable},
		{"月度花费上限 429", 429, e("rate_limit_error", "You have reached your specified API usage limits. spend limit"), provider.ClassProviderBalance},
		{"内部错误", 500, e("api_error", "internal"), provider.ClassRetryable},
		{"超时", 504, e("timeout_error", "timeout"), provider.ClassRetryable},
		{"过载", 529, e("overloaded_error", "Overloaded"), provider.ClassRetryable},
		{"审核", 400, e("invalid_request_error", "blocked by content policy violation"), provider.ClassModeration},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := anthropicSubmit(t, tc.status, tc.body, map[string]any{}, map[string]any{})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
		})
	}
}

func TestAnthropicCheckAndImport(t *testing.T) {
	up := &fakeAnthropic{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"type":"model","id":"claude-sonnet-4-5","display_name":"Claude Sonnet 4.5"},{"type":"model","id":"claude-3-haiku"},{"type":"model","id":""},{"type":"model"}],"has_more":false}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newAnthropicEnv(t, nil)

	res, err := env.exec.Check(context.Background(), env.runtime(srv.URL))
	if err != nil || res == nil || !res.OK {
		t.Fatalf("检查应成功：%+v %v", res, err)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/v1/models" || got.Key != "sk-ant" || got.Version != "2023-06-01" {
		t.Fatalf("检查请求不符：%+v", got)
	}

	drafts, err := env.exec.Import(context.Background(), env.runtime(srv.URL), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 2 {
		t.Fatalf("应导入两个文本模型：%+v", drafts)
	}
	if drafts[0].UpstreamModel != "claude-sonnet-4-5" || drafts[0].Kind != "text" || drafts[0].Label != "Claude Sonnet 4.5" {
		t.Fatalf("草稿不符：%+v", drafts[0])
	}
	if drafts[1].Label != "claude-3-haiku" {
		t.Fatalf("没有 display_name 时用 id：%+v", drafts[1])
	}

	bad := httptest.NewServer(&fakeAnthropic{handler: func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}})
	defer bad.Close()
	res, err = env.exec.Check(context.Background(), env.runtime(bad.URL))
	if err == nil || (res != nil && res.OK) {
		t.Fatalf("401 应检查失败：%+v %v", res, err)
	}
}
