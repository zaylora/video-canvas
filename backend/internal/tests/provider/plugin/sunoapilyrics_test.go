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

// sunoapilyricsEnv 用真实 runner 预检并装载内置 sunoapi-lyrics.js，元数据取自预检结果。
type sunoapilyricsEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newSunoapilyricsEnv(t *testing.T) *sunoapilyricsEnv {
	t.Helper()
	src, err := plugins.Source("sunoapi-lyrics")
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
		t.Fatalf("内置 sunoapi-lyrics.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	exec := plugin.New(plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "sk-suno"}, IPAllowed: allowLoopback})
	return &sunoapilyricsEnv{exec: exec, meta: meta, sha: pre.SHA256}
}

func (e *sunoapilyricsEnv) snapshot(baseURL, kind, upstream string) *provider.Snapshot {
	return &provider.Snapshot{
		Model:   provider.ModelSnapshot{Key: "m", Kind: kind, UpstreamModel: upstream, Capabilities: promptCaps},
		Channel: provider.ChannelSnapshot{Key: "suno-main", PluginKey: "sunoapi-lyrics", PluginVersionID: 1, BaseURL: baseURL},
		Plugin:  provider.PluginSnapshot{Key: "sunoapi-lyrics", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},

		ModelRevisionID: 1,
	}
}

type sunoapilyricsUpstream struct {
	mu      sync.Mutex
	reqs    []sunoapilyricsReq
	handler func(w http.ResponseWriter, r *http.Request)
}

type sunoapilyricsReq struct {
	Method, Path, Auth string
	Body               map[string]any
}

func (u *sunoapilyricsUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	u.mu.Lock()
	u.reqs = append(u.reqs, sunoapilyricsReq{r.Method, r.URL.Path, r.Header.Get("Authorization"), body})
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	u.handler(w, r)
}

func (u *sunoapilyricsUpstream) last() sunoapilyricsReq {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.reqs[len(u.reqs)-1]
}

func TestSunoapilyricsPrecheckAndMeta(t *testing.T) {
	env := newSunoapilyricsEnv(t)
	if env.meta.Key != "sunoapi-lyrics" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthBearer {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	for _, kind := range []string{"text"} {
		if ep, ok := env.meta.Endpoint(kind); !ok || ep.Mode != pluginmeta.ModeAsync {
			t.Fatalf("%s 应是 async：%+v", kind, ep)
		}
	}
}

// 歌词：text 模型走 /suno/submit/LYRICS，prompt 是主题；成功后合成一段文本（标题 + 歌词）。
func TestSunoapilyricsLyricsFlow(t *testing.T) {
	up := &sunoapilyricsUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/suno/submit/LYRICS" {
			_, _ = w.Write([]byte(`{"code":"success","data":"lyr-1"}`))
			return
		}
		// 网关转存形态：data 再包一层 {task_id, data}
		_, _ = w.Write([]byte(`{"code":"success","data":[{"task_id":"lyr-1","status":"SUCCESS","data":{"task_id":"lyr-1","data":{"id":"l1","title":"夜航","text":"[Verse]\n星光落在海面"}}}]}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newSunoapilyricsEnv(t)
	snap := env.snapshot(srv.URL, "text", "suno_lyrics")
	sub, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "夜晚的航行"}})
	if err != nil || sub.ProviderTaskID != "lyr-1" {
		t.Fatalf("提交不符：%+v %v", sub, err)
	}
	if b := up.last().Body; b["prompt"] != "夜晚的航行" || up.last().Path != "/suno/submit/LYRICS" {
		t.Fatalf("歌词提交请求不符：%+v", up.last())
	}
	q, err := env.exec.Query(context.Background(), snap, provider.TaskRef{ID: 1, ProviderTaskID: "lyr-1"})
	if err != nil || q.Status != provider.StatusSucceeded || len(q.Outputs) != 1 {
		t.Fatalf("应成功：%+v %v", q, err)
	}
	if o := q.Outputs[0]; o.Type != provider.OutputText || o.Text != "夜航\n[Verse]\n星光落在海面" {
		t.Fatalf("歌词产物不符：%+v", o)
	}
}

// 失败与坏响应：FAILURE 带原因并分类；成功但没有歌词内容；code 非 success；查不到任务继续等待；提交没有 task_id 是插件级失败。
func TestSunoapilyricsQueryEdges(t *testing.T) {
	cases := []struct {
		name, body string
		status     string
		class      string
	}{
		{"审核失败", `{"code":"success","data":[{"task_id":"t","status":"FAILURE","fail_reason":"sensitive word"}]}`, provider.StatusFailed, "moderation"},
		{"余额失败", `{"code":"success","data":[{"task_id":"t","status":"FAILURE","fail_reason":"insufficient quota"}]}`, provider.StatusFailed, "provider_balance"},
		{"普通失败", `{"code":"success","data":[{"task_id":"t","status":"FAILURE"}]}`, provider.StatusFailed, "terminal"},
		{"没有地址", `{"code":"success","data":[{"task_id":"t","status":"SUCCESS","data":[]}]}`, provider.StatusFailed, "terminal"},
		{"code 失败", `{"code":"fail","message":"bad"}`, provider.StatusFailed, "terminal"},
		{"查不到", `{"code":"success","data":[]}`, provider.StatusQueued, ""},
	}
	env := newSunoapilyricsEnv(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(&sunoapilyricsUpstream{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(c.body)) }})
			defer srv.Close()
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, "text", "x"), provider.TaskRef{ID: 1, ProviderTaskID: "t"})
			if err != nil || q.Status != c.status || string(q.ErrorClass) != c.class {
				t.Fatalf("结果不符：%+v %v", q, err)
			}
		})
	}

	srv := httptest.NewServer(&sunoapilyricsUpstream{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"code":"success"}`)) }})
	defer srv.Close()
	_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "text", "x"), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("没有 task_id 应是插件级失败：%v", err)
	}

	srv2 := httptest.NewServer(&sunoapilyricsUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"fail","message":"content policy violated"}`))
	}})
	defer srv2.Close()
	sub, err := env.exec.Submit(context.Background(), env.snapshot(srv2.URL, "text", "x"), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}})
	if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusFailed || sub.Immediate.ErrorClass != provider.ClassModeration {
		t.Fatalf("提交被拒应翻译成 moderation 失败：%+v %v", sub, err)
	}
}

func TestSunoapilyricsClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"余额不足", 402, `{"message":"insufficient balance"}`, provider.ClassProviderBalance},
		{"审核拒绝", 400, `{"error":{"message":"sensitive words"}}`, provider.ClassModeration},
		{"限流", 429, `{"message":"too many requests"}`, provider.ClassRetryable},
		{"5xx", 502, `bad gateway`, provider.ClassRetryable},
		{"其它 4xx", 400, `{"message":"bad parameter"}`, provider.ClassTerminal},
	}
	env := newSunoapilyricsEnv(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(&sunoapilyricsUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}})
			defer srv.Close()
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "text", "x"), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": strings.Repeat("a", 3)}})
			if err == nil || provider.ClassOf(err) != c.class {
				t.Fatalf("分类应是 %s：%v", c.class, err)
			}
		})
	}
}
