package plugin_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/plugins"
)

const viduRefURL = "https://cdn.example.com/a.png"

type viduEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

// newViduEnv 用真实 runner 预检并装载内置 vidu.js；元数据取自预检结果。
func newViduEnv(t *testing.T) *viduEnv {
	t.Helper()
	src, err := plugins.Source("vidu")
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
		t.Fatalf("内置 vidu.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	files := map[uint64][]byte{1: []byte("a"), 2: []byte("b"), 3: []byte("c")}
	exec := plugin.New(plugin.Options{
		Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "vk-secret"},
		Assets: &fakeAssets{userID: 9, files: files}, IPAllowed: allowLoopback,
	})
	return &viduEnv{exec: exec, meta: meta, sha: pre.SHA256}
}

func (e *viduEnv) snapshot(baseURL, upstream string) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: "video", UpstreamModel: upstream, Capabilities: promptCaps},
		Channel: provider.ChannelSnapshot{
			Key: "vidu-main", PluginKey: "vidu", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "vidu", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

type viduReq struct {
	Method, Path, Auth string
	Body               map[string]any
}

type viduUpstream struct {
	mu       sync.Mutex
	requests []viduReq
}

func newViduServer(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *viduUpstream) {
	t.Helper()
	up := &viduUpstream{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		up.mu.Lock()
		up.requests = append(up.requests, viduReq{r.Method, r.URL.Path, r.Header.Get("Authorization"), body})
		up.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, up
}

func (u *viduUpstream) last() viduReq {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requests[len(u.requests)-1]
}

func viduSubmit(env *viduEnv, srvURL, model string, input map[string]any) (*provider.SubmitResult, error) {
	return env.exec.Submit(context.Background(), env.snapshot(srvURL, model), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: input,
	})
}

func TestViduPrecheckAndMeta(t *testing.T) {
	env := newViduEnv(t)
	if env.meta.Key != "vidu" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthCustom {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if len(env.meta.Endpoints) != 1 {
		t.Fatalf("应只声明 video：%+v", env.meta.Endpoints)
	}
	if ep, ok := env.meta.Endpoint("video"); !ok || ep.Mode != pluginmeta.ModeAsync {
		t.Fatalf("video 应是 async：%+v", ep)
	}
}

// 参考图张数决定路径：0 text2video、1 img2video、2 start-end2video、3 reference2video；头是 "Token <key>"。
func TestViduRoutesByImageCount(t *testing.T) {
	srv, up := newViduServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"v-1","state":"created"}`))
	})
	env := newViduEnv(t)
	cases := []struct {
		images []uint64
		path   string
	}{
		{nil, "/ent/v2/text2video"},
		{[]uint64{1}, "/ent/v2/img2video"},
		{[]uint64{1, 2}, "/ent/v2/start-end2video"},
		{[]uint64{1, 2, 3}, "/ent/v2/reference2video"},
	}
	for _, tc := range cases {
		input := map[string]any{"prompt": "猫在跳舞", "duration": float64(5), "resolution": "720P", "aspect_ratio": "16:9", "seed": float64(7)}
		if tc.images != nil {
			input["images"] = tc.images
		}
		sub, err := viduSubmit(env, srv.URL, "viduq2", input)
		if err != nil {
			t.Fatal(err)
		}
		if sub.ProviderTaskID != "v-1" || sub.Immediate != nil {
			t.Fatalf("提交结果不符：%+v", sub)
		}
		got := up.last()
		if got.Method != "POST" || got.Path != tc.path || got.Auth != "Token vk-secret" {
			t.Fatalf("请求不符：%+v", got)
		}
		b := got.Body
		if b["model"] != "viduq2" || b["prompt"] != "猫在跳舞" || b["duration"] != float64(5) || b["resolution"] != "720p" ||
			b["seed"] != float64(7) || b["movement_amplitude"] != "auto" {
			t.Fatalf("请求体不符：%v", b)
		}
		imgs, _ := b["images"].([]any)
		if len(imgs) != len(tc.images) {
			t.Fatalf("参考图数量不符：%v", b["images"])
		}
		for _, u := range imgs {
			if u != viduRefURL {
				t.Fatalf("参考图应换成地址：%v", imgs)
			}
		}
		// aspect_ratio 只给文生与参考生视频
		_, has := b["aspect_ratio"]
		if has != (len(tc.images) == 0 || len(tc.images) > 2) {
			t.Fatalf("aspect_ratio 带法不符（%d 张图）：%v", len(tc.images), b)
		}
	}
}

// 参考生视频固定用 viduq2。
func TestViduReferenceModel(t *testing.T) {
	srv, up := newViduServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"v-2"}`))
	})
	env := newViduEnv(t)
	snap := env.snapshot(srv.URL, "viduq2-pro")
	_, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x", "images": []uint64{1, 2, 3}, "resolution": "1080p"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := up.last().Body
	if b["model"] != "viduq2" || b["resolution"] != "1080p" {
		t.Fatalf("请求体不符：%v", b)
	}
}

// 各模型的默认值与组合限制：viduq1 固定 5 秒 1080p；vidu2.0 默认 4 秒 360p、不能文生、8 秒只能 720p；viduq2 默认 5 秒 720p、时长 1–10。
func TestViduModelRules(t *testing.T) {
	srv, up := newViduServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"v-3"}`))
	})
	env := newViduEnv(t)

	ok := []struct {
		model string
		input map[string]any
		dur   float64
		res   string
	}{
		{"viduq1", map[string]any{"prompt": "x", "resolution": "720p"}, 5, "1080p"},
		{"vidu2.0", map[string]any{"prompt": "x", "images": []uint64{1}}, 4, "360p"},
		{"vidu2.0", map[string]any{"prompt": "x", "images": []uint64{1}, "duration": float64(8), "resolution": "720p"}, 8, "720p"},
		{"viduq2", map[string]any{"prompt": "x"}, 5, "720p"},
		{"vidu1.5", map[string]any{"prompt": "x", "resolution": "1280x720"}, 5, "720p"},
	}
	for _, tc := range ok {
		if _, err := viduSubmit(env, srv.URL, tc.model, tc.input); err != nil {
			t.Fatalf("%s 应能提交：%v", tc.model, err)
		}
		b := up.last().Body
		if b["duration"] != tc.dur || b["resolution"] != tc.res {
			t.Fatalf("%s 默认值不符：%v", tc.model, b)
		}
	}

	bad := []struct {
		name  string
		model string
		input map[string]any
	}{
		{"vidu2.0 文生视频", "vidu2.0", map[string]any{"prompt": "x"}},
		{"viduq1 非 5 秒", "viduq1", map[string]any{"prompt": "x", "duration": float64(8)}},
		{"vidu2.0 8 秒 1080p", "vidu2.0", map[string]any{"prompt": "x", "images": []uint64{1}, "duration": float64(8), "resolution": "1080p"}},
		{"vidu2.0 时长 6", "vidu2.0", map[string]any{"prompt": "x", "images": []uint64{1}, "duration": float64(6)}},
		{"viduq2 时长 11", "viduq2", map[string]any{"prompt": "x", "duration": float64(11)}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			before := len(up.requests)
			_, err := viduSubmit(env, srv.URL, tc.model, tc.input)
			var pe *provider.Error
			if !errors.As(err, &pe) || !pe.PluginFault {
				t.Fatalf("应是插件级失败：%v", err)
			}
			if len(up.requests) != before {
				t.Fatal("非法组合不应发出请求")
			}
		})
	}
}

// 异步查询：created / queueing → 排队；processing → 进行中；success → 视频地址 + credits。
func TestViduQueryLifecycle(t *testing.T) {
	var polls int
	srv, up := newViduServer(t, func(w http.ResponseWriter, r *http.Request) {
		polls++
		switch polls {
		case 1:
			_, _ = w.Write([]byte(`{"state":"queueing"}`))
		case 2:
			_, _ = w.Write([]byte(`{"state":"processing"}`))
		default:
			_, _ = w.Write([]byte(`{"state":"success","credits":4,"creations":[{"id":"c1","url":"https://cdn.vidu.cn/out.mp4","cover_url":"https://cdn.vidu.cn/c.jpg"}]}`))
		}
	})
	env := newViduEnv(t)
	snap := env.snapshot(srv.URL, "viduq1")
	ref := provider.TaskRef{ID: 1, UserID: 9, ProviderTaskID: "v-9"}
	for i, want := range []string{provider.StatusQueued, provider.StatusRunning} {
		q, err := env.exec.Query(context.Background(), snap, ref)
		if err != nil || q.Status != want {
			t.Fatalf("第 %d 次应是 %s：%+v %v", i+1, want, q, err)
		}
	}
	q, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q.Status != provider.StatusSucceeded || len(q.Outputs) != 1 {
		t.Fatalf("应成功：%+v %v", q, err)
	}
	out := q.Outputs[0]
	if out.Type != provider.OutputURL || out.URL != "https://cdn.vidu.cn/out.mp4" || out.MediaType != "video" || out.Mime != "video/mp4" {
		t.Fatalf("产物不符：%+v", out)
	}
	if q.ProviderCost == nil || *q.ProviderCost != 4 {
		t.Fatalf("应带上 credits：%+v", q.ProviderCost)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/ent/v2/tasks/v-9/creations" || got.Auth != "Token vk-secret" {
		t.Fatalf("轮询请求不符：%+v", got)
	}
}

// 失败：err_code 按关键字分类；成功但没有地址判失败；提交即 failed 变成 immediate 失败。
func TestViduFailures(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		class provider.ErrorClass
	}{
		{"审核", `{"state":"failed","err_code":"AuditSubmitIllegal"}`, provider.ClassModeration},
		{"积分不足", `{"state":"failed","err_code":"CreditInsufficient"}`, provider.ClassProviderBalance},
		{"其它", `{"state":"failed","err_code":"InternalError"}`, provider.ClassTerminal},
		{"无 err_code", `{"state":"failed"}`, provider.ClassTerminal},
		{"成功无地址", `{"state":"success","creations":[]}`, provider.ClassTerminal},
	}
	env := newViduEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newViduServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) })
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, "viduq1"), provider.TaskRef{ID: 1, ProviderTaskID: "x"})
			if err != nil || q.Status != provider.StatusFailed || q.ErrorClass != tc.class || q.ErrorMessage == "" {
				t.Fatalf("失败结果不符：%+v %v", q, err)
			}
		})
	}

	srv, _ := newViduServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"v-5","state":"failed","err_code":"AuditSubmitIllegal"}`))
	})
	sub, err := viduSubmit(env, srv.URL, "viduq1", map[string]any{"prompt": "x"})
	if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusFailed || sub.Immediate.ErrorClass != provider.ClassModeration {
		t.Fatalf("提交即失败应翻译成 moderation：%+v %v", sub, err)
	}
}

// 提交：没有 task_id、或响应不是 JSON，都是插件级失败，不崩。
func TestViduSubmitBadResponses(t *testing.T) {
	env := newViduEnv(t)
	for _, body := range []string{`{}`, `not json`, `[]`} {
		srv, _ := newViduServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
		_, err := viduSubmit(env, srv.URL, "viduq1", map[string]any{"prompt": "x"})
		var pe *provider.Error
		if !errors.As(err, &pe) || !pe.PluginFault {
			t.Fatalf("响应 %q 应是插件级失败：%v", body, err)
		}
	}
}

// HTTP 非 2xx：余额 / 审核按文案分类，429 与 5xx 重试，其它 terminal。
func TestViduClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"积分不足", 400, `{"code":"Forbidden","message":"Credit insufficient"}`, provider.ClassProviderBalance},
		{"内容审核", 400, `{"err_code":"AuditSubmitIllegal"}`, provider.ClassModeration},
		{"限流", 429, `{"message":"too many requests"}`, provider.ClassRetryable},
		{"网关 5xx", 502, `bad gateway`, provider.ClassRetryable},
		{"参数错误", 400, `{"message":"invalid param"}`, provider.ClassTerminal},
		{"鉴权失败", 401, `{"message":"unauthorized"}`, provider.ClassTerminal},
	}
	env := newViduEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newViduServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := viduSubmit(env, srv.URL, "viduq1", map[string]any{"prompt": "x"})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
		})
	}
}
