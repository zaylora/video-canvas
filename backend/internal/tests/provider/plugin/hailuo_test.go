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

// hailuoEnv 用真实 runner 预检并装载内置 hailuo.js，元数据取自预检结果。
type hailuoEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newHailuoEnv(t *testing.T, mod func(*plugin.Options)) *hailuoEnv {
	t.Helper()
	src, err := plugins.Source("hailuo")
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
		t.Fatalf("内置 hailuo.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	opts := plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "mm-key"}, IPAllowed: allowLoopback}
	if mod != nil {
		mod(&opts)
	}
	return &hailuoEnv{exec: plugin.New(opts), meta: meta, sha: pre.SHA256}
}

// hailuoCaps 开放文生 / 图生 / 全能参考三种方式和三类参考素材，让参考图输入能通过宿主的能力校验。
var hailuoCaps = modelcfg.Capabilities{
	Ops:    []string{modelcfg.OpT2V, modelcfg.OpI2V, modelcfg.OpOmni},
	Refs:   modelcfg.Refs{Image: modelcfg.RefSpec{On: true, Max: 9, MaxMB: 10}, Video: modelcfg.RefSpec{On: true, Max: 3, MaxMB: 50}, Audio: modelcfg.RefSpec{On: true, Max: 3, MaxMB: 10}},
	Prompt: modelcfg.PromptSpec{MaxLength: 10000},
}

func (e *hailuoEnv) snapshot(baseURL, upstreamModel string) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: "video", UpstreamModel: upstreamModel, Capabilities: hailuoCaps},
		Channel: provider.ChannelSnapshot{
			Key: "hailuo-main", PluginKey: "hailuo", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "hailuo", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

// hailuoUpstream 是按路径分发的假 MiniMax，记录请求。
func hailuoUpstream(handler func(w http.ResponseWriter, r *http.Request, body map[string]any)) (*fakeNewAPI, *httptest.Server) {
	up := &fakeNewAPI{handler: handler}
	return up, httptest.NewServer(up)
}

func hailuoQuery(t *testing.T, env *hailuoEnv, snap *provider.Snapshot, state string) *provider.QueryResult {
	t.Helper()
	ref := provider.TaskRef{ID: 1, UserID: 9, ProviderTaskID: "t-1"}
	if state != "" {
		ref.State = []byte(state)
	}
	q, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestHailuoPrecheckAndMeta(t *testing.T) {
	env := newHailuoEnv(t, nil)
	if env.meta.Key != "hailuo" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthBearer {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("video"); !ok || ep.Mode != pluginmeta.ModeAsync {
		t.Fatalf("video 应是 async：%+v", ep)
	}
	if _, ok := env.meta.Endpoint("image"); ok {
		t.Fatal("hailuo 只应声明 video")
	}
}

// 老系列文生视频：POST /v1/video_generation，Bearer 由宿主注入；输入里的 prompt_optimizer / fast_pretreatment / aigc_watermark 被忽略。
func TestHailuoV1TextToVideoSubmit(t *testing.T) {
	up, srv := hailuoUpstream(func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"task_id":"106","base_resp":{"status_code":0,"status_msg":"success"}}`))
	})
	defer srv.Close()
	env := newHailuoEnv(t, nil)
	snap := env.snapshot(srv.URL, "MiniMax-Hailuo-2.3")
	res, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "海边日落", "duration": float64(10), "resolution": "1080P",
			"prompt_optimizer": false, "fast_pretreatment": true, "aigc_watermark": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderTaskID != "106" || res.Immediate != nil {
		t.Fatalf("提交结果不符：%+v", res)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/v1/video_generation" || got.Auth != "Bearer mm-key" {
		t.Fatalf("上游请求不符：%+v", got)
	}
	b := got.Body
	if b["model"] != "MiniMax-Hailuo-2.3" || b["prompt"] != "海边日落" || b["duration"] != float64(10) || b["resolution"] != "1080P" {
		t.Fatalf("请求体不符：%v", b)
	}
	for _, k := range []string{"prompt_optimizer", "fast_pretreatment", "aigc_watermark"} {
		if _, has := b[k]; has {
			t.Fatalf("%s 不是插件输入，不应发送：%v", k, b)
		}
	}
	if _, has := b["first_frame_image"]; has {
		t.Fatalf("没有参考图不应带首帧：%v", b)
	}
}

// 老系列默认值：现代系列 768P，01 系列 720P；720 在现代系列里归一成 768P。
func TestHailuoV1ResolutionDefaults(t *testing.T) {
	cases := []struct {
		model string
		in    map[string]any
		want  string
	}{
		{"MiniMax-Hailuo-02", map[string]any{"prompt": "x"}, "768P"},
		{"MiniMax-Hailuo-02", map[string]any{"prompt": "x", "resolution": "720P"}, "768P"},
		{"MiniMax-Hailuo-02", map[string]any{"prompt": "x", "resolution": "512p"}, "512P"},
		{"T2V-01", map[string]any{"prompt": "x"}, "720P"},
	}
	for _, tc := range cases {
		up, srv := hailuoUpstream(func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
			_, _ = w.Write([]byte(`{"task_id":"1","base_resp":{"status_code":0}}`))
		})
		env := newHailuoEnv(t, nil)
		if _, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, tc.model), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: tc.in}); err != nil {
			t.Fatal(err)
		}
		b := up.last().Body
		if b["resolution"] != tc.want || b["duration"] != float64(6) {
			t.Fatalf("%s %v：resolution=%v duration=%v，期望 %s / 6", tc.model, tc.in, b["resolution"], b["duration"], tc.want)
		}
		srv.Close()
	}
}

// 图生视频：第一张图是首帧，第二张是尾帧，都通过文件引用换成地址；S2V-01 的图片走 subject_reference。
func TestHailuoV1ImageReferences(t *testing.T) {
	up, srv := hailuoUpstream(func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"task_id":"1","base_resp":{"status_code":0}}`))
	})
	defer srv.Close()
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("A"), 6: []byte("B")}}
	env := newHailuoEnv(t, func(o *plugin.Options) { o.Assets = assets })

	_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "MiniMax-Hailuo-02"), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x", "op": "i2v", "images": []uint64{5, 6}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := up.last().Body
	if b["first_frame_image"] != "https://cdn.example.com/a.png" || b["last_frame_image"] != "https://cdn.example.com/a.png" {
		t.Fatalf("应带首尾帧：%v", b)
	}

	_, err = env.exec.Submit(context.Background(), env.snapshot(srv.URL, "S2V-01"), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x", "images": []uint64{5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b = up.last().Body
	subj, _ := b["subject_reference"].([]any)
	if len(subj) != 1 {
		t.Fatalf("S2V-01 应带 subject_reference：%v", b)
	}
	item := subj[0].(map[string]any)
	imgs, _ := item["image"].([]any)
	if item["type"] != "character" || len(imgs) != 1 || imgs[0] != "https://cdn.example.com/a.png" {
		t.Fatalf("subject_reference 不符：%v", item)
	}
	if _, has := b["first_frame_image"]; has {
		t.Fatalf("S2V-01 不应带首帧：%v", b)
	}
}

// H3：POST /v2/video_generation，帧图模式 content = 文本 + first_frame / last_frame，ratio 有图时默认 adaptive。
func TestHailuoH3FrameSubmit(t *testing.T) {
	up, srv := hailuoUpstream(func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"task_id":"h3-1"}`)) // v2 提交成功没有 base_resp
	})
	defer srv.Close()
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("A"), 6: []byte("B")}}
	env := newHailuoEnv(t, func(o *plugin.Options) { o.Assets = assets })
	res, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "MiniMax-H3"), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "跑步", "op": "i2v", "images": []uint64{5, 6}, "duration": float64(8), "resolution": "2k",
			"prompt_optimizer": true, "fast_pretreatment": true, "aigc_watermark": true},
	})
	if err != nil || res.ProviderTaskID != "h3-1" {
		t.Fatalf("提交失败：%+v %v", res, err)
	}
	got := up.last()
	if got.Path != "/v2/video_generation" || got.Auth != "Bearer mm-key" {
		t.Fatalf("上游请求不符：%+v", got)
	}
	b := got.Body
	if b["model"] != "MiniMax-H3" || b["resolution"] != "2K" || b["duration"] != float64(8) || b["ratio"] != "adaptive" {
		t.Fatalf("请求体不符：%v", b)
	}
	for _, k := range []string{"prompt_optimizer", "fast_pretreatment", "aigc_watermark"} {
		if _, has := b[k]; has {
			t.Fatalf("H3 也不应发送 %s：%v", k, b)
		}
	}
	content, _ := b["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("content 应有文本 + 两张帧图：%v", content)
	}
	if c := content[0].(map[string]any); c["type"] != "text" || c["text"] != "跑步" {
		t.Fatalf("第一项应是文本：%v", c)
	}
	first := content[1].(map[string]any)
	last := content[2].(map[string]any)
	if first["type"] != "image_url" || first["role"] != "first_frame" || last["role"] != "last_frame" {
		t.Fatalf("帧图角色不符：%v %v", first, last)
	}
	if u := first["image_url"].(map[string]any)["url"]; u != "https://cdn.example.com/a.png" {
		t.Fatalf("图片应换成地址：%v", first)
	}
}

// H3 参考模式：op=omni 时图片是 reference_image；带视频 / 音频参考；纯文本默认 768P / 5 秒 / 16:9，非法 ratio 回退。
func TestHailuoH3ReferenceAndDefaults(t *testing.T) {
	up, srv := hailuoUpstream(func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"task_id":"h3-2"}`))
	})
	defer srv.Close()
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("A"), 7: []byte("V"), 8: []byte("S")}}
	env := newHailuoEnv(t, func(o *plugin.Options) { o.Assets = assets })
	snap := env.snapshot(srv.URL, "MiniMax-H3")

	_, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{
			"prompt": "x", "op": "omni", "images": []uint64{5}, "videos": []uint64{7}, "audios": []uint64{8}, "aspect_ratio": "9:16"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := up.last().Body
	content, _ := b["content"].([]any)
	roles := []string{}
	for _, c := range content {
		role, _ := c.(map[string]any)["role"].(string) // 文本项没有 role
		roles = append(roles, role)
	}
	if len(content) != 4 || roles[1] != "reference_image" || roles[2] != "reference_video" || roles[3] != "reference_audio" {
		t.Fatalf("参考模式 content 不符：%v", content)
	}
	if v := content[2].(map[string]any)["video_url"].(map[string]any)["url"]; v == nil {
		t.Fatalf("视频参考缺地址：%v", content[2])
	}
	if b["ratio"] != "9:16" {
		t.Fatalf("ratio 应取 aspect_ratio：%v", b)
	}

	_, err = env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "纯文本", "aspect_ratio": "adaptive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b = up.last().Body
	if b["ratio"] != "16:9" || b["resolution"] != "768P" || b["duration"] != float64(5) {
		t.Fatalf("纯文本 adaptive 应回退 16:9 / 768P / 5 秒：%v", b)
	}
}

// 提交：上游 200 + base_resp 错误码，翻译成 failed 并分类；缺 task_id 是插件级失败。
func TestHailuoSubmitBusinessErrors(t *testing.T) {
	cases := []struct {
		body  string
		class provider.ErrorClass
	}{
		{`{"base_resp":{"status_code":1008,"status_msg":"insufficient balance"}}`, provider.ClassProviderBalance},
		{`{"base_resp":{"status_code":1026,"status_msg":"input new_sensitive"}}`, provider.ClassModeration},
		{`{"base_resp":{"status_code":1002,"status_msg":"rate limit"}}`, provider.ClassRetryable},
		{`{"base_resp":{"status_code":2013,"status_msg":"invalid params"}}`, provider.ClassTerminal},
		{`{"error":{"message":"prompt violates policy","http_code":400}}`, provider.ClassModeration},
	}
	for _, tc := range cases {
		t.Run(tc.body, func(t *testing.T) {
			_, srv := hailuoUpstream(func(w http.ResponseWriter, r *http.Request, _ map[string]any) { _, _ = w.Write([]byte(tc.body)) })
			defer srv.Close()
			env := newHailuoEnv(t, nil)
			res, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "MiniMax-H3"), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.Immediate == nil || res.Immediate.Status != provider.StatusFailed || res.Immediate.ErrorClass != tc.class {
				t.Fatalf("应翻译成 %s 失败：%+v", tc.class, res.Immediate)
			}
		})
	}

	_, srv := hailuoUpstream(func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"base_resp":{"status_code":0}}`))
	})
	defer srv.Close()
	env := newHailuoEnv(t, nil)
	_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "T2V-01"), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("缺 task_id 应是插件级失败：%v", err)
	}
}

// v1 轮询：Queueing → Processing → Success（带 file_id，状态里记下）→ 用 file_id 换地址 → succeeded。
func TestHailuoV1QueryFlow(t *testing.T) {
	var srv *httptest.Server
	up, s := hailuoUpstream(func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		switch r.URL.Path {
		case "/v1/query/video_generation":
			switch r.URL.Query().Get("task_id") {
			case "q":
				_, _ = w.Write([]byte(`{"task_id":"q","status":"Queueing","base_resp":{"status_code":0}}`))
			case "p":
				_, _ = w.Write([]byte(`{"task_id":"p","status":"Processing","base_resp":{"status_code":0}}`))
			default:
				_, _ = w.Write([]byte(`{"task_id":"d","status":"Success","file_id":"f-9","video_width":1366,"base_resp":{"status_code":0}}`))
			}
		case "/v1/files/retrieve":
			_, _ = w.Write([]byte(`{"file":{"file_id":"f-9","download_url":"` + srv.URL + `/dl/out.mp4"},"base_resp":{"status_code":0}}`))
		default:
			http.NotFound(w, r)
		}
	})
	srv = s
	defer srv.Close()
	env := newHailuoEnv(t, nil)
	snap := env.snapshot(srv.URL, "MiniMax-Hailuo-2.3")
	run := func(id, state string) *provider.QueryResult {
		q, err := env.exec.Query(context.Background(), snap, provider.TaskRef{ID: 1, ProviderTaskID: id, State: []byte(state)})
		if err != nil {
			t.Fatal(err)
		}
		return q
	}

	if q := run("q", ""); q.Status != provider.StatusQueued {
		t.Fatalf("Queueing 应是排队：%+v", q)
	}
	if q := run("p", ""); q.Status != provider.StatusRunning || q.Progress == nil {
		t.Fatalf("Processing 应是进行中：%+v", q)
	}
	q := run("d", `{"v2":false}`)
	if q.Status != provider.StatusRunning || string(q.State) == "" {
		t.Fatalf("Success 第一步应继续 running 并记下 file_id：%+v", q)
	}
	if got := up.last(); got.Path != "/v1/query/video_generation" || got.Auth != "Bearer mm-key" {
		t.Fatalf("查询请求不符：%+v", got)
	}
	q2 := run("d", string(q.State))
	if q2.Status != provider.StatusSucceeded || len(q2.Outputs) != 1 {
		t.Fatalf("第二步应成功：%+v", q2)
	}
	out := q2.Outputs[0]
	if out.URL != srv.URL+"/dl/out.mp4" || out.MediaType != "video" || out.Mime != "video/mp4" {
		t.Fatalf("产物不符：%+v", out)
	}
	if got := up.last(); got.Path != "/v1/files/retrieve" {
		t.Fatalf("第二步应请求 retrieve：%+v", got)
	}
}

// v1 轮询失败与异常响应：Fail、没有 file_id、retrieve 没地址、限流错误码（继续等待）、审核错误码。
func TestHailuoV1QueryFailures(t *testing.T) {
	cases := []struct {
		name   string
		state  string
		body   string
		status string
		class  provider.ErrorClass
	}{
		{"Fail", "", `{"status":"Fail","base_resp":{"status_code":0}}`, provider.StatusFailed, provider.ClassTerminal},
		{"Fail 带审核码", "", `{"status":"Fail","base_resp":{"status_code":1027,"status_msg":"output sensitive"}}`, provider.StatusFailed, provider.ClassModeration},
		{"Success 没有 file_id", "", `{"status":"Success","base_resp":{"status_code":0}}`, provider.StatusFailed, provider.ClassTerminal},
		{"限流等待", "", `{"base_resp":{"status_code":1002,"status_msg":"rate limit"}}`, provider.StatusRunning, ""},
		{"余额", "", `{"base_resp":{"status_code":1008,"status_msg":"insufficient balance"}}`, provider.StatusFailed, provider.ClassProviderBalance},
		{"retrieve 没地址", `{"fileId":"f-1"}`, `{"file":{},"base_resp":{"status_code":0}}`, provider.StatusFailed, provider.ClassTerminal},
		{"空对象", "", `{}`, provider.StatusQueued, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv := hailuoUpstream(func(w http.ResponseWriter, r *http.Request, _ map[string]any) { _, _ = w.Write([]byte(tc.body)) })
			defer srv.Close()
			env := newHailuoEnv(t, nil)
			q := hailuoQuery(t, env, env.snapshot(srv.URL, "T2V-01"), tc.state)
			if q.Status != tc.status || q.ErrorClass != tc.class {
				t.Fatalf("结果不符：%+v", q)
			}
		})
	}
}

// v2 轮询：queued / running / succeeded（带地址）/ failed（带原因）/ 缺地址 / cancelled。
func TestHailuoV2Query(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status string
		class  provider.ErrorClass
		url    string
	}{
		{"queued", `{"task":{"status":"queued"}}`, provider.StatusQueued, "", ""},
		{"running", `{"task":{"status":"running"}}`, provider.StatusRunning, "", ""},
		{"succeeded", `{"task":{"status":"succeeded","content":{"url":"https://cdn.example.com/h3.mp4"},"resolution":"2K"}}`, provider.StatusSucceeded, "", "https://cdn.example.com/h3.mp4"},
		{"succeeded 没地址", `{"task":{"status":"succeeded"}}`, provider.StatusFailed, provider.ClassTerminal, ""},
		{"failed 审核", `{"task":{"status":"failed","error":{"message":"content sensitive"}}}`, provider.StatusFailed, provider.ClassModeration, ""},
		{"cancelled", `{"task":{"status":"cancelled"}}`, provider.StatusFailed, provider.ClassTerminal, ""},
		{"顶层错误限流", `{"error":{"message":"busy","http_code":429}}`, provider.StatusRunning, "", ""},
		{"没有 task", `{}`, provider.StatusQueued, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up, srv := hailuoUpstream(func(w http.ResponseWriter, r *http.Request, _ map[string]any) { _, _ = w.Write([]byte(tc.body)) })
			defer srv.Close()
			env := newHailuoEnv(t, nil)
			q := hailuoQuery(t, env, env.snapshot(srv.URL, "MiniMax-H3"), "")
			if q.Status != tc.status || q.ErrorClass != tc.class {
				t.Fatalf("结果不符：%+v", q)
			}
			if tc.url != "" && (len(q.Outputs) != 1 || q.Outputs[0].URL != tc.url) {
				t.Fatalf("产物不符：%+v", q.Outputs)
			}
			if got := up.last(); got.Method != "GET" || got.Path != "/v2/query/video_generation/t-1" || got.Auth != "Bearer mm-key" {
				t.Fatalf("v2 查询请求不符：%+v", got)
			}
		})
	}
}

// HTTP 非 2xx：余额 / 审核 / 限流 / 5xx / 其它 4xx。
func TestHailuoClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"余额码", 402, `{"base_resp":{"status_code":1008,"status_msg":"insufficient balance"}}`, provider.ClassProviderBalance},
		{"审核", 400, `{"base_resp":{"status_code":1026,"status_msg":"sensitive"}}`, provider.ClassModeration},
		{"限流", 429, `{"base_resp":{"status_code":1002,"status_msg":"rate limit"}}`, provider.ClassRetryable},
		{"5xx 非 JSON", 502, `bad gateway`, provider.ClassRetryable},
		{"其它 4xx", 400, `{"base_resp":{"status_code":2013,"status_msg":"invalid params"}}`, provider.ClassTerminal},
		{"v2 error 体", 400, `{"error":{"message":"prompt violates content policy"}}`, provider.ClassModeration},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv := hailuoUpstream(func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			defer srv.Close()
			env := newHailuoEnv(t, nil)
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "MiniMax-Hailuo-02"), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"},
			})
			var pe *provider.Error
			if !errors.As(err, &pe) || pe.Class != tc.class {
				t.Fatalf("期望分类 %s，得到 %v", tc.class, err)
			}
		})
	}
}
