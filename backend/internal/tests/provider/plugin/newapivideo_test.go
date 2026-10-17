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

// newAPIVideoEnv 用真实 runner 预检并装载内置 new-api-video.js，元数据取自预检结果。
type newAPIVideoEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newNewAPIVideoEnv(t *testing.T, mod func(*plugin.Options)) *newAPIVideoEnv {
	t.Helper()
	src, err := plugins.Source("new-api-video")
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
		t.Fatalf("内置 new-api-video.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{1: []byte("a"), 2: []byte("b"), 3: []byte("c")}}
	opts := plugin.Options{
		Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "nv-key"},
		Assets: assets, IPAllowed: allowLoopback,
	}
	if mod != nil {
		mod(&opts)
	}
	return &newAPIVideoEnv{exec: plugin.New(opts), meta: meta, sha: pre.SHA256}
}

func (e *newAPIVideoEnv) snapshot(baseURL string) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: "video", UpstreamModel: "sd2.5-c1", Capabilities: newAPIVideoCaps()},
		Channel: provider.ChannelSnapshot{
			Key: "new-api-main", PluginKey: "new-api-video", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "new-api-video", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

// newAPIVideoCaps 是 sd2.5-c1 的能力：文生、图生、多素材参考（30 图 + 10 视频 + 10 音频），时长 5–30 秒。
func newAPIVideoCaps() modelcfg.Capabilities {
	minSec, maxSec := 5, 30
	return modelcfg.Capabilities{
		Ops: []string{modelcfg.OpT2V, modelcfg.OpI2V, modelcfg.OpOmni},
		Refs: modelcfg.Refs{
			Image: modelcfg.RefSpec{On: true, Max: 30, MaxMB: 10},
			Video: modelcfg.RefSpec{On: true, Max: 10, MaxMB: 50},
			Audio: modelcfg.RefSpec{On: true, Max: 10, MaxMB: 10},
		},
		Prompt: modelcfg.PromptSpec{MaxLength: 10000},
		Params: modelcfg.ParamSet{
			{Name: "duration", ParamField: modelcfg.ParamField{Type: modelcfg.ParamNumber, Label: "时长", Open: true, Min: &minSec, Max: &maxSec, Default: 5.0}},
			{Name: "aspect_ratio", ParamField: modelcfg.ParamField{Type: modelcfg.ParamEnum, Label: "画幅", Open: true, Options: []any{"16:9", "9:16"}, Default: "16:9"}},
			{Name: "resolution", ParamField: modelcfg.ParamField{Type: modelcfg.ParamEnum, Label: "分辨率", Open: true, Options: []any{"720P"}, Default: "720P"}},
			{Name: "generate_audio", ParamField: modelcfg.ParamField{Type: modelcfg.ParamBoolean, Label: "生成音频", Open: true, Default: true}},
		},
	}
}

const newAPIVideoRefURL = "https://cdn.example.com/a.png" // fakeAssets 把所有素材都解析成这个签名地址

// newAPIVideoUpstream 模拟网关的视频接口：记录每次请求的方法、路径、鉴权、请求体。
func newAPIVideoUpstream(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, body map[string]any)) (*httptest.Server, *fakeNewAPI) {
	t.Helper()
	up := &fakeNewAPI{handler: handler}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	return srv, up
}

func newAPIVideoJSON(body string) func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
	return func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func TestNewAPIVideoPrecheckAndMeta(t *testing.T) {
	env := newNewAPIVideoEnv(t, nil)
	if env.meta.Key != "new-api-video" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthBearer || env.meta.Vendor != "new-api" {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("video"); !ok || ep.Mode != pluginmeta.ModeAsync || ep.Method != "POST" || ep.Path != "/v1/videos" {
		t.Fatalf("video 应是 async POST /v1/videos：%+v", ep)
	}
	for _, kind := range []string{"text", "image", "audio"} {
		if _, ok := env.meta.Endpoint(kind); ok {
			t.Fatalf("不应声明 %s", kind)
		}
	}
}

// 文生视频：seconds 是数字；画幅 / 分辨率 / 是否生成音频放进 metadata；没有素材时没有 metadata.content。
func TestNewAPIVideoSubmitTextToVideo(t *testing.T) {
	srv, up := newAPIVideoUpstream(t, newAPIVideoJSON(`{"id":"nv_1","status":"queued"}`))
	env := newNewAPIVideoEnv(t, nil)
	sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9},
		Input: map[string]any{"prompt": "海边日落", "op": "t2v", "duration": float64(10), "aspect_ratio": "16:9",
			"resolution": "720P", "generate_audio": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.ProviderTaskID != "nv_1" || sub.Immediate != nil {
		t.Fatalf("提交结果不符：%+v", sub)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/v1/videos" || got.Auth != "Bearer nv-key" {
		t.Fatalf("上游请求不符：%+v", got)
	}
	b := got.Body
	if b["model"] != "sd2.5-c1" || b["prompt"] != "海边日落" || b["seconds"] != float64(10) {
		t.Fatalf("请求体不符：%v", b)
	}
	meta, _ := b["metadata"].(map[string]any)
	if meta["ratio"] != "16:9" || meta["resolution"] != "720P" || meta["generate_audio"] != true {
		t.Fatalf("metadata 不符：%v", b["metadata"])
	}
	if _, ok := meta["content"]; ok {
		t.Fatalf("没有素材时不应有 metadata.content：%v", meta)
	}
	for _, k := range []string{"images", "videos", "audios", "aspect_ratio", "resolution", "generate_audio"} {
		if _, ok := b[k]; ok {
			t.Fatalf("顶层不应有 %s：%v", k, b)
		}
	}
}

// 多素材参考：素材都放进 metadata.content，每项是宿主签好的地址；顶层不能出现 images / videos / audios——
// 网关把顶层 videos 当“生成几条视频”的计费数量，塞进 URL 数组会报 plugin usage value must be a number。
func TestNewAPIVideoSubmitMultiRef(t *testing.T) {
	srv, up := newAPIVideoUpstream(t, newAPIVideoJSON(`{"id":"nv_2"}`))
	env := newNewAPIVideoEnv(t, nil)
	_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL), provider.SubmitInput{
		Task: provider.TaskRef{ID: 2, UserID: 9},
		Input: map[string]any{
			"prompt": "两个人在跳舞", "op": "omni", "duration": float64(15),
			"images": []uint64{1, 2, 3}, "videos": []uint64{3}, "audios": []uint64{1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := up.last().Body
	for _, k := range []string{"images", "videos", "audios"} {
		if _, ok := b[k]; ok {
			t.Fatalf("顶层不应有 %s（会和网关的计费字段冲突）：%v", k, b)
		}
	}
	if b["seconds"] != float64(15) {
		t.Fatalf("seconds 应是数字 15：%#v", b["seconds"])
	}
	meta, _ := b["metadata"].(map[string]any)
	content, _ := meta["content"].([]any)
	if len(content) != 5 {
		t.Fatalf("metadata.content 应有 3 图 + 1 视频 + 1 音频：%v", meta["content"])
	}
	check := func(i int, typ, key, role string) {
		t.Helper()
		item, _ := content[i].(map[string]any)
		inner, _ := item[key].(map[string]any)
		if item["type"] != typ || inner["url"] != newAPIVideoRefURL || item["role"] != role {
			t.Fatalf("第 %d 项应是 %s/%s：%v", i, typ, role, item)
		}
	}
	check(0, "image_url", "image_url", "reference_image")
	check(1, "image_url", "image_url", "reference_image")
	check(2, "image_url", "image_url", "reference_image")
	check(3, "video_url", "video_url", "reference_video")
	check(4, "audio_url", "audio_url", "reference_audio")
}

// 图片角色：图生视频一张是首帧（不写 role），两张是首帧 + 尾帧。
func TestNewAPIVideoImageRoles(t *testing.T) {
	cases := []struct {
		name  string
		ids   []uint64
		roles []any
	}{
		{"一张图是首帧", []uint64{1}, []any{nil}},
		{"两张图是首帧加尾帧", []uint64{1, 2}, []any{"first_frame", "last_frame"}},
	}
	env := newNewAPIVideoEnv(t, nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, up := newAPIVideoUpstream(t, newAPIVideoJSON(`{"id":"nv_3"}`))
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL), provider.SubmitInput{
				Task:  provider.TaskRef{ID: 1, UserID: 9},
				Input: map[string]any{"prompt": "动起来", "op": "i2v", "duration": float64(5), "images": tc.ids},
			})
			if err != nil {
				t.Fatal(err)
			}
			meta, _ := up.last().Body["metadata"].(map[string]any)
			content, _ := meta["content"].([]any)
			if len(content) != len(tc.roles) {
				t.Fatalf("素材数量不符：%v", content)
			}
			for i, want := range tc.roles {
				if got := content[i].(map[string]any)["role"]; got != want {
					t.Fatalf("第 %d 张图的 role 应是 %v，实际 %v", i, want, got)
				}
			}
		})
	}
}

// 轮询：queued -> in_progress（带进度）-> completed；completed 先返回 running 并记 ready，再取 /content 落库成 video 素材。
func TestNewAPIVideoQueryLifecycle(t *testing.T) {
	var polls int
	up := &fakeNewAPI{}
	up.handler = func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		switch r.URL.Path {
		case "/v1/videos/nv_1/content":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("MP4DATA"))
		case "/v1/videos/nv_1":
			polls++
			w.Header().Set("Content-Type", "application/json")
			switch polls {
			case 1:
				_, _ = w.Write([]byte(`{"id":"nv_1","status":"queued","progress":0}`))
			case 2:
				_, _ = w.Write([]byte(`{"id":"nv_1","status":"in_progress","progress":42}`))
			default:
				_, _ = w.Write([]byte(`{"id":"nv_1","status":"completed","progress":100}`))
			}
		default:
			http.NotFound(w, r)
		}
	}
	srv := httptest.NewServer(up)
	defer srv.Close()

	saver := &fakeSaver{}
	env := newNewAPIVideoEnv(t, func(o *plugin.Options) { o.Saver = saver })
	snap := env.snapshot(srv.URL)
	ref := provider.TaskRef{ID: 3, UserID: 9, ProviderTaskID: "nv_1"}

	q1, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q1.Status != provider.StatusQueued {
		t.Fatalf("第一次应是排队：%+v %v", q1, err)
	}
	q2, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q2.Status != provider.StatusRunning || q2.Progress == nil || *q2.Progress != 42 {
		t.Fatalf("第二次应是进行中 42%%：%+v %v", q2, err)
	}
	q3, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q3.Status != provider.StatusRunning {
		t.Fatalf("completed 的第一轮应先返回 running：%+v %v", q3, err)
	}
	ref.State = q3.State
	q4, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q4.Status != provider.StatusSucceeded || len(q4.Outputs) != 1 {
		t.Fatalf("第四次应取到内容并成功：%+v %v", q4, err)
	}
	if out := q4.Outputs[0]; out.Type != provider.OutputAsset || out.AssetID != 77 || out.MediaType != "video" {
		t.Fatalf("产物应是落库的 video 素材：%+v", out)
	}
	if got := up.last(); got.Path != "/v1/videos/nv_1/content" || got.Auth != "Bearer nv-key" {
		t.Fatalf("取内容请求不符：%+v", got)
	}
	if saver.body != "MP4DATA" || saver.got.Kind != "video" || saver.got.UserID != 9 {
		t.Fatalf("二进制应按 video 落库：%+v %q", saver.got, saver.body)
	}
}

// 失败：上游报 failed 与错误码 / 文案，按审核 / 额度 / 其它分类；缺字段的怪响应不崩。
func TestNewAPIVideoQueryFailedAndOddBodies(t *testing.T) {
	cases := []struct {
		name, body string
		status     string
		class      provider.ErrorClass
	}{
		{"审核拦截", `{"status":"failed","error":{"code":"moderation_blocked","message":"blocked by moderation"}}`, provider.StatusFailed, provider.ClassModeration},
		{"余额不足", `{"status":"failed","error":{"message":"insufficient balance"}}`, provider.StatusFailed, provider.ClassProviderBalance},
		{"其它失败", `{"status":"failed","error":{"message":"renderer crashed"}}`, provider.StatusFailed, provider.ClassTerminal},
		{"失败无原因", `{"status":"failed"}`, provider.StatusFailed, provider.ClassTerminal},
		{"没有状态", `{}`, provider.StatusQueued, ""},
		{"未知状态", `{"status":"weird"}`, provider.StatusQueued, ""},
	}
	env := newNewAPIVideoEnv(t, nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newAPIVideoUpstream(t, newAPIVideoJSON(tc.body))
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL), provider.TaskRef{ID: 1, ProviderTaskID: "x"})
			if err != nil {
				t.Fatal(err)
			}
			if q.Status != tc.status || q.ErrorClass != tc.class || (tc.status == provider.StatusFailed && q.ErrorMessage == "") {
				t.Fatalf("结果不符：%+v", q)
			}
		})
	}
}

// HTTP 非 2xx 的分类：网关的 400 要把上游错误码和原文带出来（不能被默认规则吞成“平台繁忙”）；
// 余额 / 审核 / 限流 / 5xx 的分类不变。
func TestNewAPIVideoClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		class   provider.ErrorClass
		code    string
		message string
	}{
		{"网关参数校验失败", 400, `{"code":"plugin_usage_invalid","message":"plugin usage value must be a number","data":null}`,
			provider.ClassTerminal, "plugin_usage_invalid", "plugin usage value must be a number"},
		{"error 对象里的 4xx", 400, `{"error":{"code":"invalid_size","message":"invalid size"}}`,
			provider.ClassTerminal, "invalid_size", "invalid size"},
		{"额度用尽", 429, `{"error":{"code":"insufficient_quota","message":"You exceeded your current quota"}}`, provider.ClassProviderBalance, "", ""},
		{"审核拒绝", 400, `{"error":{"code":"moderation_blocked","message":"blocked"}}`, provider.ClassModeration, "", ""},
		{"限流重试", 429, `{"error":{"message":"Rate limit reached"}}`, provider.ClassRetryable, "", ""},
		{"上游 5xx 重试", 503, `bad gateway`, provider.ClassRetryable, "", ""},
		{"5xx 带 JSON 也重试", 502, `{"code":"upstream_error","message":"bad gateway"}`, provider.ClassRetryable, "", ""},
	}
	env := newNewAPIVideoEnv(t, nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newAPIVideoUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
			})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
			if tc.code != "" && provider.CodeOf(err) != tc.code {
				t.Fatalf("错误码应是 %s：%v", tc.code, err)
			}
			var pe *provider.Error
			if tc.message != "" && (!errors.As(err, &pe) || pe.Message != tc.message) {
				t.Fatalf("应带出上游原文 %q：%v", tc.message, err)
			}
		})
	}
}
