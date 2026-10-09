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

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/plugins"
)

// newapiEnv 用真实的 runner 预检并装载内置 newapi.js，元数据取自预检结果（不手写），
// 这样预检规则、宿主、插件三者的契约在同一个测试里一起被检验。
type newapiEnv struct {
	exec *plugin.Executor
	code string
	meta *pluginmeta.Meta
	sha  string
}

func newNewAPIEnv(t *testing.T, mod func(*plugin.Options)) *newapiEnv {
	t.Helper()
	list, err := plugins.Builtin()
	if err != nil || len(list) == 0 {
		t.Fatalf("读取内置插件失败：%v", err)
	}
	code := string(list[0])
	runner := plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{}).Handler())
	pre, err := runner.Precheck(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	if !pre.OK {
		t.Fatalf("内置 newapi.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	opts := plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "sk-newapi"}}
	if mod != nil {
		mod(&opts)
	}
	return &newapiEnv{exec: plugin.New(opts), code: code, meta: meta, sha: pre.SHA256}
}

func (e *newapiEnv) snapshot(baseURL, kind string, caps modelcfg.Capabilities) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: kind, UpstreamModel: "up-model", Capabilities: caps},
		Channel: provider.ChannelSnapshot{
			Key: "newapi-main", PluginKey: "newapi", PluginVersionID: 1, BaseURL: baseURL, TrustedInternal: true,
		},
		Plugin:          provider.PluginSnapshot{Key: "newapi", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

func (e *newapiEnv) runtime(baseURL string) *provider.ChannelRuntime {
	return e.snapshot(baseURL, "text", modelcfg.Capabilities{}).Runtime()
}

// promptCaps 是只有提示词的最小能力（文本、语音、视频/图片的查询测试用）。
var promptCaps = modelcfg.Capabilities{Prompt: modelcfg.PromptSpec{MaxLength: 10000}}

func newapiIntP(v int) *int { return &v }

// fakeNewAPI 模拟 New API 网关的公开接口，记录每次请求。
type fakeNewAPI struct {
	mu       sync.Mutex
	requests []recordedRequest
	handler  func(w http.ResponseWriter, r *http.Request, body map[string]any)
}

type recordedRequest struct {
	Method, Path, Auth string
	Body               map[string]any
}

func (f *fakeNewAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.requests = append(f.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Body: body})
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	f.handler(w, r, body)
}

func (f *fakeNewAPI) last() recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func TestNewAPIPrecheckAndMeta(t *testing.T) {
	env := newNewAPIEnv(t, nil)
	if env.meta.Key != "newapi" || env.meta.Auth.Type != pluginmeta.AuthBearer {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("text"); !ok || ep.Mode != pluginmeta.ModeSync {
		t.Fatalf("text 应是 sync：%+v", ep)
	}
	if ep, ok := env.meta.Endpoint("video"); !ok || ep.Mode != pluginmeta.ModeAsync {
		t.Fatalf("video 应是 async：%+v", ep)
	}
	for _, kind := range []string{"image", "audio"} {
		if ep, ok := env.meta.Endpoint(kind); !ok || ep.Mode != pluginmeta.ModeSync {
			t.Fatalf("%s 应是 sync：%+v", kind, ep)
		}
	}
}

// 文本同步：POST /v1/chat/completions，Bearer 由宿主注入，正文从 choices[0].message.content 取。
func TestNewAPITextSync(t *testing.T) {
	up := &fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"你好，世界"}}],"usage":{"prompt_tokens":12,"completion_tokens":34}}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()

	env := newNewAPIEnv(t, nil)
	snap := env.snapshot(srv.URL, "text", promptCaps)
	snap.Model.Params = map[string]any{"system": "你是助手", "max_tokens": float64(256)}
	res, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "打个招呼"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Immediate == nil || res.Immediate.Status != provider.StatusSucceeded ||
		len(res.Immediate.Outputs) != 1 || res.Immediate.Outputs[0].Text != "你好，世界" {
		t.Fatalf("文本结果不符：%+v", res)
	}
	if u := res.Immediate.Outputs[0].Usage; u == nil || u.InputTokens != 12 || u.OutputTokens != 34 {
		t.Fatalf("应回传 Token 用量：%+v", u)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/v1/chat/completions" || got.Auth != "Bearer sk-newapi" {
		t.Fatalf("上游请求不符：%+v", got)
	}
	if got.Body["model"] != "up-model" || got.Body["max_tokens"] != float64(256) || got.Body["stream"] != false {
		t.Fatalf("请求体不符：%v", got.Body)
	}
	msgs, _ := got.Body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("应有 system + user 两条消息：%v", got.Body["messages"])
	}
	if user := msgs[1].(map[string]any); user["role"] != "user" || user["content"] != "打个招呼" {
		t.Fatalf("user 消息不符：%v", msgs[1])
	}
}

// 文本：上游 200 但返回 error 体，插件把它翻译成 failed 结果（不是宿主异常）。
func TestNewAPITextUpstreamErrorBody(t *testing.T) {
	srv := httptest.NewServer(&fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"error":{"message":"content policy violated"}}`))
	}})
	defer srv.Close()
	env := newNewAPIEnv(t, nil)
	res, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "text", promptCaps), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Immediate == nil || res.Immediate.Status != provider.StatusFailed || res.Immediate.ErrorClass != provider.ClassModeration {
		t.Fatalf("应翻译成 moderation 失败：%+v", res.Immediate)
	}
}

// 视频异步：提交拿 task_id，轮询经过 排队 → 进行中（带进度）→ 完成（带地址）。
func TestNewAPIVideoSubmitAndQuery(t *testing.T) {
	var polls int
	var srv *httptest.Server
	up := &fakeNewAPI{}
	up.handler = func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/video/generations":
			_, _ = w.Write([]byte(`{"task_id":"vid-42","status":"queued"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/video/generations/vid-42":
			polls++
			switch polls {
			case 1:
				_, _ = w.Write([]byte(`{"status":"queued"}`))
			case 2:
				_, _ = w.Write([]byte(`{"status":"in_progress","progress":"35%"}`))
			default:
				_, _ = w.Write([]byte(`{"status":"completed","url":"` + srv.URL + `/files/out.mp4"}`))
			}
		default:
			http.NotFound(w, r)
		}
	}
	srv = httptest.NewServer(up)
	defer srv.Close()

	env := newNewAPIEnv(t, nil)
	caps := modelcfg.Capabilities{
		Ops:    []string{modelcfg.OpT2V},
		Prompt: modelcfg.PromptSpec{MaxLength: 10000},
		Params: modelcfg.ParamSet{{Name: "duration", ParamField: modelcfg.ParamField{
			Type: modelcfg.ParamNumber, Label: "时长", Open: true, Min: newapiIntP(1), Max: newapiIntP(60), Default: 5.0}}},
	}
	snap := env.snapshot(srv.URL, "video", caps)
	snap.Model.Params = map[string]any{"metadata": map[string]any{"quality": "high"}}
	sub, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 3, UserID: 9}, Input: map[string]any{"prompt": "猫在跳舞", "duration": float64(5)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.ProviderTaskID != "vid-42" || sub.Immediate != nil {
		t.Fatalf("提交结果不符：%+v", sub)
	}
	first := up.requests[0]
	if first.Auth != "Bearer sk-newapi" || first.Body["prompt"] != "猫在跳舞" || first.Body["duration"] != float64(5) || first.Body["model"] != "up-model" {
		t.Fatalf("提交请求不符：%+v", first)
	}
	if md, _ := first.Body["metadata"].(map[string]any); md["quality"] != "high" {
		t.Fatalf("params.metadata 应放进请求体：%v", first.Body)
	}

	ref := provider.TaskRef{ID: 3, UserID: 9, ProviderTaskID: sub.ProviderTaskID}
	q1, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q1.Status != provider.StatusQueued {
		t.Fatalf("第一次查询应是排队：%+v %v", q1, err)
	}
	q2, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q2.Status != provider.StatusRunning || q2.Progress == nil || *q2.Progress != 35 {
		t.Fatalf("第二次查询应是进行中 35%%：%+v %v", q2, err)
	}
	q3, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q3.Status != provider.StatusSucceeded || len(q3.Outputs) != 1 {
		t.Fatalf("第三次查询应成功：%+v %v", q3, err)
	}
	out := q3.Outputs[0]
	if out.Type != provider.OutputURL || out.URL != srv.URL+"/files/out.mp4" || out.MediaType != "video" || out.Mime != "video/mp4" {
		t.Fatalf("产物不符：%+v", out)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/v1/video/generations/vid-42" || got.Auth != "Bearer sk-newapi" {
		t.Fatalf("轮询请求不符：%+v", got)
	}
}

// 视频失败：上游报 failed 与原因，插件翻译成 failed 结果并按文案分类（审核 / 余额 / 其它）。
func TestNewAPIVideoQueryFailed(t *testing.T) {
	cases := []struct {
		body  string
		class provider.ErrorClass
	}{
		{`{"status":"failed","fail_reason":"nsfw content detected"}`, provider.ClassModeration},
		{`{"status":"FAILURE","error":{"message":"insufficient quota"}}`, provider.ClassProviderBalance},
		{`{"status":"failed","fail_reason":"internal renderer crashed"}`, provider.ClassTerminal},
	}
	for _, tc := range cases {
		t.Run(tc.body, func(t *testing.T) {
			srv := httptest.NewServer(&fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				_, _ = w.Write([]byte(tc.body))
			}})
			defer srv.Close()
			env := newNewAPIEnv(t, nil)
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, "video", promptCaps), provider.TaskRef{ID: 1, ProviderTaskID: "x"})
			if err != nil {
				t.Fatal(err)
			}
			if q.Status != provider.StatusFailed || q.ErrorClass != tc.class || q.ErrorMessage == "" {
				t.Fatalf("失败结果不符：%+v", q)
			}
		})
	}
}

// 视频：成功但没有地址，视为失败；提交响应里没有 task_id 且没有错误信息，是插件级失败。
func TestNewAPIVideoEdgeResponses(t *testing.T) {
	env := newNewAPIEnv(t, nil)
	srv := httptest.NewServer(&fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"status":"completed"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{}}`))
	}})
	defer srv.Close()
	snap := env.snapshot(srv.URL, "video", promptCaps)

	q, err := env.exec.Query(context.Background(), snap, provider.TaskRef{ID: 1, ProviderTaskID: "x"})
	if err != nil || q.Status != provider.StatusFailed {
		t.Fatalf("成功但没有地址应判失败：%+v %v", q, err)
	}
	_, err = env.exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("没有 task_id 应是插件级失败：%v", err)
	}
}

// HTTP 非 2xx：插件的 classifyError 按文案分类；没有特殊文案交给宿主默认规则（429 / 5xx 重试，其它 terminal）。
func TestNewAPIClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"余额不足", 402, `{"error":{"message":"insufficient balance"}}`, provider.ClassProviderBalance},
		{"审核拒绝", 400, `{"error":{"message":"sensitive words in prompt"}}`, provider.ClassModeration},
		{"限流重试", 429, `{"error":{"message":"too many requests"}}`, provider.ClassRetryable},
		{"上游 5xx 重试", 503, `bad gateway`, provider.ClassRetryable},
		{"其它 4xx 终止", 400, `{"error":{"message":"bad parameter"}}`, provider.ClassTerminal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(&fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}})
			defer srv.Close()
			env := newNewAPIEnv(t, nil)
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "text", promptCaps), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"},
			})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
		})
	}
}

// 连通性检查：GET /v1/models，成功 OK；Key 无效（401）返回失败。
func TestNewAPICheck(t *testing.T) {
	env := newNewAPIEnv(t, nil)
	good := httptest.NewServer(&fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}})
	defer good.Close()
	res, err := env.exec.Check(context.Background(), env.runtime(good.URL))
	if err != nil || res == nil || !res.OK {
		t.Fatalf("检查应成功：%+v %v", res, err)
	}

	bad := &fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid token"}}`))
	}}
	badSrv := httptest.NewServer(bad)
	defer badSrv.Close()
	res, err = env.exec.Check(context.Background(), env.runtime(badSrv.URL))
	if err == nil || (res != nil && res.OK) {
		t.Fatalf("401 应检查失败：%+v %v", res, err)
	}
	if got := bad.last(); got.Path != "/v1/models" || got.Auth != "Bearer sk-newapi" {
		t.Fatalf("检查请求不符：%+v", got)
	}
}

// 保存前检查：Key 用调用方给的草稿值（不是已保存的 sk-newapi），结果里也不会带出草稿 Key。
func TestNewAPICheckDraft(t *testing.T) {
	env := newNewAPIEnv(t, nil)
	srv := &fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	res, err := env.exec.CheckDraft(context.Background(), env.runtime(ts.URL), "sk-draft")
	if err != nil || res == nil || !res.OK {
		t.Fatalf("检查应成功：%+v %v", res, err)
	}
	if got := srv.last(); got.Path != "/v1/models" || got.Auth != "Bearer sk-draft" {
		t.Fatalf("应使用草稿 Key 发请求：%+v", got)
	}

	bad := httptest.NewServer(&fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid token sk-draft"}}`))
	}})
	defer bad.Close()
	res, err = env.exec.CheckDraft(context.Background(), env.runtime(bad.URL), "sk-draft")
	if err == nil || res == nil || res.OK || strings.Contains(res.Message, "sk-draft") {
		t.Fatalf("401 应失败，且原因里不含草稿 Key：%+v %v", res, err)
	}
}

// 导入：GET /v1/models，按模型名推断 text / video / image / audio，其余（向量、转写）跳过；草稿的上游模型名与输入表单齐全。
func TestNewAPIImport(t *testing.T) {
	srv := httptest.NewServer(&fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"object":"list","data":[
			{"id":"gpt-4o"},{"id":"kling-v2-master"},{"id":"text-embedding-3-small"},{"id":"dall-e-3"},{"id":"tts-1"},{"id":"whisper-1"},{"id":""},{"object":"model"}
		]}`))
	}})
	defer srv.Close()
	env := newNewAPIEnv(t, nil)
	drafts, err := env.exec.Import(context.Background(), env.runtime(srv.URL), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 4 {
		t.Fatalf("应只导入文本、视频、图片、语音四个模型：%+v", drafts)
	}
	byModel := map[string]provider.ModelDraft{}
	for _, d := range drafts {
		byModel[d.UpstreamModel] = d
	}
	text, video := byModel["gpt-4o"], byModel["kling-v2-master"]
	if byModel["dall-e-3"].Kind != "image" || byModel["tts-1"].Kind != "audio" {
		t.Fatalf("dall-e-3 应是 image、tts-1 应是 audio：%+v", byModel)
	}
	if text.Kind != "text" || text.Label != "gpt-4o" {
		t.Fatalf("文本草稿不符：%+v", text)
	}
	if video.Kind != "video" {
		t.Fatalf("视频草稿不符：%+v", video)
	}
}

// 图片（无参考图）：POST /v1/images/generations，固定参数 extra 只补充、不覆盖用户输入；data[].url 变成 image 产物。
func TestNewAPIImageGenerate(t *testing.T) {
	var srv *httptest.Server
	up := &fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"data":[{"url":"` + srv.URL + `/a.jpg"},{"url":"` + srv.URL + `/b.webp?x=1"}]}`))
	}}
	srv = httptest.NewServer(up)
	defer srv.Close()

	env := newNewAPIEnv(t, nil)
	caps := modelcfg.Capabilities{
		Ops:    []string{modelcfg.OpT2I},
		Prompt: modelcfg.PromptSpec{MaxLength: 10000},
		Params: modelcfg.ParamSet{{Name: "size", ParamField: modelcfg.ParamField{
			Type: modelcfg.ParamEnum, Label: "尺寸", Open: true, Options: []any{"1024x1024", "512x512"}, Default: "512x512"}}},
	}
	snap := env.snapshot(srv.URL, "image", caps)
	snap.Model.Params = map[string]any{"extra": map[string]any{"size": "512x512", "background": "transparent"}}
	sub, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 5, UserID: 9}, Input: map[string]any{"prompt": "一只猫", "size": "1024x1024"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Immediate == nil || sub.Immediate.Status != provider.StatusSucceeded || len(sub.Immediate.Outputs) != 2 {
		t.Fatalf("图片应同步成功且有两个产物：%+v", sub)
	}
	first, second := sub.Immediate.Outputs[0], sub.Immediate.Outputs[1]
	if first.Type != provider.OutputURL || first.MediaType != "image" || first.Mime != "image/jpeg" || second.Mime != "image/webp" {
		t.Fatalf("图片产物不符：%+v %+v", first, second)
	}
	got := up.last()
	if got.Path != "/v1/images/generations" || got.Auth != "Bearer sk-newapi" || got.Body["model"] != "up-model" ||
		got.Body["prompt"] != "一只猫" || got.Body["size"] != "1024x1024" || got.Body["background"] != "transparent" {
		t.Fatalf("图片请求不符：%+v", got)
	}
}

// 图片：上游只给 base64 时明确失败并提示怎么改；错误体按文案分类。
func TestNewAPIImageBadResponses(t *testing.T) {
	cases := []struct {
		name, body, wantClass, wantMsg string
	}{
		{"只有 base64", `{"data":[{"b64_json":"AAAA"}]}`, "terminal", "base64"},
		{"没有数据", `{"data":[]}`, "terminal", "没有返回图片"},
		{"额度不足", `{"error":{"message":"insufficient quota"}}`, "provider_balance", "insufficient"},
	}
	env := newNewAPIEnv(t, nil)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(&fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				_, _ = w.Write([]byte(c.body))
			}})
			defer srv.Close()
			sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "image", promptCaps), provider.SubmitInput{
				Task: provider.TaskRef{ID: 6, UserID: 9}, Input: map[string]any{"prompt": "x"},
			})
			if err != nil {
				t.Fatal(err)
			}
			r := sub.Immediate
			if r == nil || r.Status != provider.StatusFailed || string(r.ErrorClass) != c.wantClass || !strings.Contains(r.ErrorMessage, c.wantMsg) {
				t.Fatalf("结果不符：%+v", r)
			}
		})
	}
}

// fakeSaver 模拟宿主的素材存储：读完 binary 响应体并记录，返回固定的素材。
type fakeSaver struct {
	got  provider.SaveGeneratedInput
	body string
}

func (f *fakeSaver) SaveGenerated(_ context.Context, in provider.SaveGeneratedInput) (*model.Asset, string, error) {
	raw, _ := io.ReadAll(in.Body)
	f.got, f.body = in, string(raw)
	return &model.Asset{ID: 77, MimeType: "audio/mpeg", ByteSize: int64(len(raw))}, "https://files.local/77.mp3", nil
}

// 语音：POST /v1/audio/speech，音色、格式、语速取模型固定参数；响应是二进制，由宿主落库，插件返回 asset 产物。
func TestNewAPIAudioSpeech(t *testing.T) {
	up := &fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3fake"))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()

	saver := &fakeSaver{}
	env := newNewAPIEnv(t, func(o *plugin.Options) { o.Saver = saver })
	snap := env.snapshot(srv.URL, "audio", promptCaps)
	snap.Model.Params = map[string]any{"voice": "nova", "format": "mp3", "speed": float64(1.2)}
	sub, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 7, UserID: 9}, Input: map[string]any{"prompt": "你好，世界"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := up.last()
	if got.Path != "/v1/audio/speech" || got.Auth != "Bearer sk-newapi" || got.Body["input"] != "你好，世界" ||
		got.Body["voice"] != "nova" || got.Body["response_format"] != "mp3" || got.Body["speed"] != 1.2 || got.Body["model"] != "up-model" {
		t.Fatalf("语音请求不符：%+v", got)
	}
	if saver.body != "ID3fake" || saver.got.Kind != "audio" || saver.got.UserID != 9 {
		t.Fatalf("二进制响应应按 audio 落库：%+v %q", saver.got, saver.body)
	}
	if sub.Immediate == nil || sub.Immediate.Status != provider.StatusSucceeded || len(sub.Immediate.Outputs) != 1 {
		t.Fatalf("语音应同步成功：%+v", sub)
	}
	out := sub.Immediate.Outputs[0]
	if out.Type != provider.OutputAsset || out.AssetID != 77 || out.MediaType != "audio" || out.Mime != "audio/mpeg" {
		t.Fatalf("语音产物不符：%+v", out)
	}
}
