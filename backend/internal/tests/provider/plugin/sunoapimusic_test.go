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

// sunoapimusicEnv 用真实 runner 预检并装载内置 sunoapi-music.js，元数据取自预检结果。
type sunoapimusicEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newSunoapimusicEnv(t *testing.T) *sunoapimusicEnv {
	t.Helper()
	src, err := plugins.Source("sunoapi-music")
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
		t.Fatalf("内置 sunoapi-music.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	exec := plugin.New(plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "sk-suno"}, IPAllowed: allowLoopback})
	return &sunoapimusicEnv{exec: exec, meta: meta, sha: pre.SHA256}
}

func (e *sunoapimusicEnv) snapshot(baseURL, kind, upstream string) *provider.Snapshot {
	return &provider.Snapshot{
		Model:   provider.ModelSnapshot{Key: "m", Kind: kind, UpstreamModel: upstream, Capabilities: promptCaps},
		Channel: provider.ChannelSnapshot{Key: "suno-main", PluginKey: "sunoapi-music", PluginVersionID: 1, BaseURL: baseURL},
		Plugin:  provider.PluginSnapshot{Key: "sunoapi-music", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},

		ModelRevisionID: 1,
	}
}

type sunoapimusicUpstream struct {
	mu      sync.Mutex
	reqs    []sunoapimusicReq
	handler func(w http.ResponseWriter, r *http.Request)
}

type sunoapimusicReq struct {
	Method, Path, Auth string
	Body               map[string]any
}

func (u *sunoapimusicUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	u.mu.Lock()
	u.reqs = append(u.reqs, sunoapimusicReq{r.Method, r.URL.Path, r.Header.Get("Authorization"), body})
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	u.handler(w, r)
}

func (u *sunoapimusicUpstream) last() sunoapimusicReq {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.reqs[len(u.reqs)-1]
}

func TestSunoapimusicPrecheckAndMeta(t *testing.T) {
	env := newSunoapimusicEnv(t)
	if env.meta.Key != "sunoapi-music" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthBearer {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	for _, kind := range []string{"audio"} {
		if ep, ok := env.meta.Endpoint(kind); !ok || ep.Mode != pluginmeta.ModeAsync {
			t.Fatalf("%s 应是 async：%+v", kind, ep)
		}
	}
}

// 音乐：提交 POST /suno/submit/MUSIC，轮询 POST /suno/fetch；排队 → 进行中 → 成功，每首歌一个音频产物。
func TestSunoapimusicMusicFlow(t *testing.T) {
	polls := 0
	up := &sunoapimusicUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/suno/submit/MUSIC":
			_, _ = w.Write([]byte(`{"code":"success","message":"","data":"task-9"}`))
		case "/suno/fetch":
			polls++
			switch polls {
			case 1:
				_, _ = w.Write([]byte(`{"code":"success","data":[{"task_id":"task-9","status":"SUBMITTED"}]}`))
			case 2:
				_, _ = w.Write([]byte(`{"code":"success","data":[{"task_id":"task-9","status":"IN_PROGRESS","progress":"40%"}]}`))
			default:
				_, _ = w.Write([]byte(`{"code":"success","data":[{"task_id":"task-9","status":"SUCCESS","data":[
					{"id":"c1","audio_url":"https://cdn.example.com/c1.mp3","image_url":"https://cdn.example.com/c1.jpg"},
					{"id":"c2","audio_url":"https://cdn.example.com/c2.mp3"}]}]}`))
			}
		default:
			http.NotFound(w, r)
		}
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newSunoapimusicEnv(t)
	snap := env.snapshot(srv.URL, "audio", "chirp-v4")
	sub, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "轻快的民谣", "make_instrumental": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.ProviderTaskID != "task-9" || sub.Immediate != nil {
		t.Fatalf("提交结果不符：%+v", sub)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/suno/submit/MUSIC" || got.Auth != "Bearer sk-suno" ||
		got.Body["gpt_description_prompt"] != "轻快的民谣" || got.Body["make_instrumental"] != true || got.Body["mv"] != "chirp-v4" {
		t.Fatalf("提交请求不符：%+v", got)
	}

	ref := provider.TaskRef{ID: 1, UserID: 9, ProviderTaskID: "task-9"}
	q, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q.Status != provider.StatusQueued {
		t.Fatalf("应排队：%+v %v", q, err)
	}
	if got := up.last(); got.Method != "POST" || got.Path != "/suno/fetch" || got.Auth != "Bearer sk-suno" {
		t.Fatalf("轮询请求不符：%+v", got)
	}
	if ids, _ := up.last().Body["ids"].([]any); len(ids) != 1 || ids[0] != "task-9" {
		t.Fatalf("轮询 ids 不符：%v", up.last().Body)
	}
	q, _ = env.exec.Query(context.Background(), snap, ref)
	if q.Status != provider.StatusRunning || q.Progress == nil || *q.Progress != 40 {
		t.Fatalf("应进行中 40%%：%+v", q)
	}
	q, err = env.exec.Query(context.Background(), snap, ref)
	if err != nil || q.Status != provider.StatusSucceeded || len(q.Outputs) != 2 {
		t.Fatalf("应成功且有两首歌：%+v %v", q, err)
	}
	if o := q.Outputs[0]; o.Type != provider.OutputURL || o.URL != "https://cdn.example.com/c1.mp3" || o.MediaType != "audio" || o.Mime != "audio/mpeg" {
		t.Fatalf("产物不符：%+v", o)
	}
}

// 音乐提交默认 mv 是 chirp-v3-0；输入里的 mv 优先于 chirp- 开头的上游模型名。
func TestSunoapimusicMusicDefaults(t *testing.T) {
	up := &sunoapimusicUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"success","data":"t"}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newSunoapimusicEnv(t)
	snap := env.snapshot(srv.URL, "audio", "suno_music")
	if _, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}}); err != nil {
		t.Fatal(err)
	}
	if b := up.last().Body; b["mv"] != "chirp-v3-0" || b["make_instrumental"] != false {
		t.Fatalf("默认值不符：%v", b)
	}
	if _, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 3}, Input: map[string]any{"prompt": "x", "mv": "chirp-v3-5"}}); err != nil {
		t.Fatal(err)
	}
	if b := up.last().Body; b["mv"] != "chirp-v3-5" {
		t.Fatalf("输入里的 mv 应优先：%v", b)
	}
	// 输入 mv 优先于 chirp- 开头的上游模型名
	snap = env.snapshot(srv.URL, "audio", "chirp-v4")
	if _, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 4}, Input: map[string]any{"prompt": "x", "mv": "chirp-v3-5"}}); err != nil {
		t.Fatal(err)
	}
	if b := up.last().Body; b["mv"] != "chirp-v3-5" {
		t.Fatalf("输入 mv 应优先于上游模型名：%v", b)
	}
}

// 失败与坏响应：FAILURE 带原因并分类；成功但没有音频地址；code 非 success；查不到任务继续等待；提交没有 task_id 是插件级失败。
func TestSunoapimusicQueryEdges(t *testing.T) {
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
	env := newSunoapimusicEnv(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(&sunoapimusicUpstream{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(c.body)) }})
			defer srv.Close()
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, "audio", "x"), provider.TaskRef{ID: 1, ProviderTaskID: "t"})
			if err != nil || q.Status != c.status || string(q.ErrorClass) != c.class {
				t.Fatalf("结果不符：%+v %v", q, err)
			}
		})
	}

	srv := httptest.NewServer(&sunoapimusicUpstream{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"code":"success"}`)) }})
	defer srv.Close()
	_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "audio", "x"), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("没有 task_id 应是插件级失败：%v", err)
	}

	srv2 := httptest.NewServer(&sunoapimusicUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"fail","message":"content policy violated"}`))
	}})
	defer srv2.Close()
	sub, err := env.exec.Submit(context.Background(), env.snapshot(srv2.URL, "audio", "x"), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}})
	if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusFailed || sub.Immediate.ErrorClass != provider.ClassModeration {
		t.Fatalf("提交被拒应翻译成 moderation 失败：%+v %v", sub, err)
	}
}

func TestSunoapimusicClassifyHTTPErrors(t *testing.T) {
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
	env := newSunoapimusicEnv(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(&sunoapimusicUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}})
			defer srv.Close()
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "audio", "x"), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": strings.Repeat("a", 3)}})
			if err == nil || provider.ClassOf(err) != c.class {
				t.Fatalf("分类应是 %s：%v", c.class, err)
			}
		})
	}
}
