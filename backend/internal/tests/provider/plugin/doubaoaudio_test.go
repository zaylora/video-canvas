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

// doubaoaudioEnv 用真实 runner 预检并装载内置 doubao-audio.js，元数据取自预检结果。
type doubaoaudioEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newDoubaoaudioEnv(t *testing.T) *doubaoaudioEnv {
	t.Helper()
	src, err := plugins.Source("doubao-audio")
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
		t.Fatalf("内置 doubao-audio.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{1: []byte("a"), 2: []byte("b"), 3: []byte("c")}}
	exec := plugin.New(plugin.Options{
		Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "speech-key"},
		Assets: assets, IPAllowed: allowLoopback,
	})
	return &doubaoaudioEnv{exec: exec, meta: meta, sha: pre.SHA256}
}

func (e *doubaoaudioEnv) snapshot(baseURL, upstream string) *provider.Snapshot {
	return &provider.Snapshot{
		Model:   provider.ModelSnapshot{Key: "m", Kind: "audio", UpstreamModel: upstream, Capabilities: promptCaps},
		Channel: provider.ChannelSnapshot{Key: "doubao-audio-main", PluginKey: "doubao-audio", PluginVersionID: 1, BaseURL: baseURL},
		Plugin:  provider.PluginSnapshot{Key: "doubao-audio", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},

		ModelRevisionID: 1,
	}
}

// doubaoaudioUpstream 是假的豆包语音上游：记录请求方法、路径、X-Api-Key 头和请求体。
type doubaoaudioUpstream struct {
	mu      sync.Mutex
	reqs    []doubaoaudioReq
	handler func(w http.ResponseWriter, r *http.Request)
}

type doubaoaudioReq struct {
	Method, Path, APIKey, Auth string
	Body                       map[string]any
}

func (u *doubaoaudioUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	u.mu.Lock()
	u.reqs = append(u.reqs, doubaoaudioReq{r.Method, r.URL.Path, r.Header.Get("X-Api-Key"), r.Header.Get("Authorization"), body})
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	u.handler(w, r)
}

func (u *doubaoaudioUpstream) last() doubaoaudioReq {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.reqs[len(u.reqs)-1]
}

func doubaoaudioServer(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *doubaoaudioUpstream) {
	t.Helper()
	up := &doubaoaudioUpstream{handler: h}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	return srv, up
}

const doubaoaudioOK = `{"audio":"QUJD","duration":3.2,"original_duration":3.2,"url":"https://cdn.example.com/a.mp3"}`

func TestDoubaoaudioPrecheckAndMeta(t *testing.T) {
	env := newDoubaoaudioEnv(t)
	if env.meta.Key != "doubao-audio" || env.meta.Version != "1.1.0" || env.meta.Auth.Type != pluginmeta.AuthHeader || env.meta.Auth.Name != "X-Api-Key" {
		t.Fatalf("meta 不符合预期（鉴权应是 X-Api-Key 头）：%+v", env.meta)
	}
	if ep, ok := env.meta.Endpoint("audio"); !ok || ep.Mode != pluginmeta.ModeSync || ep.Path != "/api/v3/tts/create" {
		t.Fatalf("audio 应是 sync 且路径是 /api/v3/tts/create：%+v", ep)
	}
	if _, ok := env.meta.Endpoint("video"); ok {
		t.Fatalf("不应声明 video：%+v", env.meta.Endpoints)
	}
}

// 纯文本：POST /api/v3/tts/create，X-Api-Key 注入、不带 Authorization；text_prompt 取提示词，
// model 取上游模型名，audio_config 只带用户给的字段，返回的 url 变成 audio 产物。
func TestDoubaoaudioTextFlow(t *testing.T) {
	srv, up := doubaoaudioServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(doubaoaudioOK)) })
	env := newDoubaoaudioEnv(t)
	sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "seed-audio-1.0"), provider.SubmitInput{
		Task:  provider.TaskRef{ID: 1, UserID: 9},
		Input: map[string]any{"prompt": "一声手机震动，鸟鸣声", "format": "mp3", "sample_rate": 48000, "speech_rate": 10, "loudness_rate": -5, "pitch_rate": 2},
	})
	if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusSucceeded {
		t.Fatalf("同步接口应直接成功：%+v %v", sub, err)
	}
	got := up.last()
	if got.Method != "POST" || got.Path != "/api/v3/tts/create" || got.APIKey != "speech-key" || got.Auth != "" {
		t.Fatalf("请求不符（应带 X-Api-Key、不带 Authorization）：%+v", got)
	}
	b := got.Body
	if b["model"] != "seed-audio-1.0" || b["text_prompt"] != "一声手机震动，鸟鸣声" {
		t.Fatalf("model / text_prompt 不符：%v", b)
	}
	cfg, _ := b["audio_config"].(map[string]any)
	if cfg["format"] != "mp3" || cfg["sample_rate"] != float64(48000) || cfg["speech_rate"] != float64(10) ||
		cfg["loudness_rate"] != float64(-5) || cfg["pitch_rate"] != float64(2) {
		t.Fatalf("audio_config 不符：%v", cfg)
	}
	if _, has := b["references"]; has {
		t.Fatalf("没有参考素材和音色时不应带 references：%v", b)
	}
	outs := sub.Immediate.Outputs
	if len(outs) != 1 || outs[0].Type != provider.OutputURL || outs[0].URL != "https://cdn.example.com/a.mp3" ||
		outs[0].MediaType != "audio" || outs[0].Mime != "audio/mpeg" {
		t.Fatalf("产物不符：%+v", outs)
	}
}

// 默认值：上游模型名为空时用 seed-audio-1.0；不传 format 时按 mp3 出（浏览器能直接播），产物 mime 跟着变；
// 输入里没有的数值字段不会被补成 0。
func TestDoubaoaudioDefaultsAndMime(t *testing.T) {
	srv, up := doubaoaudioServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(doubaoaudioOK)) })
	env := newDoubaoaudioEnv(t)
	sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, ""), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "你好"},
	})
	if err != nil || sub.Immediate == nil {
		t.Fatalf("应成功：%+v %v", sub, err)
	}
	b := up.last().Body
	cfg, _ := b["audio_config"].(map[string]any)
	if b["model"] != "seed-audio-1.0" || cfg["format"] != "mp3" {
		t.Fatalf("默认模型与格式不符：%v", b)
	}
	for _, k := range []string{"sample_rate", "speech_rate", "loudness_rate", "pitch_rate"} {
		if _, has := cfg[k]; has {
			t.Fatalf("没填的 %s 不应出现在 audio_config：%v", k, cfg)
		}
	}

	for format, mime := range map[string]string{"wav": "audio/wav", "ogg_opus": "audio/ogg", "mp3": "audio/mpeg"} {
		sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, ""), provider.SubmitInput{
			Task: provider.TaskRef{ID: 2, UserID: 9}, Input: map[string]any{"prompt": "x", "format": format},
		})
		if err != nil || sub.Immediate == nil || len(sub.Immediate.Outputs) != 1 || sub.Immediate.Outputs[0].Mime != mime {
			t.Fatalf("%s 的 mime 应是 %s：%+v %v", format, mime, sub, err)
		}
	}
}

// 参考素材：音色 speaker 与参考音频各占 references 的一项（互斥是单项内部的约束）；参考音频按顺序换成签名 URL；
// 单独一张参考图走 image_url。
func TestDoubaoaudioReferences(t *testing.T) {
	srv, up := doubaoaudioServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(doubaoaudioOK)) })
	env := newDoubaoaudioEnv(t)
	submit := func(input map[string]any) (map[string]any, error) {
		sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "seed-audio-1.0"), provider.SubmitInput{
			Task: provider.TaskRef{ID: 3, UserID: 9}, Input: input,
		})
		if err != nil {
			return nil, err
		}
		if sub.Immediate == nil || sub.Immediate.Status != provider.StatusSucceeded {
			t.Fatalf("应成功：%+v", sub)
		}
		return up.last().Body, nil
	}

	b, err := submit(map[string]any{"prompt": "@音频1 说：你好", "speaker": "zh_female_x", "audios": []uint64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	refs, _ := b["references"].([]any)
	if len(refs) != 3 {
		t.Fatalf("应有 1 个音色 + 2 段参考音频：%v", b["references"])
	}
	if r0, _ := refs[0].(map[string]any); r0["speaker"] != "zh_female_x" || len(r0) != 1 {
		t.Fatalf("音色项不符：%v", refs[0])
	}
	for _, i := range []int{1, 2} {
		if r, _ := refs[i].(map[string]any); r["audio_url"] != "https://cdn.example.com/a.png" || len(r) != 1 {
			t.Fatalf("参考音频项 %d 不符：%v", i, refs[i])
		}
	}

	b, err = submit(map[string]any{"prompt": "读出这张图的感觉", "images": []uint64{1}})
	if err != nil {
		t.Fatal(err)
	}
	refs, _ = b["references"].([]any)
	if len(refs) != 1 {
		t.Fatalf("应只有 1 项图片参考：%v", b["references"])
	}
	if r, _ := refs[0].(map[string]any); r["image_url"] != "https://cdn.example.com/a.png" || len(r) != 1 {
		t.Fatalf("图片参考项不符：%v", refs[0])
	}

	// 上游规定图片参考不能和音频参考（含音色）混用：本地直接说清楚，不发请求。
	before := len(up.reqs)
	_, err = submit(map[string]any{"prompt": "x", "images": []uint64{1}, "speaker": "zh_female_x"})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("图片与音色混用应是插件级失败：%v", err)
	}
	if len(up.reqs) != before {
		t.Fatalf("混用时不应发出请求")
	}
}

// 提示词为空：宿主按模型能力先拦下（任务输入不符合 capabilities），不发请求。
func TestDoubaoaudioEmptyPrompt(t *testing.T) {
	srv, up := doubaoaudioServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(doubaoaudioOK)) })
	env := newDoubaoaudioEnv(t)
	_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "seed-audio-1.0"), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "   "},
	})
	if err == nil {
		t.Fatalf("空提示词应被拒绝")
	}
	if len(up.reqs) != 0 {
		t.Fatalf("不应发出请求")
	}
}

// 200 但内容不对：带非成功 code、没有 url（只有 base64，本契约不支持内联产物）都是失败；
// 错误文案里的审核、余额关键字翻译成对应分类。
func TestDoubaoaudioBadResponses(t *testing.T) {
	cases := []struct {
		name, body string
		class      provider.ErrorClass
	}{
		{"审核", `{"code":45000001,"message":"content violates policy, sensitive"}`, provider.ClassModeration},
		{"余额", `{"code":45000002,"message":"insufficient balance"}`, provider.ClassProviderBalance},
		{"普通错误", `{"code":45000003,"message":"bad parameter"}`, provider.ClassTerminal},
		{"没有地址", `{"audio":"QUJD","duration":1}`, provider.ClassTerminal},
		{"空响应", `{}`, provider.ClassTerminal},
	}
	env := newDoubaoaudioEnv(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := doubaoaudioServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(c.body)) })
			sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "seed-audio-1.0"), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
			})
			if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusFailed || sub.Immediate.ErrorClass != c.class {
				t.Fatalf("应失败且分类为 %s：%+v %v", c.class, sub, err)
			}
		})
	}

	// code 为 0 / 20000000 都算成功码（火山通用约定），不能误判成失败
	for _, ok := range []string{`{"code":0,"url":"https://cdn.example.com/a.mp3"}`, `{"code":20000000,"url":"https://cdn.example.com/a.mp3"}`} {
		srv, _ := doubaoaudioServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(ok)) })
		sub, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "seed-audio-1.0"), provider.SubmitInput{
			Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
		})
		if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusSucceeded {
			t.Fatalf("成功码不应判失败：%s %+v %v", ok, sub, err)
		}
	}
}

func TestDoubaoaudioClassifyHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  provider.ErrorClass
	}{
		{"余额不足", 402, `{"code":45000010,"message":"insufficient balance"}`, provider.ClassProviderBalance},
		{"审核拒绝", 400, `{"code":45000011,"message":"sensitive content"}`, provider.ClassModeration},
		{"限流", 429, `{"code":45000292,"message":"too many requests"}`, provider.ClassRetryable},
		{"5xx", 502, `bad gateway`, provider.ClassRetryable},
		{"其它 4xx", 400, `{"code":45000000,"message":"invalid param"}`, provider.ClassTerminal},
	}
	env := newDoubaoaudioEnv(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := doubaoaudioServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			})
			_, err := env.exec.Submit(context.Background(), env.snapshot(srv.URL, "seed-audio-1.0"), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "x"},
			})
			if err == nil || provider.ClassOf(err) != c.class {
				t.Fatalf("分类应是 %s：%v", c.class, err)
			}
		})
	}
}
