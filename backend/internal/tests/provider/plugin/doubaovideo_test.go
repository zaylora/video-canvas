package plugin_test

import (
	"context"
	"errors"
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

// doubaovideoEnv 用真实 runner 预检并装载内置 doubao-video.js，元数据取自预检结果。
type doubaovideoEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newDoubaovideoEnv(t *testing.T) *doubaovideoEnv {
	t.Helper()
	src, err := plugins.Source("doubao-video")
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
		t.Fatalf("内置 doubao-video.js 预检没有通过：%+v", pre.Issues)
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
	return &doubaovideoEnv{exec: exec, meta: meta, sha: pre.SHA256}
}

func (e *doubaovideoEnv) snapshot(baseURL, kind, upstream string) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: kind, UpstreamModel: upstream, Capabilities: doubaovideoCaps(kind)},
		Channel: provider.ChannelSnapshot{
			Key: "doubao-video-main", PluginKey: "doubao-video", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "doubao-video", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

// doubaovideoCaps 是测试用的宽松能力：开放全部素材与方舟用到的生成参数，让宿主的输入校验放行。
func doubaovideoCaps(_ string) modelcfg.Capabilities {
	ref := modelcfg.RefSpec{On: true, Max: 9, MaxMB: 10}
	enum := func(opts ...any) modelcfg.ParamField {
		return modelcfg.ParamField{Type: modelcfg.ParamEnum, Label: "x", Open: true, Options: opts, Default: opts[0]}
	}
	num := func(lo, hi int) modelcfg.ParamField {
		return modelcfg.ParamField{Type: modelcfg.ParamNumber, Label: "x", Open: true, Min: &lo, Max: &hi}
	}
	caps := modelcfg.Capabilities{Prompt: modelcfg.PromptSpec{MaxLength: 10000}}
	caps.Ops = []string{modelcfg.OpT2V, modelcfg.OpI2V, modelcfg.OpOmni}
	caps.Refs = modelcfg.Refs{Image: ref, Video: ref, Audio: ref}
	caps.Params = modelcfg.ParamSet{
		{Name: "aspect_ratio", ParamField: enum("16:9", "1:1")},
		{Name: "resolution", ParamField: enum("720p", "1080p")},
		{Name: "duration", ParamField: num(-1, 15)},
		{Name: "generate_audio", ParamField: modelcfg.ParamField{Type: modelcfg.ParamBoolean, Label: "x", Open: true, Default: true}},
		{Name: "watermark", ParamField: modelcfg.ParamField{Type: modelcfg.ParamBoolean, Label: "x", Open: true}},
		{Name: "camera_fixed", ParamField: modelcfg.ParamField{Type: modelcfg.ParamBoolean, Label: "x", Open: true}},
		{Name: "return_last_frame", ParamField: modelcfg.ParamField{Type: modelcfg.ParamBoolean, Label: "x", Open: true}},
		{Name: "service_tier", ParamField: enum("default", "flex")},
	}
	return caps
}

// doubaovideoServer 是假的方舟上游，复用包内的 fakeNewAPI 记录请求。
func doubaovideoServer(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *fakeNewAPI) {
	t.Helper()
	up := &fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) { h(w, r) }}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	return srv, up
}

const (
	doubaovideoRefURL = "https://cdn.example.com/a.png"
	doubaovideoModel  = "doubao-seedance-1-5-pro-251215"
)

func TestDoubaovideoPrecheckAndMeta(t *testing.T) {
	env := newDoubaovideoEnv(t)
	if env.meta.Key != "doubao-video" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthBearer {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("video"); !ok || ep.Mode != pluginmeta.ModeAsync {
		t.Fatalf("video 应是 async：%+v", ep)
	}
	if _, ok := env.meta.Endpoint("image"); ok {
		t.Fatalf("不应声明 image：%+v", env.meta.Endpoints)
	}
}

// 视频提交：路径、Bearer、content 数组（文本 + 首帧图）、ratio / resolution / duration / generate_audio，以及用户输入的 camera_fixed / return_last_frame / service_tier。
func TestDoubaovideoVideoSubmitI2V(t *testing.T) {
	srv, up := doubaovideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"cgt-2025-abc"}`))
	})
	env := newDoubaovideoEnv(t)
	sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "video", doubaovideoModel), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9},
		Input: map[string]any{"prompt": "猫在跳舞", "op": "i2v", "images": []uint64{1}, "aspect_ratio": "16:9",
			"resolution": "720p", "duration": float64(5), "generate_audio": false,
			"camera_fixed": true, "return_last_frame": true, "service_tier": "flex"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.ProviderTaskID != "cgt-2025-abc" || sub.Immediate != nil {
		t.Fatalf("提交结果不符：%+v", sub)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/api/v3/contents/generations/tasks" || got.Auth != "Bearer ark-key" {
		t.Fatalf("提交请求不符：%+v", got)
	}
	b := got.Body
	if b["model"] != doubaovideoModel || b["ratio"] != "16:9" || b["resolution"] != "720p" || b["duration"] != float64(5) ||
		b["generate_audio"] != false || b["watermark"] != false || b["camera_fixed"] != true || b["return_last_frame"] != true || b["service_tier"] != "flex" {
		t.Fatalf("请求体不符：%v", b)
	}
	content, _ := b["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content 应有文本和一张图：%v", b["content"])
	}
	if txt := content[0].(map[string]any); txt["type"] != "text" || txt["text"] != "猫在跳舞" {
		t.Fatalf("文本项不符：%v", content[0])
	}
	img := content[1].(map[string]any)
	if url, _ := img["image_url"].(map[string]any); img["type"] != "image_url" || url["url"] != doubaovideoRefURL || img["role"] != nil {
		t.Fatalf("单张图应是不带 role 的首帧：%v", img)
	}
}

// 视频素材角色：两张图是首帧 + 尾帧，omni 全是参考图，视频 / 音频带 reference 角色。
func TestDoubaovideoVideoRoles(t *testing.T) {
	srv, up := doubaovideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"t1"}`))
	})
	env := newDoubaovideoEnv(t)
	roles := func(input map[string]any) []string {
		t.Helper()
		_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "video", "doubao-seedance-2-0-260128"), provider.SubmitInput{
			Task: provider.TaskRef{ID: 1, UserID: 9}, Input: input,
		})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range up.last().Body["content"].([]any) {
			m := it.(map[string]any)
			role, _ := m["role"].(string)
			out = append(out, m["type"].(string)+":"+role)
		}
		return out
	}
	eq := func(got, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("content 不符：%v，期望 %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("content 不符：%v，期望 %v", got, want)
			}
		}
	}
	eq(roles(map[string]any{"prompt": "p", "op": "i2v", "images": []uint64{1, 2}}),
		[]string{"text:", "image_url:first_frame", "image_url:last_frame"})
	eq(roles(map[string]any{"prompt": "p", "op": "omni", "images": []uint64{1}, "videos": []uint64{2}, "audios": []uint64{3}}),
		[]string{"text:", "image_url:reference_image", "video_url:reference_video", "audio_url:reference_audio"})
	eq(roles(map[string]any{"prompt": "p", "op": "t2v"}), []string{"text:"})
}

// 视频异步查询：queued → running → succeeded（带 video_url）；轮询路径与鉴权。
func TestDoubaovideoVideoQuery(t *testing.T) {
	polls := 0
	var srv *httptest.Server
	srv, up := doubaovideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		polls++
		switch polls {
		case 1:
			_, _ = w.Write([]byte(`{"id":"t1","status":"queued"}`))
		case 2:
			_, _ = w.Write([]byte(`{"id":"t1","status":"running"}`))
		default:
			_, _ = w.Write([]byte(`{"id":"t1","status":"succeeded","content":{"video_url":"` + srv.URL + `/o.mp4","last_frame_url":"x"}}`))
		}
	})
	env := newDoubaovideoEnv(t)
	snap := env.snapshot(srv.URL, "video", doubaovideoModel)
	ref := provider.TaskRef{ID: 1, UserID: 9, ProviderTaskID: "t1"}
	want := []string{provider.StatusQueued, provider.StatusRunning, provider.StatusSucceeded}
	var last *provider.QueryResult
	for _, w := range want {
		q, err := env.exec.Query(context.Background(), snap, ref)
		if err != nil || q.Status != w {
			t.Fatalf("状态应是 %s：%+v %v", w, q, err)
		}
		last = q
	}
	if len(last.Outputs) != 1 || last.Outputs[0].URL != srv.URL+"/o.mp4" || last.Outputs[0].MediaType != "video" || last.Outputs[0].Mime != "video/mp4" {
		t.Fatalf("产物不符：%+v", last.Outputs)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/api/v3/contents/generations/tasks/t1" || got.Auth != "Bearer ark-key" {
		t.Fatalf("轮询请求不符：%+v", got)
	}
}

// 视频失败：failed / expired / cancelled 与错误码分类；成功但没有地址视为失败。
func TestDoubaovideoVideoQueryFailed(t *testing.T) {
	cases := []struct {
		name, body string
		class      provider.ErrorClass
	}{
		{"审核", `{"status":"failed","error":{"code":"OutputVideoSensitiveContentDetected","message":"the output video may contain sensitive information"}}`, provider.ClassModeration},
		{"欠费", `{"status":"failed","error":{"code":"AccountOverdueError","message":"account overdue"}}`, provider.ClassProviderBalance},
		{"其它", `{"status":"failed","error":{"code":"InternalServiceError","message":"boom"}}`, provider.ClassTerminal},
		{"过期", `{"status":"expired"}`, provider.ClassTerminal},
		{"取消", `{"status":"cancelled"}`, provider.ClassTerminal},
		{"无地址", `{"status":"succeeded","content":{}}`, provider.ClassTerminal},
	}
	env := newDoubaovideoEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := doubaovideoServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) })
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, "video", doubaovideoModel), provider.TaskRef{ID: 1, ProviderTaskID: "t1"})
			if err != nil {
				t.Fatal(err)
			}
			if q.Status != provider.StatusFailed || q.ErrorClass != tc.class || q.ErrorMessage == "" {
				t.Fatalf("失败结果不符：%+v", q)
			}
		})
	}
}

// 提交响应没有 id：带错误体翻译成 failed，什么都没有是插件级失败。
func TestDoubaovideoVideoSubmitBadResponses(t *testing.T) {
	env := newDoubaovideoEnv(t)
	srv, _ := doubaovideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":{"code":"x","message":"insufficient balance"}}`))
	})
	sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "video", doubaovideoModel), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
	})
	if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusFailed || sub.Immediate.ErrorClass != provider.ClassProviderBalance {
		t.Fatalf("应翻译成余额失败：%+v %v", sub, err)
	}

	srv2, _ := doubaovideoServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	_, err = env.exec.Submit(context.Background(), env.snapshot(srv2.URL, "video", doubaovideoModel), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
	})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("没有 id 应是插件级失败：%v", err)
	}
}

// HTTP 非 2xx：按方舟错误码 / 文案分类；其余交给宿主默认规则。
func TestDoubaovideoClassifyHTTPErrors(t *testing.T) {
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
	env := newDoubaovideoEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := doubaovideoServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "video", doubaovideoModel), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
			})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
		})
	}
}

// 连通检查：GET 任务列表（page_size=1），带 Bearer；401 失败。
func TestDoubaovideoCheck(t *testing.T) {
	env := newDoubaovideoEnv(t)
	good, up := doubaovideoServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"items":[],"total":0}`)) })
	res, err := env.exec.Check(context.Background(), env.snapshot(good.URL, "video", doubaovideoModel).Runtime())
	if err != nil || res == nil || !res.OK {
		t.Fatalf("检查应成功：%+v %v", res, err)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/api/v3/contents/generations/tasks" || got.Auth != "Bearer ark-key" {
		t.Fatalf("检查请求不符：%+v", got)
	}
	bad, _ := doubaovideoServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"AuthenticationError","message":"invalid api key"}}`))
	})
	res, err = env.exec.Check(context.Background(), env.snapshot(bad.URL, "video", doubaovideoModel).Runtime())
	if err == nil || (res != nil && res.OK) {
		t.Fatalf("401 应检查失败：%+v %v", res, err)
	}
}
