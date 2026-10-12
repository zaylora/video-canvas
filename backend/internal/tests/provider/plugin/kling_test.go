package plugin_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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

const (
	klingChannelKey = "ak-test|sk-secret"
	klingRefURL     = "https://cdn.example.com/a.png"
)

type klingEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

// newKlingEnv 用真实 runner 预检并装载内置 kling.js；元数据取自预检结果。
func newKlingEnv(t *testing.T, secret string) *klingEnv {
	t.Helper()
	src, err := plugins.Source("kling")
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
		t.Fatalf("内置 kling.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	exec := plugin.New(plugin.Options{
		Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: secret},
		Assets: &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("a"), 6: []byte("b")}}, IPAllowed: allowLoopback,
	})
	return &klingEnv{exec: exec, meta: meta, sha: pre.SHA256}
}

func (e *klingEnv) snapshot(baseURL, upstream string, allowCredentials bool) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: "video", UpstreamModel: upstream, Capabilities: promptCaps},
		Channel: provider.ChannelSnapshot{
			Key: "kling-main", PluginKey: "kling", PluginVersionID: 1, BaseURL: baseURL, AllowCredentials: allowCredentials,
		},
		Plugin:          provider.PluginSnapshot{Key: "kling", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

type klingReq struct {
	Method, Path, Query, Auth string
	Body                      map[string]any
}

// klingUpstream 是假的可灵上游，记录每次请求。
type klingUpstream struct {
	mu       sync.Mutex
	requests []klingReq
	handler  func(w http.ResponseWriter, r *http.Request)
}

func (u *klingUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	u.mu.Lock()
	u.requests = append(u.requests, klingReq{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), body})
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	u.handler(w, r)
}

func (u *klingUpstream) last() klingReq {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requests[len(u.requests)-1]
}

func newKlingServer(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *klingUpstream) {
	t.Helper()
	up := &klingUpstream{handler: h}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	return srv, up
}

// klingVerifyJWT 校验 Authorization 头是 sk-secret 签的 HS256 JWT，返回 claims。
func klingVerifyJWT(t *testing.T, auth string) map[string]any {
	t.Helper()
	token := strings.TrimPrefix(auth, "Bearer ")
	parts := strings.Split(token, ".")
	if token == auth || len(parts) != 3 {
		t.Fatalf("Authorization 应是 Bearer JWT：%q", auth)
	}
	mac := hmac.New(sha256.New, []byte("sk-secret"))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil)); parts[2] != want {
		t.Fatalf("JWT 签名不对：%s != %s", parts[2], want)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

func TestKlingPrecheckAndMeta(t *testing.T) {
	env := newKlingEnv(t, klingChannelKey)
	if env.meta.Key != "kling" || env.meta.Version != "1.0.0" || env.meta.Auth.Type != pluginmeta.AuthCustom {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if len(env.meta.Endpoints) != 1 {
		t.Fatalf("应只声明 video：%+v", env.meta.Endpoints)
	}
	if ep, ok := env.meta.Endpoint("video"); !ok || ep.Mode != pluginmeta.ModeAsync {
		t.Fatalf("video 应是 async：%+v", ep)
	}
}

// 文生视频：POST /v1/videos/text2video，JWT 由插件用 ak|sk 签发；v2-master 强制 pro；duration 转成字符串。
func TestKlingTextToVideoSubmit(t *testing.T) {
	srv, up := newKlingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"message":"SUCCEED","data":{"task_id":"k-1","task_status":"submitted"}}`))
	})
	env := newKlingEnv(t, klingChannelKey)
	snap := env.snapshot(srv.URL, "kling-v2-master", true)
	snap.Model.Params = map[string]any{"mode": "std", "negative_prompt": "模糊", "extra": map[string]any{"cfg_scale": 0.9, "callback_url": "https://x"}}
	sub, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task:  provider.TaskRef{ID: 1, UserID: 9},
		Input: map[string]any{"prompt": "猫在跳舞", "duration": float64(10), "aspect_ratio": "16:9"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.ProviderTaskID != "k-1" || sub.Immediate != nil {
		t.Fatalf("提交结果不符：%+v", sub)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/v1/videos/text2video" {
		t.Fatalf("请求路径不符：%+v", got)
	}
	claims := klingVerifyJWT(t, got.Auth)
	exp, _ := claims["exp"].(float64)
	nbf, _ := claims["nbf"].(float64)
	if claims["iss"] != "ak-test" || exp-nbf != 1805 {
		t.Fatalf("JWT claims 不符：%v", claims)
	}
	b := got.Body
	if b["model_name"] != "kling-v2-master" || b["mode"] != "pro" || b["prompt"] != "猫在跳舞" || b["duration"] != "10" ||
		b["aspect_ratio"] != "16:9" || b["negative_prompt"] != "模糊" || b["cfg_scale"] != 0.9 || b["callback_url"] != "https://x" {
		t.Fatalf("请求体不符：%v", b)
	}
	if _, has := b["image"]; has {
		t.Fatalf("文生视频不应带 image：%v", b)
	}
}

// 图生视频：首帧 image、尾帧 image_tail 用参考图引用换成地址；不带 aspect_ratio；其它模型默认 std；状态里记住动作用于轮询。
func TestKlingImageToVideoAndQueryPath(t *testing.T) {
	srv, up := newKlingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"k-2"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"k-2","task_status":"processing"}}`))
	})
	env := newKlingEnv(t, klingChannelKey)
	snap := env.snapshot(srv.URL, "kling-v1-6", true)
	sub, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task:  provider.TaskRef{ID: 2, UserID: 9},
		Input: map[string]any{"prompt": "动起来", "images": []uint64{5, 6}, "aspect_ratio": "9:16", "mode": "pro"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := up.last()
	if got.Path != "/v1/videos/image2video" || got.Body["image"] != klingRefURL || got.Body["image_tail"] != klingRefURL ||
		got.Body["mode"] != "pro" || got.Body["model_name"] != "kling-v1-6" {
		t.Fatalf("图生视频请求不符：%+v", got)
	}
	if _, has := got.Body["aspect_ratio"]; has {
		t.Fatalf("图生视频不应带 aspect_ratio：%v", got.Body)
	}
	if string(sub.State) != `{"action":"image2video"}` {
		t.Fatalf("应记住动作：%s", sub.State)
	}

	q, err := env.exec.Query(context.Background(), snap, provider.TaskRef{ID: 2, UserID: 9, ProviderTaskID: "k-2", State: sub.State})
	if err != nil || q.Status != provider.StatusRunning {
		t.Fatalf("应是进行中：%+v %v", q, err)
	}
	if got := up.last(); got.Method != "GET" || got.Path != "/v1/videos/image2video/k-2" {
		t.Fatalf("轮询应走 image2video：%+v", got)
	}
	klingVerifyJWT(t, up.last().Auth)
}

// 异步查询：submitted → processing → succeed（视频地址 + final_unit_deduction）；没有 state 时默认走 text2video。
func TestKlingQueryLifecycle(t *testing.T) {
	var polls int
	srv, up := newKlingServer(t, func(w http.ResponseWriter, r *http.Request) {
		polls++
		switch polls {
		case 1:
			_, _ = w.Write([]byte(`{"code":0,"data":{"task_status":"submitted"}}`))
		case 2:
			_, _ = w.Write([]byte(`{"code":0,"data":{"task_status":"processing"}}`))
		default:
			_, _ = w.Write([]byte(`{"code":0,"data":{"task_status":"succeed","final_unit_deduction":"1.5","task_result":{"videos":[{"id":"v1","url":"https://cdn.klingai.com/out.mp4","duration":"5"}]}}}`))
		}
	})
	env := newKlingEnv(t, klingChannelKey)
	snap := env.snapshot(srv.URL, "kling-v1", true)
	ref := provider.TaskRef{ID: 3, UserID: 9, ProviderTaskID: "k-3"}
	q1, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q1.Status != provider.StatusQueued {
		t.Fatalf("应是排队：%+v %v", q1, err)
	}
	q2, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q2.Status != provider.StatusRunning {
		t.Fatalf("应是进行中：%+v %v", q2, err)
	}
	q3, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q3.Status != provider.StatusSucceeded || len(q3.Outputs) != 1 {
		t.Fatalf("应成功：%+v %v", q3, err)
	}
	out := q3.Outputs[0]
	if out.Type != provider.OutputURL || out.URL != "https://cdn.klingai.com/out.mp4" || out.MediaType != "video" || out.Mime != "video/mp4" {
		t.Fatalf("产物不符：%+v", out)
	}
	if q3.ProviderCost == nil || *q3.ProviderCost != 1.5 {
		t.Fatalf("应带上资源包单位数：%+v", q3.ProviderCost)
	}
	if got := up.last(); got.Path != "/v1/videos/text2video/k-3" {
		t.Fatalf("没有 state 时应走 text2video：%+v", got)
	}
}

// 失败任务：按文案分类（审核 / 余额 / 其它）；成功但没有地址判失败；业务码非 0 的 200 响应也翻译成失败。
func TestKlingQueryFailed(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		class provider.ErrorClass
	}{
		{"审核文案", `{"code":0,"data":{"task_status":"failed","task_status_msg":"Content violates the policy"}}`, provider.ClassModeration},
		{"余额文案", `{"code":0,"data":{"task_status":"failed","task_status_msg":"insufficient balance"}}`, provider.ClassProviderBalance},
		{"其它", `{"code":0,"data":{"task_status":"failed","task_status_msg":"render error"}}`, provider.ClassTerminal},
		{"成功无地址", `{"code":0,"data":{"task_status":"succeed","task_result":{"videos":[]}}}`, provider.ClassTerminal},
		{"业务码 1301", `{"code":1301,"message":"trigger content safety"}`, provider.ClassModeration},
		{"业务码 1102", `{"code":1102,"message":"account balance not enough"}`, provider.ClassProviderBalance},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newKlingServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) })
			env := newKlingEnv(t, klingChannelKey)
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, "kling-v1", true), provider.TaskRef{ID: 1, ProviderTaskID: "x"})
			if err != nil {
				t.Fatal(err)
			}
			if q.Status != provider.StatusFailed || q.ErrorClass != tc.class || q.ErrorMessage == "" {
				t.Fatalf("失败结果不符：%+v", q)
			}
		})
	}
}

// 提交：200 + 业务码失败变成 immediate 失败；没有 task_id 且没有错误码是插件级失败。
func TestKlingSubmitBadResponses(t *testing.T) {
	env := newKlingEnv(t, klingChannelKey)
	srv, _ := newKlingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1301,"message":"sensitive prompt"}`))
	})
	sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "kling-v1", true), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
	})
	if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusFailed || sub.Immediate.ErrorClass != provider.ClassModeration {
		t.Fatalf("应翻译成 moderation 失败：%+v %v", sub, err)
	}

	empty, _ := newKlingServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"code":0,"data":{}}`)) })
	_, err = env.exec.Submit(context.Background(), env.snapshot(empty.URL, "kling-v1", true), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
	})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("没有 task_id 应是插件级失败：%v", err)
	}

	// 响应体不是 JSON 也不崩：同样是插件级失败
	junk, _ := newKlingServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`not json`)) })
	_, err = env.exec.Submit(context.Background(), env.snapshot(junk.URL, "kling-v1", true), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
	})
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("坏响应应是插件级失败：%v", err)
	}
}

// Key 格式不对、或渠道没开 allow_credentials：插件拿不到可用凭据，是插件级失败，且不会发请求。
func TestKlingBadCredentials(t *testing.T) {
	cases := []struct {
		name   string
		secret string
		allow  bool
	}{
		{"没有竖线", "just-one-part", true},
		{"空 SecretKey", "ak|", true},
		{"没开 allow_credentials", klingChannelKey, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, up := newKlingServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
			env := newKlingEnv(t, tc.secret)
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "kling-v1", tc.allow), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
			})
			var pe *provider.Error
			if !errors.As(err, &pe) || !pe.PluginFault {
				t.Fatalf("应是插件级失败：%v", err)
			}
			if len(up.requests) != 0 {
				t.Fatalf("不应发出请求：%+v", up.requests)
			}
		})
	}
}

// HTTP 非 2xx：按业务码与文案分类，429 / 5xx 重试，其它 terminal。
func TestKlingClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"余额不足", 429, `{"code":1102,"message":"Account balance not enough"}`, provider.ClassProviderBalance},
		{"内容审核", 400, `{"code":1301,"message":"Trigger content safety policy"}`, provider.ClassModeration},
		{"限流", 429, `{"code":1302,"message":"rate limit"}`, provider.ClassRetryable},
		{"并发超限", 429, `{"code":1303,"message":"parallel task over resource pack limit"}`, provider.ClassRetryable},
		{"服务端错误码", 500, `{"code":5000,"message":"internal error"}`, provider.ClassRetryable},
		{"网关 5xx", 503, `bad gateway`, provider.ClassRetryable},
		{"参数错误", 400, `{"code":1201,"message":"invalid parameter"}`, provider.ClassTerminal},
		{"鉴权失败", 401, `{"code":1000,"message":"auth failed"}`, provider.ClassTerminal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newKlingServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			env := newKlingEnv(t, klingChannelKey)
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "kling-v1", true), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
			})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
		})
	}
}

// 连通检查：GET 任务列表，带 JWT；401 失败。
func TestKlingCheck(t *testing.T) {
	env := newKlingEnv(t, klingChannelKey)
	good, up := newKlingServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"code":0,"data":[]}`)) })
	rt := env.snapshot(good.URL, "kling-v1", true).Runtime()
	res, err := env.exec.Check(context.Background(), rt)
	if err != nil || res == nil || !res.OK {
		t.Fatalf("检查应成功：%+v %v", res, err)
	}
	got := up.last()
	if got.Method != "GET" || got.Path != "/v1/videos/text2video" || got.Query != "pageNum=1&pageSize=1" {
		t.Fatalf("检查请求不符：%+v", got)
	}
	klingVerifyJWT(t, got.Auth)

	bad, _ := newKlingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":1000,"message":"auth failed"}`))
	})
	res, err = env.exec.Check(context.Background(), env.snapshot(bad.URL, "kling-v1", true).Runtime())
	if err == nil || (res != nil && res.OK) {
		t.Fatalf("401 应检查失败：%+v %v", res, err)
	}
}
