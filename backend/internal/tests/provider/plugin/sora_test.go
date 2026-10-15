package plugin_test

import (
	"context"
	"encoding/json"
	"errors"
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

// soraEnv 用真实 runner 预检并装载内置 sora.js，元数据取自预检结果。
type soraEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newSoraEnv(t *testing.T, mod func(*plugin.Options)) *soraEnv {
	t.Helper()
	src, err := plugins.Source("sora")
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
		t.Fatalf("内置 sora.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	opts := plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "sk-sora"}, IPAllowed: allowLoopback}
	if mod != nil {
		mod(&opts)
	}
	return &soraEnv{exec: plugin.New(opts), meta: meta, sha: pre.SHA256}
}

func (e *soraEnv) snapshot(baseURL string, caps modelcfg.Capabilities) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: "video", UpstreamModel: "sora-2", Capabilities: caps},
		Channel: provider.ChannelSnapshot{
			Key: "sora-main", PluginKey: "sora", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "sora", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

// soraCaps 是文生 + 图生，开放 duration 与 size 两个参数。
func soraCaps() modelcfg.Capabilities {
	minSec, maxSec := 4, 12
	return modelcfg.Capabilities{
		Ops:    []string{modelcfg.OpT2V, modelcfg.OpI2V},
		Refs:   modelcfg.Refs{Image: modelcfg.RefSpec{On: true, Max: 1, MaxMB: 10}},
		Prompt: modelcfg.PromptSpec{MaxLength: 10000},
		Params: modelcfg.ParamSet{
			{Name: "duration", ParamField: modelcfg.ParamField{Type: modelcfg.ParamNumber, Label: "时长", Open: true, Min: &minSec, Max: &maxSec, Default: 4.0}},
			{Name: "size", ParamField: modelcfg.ParamField{Type: modelcfg.ParamEnum, Label: "尺寸", Open: true, Options: []any{"720x1280", "1280x720"}, Default: "1280x720"}},
		},
	}
}

type soraRecorded struct {
	Method, Path, Auth, ContentType string
	JSON                            map[string]any
	Fields                          map[string]string
	Files                           map[string]string
}

// soraUpstream 模拟 OpenAI 视频接口，记录每次请求（JSON 或 multipart 都解析）。
type soraUpstream struct {
	mu       sync.Mutex
	requests []soraRecorded
	handler  func(w http.ResponseWriter, r *http.Request)
}

func (u *soraUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := soraRecorded{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), ContentType: r.Header.Get("Content-Type")}
	if strings.HasPrefix(rec.ContentType, "multipart/") {
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			rec.Fields, rec.Files = map[string]string{}, map[string]string{}
			for k, v := range r.MultipartForm.Value {
				rec.Fields[k] = v[0]
			}
			for k, v := range r.MultipartForm.File {
				f, _ := v[0].Open()
				raw, _ := io.ReadAll(f)
				_ = f.Close()
				rec.Files[k] = string(raw)
			}
		}
	} else {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &rec.JSON)
	}
	u.mu.Lock()
	u.requests = append(u.requests, rec)
	u.mu.Unlock()
	u.handler(w, r)
}

func (u *soraUpstream) last() soraRecorded {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requests[len(u.requests)-1]
}

func soraJSON(body string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func TestSoraPrecheckAndMeta(t *testing.T) {
	env := newSoraEnv(t, nil)
	if env.meta.Key != "sora" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthBearer {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("video"); !ok || ep.Mode != pluginmeta.ModeAsync {
		t.Fatalf("video 应是 async：%+v", ep)
	}
	for _, kind := range []string{"text", "image", "audio"} {
		if _, ok := env.meta.Endpoint(kind); ok {
			t.Fatalf("不应声明 %s", kind)
		}
	}
}

// 文生视频：无参考图发 JSON，seconds 是字符串，size 直传。
func TestSoraSubmitTextToVideo(t *testing.T) {
	up := &soraUpstream{handler: soraJSON(`{"id":"video_123","status":"queued","progress":0}`)}
	srv := httptest.NewServer(up)
	defer srv.Close()

	env := newSoraEnv(t, nil)
	snap := env.snapshot(srv.URL, soraCaps())
	sub, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task:  provider.TaskRef{ID: 1, UserID: 9},
		Input: map[string]any{"prompt": "海边日落", "op": "t2v", "duration": float64(8), "size": "1280x720"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.ProviderTaskID != "video_123" || sub.Immediate != nil {
		t.Fatalf("提交结果不符：%+v", sub)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/v1/videos" || got.Auth != "Bearer sk-sora" {
		t.Fatalf("上游请求不符：%+v", got)
	}
	if got.JSON["model"] != "sora-2" || got.JSON["prompt"] != "海边日落" || got.JSON["seconds"] != "8" ||
		got.JSON["size"] != "1280x720" {
		t.Fatalf("请求体不符：%v", got.JSON)
	}
}

// 没传 size 时由画幅 + 分辨率推出尺寸。
func TestSoraSizeFromAspectRatio(t *testing.T) {
	cases := []struct {
		ratio, res, want string
	}{
		{"16:9", "", "1280x720"},
		{"9:16", "", "720x1280"},
		{"16:9", "1080p", "1792x1024"},
		{"9:16", "high", "1024x1792"},
	}
	for _, tc := range cases {
		t.Run(tc.ratio+tc.res, func(t *testing.T) {
			up := &soraUpstream{handler: soraJSON(`{"id":"v1","status":"queued"}`)}
			srv := httptest.NewServer(up)
			defer srv.Close()
			caps := modelcfg.Capabilities{Ops: []string{modelcfg.OpT2V}, Prompt: modelcfg.PromptSpec{MaxLength: 100}, Params: modelcfg.ParamSet{
				{Name: "aspect_ratio", ParamField: modelcfg.ParamField{Type: modelcfg.ParamEnum, Label: "画幅", Open: true, Options: []any{"16:9", "9:16"}, Default: "16:9"}},
				{Name: "resolution", ParamField: modelcfg.ParamField{Type: modelcfg.ParamEnum, Label: "分辨率", Open: true, Options: []any{"720p", "1080p", "high"}, Default: "720p"}},
			}}
			env := newSoraEnv(t, nil)
			in := map[string]any{"prompt": "x", "aspect_ratio": tc.ratio}
			if tc.res != "" {
				in["resolution"] = tc.res
			}
			if _, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, caps), provider.SubmitInput{Task: provider.TaskRef{ID: 1, UserID: 9}, Input: in}); err != nil {
				t.Fatal(err)
			}
			if got := up.last().JSON["size"]; got != tc.want {
				t.Fatalf("size 应是 %s，实际 %v", tc.want, got)
			}
		})
	}
}

// 图生视频：有参考图走 multipart，字段全是字符串，参考图以文件字段 input_reference 上传。
func TestSoraSubmitImageToVideoMultipart(t *testing.T) {
	up := &soraUpstream{handler: soraJSON(`{"id":"video_img","status":"queued"}`)}
	srv := httptest.NewServer(up)
	defer srv.Close()

	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("PNGDATA")}}
	env := newSoraEnv(t, func(o *plugin.Options) { o.Assets = assets })
	sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, soraCaps()), provider.SubmitInput{
		Task:  provider.TaskRef{ID: 2, UserID: 9},
		Input: map[string]any{"prompt": "让它动起来", "op": "i2v", "duration": float64(4), "size": "1280x720", "images": []uint64{5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.ProviderTaskID != "video_img" {
		t.Fatalf("提交结果不符：%+v", sub)
	}
	got := up.last()
	if got.Path != "/v1/videos" || got.Auth != "Bearer sk-sora" || !strings.HasPrefix(got.ContentType, "multipart/form-data") {
		t.Fatalf("应是带鉴权的 multipart 请求：%+v", got)
	}
	if got.Fields["model"] != "sora-2" || got.Fields["prompt"] != "让它动起来" || got.Fields["seconds"] != "4" || got.Fields["size"] != "1280x720" {
		t.Fatalf("表单字段不符：%v", got.Fields)
	}
	if got.Files["input_reference"] != "PNGDATA" {
		t.Fatalf("参考图应以 input_reference 上传：%v", got.Files)
	}
}

// 轮询：queued -> in_progress（带进度）-> completed；completed 先返回 running 并记 ready，再取 /content 落库成 asset。
func TestSoraQueryLifecycle(t *testing.T) {
	var polls int
	up := &soraUpstream{}
	up.handler = func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/video_1/content":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("MP4DATA"))
		case "/v1/videos/video_1":
			polls++
			w.Header().Set("Content-Type", "application/json")
			switch polls {
			case 1:
				_, _ = w.Write([]byte(`{"id":"video_1","status":"queued","progress":0}`))
			case 2:
				_, _ = w.Write([]byte(`{"id":"video_1","status":"in_progress","progress":37}`))
			default:
				_, _ = w.Write([]byte(`{"id":"video_1","status":"completed","progress":100}`))
			}
		default:
			http.NotFound(w, r)
		}
	}
	srv := httptest.NewServer(up)
	defer srv.Close()

	saver := &fakeSaver{}
	env := newSoraEnv(t, func(o *plugin.Options) { o.Saver = saver })
	snap := env.snapshot(srv.URL, soraCaps())
	ref := provider.TaskRef{ID: 3, UserID: 9, ProviderTaskID: "video_1"}

	q1, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q1.Status != provider.StatusQueued {
		t.Fatalf("第一次应是排队：%+v %v", q1, err)
	}
	q2, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q2.Status != provider.StatusRunning || q2.Progress == nil || *q2.Progress != 37 {
		t.Fatalf("第二次应是进行中 37%%：%+v %v", q2, err)
	}
	q3, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q3.Status != provider.StatusRunning {
		t.Fatalf("completed 的第一轮应先返回 running：%+v %v", q3, err)
	}
	if got := up.last(); got.Path != "/v1/videos/video_1" || got.Auth != "Bearer sk-sora" {
		t.Fatalf("状态轮询请求不符：%+v", got)
	}

	// 宿主把上一轮返回的 state 带回 ctx.task.state
	ref.State = q3.State
	q4, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q4.Status != provider.StatusSucceeded || len(q4.Outputs) != 1 {
		t.Fatalf("第四次应取到内容并成功：%+v %v", q4, err)
	}
	out := q4.Outputs[0]
	if out.Type != provider.OutputAsset || out.AssetID != 77 || out.MediaType != "video" {
		t.Fatalf("产物应是落库的 video 素材：%+v", out)
	}
	if got := up.last(); got.Path != "/v1/videos/video_1/content" || got.Auth != "Bearer sk-sora" {
		t.Fatalf("取内容请求不符：%+v", got)
	}
	if saver.body != "MP4DATA" || saver.got.Kind != "video" || saver.got.UserID != 9 {
		t.Fatalf("二进制应按 video 落库：%+v %q", saver.got, saver.body)
	}
}

// 失败：上游报 failed 与错误码 / 文案，按审核 / 额度 / 其它分类；缺字段的怪响应不崩。
func TestSoraQueryFailedAndOddBodies(t *testing.T) {
	cases := []struct {
		name, body string
		status     string
		class      provider.ErrorClass
	}{
		{"审核拦截", `{"status":"failed","error":{"code":"moderation_blocked","message":"Your request was blocked by our moderation system."}}`, provider.StatusFailed, provider.ClassModeration},
		{"其它失败", `{"status":"failed","error":{"message":"renderer crashed"}}`, provider.StatusFailed, provider.ClassTerminal},
		{"失败无原因", `{"status":"failed"}`, provider.StatusFailed, provider.ClassTerminal},
		{"没有状态", `{}`, provider.StatusQueued, ""},
		{"未知状态", `{"status":"weird"}`, provider.StatusQueued, ""},
	}
	env := newSoraEnv(t, nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(&soraUpstream{handler: soraJSON(tc.body)})
			defer srv.Close()
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, soraCaps()), provider.TaskRef{ID: 1, ProviderTaskID: "x"})
			if err != nil {
				t.Fatal(err)
			}
			if q.Status != tc.status || q.ErrorClass != tc.class || (tc.status == provider.StatusFailed && q.ErrorMessage == "") {
				t.Fatalf("结果不符：%+v", q)
			}
		})
	}
}

// 提交响应没有 id：带错误信息就是 failed 结果，什么都没有是插件级失败。
func TestSoraSubmitMissingID(t *testing.T) {
	env := newSoraEnv(t, nil)
	srv := httptest.NewServer(&soraUpstream{handler: soraJSON(`{"error":{"message":"content policy violation"}}`)})
	defer srv.Close()
	input := provider.SubmitInput{Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"}}
	sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, soraCaps()), input)
	if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusFailed || sub.Immediate.ErrorClass != provider.ClassModeration {
		t.Fatalf("带错误信息应是 moderation 失败：%+v %v", sub, err)
	}

	empty := httptest.NewServer(&soraUpstream{handler: soraJSON(`{}`)})
	defer empty.Close()
	_, err = env.exec.Submit(context.Background(), env.snapshot(empty.URL, soraCaps()), input)
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("没有 id 应是插件级失败：%v", err)
	}
}

// HTTP 非 2xx 的分类：余额 / 审核 / 限流 / 5xx / 其它。
func TestSoraClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"额度用尽", 429, `{"error":{"code":"insufficient_quota","message":"You exceeded your current quota"}}`, provider.ClassProviderBalance},
		{"账单上限", 400, `{"error":{"code":"billing_hard_limit_reached","message":"Billing hard limit has been reached"}}`, provider.ClassProviderBalance},
		{"审核拒绝", 400, `{"error":{"code":"moderation_blocked","message":"blocked"}}`, provider.ClassModeration},
		{"限流重试", 429, `{"error":{"message":"Rate limit reached"}}`, provider.ClassRetryable},
		{"上游 5xx 重试", 503, `bad gateway`, provider.ClassRetryable},
		{"其它 4xx 终止", 400, `{"error":{"message":"invalid size"}}`, provider.ClassTerminal},
	}
	env := newSoraEnv(t, nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(&soraUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}})
			defer srv.Close()
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, soraCaps()), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
			})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
		})
	}
}

// 连通性检查：GET /v1/models 带 Bearer。
func TestSoraCheck(t *testing.T) {
	up := &soraUpstream{handler: soraJSON(`{"data":[]}`)}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newSoraEnv(t, nil)
	res, err := env.exec.Check(context.Background(), env.snapshot(srv.URL, soraCaps()).Runtime())
	if err != nil || res == nil || !res.OK {
		t.Fatalf("检查应成功：%+v %v", res, err)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/v1/models" || got.Auth != "Bearer sk-sora" {
		t.Fatalf("检查请求不符：%+v", got)
	}
}
