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

// openaiimageEnv 用真实 runner 预检并装载内置 openai-image.js，元数据取自预检结果。
type openaiimageEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newOpenAIImageEnv(t *testing.T) *openaiimageEnv {
	t.Helper()
	src, err := plugins.Source("openai-image")
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
		t.Fatalf("内置 openai-image.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{1: []byte("PNG1"), 2: []byte("PNG2")}}
	exec := plugin.New(plugin.Options{
		Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "sk-img"},
		Assets: assets, IPAllowed: allowLoopback,
	})
	return &openaiimageEnv{exec: exec, meta: meta, sha: pre.SHA256}
}

func (e *openaiimageEnv) snapshot(baseURL, upstream string, params map[string]any) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: "image", UpstreamModel: upstream, Params: params, Capabilities: openaiimageCaps()},
		Channel: provider.ChannelSnapshot{
			Key: "openai-image-main", PluginKey: "openai-image", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "openai-image", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

// openaiimageCaps 是测试用的宽松能力：开放参考图和生图参数，让宿主的输入校验放行。
func openaiimageCaps() modelcfg.Capabilities {
	enum := func(opts ...any) modelcfg.ParamField {
		return modelcfg.ParamField{Type: modelcfg.ParamEnum, Label: "x", Open: true, Options: opts}
	}
	lo, hi := 1, 10
	caps := modelcfg.Capabilities{Prompt: modelcfg.PromptSpec{MaxLength: 10000}}
	caps.Ops = []string{modelcfg.OpT2I, modelcfg.OpI2I}
	caps.Refs = modelcfg.Refs{Image: modelcfg.RefSpec{On: true, Max: 9, MaxMB: 10}}
	caps.Params = modelcfg.ParamSet{
		{Name: "size", ParamField: enum("1024x1024", "1792x1024", "1024x1792", "1536x1024", "1024x1536", "512x512", "auto")},
		{Name: "quality", ParamField: enum("standard", "hd", "low", "medium", "high")},
		{Name: "style", ParamField: enum("vivid", "natural")},
		{Name: "aspect_ratio", ParamField: enum("1:1", "3:2", "2:3", "16:9", "9:16", "4:3", "3:4", "21:9")},
		{Name: "n", ParamField: modelcfg.ParamField{Type: modelcfg.ParamNumber, Label: "x", Open: true, Min: &lo, Max: &hi}},
	}
	return caps
}

// openaiimageRecorded 记录一次上游请求：JSON 体或 multipart 的字段与文件（同名文件字段可多个）。
type openaiimageRecorded struct {
	Method, Path, Auth, ContentType string
	JSON                            map[string]any
	Fields                          map[string]string
	Files                           map[string][]string
}

type openaiimageUpstream struct {
	mu       sync.Mutex
	requests []openaiimageRecorded
	handler  func(w http.ResponseWriter, r *http.Request)
}

func (u *openaiimageUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := openaiimageRecorded{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), ContentType: r.Header.Get("Content-Type")}
	if strings.HasPrefix(rec.ContentType, "multipart/") {
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			rec.Fields, rec.Files = map[string]string{}, map[string][]string{}
			for k, v := range r.MultipartForm.Value {
				rec.Fields[k] = v[0]
			}
			for k, list := range r.MultipartForm.File {
				for _, fh := range list {
					f, _ := fh.Open()
					raw, _ := io.ReadAll(f)
					_ = f.Close()
					rec.Files[k] = append(rec.Files[k], string(raw))
				}
			}
		}
	} else {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &rec.JSON)
	}
	u.mu.Lock()
	u.requests = append(u.requests, rec)
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	u.handler(w, r)
}

func (u *openaiimageUpstream) last() openaiimageRecorded {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requests[len(u.requests)-1]
}

func (u *openaiimageUpstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.requests)
}

func openaiimageServer(t *testing.T, status int, body string) (*httptest.Server, *openaiimageUpstream) {
	t.Helper()
	up := &openaiimageUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	}}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	return srv, up
}

const openaiimageOK = `{"created":1,"data":[{"url":"https://cdn.example.com/a.png","revised_prompt":"r"}]}`

func (e *openaiimageEnv) submit(t *testing.T, baseURL, model string, params, input map[string]any) (*provider.SubmitResult, error) {
	t.Helper()
	return e.exec.Submit(context.Background(), e.snapshot(baseURL, model, params), provider.SubmitInput{
		Task: provider.TaskRef{ID: 2, UserID: 9}, Input: input,
	})
}

func TestOpenAIImagePrecheckAndMeta(t *testing.T) {
	env := newOpenAIImageEnv(t)
	if env.meta.Key != "openai-image" || env.meta.Name != "OpenAI · 图片" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthBearer {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if len(env.meta.Endpoints) != 1 {
		t.Fatalf("只应声明一种类型：%+v", env.meta.Endpoints)
	}
	if ep, ok := env.meta.Endpoint("image"); !ok || ep.Mode != pluginmeta.ModeSync {
		t.Fatalf("image 应是 sync：%+v", ep)
	}
}

// dall-e-3：JSON 文生图，带 response_format=url、quality、style；size 优先于 aspect_ratio；extra 只补充不覆盖。
func TestOpenAIImageDalle3Generate(t *testing.T) {
	srv, up := openaiimageServer(t, 0, openaiimageOK)
	env := newOpenAIImageEnv(t)
	params := map[string]any{"extra": map[string]any{"user": "u1", "quality": "standard", "response_format": "b64_json"}}
	sub, err := env.submit(t, srv.URL, "dall-e-3", params, map[string]any{
		"prompt": "一只猫", "size": "1792x1024", "aspect_ratio": "9:16", "quality": "hd", "style": "natural",
	})
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
	b := got.JSON
	if got.Method != "POST" || got.Path != "/v1/images/generations" || got.Auth != "Bearer sk-img" {
		t.Fatalf("请求行不符：%+v", got)
	}
	if b["model"] != "dall-e-3" || b["prompt"] != "一只猫" || b["size"] != "1792x1024" || b["quality"] != "hd" || b["style"] != "natural" ||
		b["response_format"] != "url" || b["user"] != "u1" {
		t.Fatalf("请求体不符：%v", b)
	}
	if _, ok := b["n"]; ok {
		t.Fatalf("没给 n 就不发：%v", b)
	}
}

// dall-e-3 一次只能 1 张：n>1 是插件级失败，不发请求；n=1 照发。
func TestOpenAIImageDalle3OnlyOne(t *testing.T) {
	srv, up := openaiimageServer(t, 0, openaiimageOK)
	env := newOpenAIImageEnv(t)
	if _, err := env.submit(t, srv.URL, "dall-e-3", nil, map[string]any{"prompt": "p", "n": float64(2)}); err == nil {
		t.Fatal("dall-e-3 n=2 应失败")
	}
	if up.count() != 0 {
		t.Fatalf("不应发出请求：%d", up.count())
	}
	if _, err := env.submit(t, srv.URL, "dall-e-3", nil, map[string]any{"prompt": "p", "n": float64(1)}); err != nil {
		t.Fatal(err)
	}
	if n := up.last().JSON["n"]; n != float64(1) {
		t.Fatalf("n=1 应透传：%v", up.last().JSON)
	}
}

// dall-e-2：n 透传（1–10），不发 quality / style，发 response_format；aspect_ratio 只能落到 1024x1024。
func TestOpenAIImageDalle2Generate(t *testing.T) {
	srv, up := openaiimageServer(t, 0, `{"data":[{"url":"https://cdn.example.com/1.jpg"},{"url":"https://cdn.example.com/2.webp?x=1"}]}`)
	env := newOpenAIImageEnv(t)
	sub, err := env.submit(t, srv.URL, "dall-e-2", nil, map[string]any{"prompt": "p", "n": float64(3), "aspect_ratio": "16:9", "quality": "hd", "style": "vivid"})
	if err != nil {
		t.Fatal(err)
	}
	outs := sub.Immediate.Outputs
	if len(outs) != 2 || outs[0].Mime != "image/jpeg" || outs[1].Mime != "image/webp" {
		t.Fatalf("多图产物不符：%+v", outs)
	}
	b := up.last().JSON
	if b["n"] != float64(3) || b["size"] != "1024x1024" || b["response_format"] != "url" {
		t.Fatalf("请求体不符：%v", b)
	}
	if _, ok := b["quality"]; ok {
		t.Fatalf("dall-e-2 不支持 quality：%v", b)
	}
	if _, ok := b["style"]; ok {
		t.Fatalf("dall-e-2 不支持 style：%v", b)
	}
	if _, err := env.submit(t, srv.URL, "dall-e-2", nil, map[string]any{"prompt": "p", "n": float64(11)}); err == nil {
		t.Fatal("n=11 应失败")
	}
}

// gpt-image 系：不发 response_format 和 style；size 由 aspect_ratio 换算到 1024x1024 / 1536x1024 / 1024x1536；extra 补充 output_format 等。
func TestOpenAIImageGPTImageGenerate(t *testing.T) {
	srv, up := openaiimageServer(t, 0, openaiimageOK)
	env := newOpenAIImageEnv(t)
	params := map[string]any{"extra": map[string]any{"output_format": "webp", "background": "transparent"}}
	ratios := map[string]string{"1:1": "1024x1024", "3:2": "1536x1024", "2:3": "1024x1536", "16:9": "1536x1024", "9:16": "1024x1536", "4:3": "1536x1024", "3:4": "1024x1536"}
	for ratio, want := range ratios {
		if _, err := env.submit(t, srv.URL, "gpt-image-1", params, map[string]any{"prompt": "p", "aspect_ratio": ratio, "quality": "high", "n": float64(2), "style": "vivid"}); err != nil {
			t.Fatal(err)
		}
		b := up.last().JSON
		if b["size"] != want || b["quality"] != "high" || b["n"] != float64(2) || b["output_format"] != "webp" || b["background"] != "transparent" {
			t.Fatalf("%s 请求体不符：%v", ratio, b)
		}
		if _, ok := b["response_format"]; ok {
			t.Fatalf("gpt-image 不支持 response_format：%v", b)
		}
		if _, ok := b["style"]; ok {
			t.Fatalf("gpt-image 不支持 style：%v", b)
		}
	}
	// 其它 gpt-image / chatgpt-image 前缀同样按 gpt 系处理；size=auto 原样透传。
	for _, m := range []string{"gpt-image-1-mini", "gpt-image-2", "chatgpt-image-latest"} {
		if _, err := env.submit(t, srv.URL, m, nil, map[string]any{"prompt": "p", "size": "auto"}); err != nil {
			t.Fatal(err)
		}
		b := up.last().JSON
		if b["size"] != "auto" {
			t.Fatalf("%s size 应透传：%v", m, b)
		}
		if _, ok := b["response_format"]; ok {
			t.Fatalf("%s 不应发 response_format：%v", m, b)
		}
	}
}

// 没有 size 也没有 aspect_ratio 就不发 size；未知模型名不发 response_format，size / quality 原样透传。
func TestOpenAIImageUnknownModelPassThrough(t *testing.T) {
	srv, up := openaiimageServer(t, 0, openaiimageOK)
	env := newOpenAIImageEnv(t)
	if _, err := env.submit(t, srv.URL, "flux-pro", nil, map[string]any{"prompt": "p"}); err != nil {
		t.Fatal(err)
	}
	b := up.last().JSON
	if _, ok := b["size"]; ok {
		t.Fatalf("没给 size 不应发：%v", b)
	}
	if _, ok := b["response_format"]; ok {
		t.Fatalf("未知模型不发 response_format：%v", b)
	}
	if _, err := env.submit(t, srv.URL, "flux-pro", nil, map[string]any{"prompt": "p", "size": "512x512", "quality": "hd"}); err != nil {
		t.Fatal(err)
	}
	b = up.last().JSON
	if b["size"] != "512x512" || b["quality"] != "hd" {
		t.Fatalf("size / quality 应原样透传：%v", b)
	}
}

// dall-e-3 的 aspect_ratio 换算：选最接近的合法 size。
func TestOpenAIImageDalle3RatioSize(t *testing.T) {
	srv, up := openaiimageServer(t, 0, openaiimageOK)
	env := newOpenAIImageEnv(t)
	want := map[string]string{"1:1": "1024x1024", "16:9": "1792x1024", "9:16": "1024x1792", "3:2": "1792x1024", "2:3": "1024x1792"}
	for ratio, size := range want {
		if _, err := env.submit(t, srv.URL, "dall-e-3", nil, map[string]any{"prompt": "p", "aspect_ratio": ratio}); err != nil {
			t.Fatal(err)
		}
		if got := up.last().JSON["size"]; got != size {
			t.Fatalf("%s 应换算成 %s：%v", ratio, size, got)
		}
	}
}

// 有参考图走 /v1/images/edits（multipart）：gpt-image 用 image[]，可多张；字段全是字符串。
func TestOpenAIImageEditsGPT(t *testing.T) {
	srv, up := openaiimageServer(t, 0, openaiimageOK)
	env := newOpenAIImageEnv(t)
	params := map[string]any{"extra": map[string]any{"output_format": "png", "input_fidelity": "high"}}
	sub, err := env.submit(t, srv.URL, "gpt-image-1", params, map[string]any{
		"prompt": "换成夜景", "op": "i2i", "images": []uint64{1, 2}, "n": float64(2), "size": "1536x1024", "quality": "medium",
	})
	if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusSucceeded {
		t.Fatalf("应成功：%+v %v", sub, err)
	}
	got := up.last()
	if got.Path != "/v1/images/edits" || got.Auth != "Bearer sk-img" || !strings.HasPrefix(got.ContentType, "multipart/form-data") {
		t.Fatalf("应是带鉴权的 multipart 编辑请求：%+v", got)
	}
	f := got.Fields
	if f["model"] != "gpt-image-1" || f["prompt"] != "换成夜景" || f["n"] != "2" || f["size"] != "1536x1024" || f["quality"] != "medium" ||
		f["output_format"] != "png" || f["input_fidelity"] != "high" {
		t.Fatalf("表单字段不符：%v", f)
	}
	if _, ok := f["response_format"]; ok {
		t.Fatalf("gpt-image 不发 response_format：%v", f)
	}
	if imgs := got.Files["image[]"]; len(imgs) != 2 || imgs[0] != "PNG1" || imgs[1] != "PNG2" {
		t.Fatalf("参考图应以 image[] 上传：%v", got.Files)
	}
}

// dall-e-2 编辑：只一张，字段名 image，发 response_format=url；多张拒绝。
func TestOpenAIImageEditsDalle2(t *testing.T) {
	srv, up := openaiimageServer(t, 0, openaiimageOK)
	env := newOpenAIImageEnv(t)
	if _, err := env.submit(t, srv.URL, "dall-e-2", nil, map[string]any{"prompt": "p", "op": "i2i", "images": []uint64{1}, "size": "512x512"}); err != nil {
		t.Fatal(err)
	}
	got := up.last()
	if got.Path != "/v1/images/edits" || got.Files["image"][0] != "PNG1" || got.Fields["response_format"] != "url" || got.Fields["size"] != "512x512" {
		t.Fatalf("dall-e-2 编辑请求不符：%+v", got)
	}
	before := up.count()
	if _, err := env.submit(t, srv.URL, "dall-e-2", nil, map[string]any{"prompt": "p", "op": "i2i", "images": []uint64{1, 2}}); err == nil {
		t.Fatal("dall-e-2 多张参考图应失败")
	}
	if up.count() != before {
		t.Fatal("拒绝时不应发请求")
	}
}

// dall-e-3 不支持编辑：有参考图直接插件级失败，不发请求。
func TestOpenAIImageDalle3RejectsEdit(t *testing.T) {
	srv, up := openaiimageServer(t, 0, openaiimageOK)
	env := newOpenAIImageEnv(t)
	if _, err := env.submit(t, srv.URL, "dall-e-3", nil, map[string]any{"prompt": "p", "op": "i2i", "images": []uint64{1}}); err == nil {
		t.Fatal("dall-e-3 带参考图应失败")
	}
	if up.count() != 0 {
		t.Fatalf("不应发出请求：%d", up.count())
	}
}

// 200 但没有可用图片：只有 b64_json 要明确说明不支持；空数据与全无 url 是“没有返回图片”；200 带 error 对象按错误分类。
func TestOpenAIImageBadResponses(t *testing.T) {
	cases := []struct {
		name, body, contains string
		class                provider.ErrorClass
	}{
		{"只有 base64", `{"data":[{"b64_json":"AAAA"}]}`, "上游只返回 base64 图片，本协议暂不支持", provider.ClassTerminal},
		{"url 为空串的 base64", `{"data":[{"url":"","b64_json":"AAAA"}]}`, "base64", provider.ClassTerminal},
		{"空数据", `{"data":[]}`, "没有返回图片", provider.ClassTerminal},
		{"没有 data", `{}`, "没有返回图片", provider.ClassTerminal},
		{"全无 url", `{"data":[{"revised_prompt":"x"}]}`, "没有返回图片", provider.ClassTerminal},
		{"非 http 的 url", `{"data":[{"url":"ftp://x/a.png"}]}`, "没有返回图片", provider.ClassTerminal},
		{"200 带审核错误", `{"error":{"message":"Your request was rejected by the safety system.","type":"image_generation_user_error","code":"moderation_blocked"}}`, "safety system", provider.ClassModeration},
		{"200 带额度错误", `{"error":{"message":"You exceeded your current quota","type":"insufficient_quota","code":"insufficient_quota"}}`, "quota", provider.ClassProviderBalance},
		{"200 带其它错误", `{"error":{"message":"boom","type":"server_error","code":null}}`, "boom", provider.ClassTerminal},
	}
	env := newOpenAIImageEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := openaiimageServer(t, 0, tc.body)
			sub, err := env.submit(t, srv.URL, "dall-e-3", nil, map[string]any{"prompt": "x"})
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

// 多张里夹着没有 url 的成员：只收有 url 的。
func TestOpenAIImagePartialData(t *testing.T) {
	srv, _ := openaiimageServer(t, 0, `{"data":[{"b64_json":"AAAA"},{"url":"https://cdn.example.com/ok.png"}]}`)
	env := newOpenAIImageEnv(t)
	sub, err := env.submit(t, srv.URL, "dall-e-2", nil, map[string]any{"prompt": "x", "n": float64(2)})
	if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusSucceeded || len(sub.Immediate.Outputs) != 1 {
		t.Fatalf("应只收有 url 的成员：%+v %v", sub, err)
	}
}

// HTTP 非 2xx：显式分类，不依赖宿主按文案猜。
func TestOpenAIImageClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"额度", 429, `{"error":{"message":"You exceeded your current quota","type":"insufficient_quota","code":"insufficient_quota"}}`, provider.ClassProviderBalance},
		{"账单上限", 400, `{"error":{"message":"Billing hard limit has been reached","type":"invalid_request_error","code":"billing_hard_limit_reached"}}`, provider.ClassProviderBalance},
		{"内容策略", 400, `{"error":{"message":"Your prompt was flagged","type":"invalid_request_error","code":"content_policy_violation"}}`, provider.ClassModeration},
		{"审核拦截", 400, `{"error":{"message":"rejected","type":"image_generation_user_error","code":"moderation_blocked"}}`, provider.ClassModeration},
		{"安全系统文案", 400, `{"error":{"message":"Your request was rejected by the safety system.","type":"invalid_request_error","code":null}}`, provider.ClassModeration},
		{"限流", 429, `{"error":{"message":"Rate limit reached","type":"requests","code":"rate_limit_exceeded"}}`, provider.ClassRetryable},
		{"5xx", 503, `{"error":{"message":"overloaded","type":"server_error","code":null}}`, provider.ClassRetryable},
		{"纯文本 502", 502, `bad gateway`, provider.ClassRetryable},
		{"参数错误", 400, `{"error":{"message":"Invalid value for size","type":"invalid_request_error","code":"invalid_value"}}`, provider.ClassTerminal},
		{"401", 401, `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`, provider.ClassTerminal},
		{"403", 403, `{"error":{"message":"credit check failed for this region","type":"invalid_request_error","code":null}}`, provider.ClassTerminal},
		{"404", 404, `{"error":{"message":"model not found","type":"invalid_request_error","code":"model_not_found"}}`, provider.ClassTerminal},
	}
	env := newOpenAIImageEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := openaiimageServer(t, tc.status, tc.body)
			_, err := env.submit(t, srv.URL, "dall-e-3", nil, map[string]any{"prompt": "x"})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
		})
	}
}

// 连通检查：GET /v1/models，带 Bearer；401 失败。
func TestOpenAIImageCheck(t *testing.T) {
	env := newOpenAIImageEnv(t)
	good, up := openaiimageServer(t, 0, `{"object":"list","data":[]}`)
	res, err := env.exec.Check(context.Background(), env.snapshot(good.URL, "dall-e-3", nil).Runtime())
	if err != nil || res == nil || !res.OK {
		t.Fatalf("检查应成功：%+v %v", res, err)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/v1/models" || got.Auth != "Bearer sk-img" {
		t.Fatalf("检查请求不符：%+v", got)
	}
	bad, _ := openaiimageServer(t, 401, `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`)
	res, err = env.exec.Check(context.Background(), env.snapshot(bad.URL, "dall-e-3", nil).Runtime())
	if err == nil || (res != nil && res.OK) {
		t.Fatalf("401 应检查失败：%+v %v", res, err)
	}
}

// 导入：只导图片类（dall-e- / gpt-image- / chatgpt-image-），kind 为 image，label 取 id。
func TestOpenAIImageImport(t *testing.T) {
	srv, up := openaiimageServer(t, 0, `{"object":"list","data":[
		{"id":"dall-e-2"},{"id":"dall-e-3"},{"id":"gpt-image-1"},{"id":"gpt-image-1-mini"},{"id":"chatgpt-image-latest"},
		{"id":"gpt-4o"},{"id":"o3"},{"id":"text-embedding-3-small"},{"id":"whisper-1"},{"id":"tts-1"},{"id":"sora-2"},{"id":""},{"object":"model"}
	]}`)
	env := newOpenAIImageEnv(t)
	drafts, err := env.exec.Import(context.Background(), env.snapshot(srv.URL, "dall-e-3", nil).Runtime(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/v1/models" {
		t.Fatalf("导入请求不符：%+v", got)
	}
	want := []string{"dall-e-2", "dall-e-3", "gpt-image-1", "gpt-image-1-mini", "chatgpt-image-latest"}
	if len(drafts) != len(want) {
		t.Fatalf("应只导入 %d 个图片模型：%+v", len(want), drafts)
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
