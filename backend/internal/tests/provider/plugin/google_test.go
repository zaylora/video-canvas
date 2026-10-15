package plugin_test

import (
	"context"
	"encoding/base64"
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

// googleEnv 用真实 runner 预检并装载内置 google.js，元数据取自预检结果。
type googleEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newGoogleEnv(t *testing.T, mod func(*plugin.Options)) *googleEnv {
	t.Helper()
	src, err := plugins.Source("google")
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
		t.Fatalf("内置 google.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	opts := plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "AIza-key"}, IPAllowed: allowLoopback}
	if mod != nil {
		mod(&opts)
	}
	return &googleEnv{exec: plugin.New(opts), meta: meta, sha: pre.SHA256}
}

func (e *googleEnv) snapshot(baseURL string, caps modelcfg.Capabilities) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "veo", Kind: "video", UpstreamModel: "veo-3.0-generate-001", Capabilities: caps},
		Channel: provider.ChannelSnapshot{
			Key: "google-main", PluginKey: "google", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "google", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

type googleRecorded struct {
	Method, Path, Key, RawQuery string
	Body                        map[string]any
}

// googleFake 记录每次请求，handler 决定响应。
type googleFake struct {
	mu       sync.Mutex
	requests []googleRecorded
	handler  func(w http.ResponseWriter, r *http.Request)
}

func (f *googleFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.requests = append(f.requests, googleRecorded{Method: r.Method, Path: r.URL.Path, Key: r.Header.Get("x-goog-api-key"), RawQuery: r.URL.RawQuery, Body: body})
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	f.handler(w, r)
}

func (f *googleFake) last() googleRecorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func TestGooglePrecheckAndMeta(t *testing.T) {
	env := newGoogleEnv(t, nil)
	if env.meta.Key != "google" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthHeader || env.meta.Auth.Name != "x-goog-api-key" {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("video"); !ok || ep.Mode != pluginmeta.ModeAsync {
		t.Fatalf("video 应是 async：%+v", ep)
	}
	if _, ok := env.meta.Endpoint("text"); ok {
		t.Fatal("不应声明 text")
	}
}

var googleVideoCaps = modelcfg.Capabilities{
	Ops:    []string{modelcfg.OpT2V, modelcfg.OpI2V},
	Refs:   modelcfg.Refs{Image: modelcfg.RefSpec{On: true, Max: 1, MaxMB: 10}},
	Prompt: modelcfg.PromptSpec{MaxLength: 10000},
}

// 文生视频：POST /v1beta/models/{model}:predictLongRunning，Key 放 x-goog-api-key，参数映射成 Veo 的字段。
func TestGoogleSubmitText2Video(t *testing.T) {
	up := &googleFake{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"models/veo-3.0-generate-001/operations/op-1"}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newGoogleEnv(t, nil)
	snap := env.snapshot(srv.URL, googleVideoCaps)
	res, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9},
		Input: map[string]any{"prompt": "海边日落", "duration": float64(8), "aspect_ratio": "9:16", "resolution": "1080P",
			"negative_prompt": "模糊"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderTaskID != "models/veo-3.0-generate-001/operations/op-1" || res.Immediate != nil {
		t.Fatalf("提交结果不符：%+v", res)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/v1beta/models/veo-3.0-generate-001:predictLongRunning" || got.Key != "AIza-key" {
		t.Fatalf("上游请求不符：%+v", got)
	}
	inst := got.Body["instances"].([]any)[0].(map[string]any)
	if inst["prompt"] != "海边日落" || inst["image"] != nil {
		t.Fatalf("instances 不符：%v", inst)
	}
	p := got.Body["parameters"].(map[string]any)
	if p["durationSeconds"] != float64(8) || p["aspectRatio"] != "9:16" || p["resolution"] != "1080p" ||
		p["numberOfVideos"] != float64(1) || p["negativePrompt"] != "模糊" {
		t.Fatalf("parameters 不符：%v", p)
	}
	if len(p) != 5 {
		t.Fatalf("parameters 只应有用户输入映射出的字段：%v", p)
	}
}

// 渠道设置 apiVersion=v1 时路径换成 /v1。
func TestGoogleApiVersionSetting(t *testing.T) {
	up := &googleFake{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"models/m/operations/x"}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newGoogleEnv(t, nil)
	snap := env.snapshot(srv.URL, googleVideoCaps)
	snap.Channel.Settings = map[string]any{"apiVersion": "v1"}
	if _, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := up.last(); got.Path != "/v1/models/veo-3.0-generate-001:predictLongRunning" {
		t.Fatalf("应走 /v1：%+v", got)
	}
}

// 图生视频：images[0] 以 base64 放进 instances[0].image.inlineData，MIME 固定 image/png；输入里的 imageMimeType 被忽略。
func TestGoogleSubmitImage2Video(t *testing.T) {
	up := &googleFake{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"models/m/operations/x"}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("PNGDATA")}}
	env := newGoogleEnv(t, func(o *plugin.Options) { o.Assets = assets })
	snap := env.snapshot(srv.URL, googleVideoCaps)
	if _, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "让它动起来", "op": "i2v", "images": []uint64{5}, "imageMimeType": "image/jpeg"},
	}); err != nil {
		t.Fatal(err)
	}
	inst := up.last().Body["instances"].([]any)[0].(map[string]any)
	inline := inst["image"].(map[string]any)["inlineData"].(map[string]any)
	if inline["data"] != base64.StdEncoding.EncodeToString([]byte("PNGDATA")) || inline["mimeType"] != "image/png" {
		t.Fatalf("参考图不符：%v", inst["image"])
	}
}

// 提交响应缺 name 是插件级失败；done=true 直接带结果时走 immediate。
func TestGoogleSubmitEdgeResponses(t *testing.T) {
	env := newGoogleEnv(t, nil)
	run := func(body string) (*provider.SubmitResult, error) {
		srv := httptest.NewServer(&googleFake{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }})
		defer srv.Close()
		return env.exec.Submit(context.Background(), env.snapshot(srv.URL, googleVideoCaps), provider.SubmitInput{
			Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
		})
	}
	if _, err := run(`{}`); err == nil {
		t.Fatal("缺 name 应失败")
	}
	if _, err := run(`not json`); err == nil {
		t.Fatal("坏响应应失败而不是崩溃")
	}
	// done=true 且有视频：不直接交地址，把 ready 与地址记进 state，等下一轮查询取内容
	res, err := run(`{"name":"models/m/operations/y","done":true,"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"https://files.example.com/v.mp4"}}]}}}`)
	if err != nil || res.Immediate != nil || res.ProviderTaskID != "models/m/operations/y" ||
		!strings.Contains(string(res.State), `"ready":true`) || !strings.Contains(string(res.State), "https://files.example.com/v.mp4") {
		t.Fatalf("done=true 应记 ready 状态：%+v %v", res, err)
	}
	// done=true 的错误直接是 immediate 失败
	res, err = run(`{"name":"models/m/operations/z","done":true,"error":{"message":"boom"}}`)
	if err != nil || res.Immediate == nil || res.Immediate.Status != provider.StatusFailed {
		t.Fatalf("done 带 error 应立即失败：%+v %v", res, err)
	}
}

// 查询：未完成（done 省略）→ running；done 带 uri → succeeded；路径是 /v1beta/{operation name}。
func TestGoogleQueryStates(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status string
		class  provider.ErrorClass
		url    string
	}{
		{"进行中", `{"name":"models/m/operations/op-1"}`, provider.StatusRunning, "", ""},
		{"done=false", `{"name":"models/m/operations/op-1","done":false}`, provider.StatusRunning, "", ""},
		{"完成（下一轮取内容）", `{"name":"models/m/operations/op-1","done":true,"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"https://g.example.com/files/v:download?alt=media"}}]}}}`,
			provider.StatusRunning, "", ""},
		{"错误体", `{"name":"models/m/operations/op-1","done":true,"error":{"code":13,"message":"internal renderer crashed"}}`, provider.StatusFailed, provider.ClassTerminal, ""},
		{"安全过滤的错误", `{"done":true,"error":{"code":3,"message":"Request blocked by safety filter"}}`, provider.StatusFailed, provider.ClassModeration, ""},
		{"RAI 过滤", `{"name":"models/m/operations/op-1","done":true,"response":{"generateVideoResponse":{"raiMediaFilteredCount":1,"raiMediaFilteredReasons":["Support codes: 58061214"]}}}`,
			provider.StatusFailed, provider.ClassModeration, ""},
		{"完成但无视频", `{"name":"models/m/operations/op-1","done":true,"response":{}}`, provider.StatusFailed, provider.ClassTerminal, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := &googleFake{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }}
			srv := httptest.NewServer(up)
			defer srv.Close()
			env := newGoogleEnv(t, nil)
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, googleVideoCaps),
				provider.TaskRef{ID: 1, UserID: 9, ProviderTaskID: "models/m/operations/op-1"})
			if err != nil {
				t.Fatal(err)
			}
			if q.Status != tc.status || q.ErrorClass != tc.class {
				t.Fatalf("结果不符：%+v", q)
			}
			if tc.url != "" && (len(q.Outputs) != 1 || q.Outputs[0].URL != tc.url || q.Outputs[0].MediaType != "video") {
				t.Fatalf("产物不符：%+v", q.Outputs)
			}
			if tc.status == provider.StatusFailed && q.ErrorMessage == "" {
				t.Fatalf("失败应有原因：%+v", q)
			}
			if got := up.last(); got.Method != "GET" || got.Path != "/v1beta/models/m/operations/op-1" || got.Key != "AIza-key" {
				t.Fatalf("查询请求不符：%+v", got)
			}
		})
	}
}

// 查询响应不是 operation（没有 name 也没有 error）是插件级失败，不崩溃。
func TestGoogleQueryBadBody(t *testing.T) {
	srv := httptest.NewServer(&googleFake{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"foo":1}`)) }})
	defer srv.Close()
	env := newGoogleEnv(t, nil)
	if _, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, googleVideoCaps), provider.TaskRef{ID: 1, ProviderTaskID: "models/m/operations/x"}); err == nil {
		t.Fatal("无法识别的响应应失败")
	}
}

// HTTP 非 2xx：429 / 5xx 重试，账单问题归余额，安全拦截归审核，其它 4xx 终止。
func TestGoogleClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"限流", 429, `{"error":{"code":429,"message":"Quota exceeded for requests per minute","status":"RESOURCE_EXHAUSTED"}}`, provider.ClassRetryable},
		{"5xx", 503, `{"error":{"code":503,"message":"The model is overloaded"}}`, provider.ClassRetryable},
		{"需开通计费", 400, `{"error":{"code":400,"message":"Veo requires billing to be enabled","status":"FAILED_PRECONDITION"}}`, provider.ClassProviderBalance},
		{"安全拦截", 400, `{"error":{"code":400,"message":"The prompt was blocked by safety settings"}}`, provider.ClassModeration},
		{"参数错误", 400, `{"error":{"code":400,"message":"durationSeconds must be one of 4,6,8"}}`, provider.ClassTerminal},
		{"Key 无效", 403, `{"error":{"code":403,"message":"API key not valid"}}`, provider.ClassTerminal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(&googleFake{handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}})
			defer srv.Close()
			env := newGoogleEnv(t, nil)
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, googleVideoCaps), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
			})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
		})
	}
}

// 连通性检查：GET /v1beta/models?pageSize=1，带 Key；401 失败。
func TestGoogleCheck(t *testing.T) {
	up := &googleFake{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"models":[]}`)) }}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newGoogleEnv(t, nil)
	res, err := env.exec.Check(context.Background(), env.snapshot(srv.URL, googleVideoCaps).Runtime())
	if err != nil || res == nil || !res.OK {
		t.Fatalf("检查应成功：%+v %v", res, err)
	}
	if got := up.last(); got.Path != "/v1beta/models" || got.Key != "AIza-key" || got.RawQuery != "pageSize=1" {
		t.Fatalf("检查请求不符：%+v", got)
	}
	bad := httptest.NewServer(&googleFake{handler: func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"API key not valid"}}`))
	}})
	defer bad.Close()
	res, err = env.exec.Check(context.Background(), env.snapshot(bad.URL, googleVideoCaps).Runtime())
	if err == nil || (res != nil && res.OK) {
		t.Fatalf("403 应检查失败：%+v %v", res, err)
	}
}

// 取片：done 那一轮返回 running + state(ready, url)，下一轮对该地址发 binary 请求（同源，宿主注入 x-goog-api-key），
// 响应由宿主落库，产物是 asset。
func TestGoogleFetchVideoAsAsset(t *testing.T) {
	var srv *httptest.Server
	up := &googleFake{}
	up.handler = func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1beta/files/") {
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("MP4DATA"))
			return
		}
		_, _ = w.Write([]byte(`{"name":"models/m/operations/op-1","done":true,"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"` +
			srv.URL + `/v1beta/files/abc:download?alt=media"}}]}}}`))
	}
	srv = httptest.NewServer(up)
	defer srv.Close()
	saver := &fakeSaver{}
	env := newGoogleEnv(t, func(o *plugin.Options) { o.Saver = saver })
	snap := env.snapshot(srv.URL, googleVideoCaps)
	ref := provider.TaskRef{ID: 1, UserID: 9, ProviderTaskID: "models/m/operations/op-1"}

	q1, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q1.Status != provider.StatusRunning || len(q1.State) == 0 {
		t.Fatalf("第一轮应 running 并带 state：%+v %v", q1, err)
	}
	ref.State = q1.State
	q2, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q2.Status != provider.StatusSucceeded || len(q2.Outputs) != 1 || q2.Outputs[0].Type != provider.OutputAsset || q2.Outputs[0].AssetID != 77 {
		t.Fatalf("第二轮应是 asset 产物：%+v %v", q2, err)
	}
	got := up.last()
	if got.Method != "GET" || got.Path != "/v1beta/files/abc:download" || got.RawQuery != "alt=media" || got.Key != "AIza-key" {
		t.Fatalf("取片请求不符：%+v", got)
	}
	if saver.body != "MP4DATA" || saver.got.Kind != "video" || saver.got.UserID != 9 {
		t.Fatalf("应按 video 落库：%+v %q", saver.got, saver.body)
	}
}
