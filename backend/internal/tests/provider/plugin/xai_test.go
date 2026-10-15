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
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/plugins"
)

// xaiEnv 用真实 runner 预检并装载内置 xai.js，元数据取自预检结果。
type xaiEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newXaiEnv(t *testing.T) *xaiEnv {
	t.Helper()
	src, err := plugins.Source("xai")
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
		t.Fatalf("内置 xai.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	exec := plugin.New(plugin.Options{
		Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "xai-key"}, IPAllowed: allowLoopback,
		Assets: &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("a"), 6: []byte("b"), 7: []byte("c")}},
	})
	return &xaiEnv{exec: exec, meta: meta, sha: pre.SHA256}
}

func (e *xaiEnv) snapshot(baseURL, upstreamModel string, caps modelcfg.Capabilities) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: "video", UpstreamModel: upstreamModel, Capabilities: caps},
		Channel: provider.ChannelSnapshot{
			Key: "xai-main", PluginKey: "xai", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "xai", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

// xaiCaps 开放全部媒体与 duration / resolution / aspect_ratio / generate_audio / video_action。
func xaiCaps() modelcfg.Capabilities {
	min1, max15 := 1, 15
	enum := func(label string, opts ...any) modelcfg.ParamField {
		return modelcfg.ParamField{Type: modelcfg.ParamEnum, Label: label, Open: true, Options: opts, Default: opts[0]}
	}
	return modelcfg.Capabilities{
		Ops: []string{modelcfg.OpT2V, modelcfg.OpI2V, modelcfg.OpOmni},
		Refs: modelcfg.Refs{
			Image: modelcfg.RefSpec{On: true, Max: 7, MaxMB: 10},
			Video: modelcfg.RefSpec{On: true, Max: 1, MaxMB: 50},
			Audio: modelcfg.RefSpec{On: true, Max: 3, MaxMB: 10},
		},
		Prompt: modelcfg.PromptSpec{MaxLength: 10000},
		Params: modelcfg.ParamSet{
			{Name: "duration", ParamField: modelcfg.ParamField{Type: modelcfg.ParamNumber, Label: "时长", Open: true, Min: &min1, Max: &max15, Default: 8.0}},
			{Name: "resolution", ParamField: enum("分辨率", "480p", "720p", "1080p")},
			{Name: "aspect_ratio", ParamField: enum("画幅", "16:9", "9:16", "1:1")},
			{Name: "generate_audio", ParamField: modelcfg.ParamField{Type: modelcfg.ParamBoolean, Label: "音频", Open: true}},
			{Name: "video_action", ParamField: enum("视频动作", "edit", "extend")},
		},
	}
}

type xaiRecorded struct {
	Method, Path, Auth string
	Body               map[string]any
}

type xaiUpstream struct {
	mu       sync.Mutex
	requests []xaiRecorded
	handler  func(w http.ResponseWriter, r *http.Request)
}

func (u *xaiUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	u.mu.Lock()
	u.requests = append(u.requests, xaiRecorded{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Body: body})
	u.mu.Unlock()
	u.handler(w, r)
}

func (u *xaiUpstream) last() xaiRecorded {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requests[len(u.requests)-1]
}

func xaiJSON(body string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

// xaiSubmit 提交一次并返回上游记录到的请求与提交结果。
func xaiSubmit(t *testing.T, model string, input map[string]any) (xaiRecorded, *provider.SubmitResult, error) {
	t.Helper()
	up := &xaiUpstream{handler: xaiJSON(`{"request_id":"req-1"}`)}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newXaiEnv(t)
	sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, model, xaiCaps()), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: input,
	})
	if len(up.requests) == 0 {
		return xaiRecorded{}, sub, err
	}
	return up.last(), sub, err
}

func xaiURL(t *testing.T, v any) string {
	t.Helper()
	m, _ := v.(map[string]any)
	s, _ := m["url"].(string)
	if s == "" {
		t.Fatalf("应是 {url: ...}：%v", v)
	}
	return s
}

func TestXaiPrecheckAndMeta(t *testing.T) {
	env := newXaiEnv(t)
	if env.meta.Key != "xai" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthBearer {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("video"); !ok || ep.Mode != pluginmeta.ModeAsync {
		t.Fatalf("video 应是 async：%+v", ep)
	}
}

// 文生视频：POST /v1/videos/generations，Bearer 由宿主注入；时长与分辨率总是发（默认 8 秒 480p）。
func TestXaiSubmitTextToVideo(t *testing.T) {
	got, sub, err := xaiSubmit(t, "grok-imagine-video-1.5", map[string]any{
		"prompt": "一只猫在跳舞", "op": "t2v", "aspect_ratio": "16:9", "generate_audio": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.ProviderTaskID != "req-1" || sub.Immediate != nil {
		t.Fatalf("提交结果不符：%+v", sub)
	}
	if got.Method != "POST" || got.Path != "/v1/videos/generations" || got.Auth != "Bearer xai-key" {
		t.Fatalf("上游请求不符：%+v", got)
	}
	b := got.Body
	if b["model"] != "grok-imagine-video-1.5" || b["prompt"] != "一只猫在跳舞" || b["duration"] != float64(8) ||
		b["resolution"] != "480p" || b["aspect_ratio"] != "16:9" || b["generate_audio"] != true {
		t.Fatalf("请求体不符：%v", b)
	}
	for _, k := range []string{"image", "reference_images", "video"} {
		if _, ok := b[k]; ok {
			t.Fatalf("文生视频不应带 %s：%v", k, b)
		}
	}
}

// 图生视频：单张图 -> image: {url}（宿主换成签名地址）；分辨率大小写不敏感。
func TestXaiSubmitImageToVideo(t *testing.T) {
	got, _, err := xaiSubmit(t, "grok-imagine-video", map[string]any{
		"prompt": "动起来", "op": "i2v", "images": []uint64{5}, "resolution": "720p", "duration": float64(5),
	})
	if err != nil {
		t.Fatal(err)
	}
	if xaiURL(t, got.Body["image"]) != "https://cdn.example.com/a.png" || got.Body["resolution"] != "720p" || got.Body["duration"] != float64(5) {
		t.Fatalf("图生视频请求体不符：%v", got.Body)
	}
	if _, ok := got.Body["reference_images"]; ok {
		t.Fatalf("单张图不应进 reference_images：%v", got.Body)
	}
}

// 参考生视频：omni 或多张图 -> reference_images；音频 -> reference_audios。
func TestXaiSubmitReferenceToVideo(t *testing.T) {
	got, _, err := xaiSubmit(t, "grok-imagine-video-1.5", map[string]any{
		"prompt": "角色在街上走", "op": "omni", "images": []uint64{5, 6}, "audios": []uint64{7},
	})
	if err != nil {
		t.Fatal(err)
	}
	refs, _ := got.Body["reference_images"].([]any)
	if len(refs) != 2 || xaiURL(t, refs[0]) == "" {
		t.Fatalf("应带两张 reference_images：%v", got.Body)
	}
	auds, _ := got.Body["reference_audios"].([]any)
	if len(auds) != 1 {
		t.Fatalf("应带一个 reference_audios：%v", got.Body)
	}
	if _, ok := got.Body["image"]; ok {
		t.Fatalf("参考生不应带 image：%v", got.Body)
	}

	// omni 下只有一张图也走参考生
	got, _, err = xaiSubmit(t, "grok-imagine-video-1.5", map[string]any{"prompt": "x", "op": "omni", "images": []uint64{5}})
	if err != nil {
		t.Fatal(err)
	}
	if refs, _ := got.Body["reference_images"].([]any); len(refs) != 1 {
		t.Fatalf("omni 单图应进 reference_images：%v", got.Body)
	}
}

// 视频编辑：有参考视频默认走 /v1/videos/edits，只带 prompt 与 video，不带分辨率 / 画幅 / 时长。
func TestXaiSubmitVideoEdit(t *testing.T) {
	got, _, err := xaiSubmit(t, "grok-imagine-video", map[string]any{
		"prompt": "把天空换成黄昏", "op": "omni", "videos": []uint64{5}, "resolution": "720p", "aspect_ratio": "16:9", "duration": float64(8),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "/v1/videos/edits" || got.Auth != "Bearer xai-key" || got.Body["prompt"] != "把天空换成黄昏" {
		t.Fatalf("编辑请求不符：%+v", got)
	}
	if xaiURL(t, got.Body["video"]) == "" {
		t.Fatalf("应带 video：%v", got.Body)
	}
	for _, k := range []string{"resolution", "aspect_ratio", "duration"} {
		if _, ok := got.Body[k]; ok {
			t.Fatalf("编辑不应带 %s：%v", k, got.Body)
		}
	}
}

// 视频延长：video_action=extend 走 /v1/videos/extensions，带延长秒数（默认 6）。
func TestXaiSubmitVideoExtend(t *testing.T) {
	got, _, err := xaiSubmit(t, "grok-imagine-video", map[string]any{
		"prompt": "接着往前走", "op": "omni", "videos": []uint64{5}, "video_action": "extend",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "/v1/videos/extensions" || got.Body["duration"] != float64(6) {
		t.Fatalf("延长请求不符：%+v", got)
	}
	got, _, err = xaiSubmit(t, "grok-imagine-video", map[string]any{
		"prompt": "接着往前走", "op": "omni", "videos": []uint64{5}, "video_action": "extend", "duration": float64(10),
	})
	if err != nil || got.Body["duration"] != float64(10) {
		t.Fatalf("延长秒数应取用户值：%v %v", got.Body, err)
	}
}

// 参数不合法：不发请求，按插件级失败处理（1.5 以外的模型不能编辑、参考生不支持 1080p、延长秒数越界、模型名未知）。
func TestXaiSubmitInvalidInputs(t *testing.T) {
	cases := []struct {
		name  string
		model string
		input map[string]any
	}{
		{"1.5 模型不能编辑", "grok-imagine-video-1.5", map[string]any{"prompt": "x", "op": "omni", "videos": []uint64{5}}},
		{"旧模型不支持 1080p", "grok-imagine-video", map[string]any{"prompt": "x", "resolution": "1080p"}},
		{"参考生不支持 1080p", "grok-imagine-video-1.5", map[string]any{"prompt": "x", "op": "omni", "images": []uint64{5}, "resolution": "1080p"}},
		{"延长秒数过长", "grok-imagine-video", map[string]any{"prompt": "x", "op": "omni", "videos": []uint64{5}, "video_action": "extend", "duration": float64(12)}},
		{"未知模型", "grok-2", map[string]any{"prompt": "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := &xaiUpstream{handler: xaiJSON(`{"request_id":"req-1"}`)}
			srv := httptest.NewServer(up)
			defer srv.Close()
			env := newXaiEnv(t)
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, tc.model, xaiCaps()), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: 9}, Input: tc.input,
			})
			var pe *provider.Error
			if !errors.As(err, &pe) || !pe.PluginFault || len(up.requests) != 0 {
				t.Fatalf("应是插件级失败且不发请求：%v（请求 %d 个）", err, len(up.requests))
			}
		})
	}
}

// 提交响应没有 request_id：插件级失败。
func TestXaiSubmitMissingRequestID(t *testing.T) {
	srv := httptest.NewServer(&xaiUpstream{handler: xaiJSON(`{}`)})
	defer srv.Close()
	env := newXaiEnv(t)
	_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "grok-imagine-video", xaiCaps()), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
	})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("没有 request_id 应是插件级失败：%v", err)
	}
}

// 轮询：202 无正文 -> 运行中；pending（带进度）-> 运行中；done -> 成功并带视频地址。
func TestXaiQueryLifecycle(t *testing.T) {
	var polls int
	var srv *httptest.Server
	up := &xaiUpstream{}
	up.handler = func(w http.ResponseWriter, r *http.Request) {
		polls++
		switch polls {
		case 1:
			w.WriteHeader(http.StatusAccepted)
		case 2:
			xaiJSON(`{"status":"pending","progress":40}`)(w, r)
		default:
			xaiJSON(`{"status":"done","video":{"url":"`+srv.URL+`/out.mp4","duration":8,"respect_moderation":true}}`)(w, r)
		}
	}
	srv = httptest.NewServer(up)
	defer srv.Close()

	env := newXaiEnv(t)
	snap := env.snapshot(srv.URL, "grok-imagine-video", xaiCaps())
	ref := provider.TaskRef{ID: 1, UserID: 9, ProviderTaskID: "req-1"}

	q1, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q1.Status != provider.StatusRunning {
		t.Fatalf("202 应是运行中：%+v %v", q1, err)
	}
	q2, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q2.Status != provider.StatusRunning || q2.Progress == nil || *q2.Progress != 40 {
		t.Fatalf("pending 应是运行中 40%%：%+v %v", q2, err)
	}
	q3, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q3.Status != provider.StatusSucceeded || len(q3.Outputs) != 1 {
		t.Fatalf("done 应成功：%+v %v", q3, err)
	}
	out := q3.Outputs[0]
	if out.Type != provider.OutputURL || out.URL != srv.URL+"/out.mp4" || out.MediaType != "video" || out.Mime != "video/mp4" {
		t.Fatalf("产物不符：%+v", out)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/v1/videos/req-1" || got.Auth != "Bearer xai-key" {
		t.Fatalf("轮询请求不符：%+v", got)
	}
}

// 失败与异常响应：审核拦截（respect_moderation=false、失败且带费用）、普通失败、过期、done 缺地址、未知状态。
func TestXaiQueryFailuresAndOddBodies(t *testing.T) {
	cases := []struct {
		name, body string
		status     string
		class      provider.ErrorClass
	}{
		{"产物被审核拦截", `{"status":"done","video":{"respect_moderation":false}}`, provider.StatusFailed, provider.ClassModeration},
		{"失败但已计费按审核处理", `{"status":"failed","error":{"message":"blocked"},"usage":{"cost_in_usd_ticks":500}}`, provider.StatusFailed, provider.ClassModeration},
		{"失败文案含审核", `{"status":"failed","error":{"message":"Usage Guidelines Violation"}}`, provider.StatusFailed, provider.ClassModeration},
		{"失败文案含额度", `{"status":"failed","error":{"message":"insufficient credits"}}`, provider.StatusFailed, provider.ClassProviderBalance},
		{"普通失败", `{"status":"failed","error":{"message":"internal error"}}`, provider.StatusFailed, provider.ClassTerminal},
		{"失败无原因", `{"status":"failed"}`, provider.StatusFailed, provider.ClassTerminal},
		{"已过期", `{"status":"expired"}`, provider.StatusFailed, provider.ClassTerminal},
		{"完成但没有地址", `{"status":"done","video":{}}`, provider.StatusFailed, provider.ClassTerminal},
		{"完成但没有 video", `{"status":"done"}`, provider.StatusFailed, provider.ClassTerminal},
		{"未知状态继续等", `{"status":"weird"}`, provider.StatusRunning, ""},
		{"空对象继续等", `{}`, provider.StatusRunning, ""},
	}
	env := newXaiEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(&xaiUpstream{handler: xaiJSON(tc.body)})
			defer srv.Close()
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, "grok-imagine-video", xaiCaps()), provider.TaskRef{ID: 1, ProviderTaskID: "x"})
			if err != nil {
				t.Fatal(err)
			}
			if q.Status != tc.status || q.ErrorClass != tc.class || (tc.status == provider.StatusFailed && q.ErrorMessage == "") {
				t.Fatalf("结果不符：%+v", q)
			}
		})
	}
}

// HTTP 非 2xx 的分类：余额 / 审核 / 限流 / 5xx / 其它。
func TestXaiClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"余额不足", 402, `{"error":"Your team has insufficient credits"}`, provider.ClassProviderBalance},
		{"额度用尽", 429, `{"code":"x","error":"spending limit reached"}`, provider.ClassProviderBalance},
		{"审核拒绝", 400, `{"error":"Content violates usage guidelines: safety"}`, provider.ClassModeration},
		{"限流重试", 429, `{"error":"Too many requests"}`, provider.ClassRetryable},
		{"上游 5xx 重试", 502, `bad gateway`, provider.ClassRetryable},
		{"其它 4xx 终止", 400, `{"error":"invalid duration"}`, provider.ClassTerminal},
	}
	env := newXaiEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(&xaiUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}})
			defer srv.Close()
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "grok-imagine-video", xaiCaps()), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
			})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
		})
	}
}

// 连通性检查：GET /v1/models 带 Bearer。
func TestXaiCheck(t *testing.T) {
	up := &xaiUpstream{handler: xaiJSON(`{"object":"list","data":[]}`)}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newXaiEnv(t)
	res, err := env.exec.Check(context.Background(), env.snapshot(srv.URL, "grok-imagine-video", xaiCaps()).Runtime())
	if err != nil || res == nil || !res.OK {
		t.Fatalf("检查应成功：%+v %v", res, err)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/v1/models" || got.Auth != "Bearer xai-key" {
		t.Fatalf("检查请求不符：%+v", got)
	}
}
