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

// openaichatimageEnv 用真实 runner 预检并装载内置 openai-chat-image.js，元数据取自预检结果。
type openaichatimageEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newOpenAIChatImageEnv(t *testing.T) *openaichatimageEnv {
	t.Helper()
	src, err := plugins.Source("openai-chat-image")
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
		t.Fatalf("内置 openai-chat-image.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{1: []byte("PNG1"), 2: []byte("PNG2")}}
	exec := plugin.New(plugin.Options{
		Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "sk-chat"},
		Assets: assets, IPAllowed: allowLoopback,
	})
	return &openaichatimageEnv{exec: exec, meta: meta, sha: pre.SHA256}
}

func (e *openaichatimageEnv) snapshot(baseURL, upstream string) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: "image", UpstreamModel: upstream, Capabilities: openaichatimageCaps()},
		Channel: provider.ChannelSnapshot{
			Key: "yingce", PluginKey: "openai-chat-image", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "openai-chat-image", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

// openaichatimageCaps 是测试用的宽松能力：开放参考图、画面比例与分辨率，让宿主的输入校验放行。
func openaichatimageCaps() modelcfg.Capabilities {
	enum := func(opts ...any) modelcfg.ParamField {
		return modelcfg.ParamField{Type: modelcfg.ParamEnum, Label: "x", Open: true, Options: opts}
	}
	caps := modelcfg.Capabilities{Prompt: modelcfg.PromptSpec{MaxLength: 10000}}
	caps.Ops = []string{modelcfg.OpT2I, modelcfg.OpI2I}
	caps.Refs = modelcfg.Refs{Image: modelcfg.RefSpec{On: true, Max: 9, MaxMB: 10}}
	caps.Params = modelcfg.ParamSet{
		{Name: "aspect_ratio", ParamField: enum("1:1", "3:2", "2:3", "16:9", "9:16", "4:3", "3:4", "21:9")},
		{Name: "resolution", ParamField: enum("1K", "2K", "4K")},
	}
	return caps
}

type openaichatimageRecorded struct {
	Method, Path, Auth string
	JSON               map[string]any
}

type openaichatimageUpstream struct {
	mu       sync.Mutex
	requests []openaichatimageRecorded
	handler  func(w http.ResponseWriter, r *http.Request)
}

func (u *openaichatimageUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := openaichatimageRecorded{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization")}
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &rec.JSON)
	u.mu.Lock()
	u.requests = append(u.requests, rec)
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	u.handler(w, r)
}

func (u *openaichatimageUpstream) last() openaichatimageRecorded {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requests[len(u.requests)-1]
}

func (u *openaichatimageUpstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.requests)
}

func openaichatimageServer(t *testing.T, status int, body string) (*httptest.Server, *openaichatimageUpstream) {
	t.Helper()
	up := &openaichatimageUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	}}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	return srv, up
}

// chatBody 把 message 对象包成 Chat Completions 响应。
func chatBody(message string) string {
	return `{"id":"x","choices":[{"index":0,"finish_reason":"stop","message":` + message + `}]}`
}

func (e *openaichatimageEnv) submit(t *testing.T, baseURL, model string, input map[string]any) (*provider.SubmitResult, error) {
	t.Helper()
	return e.exec.Submit(context.Background(), e.snapshot(baseURL, model), provider.SubmitInput{
		Task: provider.TaskRef{ID: 2, UserID: 9}, Input: input,
	})
}

func TestOpenAIChatImagePrecheckAndMeta(t *testing.T) {
	env := newOpenAIChatImageEnv(t)
	if env.meta.Key != "openai-chat-image" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthBearer || env.meta.Vendor != "openai" {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if len(env.meta.Endpoints) != 1 {
		t.Fatalf("只应声明一种类型：%+v", env.meta.Endpoints)
	}
	ep, ok := env.meta.Endpoint("image")
	if !ok || ep.Mode != pluginmeta.ModeSync || ep.Path != "/v1/chat/completions" {
		t.Fatalf("image 应是 sync 的 /v1/chat/completions：%+v", ep)
	}
}

// 文生图：只发 model + messages + stream:false；没给画面比例和分辨率就不发 image_config。
func TestOpenAIChatImageTextToImage(t *testing.T) {
	srv, up := openaichatimageServer(t, 0, chatBody(`{"role":"assistant","content":"![image](https://cdn.example.com/a.png)"}`))
	env := newOpenAIChatImageEnv(t)
	sub, err := env.submit(t, srv.URL, "nano-banana-2.1", map[string]any{"prompt": "一只猫"})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Immediate == nil || sub.Immediate.Status != provider.StatusSucceeded || len(sub.Immediate.Outputs) != 1 {
		t.Fatalf("应同步成功：%+v", sub)
	}
	o := sub.Immediate.Outputs[0]
	if o.URL != "https://cdn.example.com/a.png" || o.MediaType != "image" || o.Mime != "image/png" {
		t.Fatalf("产物不符：%+v", o)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/v1/chat/completions" || got.Auth != "Bearer sk-chat" {
		t.Fatalf("请求行不符：%+v", got)
	}
	b := got.JSON
	if b["model"] != "nano-banana-2.1" || b["stream"] != false {
		t.Fatalf("请求体不符：%v", b)
	}
	msgs, _ := b["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["role"] != "user" || msgs[0].(map[string]any)["content"] != "一只猫" {
		t.Fatalf("消息不符：%v", msgs)
	}
	if _, ok := b["image_config"]; ok {
		t.Fatalf("没给比例与分辨率不应发 image_config：%v", b)
	}
}

// 画面比例与分辨率放进 image_config（aspect_ratio / image_size）。
func TestOpenAIChatImageImageConfig(t *testing.T) {
	srv, up := openaichatimageServer(t, 0, chatBody(`{"content":"![i](https://cdn.example.com/a.png)"}`))
	env := newOpenAIChatImageEnv(t)
	if _, err := env.submit(t, srv.URL, "nano-banana-2.1", map[string]any{"prompt": "p", "aspect_ratio": "16:9", "resolution": "2K"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := up.last().JSON["image_config"].(map[string]any)
	if cfg["aspect_ratio"] != "16:9" || cfg["image_size"] != "2K" || len(cfg) != 2 {
		t.Fatalf("image_config 不符：%v", up.last().JSON)
	}
	if _, err := env.submit(t, srv.URL, "nano-banana-2.1", map[string]any{"prompt": "p", "aspect_ratio": "1:1"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ = up.last().JSON["image_config"].(map[string]any)
	if cfg["aspect_ratio"] != "1:1" || len(cfg) != 1 {
		t.Fatalf("只给比例时 image_config 只含比例：%v", up.last().JSON)
	}
}

// 参考图：全部作为 image_url 块跟在文本后面，URL 由宿主换成签名地址。
func TestOpenAIChatImageReferenceImages(t *testing.T) {
	srv, up := openaichatimageServer(t, 0, chatBody(`{"content":"![i](https://cdn.example.com/a.png)"}`))
	env := newOpenAIChatImageEnv(t)
	if _, err := env.submit(t, srv.URL, "nano-banana-2.1", map[string]any{"prompt": "换成夜景", "op": "i2i", "images": []uint64{1, 2}}); err != nil {
		t.Fatal(err)
	}
	msgs, _ := up.last().JSON["messages"].([]any)
	parts, _ := msgs[0].(map[string]any)["content"].([]any)
	if len(parts) != 3 {
		t.Fatalf("应是 1 段文本 + 2 张图：%v", msgs)
	}
	if text := parts[0].(map[string]any); text["type"] != "text" || text["text"] != "换成夜景" {
		t.Fatalf("文本块不符：%v", parts[0])
	}
	for _, p := range parts[1:] {
		img := p.(map[string]any)
		if img["type"] != "image_url" || img["image_url"].(map[string]any)["url"] != "https://cdn.example.com/a.png" {
			t.Fatalf("图片块不符：%v", p)
		}
	}
}

// 没有提示词是插件级失败，不发请求。
func TestOpenAIChatImageRequiresPrompt(t *testing.T) {
	srv, up := openaichatimageServer(t, 0, chatBody(`{"content":"x"}`))
	env := newOpenAIChatImageEnv(t)
	if _, err := env.submit(t, srv.URL, "nano-banana-2.1", map[string]any{"prompt": ""}); err == nil {
		t.Fatal("空提示词应失败")
	}
	if up.count() != 0 {
		t.Fatalf("不应发出请求：%d", up.count())
	}
}

// 各种能拿到图片的返回形态。
func TestOpenAIChatImageParseImages(t *testing.T) {
	cases := []struct {
		name, message string
		urls          []string
		mimes         []string
	}{
		{"markdown 图片", `{"content":"这是你要的图：\n\n![image](https://cdn.example.com/a.png)"}`, []string{"https://cdn.example.com/a.png"}, []string{"image/png"}},
		{"markdown 带标题", `{"content":"![x](https://cdn.example.com/a.jpg \"t\")"}`, []string{"https://cdn.example.com/a.jpg"}, []string{"image/jpeg"}},
		{"多张去重", `{"content":"![1](https://cdn.example.com/1.png) ![2](https://cdn.example.com/2.webp?x=1) ![dup](https://cdn.example.com/1.png)"}`,
			[]string{"https://cdn.example.com/1.png", "https://cdn.example.com/2.webp?x=1"}, []string{"image/png", "image/webp"}},
		{"images 字段（对象）", `{"content":"","images":[{"type":"image_url","image_url":{"url":"https://cdn.example.com/b.png"}}]}`, []string{"https://cdn.example.com/b.png"}, []string{"image/png"}},
		{"images 字段（字符串）", `{"content":null,"images":["https://cdn.example.com/c.gif"]}`, []string{"https://cdn.example.com/c.gif"}, []string{"image/gif"}},
		{"content 数组里的 image_url", `{"content":[{"type":"text","text":"ok"},{"type":"image_url","image_url":{"url":"https://cdn.example.com/d.png"}}]}`, []string{"https://cdn.example.com/d.png"}, []string{"image/png"}},
		{"裸 URL", `{"content":"图在这里 https://cdn.example.com/e.jpeg 请查收"}`, []string{"https://cdn.example.com/e.jpeg"}, []string{"image/jpeg"}},
		{"images 与 content 混用去重", `{"content":"![i](https://cdn.example.com/f.png)","images":[{"image_url":{"url":"https://cdn.example.com/f.png"}}]}`, []string{"https://cdn.example.com/f.png"}, []string{"image/png"}},
		{"夹着 base64 的 URL 仍收 URL", `{"content":"![a](data:image/png;base64,AAAA) ![b](https://cdn.example.com/g.png)"}`, []string{"https://cdn.example.com/g.png"}, []string{"image/png"}},
	}
	env := newOpenAIChatImageEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := openaichatimageServer(t, 0, chatBody(tc.message))
			sub, err := env.submit(t, srv.URL, "nano-banana-2.1", map[string]any{"prompt": "x"})
			if err != nil {
				t.Fatal(err)
			}
			r := sub.Immediate
			if r == nil || r.Status != provider.StatusSucceeded || len(r.Outputs) != len(tc.urls) {
				t.Fatalf("结果不符：%+v", r)
			}
			for i, o := range r.Outputs {
				if o.URL != tc.urls[i] || o.MediaType != "image" || o.Mime != tc.mimes[i] {
					t.Fatalf("第 %d 个产物不符：%+v", i, o)
				}
			}
		})
	}
}

// 拿不到图片的返回：base64 要明确说明不支持；只有文字时把文字带上方便排查；拒答与内容过滤按审核分类。
func TestOpenAIChatImageBadResponses(t *testing.T) {
	cases := []struct {
		name, body, contains string
		class                provider.ErrorClass
	}{
		{"markdown base64", chatBody(`{"content":"![image](data:image/png;base64,AAAA)"}`), "上游只返回 base64 图片，本协议暂不支持", provider.ClassTerminal},
		{"images 字段 base64", chatBody(`{"content":"","images":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}`), "base64", provider.ClassTerminal},
		{"只有文字", chatBody(`{"content":"抱歉，我只能描述这张图。"}`), "抱歉，我只能描述这张图。", provider.ClassTerminal},
		{"空内容", chatBody(`{"content":""}`), "没有返回图片", provider.ClassTerminal},
		{"没有 content", chatBody(`{"role":"assistant"}`), "没有返回图片", provider.ClassTerminal},
		{"非 http 的地址", chatBody(`{"content":"![i](ftp://x/a.png)"}`), "没有返回图片", provider.ClassTerminal},
		{"拒答", chatBody(`{"content":null,"refusal":"I can't help with that"}`), "I can't help with that", provider.ClassModeration},
		{"内容过滤", `{"choices":[{"finish_reason":"content_filter","message":{"content":""}}]}`, "内容过滤", provider.ClassModeration},
		{"没有 choices", `{"choices":[]}`, "没有返回 choices", provider.ClassTerminal},
		{"200 带审核错误", `{"error":{"message":"Your request was rejected by the safety system.","type":"x","code":"moderation_blocked"}}`, "safety system", provider.ClassModeration},
		{"200 带额度错误", `{"error":{"message":"You exceeded your current quota","type":"insufficient_quota","code":"insufficient_quota"}}`, "quota", provider.ClassProviderBalance},
		{"200 带其它错误", `{"error":{"message":"boom","type":"server_error","code":null}}`, "boom", provider.ClassTerminal},
	}
	env := newOpenAIChatImageEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := openaichatimageServer(t, 0, tc.body)
			sub, err := env.submit(t, srv.URL, "nano-banana-2.1", map[string]any{"prompt": "x"})
			if err != nil {
				t.Fatal(err)
			}
			r := sub.Immediate
			if r == nil || r.Status != provider.StatusFailed || r.ErrorClass != tc.class || !strings.Contains(r.ErrorMessage, tc.contains) {
				t.Fatalf("结果不符：%+v", r)
			}
		})
	}
}

// HTTP 非 2xx：显式分类，不依赖宿主按文案猜。
func TestOpenAIChatImageClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"额度", 429, `{"error":{"message":"You exceeded your current quota","type":"insufficient_quota","code":"insufficient_quota"}}`, provider.ClassProviderBalance},
		{"令牌额度用尽", 403, `{"error":{"message":"pre_consume_token_quota failed, exceeded your current quota","type":"new_api_error","code":"insufficient_user_quota"}}`, provider.ClassProviderBalance},
		{"内容策略", 400, `{"error":{"message":"Your prompt was flagged","type":"invalid_request_error","code":"content_policy_violation"}}`, provider.ClassModeration},
		{"安全系统文案", 400, `{"error":{"message":"Your request was rejected by the safety system.","type":"invalid_request_error","code":null}}`, provider.ClassModeration},
		{"限流", 429, `{"error":{"message":"Rate limit reached","type":"requests","code":"rate_limit_exceeded"}}`, provider.ClassRetryable},
		{"5xx", 503, `{"error":{"message":"overloaded","type":"server_error","code":null}}`, provider.ClassRetryable},
		{"纯文本 502", 502, `bad gateway`, provider.ClassRetryable},
		{"参数错误", 400, `{"error":{"message":"Invalid value","type":"invalid_request_error","code":"invalid_value"}}`, provider.ClassTerminal},
		{"401", 401, `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`, provider.ClassTerminal},
		{"404", 404, `{"error":{"message":"model not found","type":"invalid_request_error","code":"model_not_found"}}`, provider.ClassTerminal},
	}
	env := newOpenAIChatImageEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := openaichatimageServer(t, tc.status, tc.body)
			_, err := env.submit(t, srv.URL, "nano-banana-2.1", map[string]any{"prompt": "x"})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
		})
	}
}

// 连通检查：GET /v1/models，带 Bearer；401 失败。
func TestOpenAIChatImageCheck(t *testing.T) {
	env := newOpenAIChatImageEnv(t)
	good, up := openaichatimageServer(t, 0, `{"object":"list","data":[]}`)
	res, err := env.exec.Check(context.Background(), env.snapshot(good.URL, "nano-banana-2.1").Runtime())
	if err != nil || res == nil || !res.OK {
		t.Fatalf("检查应成功：%+v %v", res, err)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/v1/models" || got.Auth != "Bearer sk-chat" {
		t.Fatalf("检查请求不符：%+v", got)
	}
	bad, _ := openaichatimageServer(t, 401, `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`)
	res, err = env.exec.Check(context.Background(), env.snapshot(bad.URL, "nano-banana-2.1").Runtime())
	if err == nil || (res != nil && res.OK) {
		t.Fatalf("401 应检查失败：%+v %v", res, err)
	}
}

// 导入：网关列表里什么都有，只导名字像生图模型的（image / banana / imagen / dall-e / flux / seedream 等），kind 为 image。
func TestOpenAIChatImageImport(t *testing.T) {
	srv, up := openaichatimageServer(t, 0, `{"object":"list","data":[
		{"id":"nano-banana-2.1"},{"id":"nano-banana"},{"id":"gemini-2.5-flash-image"},{"id":"gpt-image-1"},{"id":"imagen-4.0-generate-001"},
		{"id":"dall-e-3"},{"id":"flux-1.1-pro"},{"id":"doubao-seedream-4-0"},
		{"id":"gpt-4o"},{"id":"claude-sonnet-4"},{"id":"text-embedding-3-small"},{"id":"whisper-1"},{"id":"sora-2"},{"id":""},{"object":"model"}
	]}`)
	env := newOpenAIChatImageEnv(t)
	drafts, err := env.exec.Import(context.Background(), env.snapshot(srv.URL, "nano-banana-2.1").Runtime(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/v1/models" {
		t.Fatalf("导入请求不符：%+v", got)
	}
	want := []string{"nano-banana-2.1", "nano-banana", "gemini-2.5-flash-image", "gpt-image-1", "imagen-4.0-generate-001", "dall-e-3", "flux-1.1-pro", "doubao-seedream-4-0"}
	if len(drafts) != len(want) {
		t.Fatalf("应只导入 %d 个生图模型：%+v", len(want), drafts)
	}
	got := map[string]provider.ModelDraft{}
	for _, d := range drafts {
		got[d.UpstreamModel] = d
	}
	for _, id := range want {
		if d, ok := got[id]; !ok || d.Kind != "image" || d.Label != id {
			t.Fatalf("缺少或不符的草稿 %s：%+v", id, d)
		}
	}
}
