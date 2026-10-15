package plugin_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/plugins"
)

// jimengEnv 用真实 runner 预检并装载内置 jimeng.js，元数据取自预检结果。
type jimengEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newJimengEnv(t *testing.T, mod func(*plugin.Options)) *jimengEnv {
	t.Helper()
	src, err := plugins.Source("jimeng")
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
		t.Fatalf("内置 jimeng.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	opts := plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: jimengKey}, IPAllowed: allowLoopback}
	if mod != nil {
		mod(&opts)
	}
	return &jimengEnv{exec: plugin.New(opts), meta: meta, sha: pre.SHA256}
}

const jimengKey = "AKLTtest|sk/Secret+Value=="

func (e *jimengEnv) snapshot(baseURL, upstreamModel string) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: "video", UpstreamModel: upstreamModel, Capabilities: promptCaps},
		Channel: provider.ChannelSnapshot{
			Key: "jimeng-main", PluginKey: "jimeng", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "jimeng", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

type jimengRecorded struct {
	Method, Path, Action, Version string
	Header                        http.Header
	Raw                           string
	Host                          string
}

// jimengUpstream 记录原始请求字节与头，并按 Action 返回预设的响应体。
type jimengUpstream struct {
	mu       sync.Mutex
	requests []jimengRecorded
	submit   string
	query    string
	status   int
}

func (u *jimengUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	rec := jimengRecorded{
		Method: r.Method, Path: r.URL.Path, Action: r.URL.Query().Get("Action"), Version: r.URL.Query().Get("Version"),
		Header: r.Header.Clone(), Raw: string(raw), Host: r.Host,
	}
	u.mu.Lock()
	u.requests = append(u.requests, rec)
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if u.status != 0 {
		w.WriteHeader(u.status)
	}
	if rec.Action == "CVSync2AsyncGetResult" {
		_, _ = w.Write([]byte(u.query))
		return
	}
	_, _ = w.Write([]byte(u.submit))
}

func (u *jimengUpstream) last() jimengRecorded {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requests[len(u.requests)-1]
}

func jimengHMAC(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}

func jimengSHA(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// jimengExpectedAuthorization 在测试里独立按火山引擎 V4 规范重新签一遍（不复用插件代码），用于对拍 Authorization。
func jimengExpectedAuthorization(rec jimengRecorded, ak, sk string) string {
	xDate := rec.Header.Get("X-Date")
	short := xDate[:8]
	bodyHash := jimengSHA(rec.Raw)
	q := url.Values{"Action": {rec.Action}, "Version": {rec.Version}}.Encode() // 键已排序
	signed := "content-type;host;x-content-sha256;x-date"
	canonical := strings.Join([]string{
		"POST", "/", q,
		"content-type:application/json\nhost:" + rec.Host + "\nx-content-sha256:" + bodyHash + "\nx-date:" + xDate + "\n",
		signed, bodyHash,
	}, "\n")
	scope := short + "/cn-north-1/cv/request"
	sts := "HMAC-SHA256\n" + xDate + "\n" + scope + "\n" + jimengSHA(canonical)
	k := jimengHMAC([]byte(sk), short)
	k = jimengHMAC(k, "cn-north-1")
	k = jimengHMAC(k, "cv")
	k = jimengHMAC(k, "request")
	sig := hex.EncodeToString(jimengHMAC(k, sts))
	return "HMAC-SHA256 Credential=" + ak + "/" + scope + ", SignedHeaders=" + signed + ", Signature=" + sig
}

// jimengGoEscaped 把占位符换成 Go json.Marshal 会输出的转义序列（< > & U+2028），避免在源码里直接写转义。
func jimengGoEscaped(s string) string {
	return strings.NewReplacer("{LT}", "\\u003c", "{GT}", "\\u003e", "{AMP}", "\\u0026", "{LS}", "\\u2028").Replace(s)
}

func jimengSubmit(t *testing.T, env *jimengEnv, snap *provider.Snapshot, input map[string]any) (*provider.SubmitResult, error) {
	t.Helper()
	return env.exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1, UserID: 9}, Input: input})
}

func TestJimengPrecheckAndMeta(t *testing.T) {
	env := newJimengEnv(t, nil)
	if env.meta.Key != "jimeng" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthCustom {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("video"); !ok || ep.Mode != pluginmeta.ModeAsync {
		t.Fatalf("video 应是 async：%+v", ep)
	}
}

// 提交：路径 / Action / 版本、V4 签名头、请求体；签名与测试里独立计算的结果一致，body 哈希对得上实际字节。
// 提示词里放 < > & 引号 换行 与中文，验证插件对请求体的序列化和宿主发出的字节完全一致。
func TestJimengSubmitSignsLikeVolcV4(t *testing.T) {
	up := &jimengUpstream{submit: `{"code":10000,"message":"Success","data":{"task_id":"jm-7"}}`}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newJimengEnv(t, nil)
	snap := env.snapshot(srv.URL, "jimeng_vgfm_t2v_l20")

	prompt := "一只猫 <b>&\"x\"</b>\n\t跳舞 "
	res, err := jimengSubmit(t, env, snap, map[string]any{"prompt": prompt, "aspect_ratio": "9:16", "duration": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderTaskID != "jm-7" || res.Immediate != nil || !strings.Contains(string(res.State), "jimeng_vgfm_t2v_l20") {
		t.Fatalf("提交结果不符：%+v", res)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/" || got.Action != "CVSync2AsyncSubmitTask" || got.Version != "2022-08-31" {
		t.Fatalf("上游请求不符：%+v", got)
	}
	if got.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type 应是 application/json：%q", got.Header.Get("Content-Type"))
	}
	if got.Header.Get("X-Content-Sha256") != jimengSHA(got.Raw) {
		t.Fatalf("X-Content-Sha256 与实际请求体不一致：%s vs %s（body=%s）", got.Header.Get("X-Content-Sha256"), jimengSHA(got.Raw), got.Raw)
	}
	if _, err := time.Parse("20060102T150405Z", got.Header.Get("X-Date")); err != nil {
		t.Fatalf("X-Date 格式不符：%v", err)
	}
	if want := jimengExpectedAuthorization(got, "AKLTtest", "sk/Secret+Value=="); got.Header.Get("Authorization") != want {
		t.Fatalf("签名不一致：\n got %s\nwant %s", got.Header.Get("Authorization"), want)
	}
	wantBody := jimengGoEscaped(`{"aspect_ratio":"9:16","prompt":"一只猫 {LT}b{GT}{AMP}\"x\"{LT}/b{GT}\n\t跳舞{LS}","req_key":"jimeng_vgfm_t2v_l20"}`)
	if got.Raw != wantBody {
		t.Fatalf("请求体不符：\n got %s\nwant %s", got.Raw, wantBody)
	}
}

// 3.0 系列文生视频：req_key 换成 t2v 版本，frames 按时长 121 / 241；Pro 用 ti2v；extra 与 seed 参与签名。
func TestJimengV3ReqKeyAndFrames(t *testing.T) {
	cases := []struct {
		model    string
		duration any
		wantBody string
	}{
		{"jimeng_v30_1080p", float64(10), `{"frames":241,"prompt":"x","req_key":"jimeng_t2v_v30_1080p"}`},
		{"jimeng_v30_720p", nil, `{"frames":121,"prompt":"x","req_key":"jimeng_t2v_v30_720p"}`},
		{"jimeng_v30_pro", float64(5), `{"frames":121,"prompt":"x","req_key":"jimeng_ti2v_v30_pro"}`},
		{"jimeng_vgfm_t2v_l20", float64(10), `{"prompt":"x","req_key":"jimeng_vgfm_t2v_l20"}`},
	}
	for _, tc := range cases {
		up := &jimengUpstream{submit: `{"code":10000,"data":{"task_id":"1"}}`}
		srv := httptest.NewServer(up)
		env := newJimengEnv(t, nil)
		in := map[string]any{"prompt": "x"}
		if tc.duration != nil {
			in["duration"] = tc.duration
		}
		if _, err := jimengSubmit(t, env, env.snapshot(srv.URL, tc.model), in); err != nil {
			t.Fatal(err)
		}
		got := up.last()
		if got.Raw != tc.wantBody {
			t.Fatalf("%s：请求体 %s，期望 %s", tc.model, got.Raw, tc.wantBody)
		}
		if got.Header.Get("Authorization") != jimengExpectedAuthorization(got, "AKLTtest", "sk/Secret+Value==") {
			t.Fatalf("%s：签名不一致", tc.model)
		}
		srv.Close()
	}

	up := &jimengUpstream{submit: `{"code":10000,"data":{"task_id":"1"}}`}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newJimengEnv(t, nil)
	snap := env.snapshot(srv.URL, "jimeng_vgfm_t2v_l20")
	if _, err := jimengSubmit(t, env, snap, map[string]any{"prompt": "<x>", "seed": float64(42)}); err != nil {
		t.Fatal(err)
	}
	got := up.last()
	if got.Raw != jimengGoEscaped(`{"prompt":"{LT}x{GT}","req_key":"jimeng_vgfm_t2v_l20","seed":42}`) {
		t.Fatalf("seed / 转义字符请求体不符：%s", got.Raw)
	}
	if got.Header.Get("Authorization") != jimengExpectedAuthorization(got, "AKLTtest", "sk/Secret+Value==") {
		t.Fatal("带转义字符的签名不一致")
	}
}

// Key 格式不对、带参考图：都是插件级失败，不发请求。
func TestJimengSubmitPluginFaults(t *testing.T) {
	up := &jimengUpstream{submit: `{"code":10000,"data":{"task_id":"1"}}`}
	srv := httptest.NewServer(up)
	defer srv.Close()

	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("A")}}
	cases := []struct {
		name   string
		key    string
		images []uint64
	}{
		{"Key 缺半竖线", "onlyone", nil},
		{"带参考图", jimengKey, []uint64{5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newJimengEnv(t, func(o *plugin.Options) {
				o.Secrets = secretResolver{value: tc.key}
				o.Assets = assets
			})
			in := map[string]any{"prompt": "x"}
			if tc.images != nil {
				in["images"] = tc.images
			}
			_, err := jimengSubmit(t, env, env.snapshot(srv.URL, "jimeng_vgfm_t2v_l20"), in)
			var pe *provider.Error
			if !errors.As(err, &pe) || !pe.PluginFault {
				t.Fatalf("应是插件级失败：%v", err)
			}
		})
	}
	if len(up.requests) != 0 {
		t.Fatalf("插件失败时不应发出请求：%d", len(up.requests))
	}
}

// 提交：200 + 业务错误码，翻译成 failed 并分类；缺 task_id 是插件级失败。
func TestJimengSubmitBusinessErrors(t *testing.T) {
	cases := []struct {
		body  string
		class provider.ErrorClass
	}{
		{`{"code":50411,"message":"Pre Img Risk Not Pass"}`, provider.ClassModeration},
		{`{"code":50413,"message":"Post Text Risk Not Pass"}`, provider.ClassModeration},
		{`{"code":50429,"message":"Request Has Reached API Limit"}`, provider.ClassRetryable},
		{`{"code":50430,"message":"Concurrent Limit"}`, provider.ClassRetryable},
		{`{"code":50400,"message":"Account insufficient balance"}`, provider.ClassProviderBalance},
		{`{"code":50412,"message":"bad"}`, provider.ClassModeration},
		{`{"code":60001,"message":"unknown failure"}`, provider.ClassTerminal},
	}
	for _, tc := range cases {
		t.Run(tc.body, func(t *testing.T) {
			srv := httptest.NewServer(&jimengUpstream{submit: tc.body})
			defer srv.Close()
			env := newJimengEnv(t, nil)
			res, err := jimengSubmit(t, env, env.snapshot(srv.URL, "jimeng_vgfm_t2v_l20"), map[string]any{"prompt": "x"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Immediate == nil || res.Immediate.Status != provider.StatusFailed || res.Immediate.ErrorClass != tc.class {
				t.Fatalf("应翻译成 %s 失败：%+v", tc.class, res.Immediate)
			}
		})
	}

	srv := httptest.NewServer(&jimengUpstream{submit: `{"code":10000,"data":{}}`})
	defer srv.Close()
	env := newJimengEnv(t, nil)
	_, err := jimengSubmit(t, env, env.snapshot(srv.URL, "jimeng_vgfm_t2v_l20"), map[string]any{"prompt": "x"})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("缺 task_id 应是插件级失败：%v", err)
	}
}

// 轮询：请求带 req_key 与 task_id 并签名；in_queue / generating / done / not_found / expired / 业务码 / 缺地址。
func TestJimengQuery(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status string
		class  provider.ErrorClass
		url    string
	}{
		{"排队", `{"code":10000,"data":{"status":"in_queue"}}`, provider.StatusQueued, "", ""},
		{"进行中", `{"code":10000,"data":{"status":"generating"}}`, provider.StatusRunning, "", ""},
		{"完成", `{"code":10000,"data":{"status":"done","video_url":"https://cdn.example.com/v.mp4"}}`, provider.StatusSucceeded, "", "https://cdn.example.com/v.mp4"},
		{"完成没地址", `{"code":10000,"data":{"status":"done"}}`, provider.StatusFailed, provider.ClassTerminal, ""},
		{"不存在", `{"code":10000,"data":{"status":"not_found"}}`, provider.StatusFailed, provider.ClassTerminal, ""},
		{"过期", `{"code":10000,"data":{"status":"expired"}}`, provider.StatusFailed, provider.ClassTerminal, ""},
		{"输出审核", `{"code":50511,"message":"Post Img Risk Not Pass"}`, provider.StatusFailed, provider.ClassModeration, ""},
		{"限流继续等", `{"code":50429,"message":"limit"}`, provider.StatusRunning, "", ""},
		{"没有 data", `{"code":10000}`, provider.StatusQueued, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := &jimengUpstream{query: tc.body}
			srv := httptest.NewServer(up)
			defer srv.Close()
			env := newJimengEnv(t, nil)
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, "jimeng_v30_720p"), provider.TaskRef{
				ID: 1, ProviderTaskID: "jm-7", State: []byte(`{"req_key":"jimeng_t2v_v30_720p"}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			if q.Status != tc.status || q.ErrorClass != tc.class {
				t.Fatalf("结果不符：%+v", q)
			}
			if tc.url != "" {
				if len(q.Outputs) != 1 || q.Outputs[0].URL != tc.url || q.Outputs[0].MediaType != "video" || q.Outputs[0].Mime != "video/mp4" {
					t.Fatalf("产物不符：%+v", q.Outputs)
				}
			}
			got := up.last()
			if got.Action != "CVSync2AsyncGetResult" || got.Raw != `{"req_key":"jimeng_t2v_v30_720p","task_id":"jm-7"}` {
				t.Fatalf("查询请求不符：%+v", got)
			}
			if got.Header.Get("Authorization") != jimengExpectedAuthorization(got, "AKLTtest", "sk/Secret+Value==") {
				t.Fatal("查询请求签名不一致")
			}
		})
	}
}

// 没有 state（老任务）时按模型推 req_key；空响应体不崩。
func TestJimengQueryWithoutStateAndEmptyBody(t *testing.T) {
	up := &jimengUpstream{query: `{}`}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newJimengEnv(t, nil)
	q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, "jimeng_v30_pro"), provider.TaskRef{ID: 1, ProviderTaskID: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if q.Status != provider.StatusFailed {
		t.Fatalf("code 缺失不是 10000，应判失败：%+v", q)
	}
	if got := up.last(); got.Raw != `{"req_key":"jimeng_ti2v_v30_pro","task_id":"x"}` {
		t.Fatalf("无 state 时 req_key 应按模型推导：%s", got.Raw)
	}
}

// HTTP 非 2xx：业务码审核 / 余额 / 限流 / 5xx / 火山网关错误体。
func TestJimengClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"审核码", 400, `{"code":50411,"message":"risk"}`, provider.ClassModeration},
		{"限流", 429, `{"code":50429,"message":"limit"}`, provider.ClassRetryable},
		{"欠费", 403, `{"ResponseMetadata":{"Error":{"Code":"AccountOverdue","Message":"account in arrears"}}}`, provider.ClassProviderBalance},
		{"签名错误", 401, `{"ResponseMetadata":{"Error":{"Code":"SignatureDoesNotMatch","Message":"bad signature"}}}`, provider.ClassTerminal},
		{"5xx 非 JSON", 502, `bad gateway`, provider.ClassRetryable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(&jimengUpstream{status: tc.status, submit: tc.body})
			defer srv.Close()
			env := newJimengEnv(t, nil)
			_, err := jimengSubmit(t, env, env.snapshot(srv.URL, "jimeng_vgfm_t2v_l20"), map[string]any{"prompt": "x"})
			var pe *provider.Error
			if !errors.As(err, &pe) || pe.Class != tc.class {
				t.Fatalf("期望分类 %s，得到 %v", tc.class, err)
			}
		})
	}
}
