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

// doubaoimageEnv 用真实 runner 预检并装载内置 doubao-image.js，元数据取自预检结果。
type doubaoimageEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newDoubaoimageEnv(t *testing.T) *doubaoimageEnv {
	t.Helper()
	src, err := plugins.Source("doubao-image")
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
		t.Fatalf("内置 doubao-image.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{1: []byte("a"), 2: []byte("b"), 3: []byte("c")}}
	exec := plugin.New(plugin.Options{
		Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "ark-key"},
		Assets: assets, IPAllowed: allowLoopback,
	})
	return &doubaoimageEnv{exec: exec, meta: meta, sha: pre.SHA256}
}

func (e *doubaoimageEnv) snapshot(baseURL, kind, upstream string, params map[string]any) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: kind, UpstreamModel: upstream, Params: params, Capabilities: doubaoimageCaps(kind)},
		Channel: provider.ChannelSnapshot{
			Key: "doubao-image-main", PluginKey: "doubao-image", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "doubao-image", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

// doubaoimageCaps 是测试用的宽松能力：开放参考图与方舟用到的生图参数，让宿主的输入校验放行。
func doubaoimageCaps(_ string) modelcfg.Capabilities {
	ref := modelcfg.RefSpec{On: true, Max: 9, MaxMB: 10}
	enum := func(opts ...any) modelcfg.ParamField {
		return modelcfg.ParamField{Type: modelcfg.ParamEnum, Label: "x", Open: true, Options: opts, Default: opts[0]}
	}
	num := func(lo, hi int) modelcfg.ParamField {
		return modelcfg.ParamField{Type: modelcfg.ParamNumber, Label: "x", Open: true, Min: &lo, Max: &hi}
	}
	caps := modelcfg.Capabilities{Prompt: modelcfg.PromptSpec{MaxLength: 10000}}
	caps.Ops = []string{modelcfg.OpT2I, modelcfg.OpI2I}
	caps.Refs = modelcfg.Refs{Image: ref}
	caps.Params = modelcfg.ParamSet{
		{Name: "size", ParamField: enum("2K", "4K")},
		{Name: "resolution", ParamField: enum("2K", "4K")},
		{Name: "aspect_ratio", ParamField: enum("16:9", "adaptive")},
		{Name: "n", ParamField: num(1, 4)},
	}
	return caps
}

// doubaoimageServer 是假的方舟上游，复用包内的 fakeNewAPI 记录请求。
func doubaoimageServer(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *fakeNewAPI) {
	t.Helper()
	up := &fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) { h(w, r) }}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	return srv, up
}

const (
	doubaoimageModel  = "doubao-seedream-4-5-251128"
	doubaoimageRefURL = "https://cdn.example.com/a.png"
)

func TestDoubaoimagePrecheckAndMeta(t *testing.T) {
	env := newDoubaoimageEnv(t)
	if env.meta.Key != "doubao-image" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthBearer {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("image"); !ok || ep.Mode != pluginmeta.ModeSync {
		t.Fatalf("image 应是 sync：%+v", ep)
	}
	if _, ok := env.meta.Endpoint("video"); ok {
		t.Fatalf("不应声明 video：%+v", env.meta.Endpoints)
	}
}

// 图片文生图：POST /api/v3/images/generations，size 优先于 resolution，固定参数 extra 只补充；data[].url 变成 image 产物。
func TestDoubaoimageImageGenerate(t *testing.T) {
	var srv *httptest.Server
	srv, up := doubaoimageServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"url":"` + srv.URL + `/a.jpeg?sig=1","size":"2048x2048"},{"url":"` + srv.URL + `/b.png"},{"error":{"code":"x","message":"y"}}]}`))
	})
	env := newDoubaoimageEnv(t)
	params := map[string]any{"watermark": true, "extra": map[string]any{"size": "4K", "sequential_image_generation": "auto"}}
	sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "image", doubaoimageModel, params), provider.SubmitInput{
		Task: provider.TaskRef{ID: 2, UserID: 9}, Input: map[string]any{"prompt": "一只猫", "size": "2K", "n": float64(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Immediate == nil || sub.Immediate.Status != provider.StatusSucceeded || len(sub.Immediate.Outputs) != 2 {
		t.Fatalf("图片应同步成功且有两个产物（失败成员被忽略）：%+v", sub)
	}
	o1, o2 := sub.Immediate.Outputs[0], sub.Immediate.Outputs[1]
	if o1.MediaType != "image" || o1.Mime != "image/jpeg" || o2.Mime != "image/png" {
		t.Fatalf("图片产物不符：%+v %+v", o1, o2)
	}
	got := up.last()
	b := got.Body
	if got.Path != "/api/v3/images/generations" || got.Auth != "Bearer ark-key" || b["model"] != doubaoimageModel || b["prompt"] != "一只猫" ||
		b["size"] != "2K" || b["response_format"] != "url" || b["watermark"] != true || b["sequential_image_generation"] != "auto" {
		t.Fatalf("图片请求不符：%+v", got)
	}
	if _, ok := b["n"]; ok {
		t.Fatalf("方舟没有 n，不应透传：%v", b)
	}
	if _, ok := b["image"]; ok {
		t.Fatalf("无参考图不应有 image：%v", b)
	}
}

// 图片：参考图单张传字符串、多张传数组；resolution + aspect_ratio 换算成 WxH。
func TestDoubaoimageImageRefsAndRatioSize(t *testing.T) {
	srv, up := doubaoimageServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"url":"https://cdn.example.com/x.png"}]}`))
	})
	env := newDoubaoimageEnv(t)
	submit := func(input map[string]any) map[string]any {
		t.Helper()
		sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "image", doubaoimageModel, nil), provider.SubmitInput{
			Task: provider.TaskRef{ID: 2, UserID: 9}, Input: input,
		})
		if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusSucceeded {
			t.Fatalf("应成功：%+v %v", sub, err)
		}
		return up.last().Body
	}
	b := submit(map[string]any{"prompt": "p", "op": "i2i", "images": []uint64{1}, "resolution": "2K", "aspect_ratio": "16:9"})
	if b["image"] != doubaoimageRefURL || b["size"] != "2728x1528" {
		t.Fatalf("单张参考图应是字符串，16:9 的 2K 应换算成 WxH：%v", b)
	}
	b = submit(map[string]any{"prompt": "p", "op": "i2i", "images": []uint64{1, 2}, "resolution": "2K", "aspect_ratio": "adaptive"})
	if list, _ := b["image"].([]any); len(list) != 2 || list[1] != doubaoimageRefURL || b["size"] != "2K" {
		t.Fatalf("多张参考图应是数组，非比例值保留档位：%v", b)
	}
}

// 图片失败：200 + error 体按错误码分类；没有任何图片是 terminal。
func TestDoubaoimageImageBadResponses(t *testing.T) {
	cases := []struct {
		name, body string
		class      provider.ErrorClass
	}{
		{"审核", `{"error":{"code":"InputImageSensitiveContentDetected","message":"input image may contain sensitive information"}}`, provider.ClassModeration},
		{"欠费", `{"error":{"code":"AccountOverdueError","message":"overdue"}}`, provider.ClassProviderBalance},
		{"没有数据", `{"data":[]}`, provider.ClassTerminal},
		{"全部成员失败", `{"data":[{"error":{"code":"InternalServiceError","message":"member failed"}}]}`, provider.ClassTerminal},
	}
	env := newDoubaoimageEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := doubaoimageServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) })
			sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "image", doubaoimageModel, nil), provider.SubmitInput{
				Task: provider.TaskRef{ID: 2, UserID: 9}, Input: map[string]any{"prompt": "x"},
			})
			if err != nil {
				t.Fatal(err)
			}
			r := sub.Immediate
			if r == nil || r.Status != provider.StatusFailed || r.ErrorClass != tc.class || r.ErrorMessage == "" {
				t.Fatalf("结果不符：%+v", r)
			}
		})
	}
}

// HTTP 非 2xx：按方舟错误码 / 文案分类；其余交给宿主默认规则。
func TestDoubaoimageClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"欠费", 403, `{"error":{"code":"AccountOverdueError","message":"The account has an overdue balance"}}`, provider.ClassProviderBalance},
		{"审核", 400, `{"error":{"code":"InputTextSensitiveContentDetected","message":"input text may contain sensitive information"}}`, provider.ClassModeration},
		{"限流", 429, `{"error":{"code":"RateLimitExceeded.EndpointRPMExceeded","message":"too fast"}}`, provider.ClassRetryable},
		{"过载 5xx", 503, `{"error":{"code":"ServerOverloaded","message":"busy"}}`, provider.ClassRetryable},
		{"纯文本 5xx", 502, `bad gateway`, provider.ClassRetryable},
		{"参数错误", 400, `{"error":{"code":"InvalidParameter","message":"bad resolution"}}`, provider.ClassTerminal},
	}
	env := newDoubaoimageEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := doubaoimageServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "image", doubaoimageModel, nil), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
			})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
		})
	}
}

// 连通检查：GET 任务列表（page_size=1），带 Bearer；401 失败。
func TestDoubaoimageCheck(t *testing.T) {
	env := newDoubaoimageEnv(t)
	good, up := doubaoimageServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"items":[],"total":0}`)) })
	res, err := env.exec.Check(context.Background(), env.snapshot(good.URL, "image", doubaoimageModel, nil).Runtime())
	if err != nil || res == nil || !res.OK {
		t.Fatalf("检查应成功：%+v %v", res, err)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/api/v3/contents/generations/tasks" || got.Auth != "Bearer ark-key" {
		t.Fatalf("检查请求不符：%+v", got)
	}
	bad, _ := doubaoimageServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"AuthenticationError","message":"invalid api key"}}`))
	})
	res, err = env.exec.Check(context.Background(), env.snapshot(bad.URL, "image", doubaoimageModel, nil).Runtime())
	if err == nil || (res != nil && res.OK) {
		t.Fatalf("401 应检查失败：%+v %v", res, err)
	}
}
