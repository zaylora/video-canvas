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

// alibabavideoEnv 用真实 runner 预检并装载内置 alibaba-video.js，元数据取自预检结果。
type alibabavideoEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newAlibabaVideoEnv(t *testing.T, mod func(*plugin.Options)) *alibabavideoEnv {
	t.Helper()
	src, err := plugins.Source("alibaba-video")
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
		t.Fatalf("内置 alibaba-video.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	opts := plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "sk-ali"}, IPAllowed: allowLoopback}
	if mod != nil {
		mod(&opts)
	}
	return &alibabavideoEnv{exec: plugin.New(opts), meta: meta, sha: pre.SHA256}
}

func (e *alibabavideoEnv) snapshot(baseURL, kind, upstream string, caps modelcfg.Capabilities) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: kind, UpstreamModel: upstream, Capabilities: caps},
		Channel: provider.ChannelSnapshot{
			Key: "ali-main", PluginKey: "alibaba-video", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "alibaba-video", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

// alibabavideoCaps 打开提示词、参考图 / 视频 / 音频与常用生成参数，输入里的字段才不会被宿主丢掉。
func alibabavideoCaps() modelcfg.Capabilities {
	return modelcfg.Capabilities{
		Ops:    []string{modelcfg.OpT2V, modelcfg.OpI2V, modelcfg.OpOmni, modelcfg.OpT2I, modelcfg.OpI2I},
		Refs:   modelcfg.Refs{Image: modelcfg.RefSpec{On: true, Max: 10, MaxMB: 10}},
		Prompt: modelcfg.PromptSpec{MaxLength: 10000},
	}
}

type alibabavideoReq struct {
	Method, Path, Auth, Async string
	Body                      map[string]any
}

type alibabavideoUp struct {
	mu      sync.Mutex
	reqs    []alibabavideoReq
	handler func(w http.ResponseWriter, r *http.Request)
}

func (u *alibabavideoUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	u.mu.Lock()
	u.reqs = append(u.reqs, alibabavideoReq{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-DashScope-Async"), body})
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	u.handler(w, r)
}

func (u *alibabavideoUp) last() alibabavideoReq {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.reqs[len(u.reqs)-1]
}

func alibabavideoSubmit(t *testing.T, env *alibabavideoEnv, snap *provider.Snapshot, input map[string]any) (*provider.SubmitResult, error) {
	t.Helper()
	return env.exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1, UserID: 9}, Input: input})
}

func alibabavideoSub(m map[string]any, keys ...string) map[string]any {
	cur := m
	for _, k := range keys {
		next, _ := cur[k].(map[string]any)
		if next == nil {
			return map[string]any{}
		}
		cur = next
	}
	return cur
}

func alibabavideoStatic(body string) *httptest.Server {
	return httptest.NewServer(&alibabavideoUp{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }})
}

// 视频提交：wan2.6-t2v 属于“size”系列，分辨率 + 比例换算成 parameters.size；带异步头与 Bearer；时长与生成音频照传。
func TestAlibabaVideoVideoSubmitT2VSize(t *testing.T) {
	up := &alibabavideoUp{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"task_id":"t-1","task_status":"PENDING"},"request_id":"r"}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newAlibabaVideoEnv(t, nil)
	snap := env.snapshot(srv.URL, "video", "wan2.6-t2v", alibabavideoCaps())
	sub, err := alibabavideoSubmit(t, env, snap, map[string]any{
		"prompt": "猫在跳舞", "duration": float64(10), "resolution": "720p", "aspect_ratio": "9:16", "generate_audio": true,
		"shot_type": "multi", "prompt_extend": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.ProviderTaskID != "t-1" || sub.Immediate != nil {
		t.Fatalf("提交结果不符：%+v", sub)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/api/v1/services/aigc/video-generation/video-synthesis" || got.Auth != "Bearer sk-ali" || got.Async != "enable" {
		t.Fatalf("请求不符：%+v", got)
	}
	par := alibabavideoSub(got.Body, "parameters")
	if got.Body["model"] != "wan2.6-t2v" || alibabavideoSub(got.Body, "input")["prompt"] != "猫在跳舞" ||
		par["size"] != "720*1280" || par["duration"] != float64(10) || par["audio"] != true || par["prompt_extend"] != false || par["shot_type"] != "multi" {
		t.Fatalf("请求体不符：%v", got.Body)
	}
	if _, has := par["resolution"]; has {
		t.Fatalf("size 系列不应再传 resolution：%v", par)
	}
}

// wan2.7-t2v 用 resolution + ratio；分辨率不在模型支持范围内是插件级失败；日期快照按去日期的名字处理。
func TestAlibabaVideoVideoT2VModernAndSnapshot(t *testing.T) {
	up := &alibabavideoUp{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"task_id":"t-2","task_status":"PENDING"}}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newAlibabaVideoEnv(t, nil)
	snap := env.snapshot(srv.URL, "video", "wan2.7-t2v-2026-04-25", alibabavideoCaps())
	if _, err := alibabavideoSubmit(t, env, snap, map[string]any{"prompt": "x", "resolution": "1080P", "aspect_ratio": "4:3", "duration": float64(8)}); err != nil {
		t.Fatal(err)
	}
	par := alibabavideoSub(up.last().Body, "parameters")
	if par["resolution"] != "1080P" || par["ratio"] != "4:3" || par["duration"] != float64(8) {
		t.Fatalf("参数不符：%v", par)
	}
	_, err := alibabavideoSubmit(t, env, snap, map[string]any{"prompt": "x", "resolution": "480P"})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("不支持的分辨率应是插件级失败：%v", err)
	}
}

// 图生视频：wan2.6-i2v 用 input.img_url（参考图换成签名 URL）；wan2.2-kf2v-flash 走 image2video 路径带首尾帧。
func TestAlibabaVideoVideoImageRefs(t *testing.T) {
	up := &alibabavideoUp{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"task_id":"t-3","task_status":"PENDING"}}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("a"), 6: []byte("b")}}
	env := newAlibabaVideoEnv(t, func(o *plugin.Options) { o.Assets = assets })

	_, err := alibabavideoSubmit(t, env, env.snapshot(srv.URL, "video", "wan2.6-i2v", alibabavideoCaps()),
		map[string]any{"prompt": "动起来", "op": "i2v", "images": []uint64{5}, "duration": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	got := up.last()
	if got.Path != "/api/v1/services/aigc/video-generation/video-synthesis" ||
		alibabavideoSub(got.Body, "input")["img_url"] != "https://cdn.example.com/a.png" || alibabavideoSub(got.Body, "parameters")["resolution"] != "1080P" {
		t.Fatalf("i2v 请求不符：%v", got)
	}
	if _, has := alibabavideoSub(got.Body, "parameters")["ratio"]; has {
		t.Fatal("图生视频不应传比例")
	}

	_, err = alibabavideoSubmit(t, env, env.snapshot(srv.URL, "video", "wan2.2-kf2v-flash", alibabavideoCaps()),
		map[string]any{"prompt": "过渡", "op": "i2v", "images": []uint64{5, 6}})
	if err != nil {
		t.Fatal(err)
	}
	got = up.last()
	in := alibabavideoSub(got.Body, "input")
	if got.Path != "/api/v1/services/aigc/image2video/video-synthesis" || in["first_frame_url"] == nil || in["last_frame_url"] == nil {
		t.Fatalf("首尾帧请求不符：%v", got)
	}

	// 没给参考图的图生视频模型：插件级失败
	_, err = alibabavideoSubmit(t, env, env.snapshot(srv.URL, "video", "wan2.6-i2v", alibabavideoCaps()), map[string]any{"prompt": "x"})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("缺参考图应是插件级失败：%v", err)
	}
}

// wan3.0：默认按首尾帧填 input.media；omni 时全部当参考图；时长 -1 表示智能时长。
func TestAlibabaVideoVideoWan3Media(t *testing.T) {
	up := &alibabavideoUp{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"task_id":"t-4","task_status":"PENDING"}}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("a"), 6: []byte("b")}}
	env := newAlibabaVideoEnv(t, func(o *plugin.Options) { o.Assets = assets })
	snap := env.snapshot(srv.URL, "video", "wan3.0-video", alibabavideoCaps())

	if _, err := alibabavideoSubmit(t, env, snap, map[string]any{"prompt": "x", "op": "i2v", "images": []uint64{5, 6}, "duration": float64(-1)}); err != nil {
		t.Fatal(err)
	}
	media, _ := alibabavideoSub(up.last().Body, "input")["media"].([]any)
	if len(media) != 2 || media[0].(map[string]any)["type"] != "first_frame" || media[1].(map[string]any)["type"] != "last_frame" {
		t.Fatalf("首尾帧 media 不符：%v", media)
	}
	par := alibabavideoSub(up.last().Body, "parameters")
	if par["duration"] != float64(-1) || par["ratio"] != "adaptive" || par["resolution"] != "1080P" {
		t.Fatalf("wan3.0 参数不符：%v", par)
	}

	if _, err := alibabavideoSubmit(t, env, snap, map[string]any{"prompt": "x", "op": "omni", "images": []uint64{5, 6}}); err != nil {
		t.Fatal(err)
	}
	media, _ = alibabavideoSub(up.last().Body, "input")["media"].([]any)
	if len(media) != 2 || media[0].(map[string]any)["type"] != "reference_image" {
		t.Fatalf("omni 应全部当参考图：%v", media)
	}
}

// 视频查询：PENDING → RUNNING → SUCCEEDED（带 video_url）；路径是 /api/v1/tasks/{id}。
func TestAlibabaVideoVideoQueryLifecycle(t *testing.T) {
	var polls int
	up := &alibabavideoUp{}
	up.handler = func(w http.ResponseWriter, r *http.Request) {
		polls++
		switch polls {
		case 1:
			_, _ = w.Write([]byte(`{"output":{"task_id":"t-9","task_status":"PENDING"}}`))
		case 2:
			_, _ = w.Write([]byte(`{"output":{"task_id":"t-9","task_status":"RUNNING"}}`))
		default:
			_, _ = w.Write([]byte(`{"output":{"task_id":"t-9","task_status":"SUCCEEDED","video_url":"https://cdn.example.com/v.mp4?x=1"}}`))
		}
	}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newAlibabaVideoEnv(t, nil)
	snap := env.snapshot(srv.URL, "video", "wan2.6-t2v", alibabavideoCaps())
	ref := provider.TaskRef{ID: 1, UserID: 9, ProviderTaskID: "t-9"}
	q1, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q1.Status != provider.StatusQueued {
		t.Fatalf("应排队：%+v %v", q1, err)
	}
	q2, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q2.Status != provider.StatusRunning {
		t.Fatalf("应进行中：%+v %v", q2, err)
	}
	q3, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q3.Status != provider.StatusSucceeded || len(q3.Outputs) != 1 {
		t.Fatalf("应成功：%+v %v", q3, err)
	}
	if o := q3.Outputs[0]; o.URL != "https://cdn.example.com/v.mp4?x=1" || o.MediaType != "video" || o.Mime != "video/mp4" {
		t.Fatalf("产物不符：%+v", o)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/api/v1/tasks/t-9" || got.Auth != "Bearer sk-ali" {
		t.Fatalf("轮询请求不符：%+v", got)
	}
}

// 查询失败：FAILED / CANCELED 带错误码与文案，审核 / 欠费归类；成功但没有地址判失败。
func TestAlibabaVideoQueryFailed(t *testing.T) {
	cases := []struct {
		name, body string
		class      provider.ErrorClass
	}{
		{"审核", `{"output":{"task_status":"FAILED","code":"DataInspectionFailed","message":"Input data may contain inappropriate content."}}`, provider.ClassModeration},
		{"欠费", `{"output":{"task_status":"FAILED","code":"Arrearage","message":"Access denied, overdue payment"}}`, provider.ClassProviderBalance},
		{"其它", `{"output":{"task_status":"CANCELED","message":"canceled by user"}}`, provider.ClassTerminal},
		{"未知任务", `{"output":{"task_status":"UNKNOWN"}}`, provider.ClassTerminal},
		{"成功无地址", `{"output":{"task_status":"SUCCEEDED"}}`, provider.ClassTerminal},
	}
	env := newAlibabaVideoEnv(t, nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := alibabavideoStatic(tc.body)
			defer srv.Close()
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, "video", "wan2.6-t2v", alibabavideoCaps()), provider.TaskRef{ID: 1, ProviderTaskID: "x"})
			if err != nil {
				t.Fatal(err)
			}
			if q.Status != provider.StatusFailed || q.ErrorClass != tc.class || q.ErrorMessage == "" {
				t.Fatalf("失败结果不符：%+v", q)
			}
		})
	}
}

// 提交响应异常：200 + code 是 failed 结果；没有 task_id 也没有错误信息是插件级失败。
func TestAlibabaVideoSubmitBadResponses(t *testing.T) {
	env := newAlibabaVideoEnv(t, nil)
	srv := alibabavideoStatic(`{"code":"InvalidParameter","message":"bad prompt"}`)
	defer srv.Close()
	sub, err := alibabavideoSubmit(t, env, env.snapshot(srv.URL, "video", "wan2.6-t2v", alibabavideoCaps()), map[string]any{"prompt": "x"})
	if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusFailed || !strings.Contains(sub.Immediate.ErrorMessage, "InvalidParameter") {
		t.Fatalf("应翻译成 failed：%+v %v", sub, err)
	}
	empty := alibabavideoStatic(`{"output":{}}`)
	defer empty.Close()
	_, err = alibabavideoSubmit(t, env, env.snapshot(empty.URL, "video", "wan2.6-t2v", alibabavideoCaps()), map[string]any{"prompt": "x"})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("没有 task_id 应是插件级失败：%v", err)
	}
	// 非 JSON 响应也不崩
	bad := alibabavideoStatic(`not json`)
	defer bad.Close()
	if _, err = alibabavideoSubmit(t, env, env.snapshot(bad.URL, "video", "wan2.6-t2v", alibabavideoCaps()), map[string]any{"prompt": "x"}); err == nil {
		t.Fatal("坏响应应报错")
	}
	// 不认识的模型：插件级失败
	_, err = alibabavideoSubmit(t, env, env.snapshot(srv.URL, "video", "no-such-model", alibabavideoCaps()), map[string]any{"prompt": "x"})
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("未知模型应是插件级失败：%v", err)
	}
}

// HTTP 非 2xx：欠费 / 审核 / 限流（Throttling 里带 quota 也要重试）/ 5xx / 其它 4xx。
func TestAlibabaVideoClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"欠费", 400, `{"code":"Arrearage","message":"Access denied, please make sure your account is in good standing."}`, provider.ClassProviderBalance},
		{"审核", 400, `{"code":"DataInspectionFailed","message":"Input data may contain inappropriate content."}`, provider.ClassModeration},
		{"限流含 quota", 429, `{"code":"Throttling.RateQuota","message":"Requests rate limit exceeded"}`, provider.ClassRetryable},
		{"5xx", 503, `bad gateway`, provider.ClassRetryable},
		{"参数错误", 400, `{"code":"InvalidParameter","message":"bad size"}`, provider.ClassTerminal},
		{"Key 无效", 401, `{"code":"InvalidApiKey","message":"Invalid API-key provided."}`, provider.ClassTerminal},
	}
	env := newAlibabaVideoEnv(t, nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(&alibabavideoUp{handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}})
			defer srv.Close()
			_, err := alibabavideoSubmit(t, env, env.snapshot(srv.URL, "video", "wan2.6-t2v", alibabavideoCaps()), map[string]any{"prompt": "x"})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
		})
	}
}

// 连通性检查：GET /compatible-mode/v1/models，带 Bearer；401 失败。
func TestAlibabaVideoCheck(t *testing.T) {
	env := newAlibabaVideoEnv(t, nil)
	up := &alibabavideoUp{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":[]}`)) }}
	srv := httptest.NewServer(up)
	defer srv.Close()
	res, err := env.exec.Check(context.Background(), env.snapshot(srv.URL, "video", "wan2.6-t2v", alibabavideoCaps()).Runtime())
	if err != nil || res == nil || !res.OK {
		t.Fatalf("检查应成功：%+v %v", res, err)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/compatible-mode/v1/models" || got.Auth != "Bearer sk-ali" {
		t.Fatalf("检查请求不符：%+v", got)
	}
	bad := httptest.NewServer(&alibabavideoUp{handler: func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"InvalidApiKey"}`))
	}})
	defer bad.Close()
	res, err = env.exec.Check(context.Background(), env.snapshot(bad.URL, "video", "wan2.6-t2v", alibabavideoCaps()).Runtime())
	if err == nil || (res != nil && res.OK) {
		t.Fatalf("401 应检查失败：%+v %v", res, err)
	}
}
