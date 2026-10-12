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

// typesafeEnv 用真实 runner 预检并装载内置 typesafe.js，元数据取自预检结果。
type typesafeEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newTypesafeEnv(t *testing.T) *typesafeEnv {
	t.Helper()
	src, err := plugins.Source("typesafe")
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
		t.Fatalf("内置 typesafe.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	exec := plugin.New(plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "sk-ts"}, IPAllowed: allowLoopback})
	return &typesafeEnv{exec: exec, meta: meta, sha: pre.SHA256}
}

// typesafeQuestions 是测试用的判断规则：一个选择题、一个打分题、一个是否题。
func typesafeQuestions() map[string]any {
	return map[string]any{
		"tone":  map[string]any{"type": "choice", "criteria": map[string]any{"polite": "礼貌", "rude": "无礼"}},
		"score": map[string]any{"type": "score", "criteria": []any{"差", "中", "好"}},
		"spam":  map[string]any{"type": "noul"},
	}
}

func (e *typesafeEnv) snapshot(baseURL string, params map[string]any) *provider.Snapshot {
	return &provider.Snapshot{
		Model:   provider.ModelSnapshot{Key: "m", Kind: "text", UpstreamModel: "jev-latest", Capabilities: promptCaps, Params: params},
		Channel: provider.ChannelSnapshot{Key: "ts-main", PluginKey: "typesafe", PluginVersionID: 1, BaseURL: baseURL},
		Plugin:  provider.PluginSnapshot{Key: "typesafe", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},

		ModelRevisionID: 1,
	}
}

type typesafeUpstream struct {
	mu      sync.Mutex
	reqs    []typesafeReq
	handler func(w http.ResponseWriter, r *http.Request)
}

type typesafeReq struct {
	Method, Path, Auth string
	Body               map[string]any
}

func (u *typesafeUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	u.mu.Lock()
	u.reqs = append(u.reqs, typesafeReq{r.Method, r.URL.Path, r.Header.Get("Authorization"), body})
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	u.handler(w, r)
}

func (u *typesafeUpstream) last() typesafeReq {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.reqs[len(u.reqs)-1]
}

const typesafeOKBody = `{"model":"jev-1.13.0","answers":{
	"tone":{"type":"choice","choice":"polite"},
	"score":{"type":"score","score":2},
	"spam":{"type":"noul","value":false}},
	"usage":{"input_tokens":321}}`

func TestTypesafePrecheckAndMeta(t *testing.T) {
	env := newTypesafeEnv(t)
	if env.meta.Key != "typesafe" || env.meta.Version != "1.0.0" || env.meta.Auth.Type != pluginmeta.AuthBearer {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("text"); !ok || ep.Mode != pluginmeta.ModeSync {
		t.Fatalf("text 应是 sync：%+v", ep)
	}
}

// 同步：POST /v1/systemone，state 是用户内容，questions 取模型固定参数；answers 变成 JSON 文本，usage 带输入 token、输出记 0。
func TestTypesafeSystemOne(t *testing.T) {
	up := &typesafeUpstream{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(typesafeOKBody)) }}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newTypesafeEnv(t)
	res, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, map[string]any{"questions": typesafeQuestions()}), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "请问能帮我看一下吗"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/v1/systemone" || got.Auth != "Bearer sk-ts" ||
		got.Body["model"] != "jev-latest" || got.Body["state"] != "请问能帮我看一下吗" {
		t.Fatalf("上游请求不符：%+v", got)
	}
	if qs, _ := got.Body["questions"].(map[string]any); len(qs) != 3 {
		t.Fatalf("questions 不符：%v", got.Body["questions"])
	}
	r := res.Immediate
	if r == nil || r.Status != provider.StatusSucceeded || len(r.Outputs) != 1 || r.Outputs[0].Type != provider.OutputText {
		t.Fatalf("应同步成功出文本：%+v", res)
	}
	var answers map[string]map[string]any
	if err := json.Unmarshal([]byte(r.Outputs[0].Text), &answers); err != nil || answers["tone"]["choice"] != "polite" || answers["spam"]["type"] != "noul" {
		t.Fatalf("正文应是 answers JSON：%q %v", r.Outputs[0].Text, err)
	}
	if u := r.Outputs[0].Usage; u == nil || u.InputTokens != 321 || u.OutputTokens != 0 {
		t.Fatalf("应回传输入 token 用量：%+v", u)
	}
}

// Base URL 末尾带 /v1 时不重复拼；上游没给合法用量时不带 usage。
func TestTypesafeBaseURLWithV1AndNoUsage(t *testing.T) {
	up := &typesafeUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"m","answers":{"spam":{"type":"noul","value":true}},"usage":{"input_tokens":-1}}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newTypesafeEnv(t)
	params := map[string]any{"questions": map[string]any{"spam": map[string]any{"type": "noul"}}}
	res, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL+"/v1/", params), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if up.last().Path != "/v1/systemone" {
		t.Fatalf("路径不符：%s", up.last().Path)
	}
	if res.Immediate == nil || res.Immediate.Status != provider.StatusSucceeded || res.Immediate.Outputs[0].Usage != nil {
		t.Fatalf("非法用量不应带 usage：%+v", res.Immediate)
	}
}

// 响应缺 answers，或缺少与问题匹配的回答：翻译成 failed 结果，不崩。
func TestTypesafeBadResponses(t *testing.T) {
	for name, body := range map[string]string{
		"没有 answers": `{"model":"m"}`,
		"类型不符":       `{"model":"m","answers":{"tone":{"type":"score"},"score":{"type":"score"},"spam":{"type":"noul"}}}`,
		"缺回答":        `{"model":"m","answers":{"tone":{"type":"choice"}}}`,
		"不是对象":       `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(&typesafeUpstream{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }})
			defer srv.Close()
			env := newTypesafeEnv(t)
			res, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, map[string]any{"questions": typesafeQuestions()}), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}})
			if err != nil || res.Immediate == nil || res.Immediate.Status != provider.StatusFailed || res.Immediate.ErrorMessage == "" {
				t.Fatalf("应翻译成 failed：%+v %v", res, err)
			}
		})
	}
}

// 模型没配 questions 或配置不合法：插件级失败，不发请求。
func TestTypesafeRejectsBadQuestions(t *testing.T) {
	up := &typesafeUpstream{handler: func(w http.ResponseWriter, r *http.Request) { t.Error("不应发请求") }}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newTypesafeEnv(t)
	for name, params := range map[string]map[string]any{
		"没配":         {},
		"空对象":        {"questions": map[string]any{}},
		"未知类型":       {"questions": map[string]any{"q": map[string]any{"type": "text"}}},
		"score 太少":   {"questions": map[string]any{"q": map[string]any{"type": "score", "criteria": []any{"a"}}}},
		"choice 没选项": {"questions": map[string]any{"q": map[string]any{"type": "choice"}}},
		"noul 乱键":    {"questions": map[string]any{"q": map[string]any{"type": "noul", "criteria": map[string]any{"maybe": "x"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, params), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}})
			var pe *provider.Error
			if !errors.As(err, &pe) || !pe.PluginFault {
				t.Fatalf("应是插件级失败：%v", err)
			}
		})
	}
}

func TestTypesafeClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"余额不足", 402, `{"detail":"insufficient credit"}`, provider.ClassProviderBalance},
		{"审核拒绝", 400, `{"detail":"content policy violation"}`, provider.ClassModeration},
		{"限流", 429, `{"detail":"rate limited"}`, provider.ClassRetryable},
		{"5xx", 500, `oops`, provider.ClassRetryable},
		{"其它 4xx", 422, `{"detail":"bad questions"}`, provider.ClassTerminal},
	}
	env := newTypesafeEnv(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(&typesafeUpstream{handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}})
			defer srv.Close()
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, map[string]any{"questions": typesafeQuestions()}), provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "x"}})
			if err == nil || provider.ClassOf(err) != c.class {
				t.Fatalf("分类应是 %s：%v", c.class, err)
			}
		})
	}
}
