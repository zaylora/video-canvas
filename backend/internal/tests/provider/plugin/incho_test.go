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
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/plugins"
)

// inchoEnv 用真实 runner 预检并装载内置 incho.js，元数据取自预检结果。
type inchoEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newInchoEnv(t *testing.T) *inchoEnv {
	t.Helper()
	src, err := plugins.Source("incho")
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
		t.Fatalf("内置 incho.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	exec := plugin.New(plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "sk-incho"}, IPAllowed: allowLoopback})
	return &inchoEnv{exec: exec, meta: meta, sha: pre.SHA256}
}

func (e *inchoEnv) snapshot(baseURL, upstream string) *provider.Snapshot {
	return &provider.Snapshot{
		Model:   provider.ModelSnapshot{Key: "m", Kind: "audio", UpstreamModel: upstream, Capabilities: promptCaps},
		Channel: provider.ChannelSnapshot{Key: "incho-main", PluginKey: "incho", PluginVersionID: 1, BaseURL: baseURL},
		Plugin:  provider.PluginSnapshot{Key: "incho", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},

		ModelRevisionID: 1,
	}
}

type inchoUpstream struct {
	mu      sync.Mutex
	reqs    []inchoReq
	handler func(w http.ResponseWriter, r *http.Request)
}

type inchoReq struct {
	Method, Path, RawQuery, Auth, Source string
	Body                                 map[string]any
}

func (u *inchoUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	u.mu.Lock()
	u.reqs = append(u.reqs, inchoReq{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("X-Incho-Source"), body})
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	u.handler(w, r)
}

func (u *inchoUpstream) last() inchoReq {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.reqs[len(u.reqs)-1]
}

func TestInchoPrecheckAndMeta(t *testing.T) {
	env := newInchoEnv(t)
	if env.meta.Key != "incho" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthBearer {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("audio"); !ok || ep.Mode != pluginmeta.ModeAsync {
		t.Fatalf("audio 应是 async：%+v", ep)
	}
	if _, ok := env.meta.Endpoint("text"); ok {
		t.Fatal("不应声明 text")
	}
}

// 提交：POST /api/v1/song/generate，Bearer 由宿主注入；incho_music 映射到 v4.0；n 来自输入。
func TestInchoSubmit(t *testing.T) {
	up := &inchoUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"task-uuid-1","task_type":"normal","choices":[],"create_at":1}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newInchoEnv(t)

	sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "incho_music"), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "夏天的海边", "n": float64(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.ProviderTaskID != "task-uuid-1" || sub.Immediate != nil {
		t.Fatalf("提交结果不符：%+v", sub)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/api/v1/song/generate" || got.Auth != "Bearer sk-incho" || got.Source != "plugins" {
		t.Fatalf("上游请求不符：%+v", got)
	}
	if got.Body["model"] != "v4.0" || got.Body["task_type"] != "normal" || got.Body["prompt"] != "夏天的海边" || got.Body["n"] != float64(1) {
		t.Fatalf("请求体不符：%v", got.Body)
	}

	// 显式的上游模型名保留；没有 n 时不发 n
	if _, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "v3.5"), provider.SubmitInput{
		Task: provider.TaskRef{ID: 2, UserID: 9}, Input: map[string]any{"prompt": "x"},
	}); err != nil {
		t.Fatal(err)
	}
	got = up.last()
	if _, has := got.Body["n"]; has || got.Body["model"] != "v3.5" {
		t.Fatalf("v3.5 请求体不符：%v", got.Body)
	}
}

// 输入越界（超长提示词、n 不是 1/2）是配置/输入问题，属于插件级失败，不会发出请求。
func TestInchoSubmitRejectsBadInput(t *testing.T) {
	up := &inchoUpstream{handler: func(w http.ResponseWriter, r *http.Request) { t.Error("不应发请求") }}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newInchoEnv(t)
	for name, in := range map[string]map[string]any{
		"超长":  {"prompt": strings.Repeat("歌", 1001)},
		"n=3": {"prompt": "x", "n": float64(3)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "v4.0"), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: in})
			var pe *provider.Error
			if err == nil || !errors.As(err, &pe) || !pe.PluginFault {
				t.Fatalf("应是插件级失败：%v", err)
			}
		})
	}
}

// 提交：200 但带 detail 错误，翻译成 failed 结果并分类；没有 id 是插件级失败。
func TestInchoSubmitErrorBody(t *testing.T) {
	cases := []struct{ body, class string }{
		{`{"detail":"insufficient balance"}`, "provider_balance"},
		{`{"detail":[{"msg":"prompt too sensitive"}]}`, "moderation"},
		{`{"detail":"bad request"}`, "terminal"},
	}
	env := newInchoEnv(t)
	for _, c := range cases {
		t.Run(c.body, func(t *testing.T) {
			srv := httptest.NewServer(&inchoUpstream{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(c.body)) }})
			defer srv.Close()
			sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "v4.0"), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}})
			if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusFailed || string(sub.Immediate.ErrorClass) != c.class {
				t.Fatalf("结果不符：%+v %v", sub, err)
			}
		})
	}
	srv := httptest.NewServer(&inchoUpstream{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }})
	defer srv.Close()
	_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "v4.0"), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}})
	var pe *provider.Error
	if err == nil || !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("没有 id 应是插件级失败：%v", err)
	}
}

// 查询：空 choices 排队 → 一首在跑 → 部分完成（进度）→ 全部终态且有 done 时成功，只含 done 的歌。
func TestInchoQueryProgression(t *testing.T) {
	bodies := []string{
		`{"id":"t1","choices":[]}`,
		`{"id":"t1","choices":[{"id":"a","status":"pending"},{"id":"b","status":"running"}]}`,
		`{"id":"t1","choices":[{"id":"a","status":"done","audio_url":"https://cdn.example.com/a.mp3"},{"id":"b","status":"stream"}]}`,
		`{"id":"t1","choices":[{"id":"a","status":"done","audio_url":"https://cdn.example.com/a.mp3"},{"id":"b","status":"fail","error":"boom","error_code":"E1"}]}`,
	}
	var i int
	up := &inchoUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodies[i]))
		i++
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newInchoEnv(t)
	snap := env.snapshot(srv.URL, "v4.0")
	ref := provider.TaskRef{ID: 1, UserID: 9, ProviderTaskID: "t1"}

	q, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q.Status != provider.StatusQueued {
		t.Fatalf("空 choices 应排队：%+v %v", q, err)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/api/v1/task/query" || got.RawQuery != "task_id=t1" || got.Auth != "Bearer sk-incho" {
		t.Fatalf("查询请求不符：%+v", got)
	}
	q, _ = env.exec.Query(context.Background(), snap, ref)
	if q.Status != provider.StatusRunning {
		t.Fatalf("应是进行中：%+v", q)
	}
	q, _ = env.exec.Query(context.Background(), snap, ref)
	if q.Status != provider.StatusRunning || q.Progress == nil || *q.Progress != 50 {
		t.Fatalf("一首完成应进行中 50%%：%+v", q)
	}
	q, err = env.exec.Query(context.Background(), snap, ref)
	if err != nil || q.Status != provider.StatusSucceeded || len(q.Outputs) != 1 {
		t.Fatalf("部分失败也应成功且只含 done 的歌：%+v %v", q, err)
	}
	out := q.Outputs[0]
	if out.Type != provider.OutputURL || out.URL != "https://cdn.example.com/a.mp3" || out.MediaType != "audio" || out.Mime != "audio/mpeg" {
		t.Fatalf("产物不符：%+v", out)
	}
}

// 查询：全部失败 / 取消、成功但没有地址、detail 错误，都变成 failed，并按文案分类。
func TestInchoQueryFailures(t *testing.T) {
	cases := []struct {
		name, body, class, msg string
	}{
		{"全部失败", `{"id":"t","choices":[{"status":"fail","error":"insufficient credit","error_code":"E9"},{"status":"cancelled"}]}`, "provider_balance", "E9"},
		{"审核", `{"id":"t","choices":[{"status":"fail","error":"content policy violated"}]}`, "moderation", "policy"},
		{"没有地址", `{"id":"t","choices":[{"status":"done"}]}`, "terminal", "没有音频地址"},
		{"detail", `{"detail":"task not found"}`, "terminal", "not found"},
	}
	env := newInchoEnv(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(&inchoUpstream{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(c.body)) }})
			defer srv.Close()
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, "v4.0"), provider.TaskRef{ID: 1, ProviderTaskID: "t"})
			if err != nil || q.Status != provider.StatusFailed || string(q.ErrorClass) != c.class || !strings.Contains(q.ErrorMessage, c.msg) {
				t.Fatalf("结果不符：%+v %v", q, err)
			}
		})
	}
}

// HTTP 非 2xx：余额 / 审核按文案分类，429 / 5xx 重试，其它 4xx 终止。
func TestInchoClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"余额不足", 402, `{"detail":"insufficient balance"}`, provider.ClassProviderBalance},
		{"审核拒绝", 400, `{"detail":"sensitive content"}`, provider.ClassModeration},
		{"限流", 429, `{"detail":"too many requests"}`, provider.ClassRetryable},
		{"5xx", 503, `bad gateway`, provider.ClassRetryable},
		{"其它 4xx", 400, `{"detail":"bad parameter"}`, provider.ClassTerminal},
	}
	env := newInchoEnv(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(&inchoUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}})
			defer srv.Close()
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "v4.0"), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}})
			if err == nil || provider.ClassOf(err) != c.class {
				t.Fatalf("分类应是 %s：%v", c.class, err)
			}
		})
	}
}
