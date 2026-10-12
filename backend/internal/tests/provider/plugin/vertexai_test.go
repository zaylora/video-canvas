package plugin_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
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
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/plugins"
)

const (
	vertexaiOpName = "projects/123456/locations/us-central1/publishers/google/models/veo-3.0-generate-001/operations/op-1"
	vertexaiToken  = "ya29.fake-token"
)

// vertexaiEnv 用真实 runner 预检并装载内置 vertex-ai.js；渠道 Key 是一份带真实 RSA 私钥的服务账号 JSON。
type vertexaiEnv struct {
	exec   *plugin.Executor
	meta   *pluginmeta.Meta
	sha    string
	pub    *rsa.PublicKey
	nowSec int64
}

func newVertexaiEnv(t *testing.T, mod func(*plugin.Options)) *vertexaiEnv {
	t.Helper()
	src, err := plugins.Source("vertex-ai")
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
		t.Fatalf("内置 vertex-ai.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pemText := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	saJSON, _ := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "my-proj", "client_email": "veo@my-proj.iam.gserviceaccount.com", "private_key": pemText,
	})
	now := time.Now()
	opts := plugin.Options{
		Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: string(saJSON)},
		IPAllowed: allowLoopback, Now: func() time.Time { return now },
	}
	if mod != nil {
		mod(&opts)
	}
	return &vertexaiEnv{exec: plugin.New(opts), meta: meta, sha: pre.SHA256, pub: &priv.PublicKey, nowSec: now.Unix()}
}

func (e *vertexaiEnv) snapshot(baseURL string, settings map[string]any) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "veo", Kind: "video", UpstreamModel: "veo-3.0-generate-001", Capabilities: vertexaiCaps},
		Channel: provider.ChannelSnapshot{
			Key: "vertex-main", PluginKey: "vertex-ai", PluginVersionID: 1, BaseURL: baseURL,
			AllowCredentials: true, Settings: settings,
		},
		Plugin:          provider.PluginSnapshot{Key: "vertex-ai", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

var vertexaiCaps = modelcfg.Capabilities{
	Ops:    []string{modelcfg.OpT2V, modelcfg.OpI2V},
	Refs:   modelcfg.Refs{Image: modelcfg.RefSpec{On: true, Max: 1, MaxMB: 10}},
	Prompt: modelcfg.PromptSpec{MaxLength: 10000},
}

type vertexaiRecorded struct {
	Method, Path, Auth, Project, ContentType string
	Form                                     url.Values
	Body                                     map[string]any
}

// vertexaiFake 同时扮演令牌端点（/token）和 Vertex 接口。
type vertexaiFake struct {
	mu       sync.Mutex
	requests []vertexaiRecorded
	handler  func(w http.ResponseWriter, r *http.Request)
}

func (f *vertexaiFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	rec := vertexaiRecorded{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"),
		Project: r.Header.Get("x-goog-user-project"), ContentType: r.Header.Get("Content-Type"), Body: map[string]any{}}
	if strings.HasPrefix(rec.ContentType, "application/x-www-form-urlencoded") {
		rec.Form, _ = url.ParseQuery(string(raw))
	} else {
		_ = json.Unmarshal(raw, &rec.Body)
	}
	f.mu.Lock()
	f.requests = append(f.requests, rec)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/token" {
		_, _ = w.Write([]byte(`{"access_token":"` + vertexaiToken + `","expires_in":3599,"token_type":"Bearer"}`))
		return
	}
	f.handler(w, r)
}

func (f *vertexaiFake) all() []vertexaiRecorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]vertexaiRecorded(nil), f.requests...)
}

func vertexaiSettings(srvURL string) map[string]any {
	return map[string]any{"storageUri": "gs://veo-out/videos/", "tokenUrl": srvURL + "/token"}
}

func TestVertexaiPrecheckAndMeta(t *testing.T) {
	env := newVertexaiEnv(t, nil)
	if env.meta.Key != "vertex-ai" || env.meta.Version != "1.0.0" || env.meta.Auth.Type != pluginmeta.AuthCustom {
		t.Fatalf("meta 不符合预期：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("video"); !ok || ep.Mode != pluginmeta.ModeAsync {
		t.Fatalf("video 应是 async：%+v", ep)
	}
}

// 提交：先走准备阶段拿 access token（服务账号 JWT，RS256，签名可用公钥验证，且不带渠道 Key），
// 再用 Bearer token 提交，响应里的 token 进 state 给查询阶段用。
func TestVertexaiSubmitFlow(t *testing.T) {
	up := &vertexaiFake{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"` + vertexaiOpName + `"}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newVertexaiEnv(t, nil)
	snap := env.snapshot(srv.URL, vertexaiSettings(srv.URL))
	snap.Model.Params = map[string]any{"extra": map[string]any{"personGeneration": "allow_adult"}}
	var saved json.RawMessage
	res, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9},
		Input: map[string]any{"prompt": "海边日落", "duration": float64(6), "aspect_ratio": "16:9", "resolution": "720p",
			"generate_audio": false},
		OnPrepared: func(_ context.Context, p json.RawMessage) error { saved = p; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderTaskID != vertexaiOpName || res.Immediate != nil || !strings.Contains(string(res.State), vertexaiToken) {
		t.Fatalf("提交结果不符：%+v state=%s", res, res.State)
	}
	if !strings.Contains(string(saved), vertexaiToken) {
		t.Fatalf("准备结果应交给 OnPrepared 持久化：%s", saved)
	}

	reqs := up.all()
	if len(reqs) != 2 {
		t.Fatalf("应有换 token 与提交两个请求：%+v", reqs)
	}
	tok := reqs[0]
	if tok.Method != "POST" || tok.Path != "/token" || tok.Auth != "" || tok.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
		t.Fatalf("换 token 请求不符：%+v", tok)
	}
	vertexaiVerifyJWT(t, env, tok.Form.Get("assertion"), srv.URL+"/token")

	sub := reqs[1]
	if sub.Method != "POST" || sub.Auth != "Bearer "+vertexaiToken || sub.Project != "my-proj" ||
		sub.Path != "/v1/projects/my-proj/locations/us-central1/publishers/google/models/veo-3.0-generate-001:predictLongRunning" {
		t.Fatalf("提交请求不符：%+v", sub)
	}
	inst := sub.Body["instances"].([]any)[0].(map[string]any)
	p := sub.Body["parameters"].(map[string]any)
	if inst["prompt"] != "海边日落" || p["durationSeconds"] != float64(6) || p["aspectRatio"] != "16:9" || p["resolution"] != "720p" ||
		p["generateAudio"] != false || p["sampleCount"] != float64(1) || p["storageUri"] != "gs://veo-out/videos/" || p["personGeneration"] != "allow_adult" {
		t.Fatalf("请求体不符：%v", sub.Body)
	}
}

// vertexaiVerifyJWT 校验服务账号 JWT：header、claims 与签名。
func vertexaiVerifyJWT(t *testing.T, env *vertexaiEnv, jwt, aud string) {
	t.Helper()
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT 格式不对：%q", jwt)
	}
	dec := func(s string) []byte {
		b, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var header, claims map[string]any
	if json.Unmarshal(dec(parts[0]), &header) != nil || json.Unmarshal(dec(parts[1]), &claims) != nil {
		t.Fatal("JWT 解码失败")
	}
	if header["alg"] != "RS256" || claims["iss"] != "veo@my-proj.iam.gserviceaccount.com" || claims["aud"] != aud ||
		claims["scope"] != "https://www.googleapis.com/auth/cloud-platform" ||
		claims["iat"] != float64(env.nowSec) || claims["exp"] != float64(env.nowSec+3600) {
		t.Fatalf("JWT 内容不符：%v %v", header, claims)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(env.pub, crypto.SHA256, sum[:], dec(parts[2])); err != nil {
		t.Fatalf("JWT 签名校验失败：%v", err)
	}
}

// 图生视频：参考图 base64 + MIME 放进 instances[0].image。
func TestVertexaiSubmitImage2Video(t *testing.T) {
	up := &vertexaiFake{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"` + vertexaiOpName + `"}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("PNGDATA")}}
	env := newVertexaiEnv(t, func(o *plugin.Options) { o.Assets = assets })
	if _, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, vertexaiSettings(srv.URL)), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "动起来", "op": "i2v", "images": []uint64{5}},
	}); err != nil {
		t.Fatal(err)
	}
	reqs := up.all()
	inst := reqs[len(reqs)-1].Body["instances"].([]any)[0].(map[string]any)
	img := inst["image"].(map[string]any)
	if img["bytesBase64Encoded"] != base64.StdEncoding.EncodeToString([]byte("PNGDATA")) || img["mimeType"] != "image/png" {
		t.Fatalf("参考图不符：%v", img)
	}
}

// 配置缺失都是插件级失败，且不发提交请求：没开 allow_credentials、没配 storageUri、Key 不是服务账号 JSON、token 换取失败。
func TestVertexaiSubmitConfigErrors(t *testing.T) {
	up := &vertexaiFake{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"name":"x"}`)) }}
	srv := httptest.NewServer(up)
	defer srv.Close()
	submit := func(env *vertexaiEnv, mut func(*provider.Snapshot)) error {
		snap := env.snapshot(srv.URL, vertexaiSettings(srv.URL))
		mut(snap)
		_, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"}})
		return err
	}
	env := newVertexaiEnv(t, nil)
	cases := map[string]func(*provider.Snapshot){
		"没开 allow_credentials": func(s *provider.Snapshot) { s.Channel.AllowCredentials = false },
		"没配 storageUri":        func(s *provider.Snapshot) { s.Channel.Settings = map[string]any{"tokenUrl": srv.URL + "/token"} },
	}
	for name, mut := range cases {
		err := submit(env, mut)
		var pe *provider.Error
		if err == nil || !errorsAsProvider(err, &pe) || !pe.PluginFault {
			t.Fatalf("%s：应是插件级失败：%v", name, err)
		}
	}
	bad := newVertexaiEnv(t, func(o *plugin.Options) { o.Secrets = secretResolver{value: "not-json"} })
	if err := submit(bad, func(*provider.Snapshot) {}); err == nil {
		t.Fatal("Key 不是 JSON 应失败")
	}
	// 令牌端点不返回 access_token
	noTok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"error":"invalid_grant"}`)) }))
	defer noTok.Close()
	snap := env.snapshot(srv.URL, map[string]any{"storageUri": "gs://b/p/", "tokenUrl": noTok.URL})
	if _, err := env.exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"}}); err == nil {
		t.Fatal("换不到 token 应失败")
	}
}

func errorsAsProvider(err error, target **provider.Error) bool {
	return errors.As(err, target)
}

// 令牌端点返回非 2xx（如 400 invalid_grant）按宿主默认规则终止。
func TestVertexaiTokenEndpointError(t *testing.T) {
	tokSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokSrv.Close()
	up := httptest.NewServer(http.NotFoundHandler())
	defer up.Close()
	env := newVertexaiEnv(t, nil)
	_, err := env.exec.Submit(context.Background(), env.snapshot(up.URL, map[string]any{"storageUri": "gs://b/p/", "tokenUrl": tokSrv.URL}),
		provider.SubmitInput{Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"}})
	if err == nil || provider.ClassOf(err) != provider.ClassTerminal {
		t.Fatalf("应终止：%v", err)
	}
}

// 提交即完成（done=true）：不直接交结果，gs:// 转成 https 地址记进 state（ready），下一轮查询再取视频。
func TestVertexaiSubmitDoneReady(t *testing.T) {
	up := &vertexaiFake{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"` + vertexaiOpName + `","done":true,"response":{"videos":[{"gcsUri":"gs://veo-out/videos/a b.mp4","mimeType":"video/mp4"}]}}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newVertexaiEnv(t, nil)
	res, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, vertexaiSettings(srv.URL)), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
	})
	if err != nil || res.Immediate != nil || !strings.Contains(string(res.State), `"ready":true`) ||
		!strings.Contains(string(res.State), "https://storage.googleapis.com/veo-out/videos/a%20b.mp4") || !strings.Contains(string(res.State), vertexaiToken) {
		t.Fatalf("应记 ready 与地址（并保留 token）：%+v %v", res, err)
	}
}

func vertexaiState(env *vertexaiEnv, expiresAt int64) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"accessToken": vertexaiToken, "expiresAt": expiresAt, "projectId": "my-proj"})
	return b
}

// 查询：用 state 里的 token，POST :fetchPredictOperation，project / location / model 从 operation name 解析；
// 覆盖进行中、成功、各类失败。
func TestVertexaiQueryStates(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status string
		class  provider.ErrorClass
		url    string
	}{
		{"进行中", `{"name":"` + vertexaiOpName + `"}`, provider.StatusRunning, "", ""},
		{"完成（下一轮取内容）", `{"name":"` + vertexaiOpName + `","done":true,"response":{"videos":[{"gcsUri":"gs://veo-out/videos/a.mp4","mimeType":"video/mp4"}]}}`,
			provider.StatusRunning, "", ""},
		{"错误体", `{"name":"` + vertexaiOpName + `","done":true,"error":{"code":13,"message":"internal"}}`, provider.StatusFailed, provider.ClassTerminal, ""},
		{"审核错误", `{"done":true,"error":{"code":3,"message":"Prompt blocked by Responsible AI policy"}}`, provider.StatusFailed, provider.ClassModeration, ""},
		{"RAI 过滤", `{"name":"` + vertexaiOpName + `","done":true,"response":{"raiMediaFilteredCount":1,"raiMediaFilteredReasons":["Support codes: 17301594"]}}`,
			provider.StatusFailed, provider.ClassModeration, ""},
		{"只有内联 base64", `{"name":"` + vertexaiOpName + `","done":true,"response":{"videos":[{"bytesBase64Encoded":"AAAA","mimeType":"video/mp4"}]}}`,
			provider.StatusFailed, provider.ClassTerminal, ""},
		{"完成但无视频", `{"name":"` + vertexaiOpName + `","done":true,"response":{}}`, provider.StatusFailed, provider.ClassTerminal, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := &vertexaiFake{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }}
			srv := httptest.NewServer(up)
			defer srv.Close()
			env := newVertexaiEnv(t, nil)
			q, err := env.exec.Query(context.Background(), env.snapshot(srv.URL, vertexaiSettings(srv.URL)), provider.TaskRef{
				ID: 1, UserID: 9, ProviderTaskID: vertexaiOpName, State: vertexaiState(env, env.nowSec+1800),
			})
			if err != nil {
				t.Fatal(err)
			}
			if q.Status != tc.status || q.ErrorClass != tc.class {
				t.Fatalf("结果不符：%+v", q)
			}
			if tc.url != "" && (len(q.Outputs) != 1 || q.Outputs[0].URL != tc.url || q.Outputs[0].MediaType != "video") {
				t.Fatalf("产物不符：%+v", q.Outputs)
			}
			if tc.status == provider.StatusFailed && q.ErrorMessage == "" {
				t.Fatalf("失败应有原因：%+v", q)
			}
			reqs := up.all()
			got := reqs[len(reqs)-1]
			if len(reqs) != 1 || got.Method != "POST" || got.Auth != "Bearer "+vertexaiToken || got.Project != "my-proj" ||
				got.Path != "/v1/projects/123456/locations/us-central1/publishers/google/models/veo-3.0-generate-001:fetchPredictOperation" ||
				got.Body["operationName"] != vertexaiOpName {
				t.Fatalf("查询请求不符（不应再换 token）：%+v", reqs)
			}
		})
	}
}

// 查询阶段拿不到可用 token（没有 state、或已过期）是插件级失败，不会发请求；operation name 解析不出来、响应无法识别同样不崩。
func TestVertexaiQueryEdgeCases(t *testing.T) {
	up := &vertexaiFake{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"foo":1}`)) }}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newVertexaiEnv(t, nil)
	snap := env.snapshot(srv.URL, vertexaiSettings(srv.URL))
	query := func(id string, state json.RawMessage) error {
		_, err := env.exec.Query(context.Background(), snap, provider.TaskRef{ID: 1, UserID: 9, ProviderTaskID: id, State: state})
		return err
	}
	if query(vertexaiOpName, nil) == nil {
		t.Fatal("没有 token 应失败")
	}
	if query(vertexaiOpName, vertexaiState(env, env.nowSec-10)) == nil {
		t.Fatal("token 过期应失败")
	}
	if query("garbage", vertexaiState(env, env.nowSec+1800)) == nil {
		t.Fatal("operation name 不合法应失败")
	}
	if query(vertexaiOpName, vertexaiState(env, env.nowSec+1800)) == nil {
		t.Fatal("无法识别的响应应失败")
	}
	if n := len(up.all()); n != 1 {
		t.Fatalf("只有最后一个用例会发请求：%d", n)
	}
}

// HTTP 非 2xx：429 / 5xx 重试，计费问题归余额，安全拦截归审核，其它 4xx 终止。
func TestVertexaiClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"限流", 429, `{"error":{"code":429,"message":"Quota exceeded for aiplatform.googleapis.com","status":"RESOURCE_EXHAUSTED"}}`, provider.ClassRetryable},
		{"5xx", 503, `{"error":{"code":503,"message":"unavailable"}}`, provider.ClassRetryable},
		{"计费未开通", 403, `{"error":{"code":403,"message":"This API method requires billing to be enabled"}}`, provider.ClassProviderBalance},
		{"安全拦截", 400, `{"error":{"code":400,"message":"The prompt could not be submitted. It was blocked by safety filters"}}`, provider.ClassModeration},
		{"无权限", 403, `{"error":{"code":403,"message":"Permission denied on resource project"}}`, provider.ClassTerminal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(&vertexaiFake{handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}})
			defer srv.Close()
			env := newVertexaiEnv(t, nil)
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, vertexaiSettings(srv.URL)), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
			})
			if err == nil || provider.ClassOf(err) != tc.class {
				t.Fatalf("分类应是 %s：%v", tc.class, err)
			}
		})
	}
}

// 取片：done 那一轮 running + state(ready, url)（token 一并保留），下一轮带 Bearer 对 GCS 地址发 binary 请求，
// 响应由宿主落库，产物是 asset；GCS 主机取自渠道设置 gcsBaseUrl（这里指向本机假服务）。
func TestVertexaiFetchVideoAsAsset(t *testing.T) {
	up := &vertexaiFake{handler: func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/veo-out/") {
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("MP4DATA"))
			return
		}
		_, _ = w.Write([]byte(`{"name":"` + vertexaiOpName + `","done":true,"response":{"videos":[{"gcsUri":"gs://veo-out/videos/a.mp4","mimeType":"video/mp4"}]}}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	saver := &fakeSaver{}
	env := newVertexaiEnv(t, func(o *plugin.Options) { o.Saver = saver })
	settings := vertexaiSettings(srv.URL)
	settings["gcsBaseUrl"] = srv.URL
	snap := env.snapshot(srv.URL, settings)
	ref := provider.TaskRef{ID: 1, UserID: 9, ProviderTaskID: vertexaiOpName, State: vertexaiState(env, env.nowSec+1800)}

	q1, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q1.Status != provider.StatusRunning || !strings.Contains(string(q1.State), `"ready":true`) || !strings.Contains(string(q1.State), vertexaiToken) {
		t.Fatalf("第一轮应 running 并带 ready 与 token：%+v %v", q1, err)
	}
	ref.State = q1.State
	q2, err := env.exec.Query(context.Background(), snap, ref)
	if err != nil || q2.Status != provider.StatusSucceeded || len(q2.Outputs) != 1 || q2.Outputs[0].Type != provider.OutputAsset || q2.Outputs[0].AssetID != 77 {
		t.Fatalf("第二轮应是 asset 产物：%+v %v", q2, err)
	}
	reqs := up.all()
	got := reqs[len(reqs)-1]
	if got.Method != "GET" || got.Path != "/veo-out/videos/a.mp4" || got.Auth != "Bearer "+vertexaiToken {
		t.Fatalf("取片请求不符：%+v", got)
	}
	if saver.body != "MP4DATA" || saver.got.Kind != "video" {
		t.Fatalf("应按 video 落库：%+v %q", saver.got, saver.body)
	}
}
