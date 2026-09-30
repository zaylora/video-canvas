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

func (e *newapiEnv) snapshot(baseURL, kind string, schema modelcfg.InputSchema) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: kind, UpstreamModel: "up-model", InputSchema: schema},
		Channel: provider.ChannelSnapshot{
			Key: "newapi-main", PluginKey: "newapi", PluginVersionID: 1, BaseURL: baseURL, TrustedInternal: true,
		},
		Plugin:          provider.PluginSnapshot{Key: "newapi", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

func (e *newapiEnv) runtime(baseURL string) *provider.ChannelRuntime {
	return e.snapshot(baseURL, "text", nil).Runtime()
}

var promptSchema = modelcfg.InputSchema{
	{Name: "prompt", InputField: modelcfg.InputField{Type: modelcfg.FieldText, Label: "提示词", Required: true}},
}

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
}

// 文本同步：POST /v1/chat/completions，Bearer 由宿主注入，正文从 choices[0].message.content 取。
func TestNewAPITextSync(t *testing.T) {
	up := &fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"你好，世界"}}]}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()

	env := newNewAPIEnv(t, nil)
	snap := env.snapshot(srv.URL, "text", promptSchema)
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
	res, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "text", promptSchema), provider.SubmitInput{
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
	schema := modelcfg.InputSchema{
		{Name: "prompt", InputField: modelcfg.InputField{Type: modelcfg.FieldText, Label: "提示词", Required: true}},
		{Name: "duration", InputField: modelcfg.InputField{Type: modelcfg.FieldNumber, Label: "时长"}},
	}
	snap := env.snapshot(srv.URL, "video", schema)
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
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, "video", promptSchema), provider.TaskRef{ID: 1, ProviderTaskID: "x"})
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
	snap := env.snapshot(srv.URL, "video", promptSchema)

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
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "text", promptSchema), provider.SubmitInput{
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

// 导入：GET /v1/models，按模型名推断 text / video，其余（向量、图片、语音）跳过；草稿的上游模型名与输入表单齐全。
func TestNewAPIImport(t *testing.T) {
	srv := httptest.NewServer(&fakeNewAPI{handler: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		_, _ = w.Write([]byte(`{"object":"list","data":[
			{"id":"gpt-4o"},{"id":"kling-v2-master"},{"id":"text-embedding-3-small"},{"id":"dall-e-3"},{"id":""},{"object":"model"}
		]}`))
	}})
	defer srv.Close()
	env := newNewAPIEnv(t, nil)
	drafts, err := env.exec.Import(context.Background(), env.runtime(srv.URL), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 2 {
		t.Fatalf("应只导入文本与视频两个模型：%+v", drafts)
	}
	byModel := map[string]provider.ModelDraft{}
	for _, d := range drafts {
		byModel[d.UpstreamModel] = d
	}
	text, video := byModel["gpt-4o"], byModel["kling-v2-master"]
	if text.Kind != "text" || text.Label != "gpt-4o" {
		t.Fatalf("文本草稿不符：%+v", text)
	}
	if _, ok := text.InputSchema.Get("prompt"); !ok {
		t.Fatalf("文本草稿应有 prompt 字段：%+v", text.InputSchema)
	}
	if video.Kind != "video" {
		t.Fatalf("视频草稿不符：%+v", video)
	}
	// 输入表单键序按插件书写顺序保留
	var names []string
	for _, e := range video.InputSchema {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "prompt,image,duration" {
		t.Fatalf("视频草稿字段顺序应为 prompt,image,duration：%v", names)
	}
}
