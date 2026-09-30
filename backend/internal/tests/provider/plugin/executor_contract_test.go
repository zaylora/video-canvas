package plugin_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginproto"
	"video-canvas/internal/provider/pluginrunner"
)

// faultyRunner 包装真实 runner 客户端，让 Call 按需失败，用来模拟 runner 崩溃 / 不可用。
type faultyRunner struct {
	plugin.RunnerClient
	callErr error
}

func (f faultyRunner) Call(ctx context.Context, sha, hook string, args []json.RawMessage, timeout time.Duration) (*pluginproto.CallResponse, error) {
	if f.callErr != nil {
		return nil, f.callErr
	}
	return f.RunnerClient.Call(ctx, sha, hook, args, timeout)
}

func realRunner(opts pluginrunner.Options) plugin.RunnerClient {
	return plugin.NewInProcessRunnerClient(pluginrunner.NewServer(opts).Handler())
}

func execOn(runner plugin.RunnerClient, code string, mod func(*plugin.Options)) *plugin.Executor {
	opts := plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "k"}}
	if mod != nil {
		mod(&opts)
	}
	return plugin.New(opts)
}

func textSnap(baseURL, code string) *provider.Snapshot {
	return testSnapshot(baseURL, code, modelcfg.KindText, pluginmeta.Auth{Type: pluginmeta.AuthNone})
}

func submit(exec *plugin.Executor, snap *provider.Snapshot) (*provider.SubmitResult, error) {
	return exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1, UserID: 9}})
}

// runner 调用中崩溃：可重试，带 CodeRunnerCrashed，并标记为插件级失败；连不上则是 CodeRunnerUnavailable，不算插件级失败。
func TestExecutorRunnerCrashAndUnavailableClassification(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer upstream.Close()

	crashed := execOn(faultyRunner{RunnerClient: realRunner(pluginrunner.Options{}), callErr: plugin.ErrRunnerCrashed}, syncTextPlugin, nil)
	_, err := submit(crashed, textSnap(upstream.URL, syncTextPlugin))
	var pe *provider.Error
	if !errors.As(err, &pe) || pe.Class != provider.ClassRetryable || pe.Code != provider.CodeRunnerCrashed || !pe.PluginFault {
		t.Fatalf("runner 崩溃应是可重试的插件级失败：%+v", err)
	}

	down := execOn(faultyRunner{RunnerClient: realRunner(pluginrunner.Options{}), callErr: plugin.ErrRunnerUnavailable}, syncTextPlugin, nil)
	_, err = submit(down, textSnap(upstream.URL, syncTextPlugin))
	if !errors.As(err, &pe) || pe.Class != provider.ClassRetryable || pe.Code != provider.CodeRunnerUnavailable || pe.PluginFault {
		t.Fatalf("runner 不可用应是可重试且不算插件级失败：%+v", err)
	}
}

// 钩子超时被 runner 中断：宿主报插件级失败（terminal），runner 仍然健康，同一 runner 上别的插件继续可用。
func TestExecutorHookTimeoutInterrupted(t *testing.T) {
	const loopPlugin = `
module.exports = {
  buildSubmitRequest: function(ctx) { while (true) {} },
  parseSubmitResponse: function() { return {}; }
};`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer upstream.Close()
	runner := realRunner(pluginrunner.Options{})

	start := time.Now()
	loopExec := execOn(runner, loopPlugin, func(o *plugin.Options) { o.HookTimeout = 100 * time.Millisecond })
	_, err := submit(loopExec, textSnap(upstream.URL, loopPlugin))
	var pe *provider.Error
	if !errors.As(err, &pe) || pe.Class != provider.ClassTerminal || pe.Code != provider.CodePluginError || !pe.PluginFault ||
		!strings.Contains(err.Error(), "timeout") {
		t.Fatalf("死循环钩子应是 terminal 插件级失败（timeout）：%v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("死循环没有被及时中断：%v", time.Since(start))
	}
	if err := runner.Ready(context.Background()); err != nil {
		t.Fatalf("runner 应仍然健康：%v", err)
	}
	okExec := execOn(runner, syncTextPlugin, nil)
	if _, err := submit(okExec, textSnap(upstream.URL, syncTextPlugin)); err != nil {
		t.Fatalf("超时之后其他插件应可用：%v", err)
	}
}

// 请求描述指向非法主机：不发送，报插件级失败。
func TestExecutorRejectsIllegalHosts(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	cases := map[string]string{
		"不在白名单的绝对地址": `{method: "GET", url: "http://evil.example.org/steal"}`,
		"带用户名密码":     `{method: "GET", url: "http://user:pw@evil.example.org/"}`,
		"path 换主机":   `{method: "GET", path: "//evil.example.org/x"}`,
		"path 里的 @":  `{method: "GET", path: "@evil.example.org/x"}`,
		"非 http 协议":  `{method: "GET", url: "file:///etc/passwd"}`,
		"路径穿越":       `{method: "GET", path: "/a/../../b"}`,
	}
	for name, desc := range cases {
		t.Run(name, func(t *testing.T) {
			code := `module.exports = {
  buildSubmitRequest: function() { return ` + desc + `; },
  parseSubmitResponse: function() { return {}; }
};`
			exec := execWith(code, nil)
			_, err := submit(exec, textSnap(upstream.URL, code))
			var pe *provider.Error
			if !errors.As(err, &pe) || pe.Class != provider.ClassTerminal || !pe.PluginFault {
				t.Fatalf("应报插件级失败：%v", err)
			}
		})
	}
	if hits.Load() != 0 {
		t.Fatalf("非法请求不应到达上游，实际 %d 次", hits.Load())
	}
}

// 结果结构不合规：一律是 terminal 插件级失败。
func TestExecutorRejectsInvalidResults(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer upstream.Close()

	submitCases := map[string]string{
		"sync 没有 immediate":        `{}`,
		"status 不合法":               `{immediate: {status: "weird"}}`,
		"succeeded 没有产物":           `{immediate: {status: "succeeded", outputs: []}}`,
		"failed 没有 error":          `{immediate: {status: "failed"}}`,
		"failed 的 class 不合法":       `{immediate: {status: "failed", error: {class: "nope", message: "x"}}}`,
		"immediate 是 running":      `{immediate: {status: "running"}}`,
		"产物 type 不合法":              `{immediate: {status: "succeeded", outputs: [{type: "blob"}]}}`,
		"文本模型的 url 产物":             `{immediate: {status: "succeeded", outputs: [{type: "url", url: "http://x/y"}]}}`,
		"state 超过 64KB":            `{immediate: {status: "succeeded", outputs: [{type: "text", text: "x"}]}, state: {blob: new Array(70000).join("a")}}`,
		"返回值不能编码成 JSON（undefined）": `undefined`,
	}
	for name, ret := range submitCases {
		t.Run(name, func(t *testing.T) {
			code := `module.exports = {
  buildSubmitRequest: function() { return {method: "GET", path: "/x"}; },
  parseSubmitResponse: function() { return ` + ret + `; }
};`
			_, err := submit(execWith(code, nil), textSnap(upstream.URL, code))
			var pe *provider.Error
			if !errors.As(err, &pe) || pe.Class != provider.ClassTerminal || !pe.PluginFault {
				t.Fatalf("应报 terminal 插件级失败：%v", err)
			}
		})
	}

	t.Run("异步没有 providerTaskId", func(t *testing.T) {
		code := `module.exports = {
  buildSubmitRequest: function() { return {method: "GET", path: "/x"}; },
  parseSubmitResponse: function() { return {}; },
  buildQueryRequest: function() { return {method: "GET", path: "/q"}; },
  parseQueryResponse: function() { return {}; }
};`
		snap := testSnapshot(upstream.URL, code, "video", pluginmeta.Auth{Type: pluginmeta.AuthNone})
		snap.Plugin.Meta.Endpoints["video"] = pluginmeta.Endpoint{Mode: pluginmeta.ModeAsync}
		_, err := submit(execWith(code, nil), snap)
		var pe *provider.Error
		if !errors.As(err, &pe) || pe.Class != provider.ClassTerminal || !pe.PluginFault {
			t.Fatalf("应报 terminal 插件级失败：%v", err)
		}
	})

	t.Run("查询结果 status 缺失", func(t *testing.T) {
		code := `module.exports = {
  buildQueryRequest: function() { return {method: "GET", path: "/q"}; },
  parseQueryResponse: function() { return {progress: 10}; }
};`
		snap := testSnapshot(upstream.URL, code, "video", pluginmeta.Auth{Type: pluginmeta.AuthNone})
		snap.Plugin.Meta.Endpoints["video"] = pluginmeta.Endpoint{Mode: pluginmeta.ModeAsync}
		_, err := execWith(code, nil).Query(context.Background(), snap, provider.TaskRef{ID: 1, ProviderTaskID: "p"})
		var pe *provider.Error
		if !errors.As(err, &pe) || pe.Class != provider.ClassTerminal || !pe.PluginFault {
			t.Fatalf("应报 terminal 插件级失败：%v", err)
		}
	})
}

// state 往返：提交返回的 state 由 worker 保存，查询时经 ctx.task.state 原样交回，查询返回的新 state 同样带回。
func TestExecutorStateRoundTrip(t *testing.T) {
	const code = `
module.exports = {
  buildSubmitRequest: function(ctx) { return {method: "POST", path: "/submit", json: {}}; },
  parseSubmitResponse: function(ctx, resp) { return {providerTaskId: "p1", state: {cursor: 3, tag: "a"}}; },
  buildQueryRequest: function(ctx) {
    return {method: "GET", path: "/q/" + ctx.task.state.cursor + "/" + ctx.task.providerTaskId};
  },
  parseQueryResponse: function(ctx, resp) {
    return {status: "running", progress: 40, state: {cursor: ctx.task.state.cursor + 1, tag: ctx.task.state.tag}};
  }
};`
	var mu sync.Mutex
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	exec := execWith(code, nil)
	snap := testSnapshot(upstream.URL, code, "video", pluginmeta.Auth{Type: pluginmeta.AuthNone})
	snap.Plugin.Meta.Endpoints["video"] = pluginmeta.Endpoint{Mode: pluginmeta.ModeAsync}

	sub, err := submit(exec, snap)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(sub.State, `{"cursor":3,"tag":"a"}`) {
		t.Fatalf("提交返回的 state 不符：%s", sub.State)
	}
	q, err := exec.Query(context.Background(), snap, provider.TaskRef{ID: 1, ProviderTaskID: sub.ProviderTaskID, State: sub.State})
	if err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(q.State, `{"cursor":4,"tag":"a"}`) || q.Status != provider.StatusRunning {
		t.Fatalf("查询返回的 state 不符：%+v", q)
	}
	// 下一轮用新的 state 继续
	if _, err := exec.Query(context.Background(), snap, provider.TaskRef{ID: 1, ProviderTaskID: "p1", State: q.State}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"/submit", "/q/3/p1", "/q/4/p1"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("上游收到的路径 %v，期望 %v", paths, want)
	}
}

func jsonEqual(raw json.RawMessage, want string) bool {
	var a, b any
	return json.Unmarshal(raw, &a) == nil && json.Unmarshal([]byte(want), &b) == nil && jsonString(a) == jsonString(b)
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// fakeAssets 是只认一个用户的素材库；记录被打开的素材，用来验证文件引用的归属校验。
type fakeAssets struct {
	userID uint64
	files  map[uint64][]byte
	opened atomic.Int32
}

func (f *fakeAssets) Get(_ context.Context, userID, id uint64) (*model.Asset, error) {
	if _, ok := f.files[id]; !ok || userID != f.userID {
		return nil, provider.ErrAssetNotFound
	}
	return &model.Asset{ID: id, UserID: userID, Kind: "image", MimeType: "image/png", FileName: "a.png"}, nil
}

func (f *fakeAssets) Open(ctx context.Context, userID, id uint64) (*provider.AssetFile, error) {
	a, err := f.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	f.opened.Add(1)
	return &provider.AssetFile{Asset: a, Body: io.NopCloser(bytes.NewReader(f.files[id])), URL: "https://cdn.example.com/a.png"}, nil
}

func imageSnap(baseURL, code string) *provider.Snapshot {
	snap := textSnap(baseURL, code)
	snap.Model.InputSchema = modelcfg.InputSchema{
		{Name: "image", InputField: modelcfg.InputField{Type: modelcfg.FieldImage, Label: "图"}},
	}
	return snap
}

// 文件引用：url / base64 / dataUrl 三种形态由宿主注入；插件看到的只是 "input:image"，永远拿不到素材 ID。
func TestExecutorFileRefs(t *testing.T) {
	const code = `
module.exports = {
  buildSubmitRequest: function(ctx) {
    if (ctx.input.image !== "input:image") { throw new Error("插件应只看到文件引用：" + ctx.input.image); }
    return {method: "POST", path: "/run", json: {
      u: {__fileRef: ctx.input.image},
      b: {__fileRef: ctx.input.image, as: "base64"},
      d: {__fileRef: ctx.input.image, as: "dataUrl"},
      list: [{__fileRef: ctx.input.image, as: "url"}]
    }};
  },
  parseSubmitResponse: function() { return {immediate: {status: "succeeded", outputs: [{type: "text", text: "ok"}]}}; }
};`
	var got map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("PNGDATA")}}
	exec := execWith(code, func(o *plugin.Options) { o.Assets = assets })
	_, err := exec.Submit(context.Background(), imageSnap(upstream.URL, code), provider.SubmitInput{
		Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"image": uint64(5)},
	})
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString([]byte("PNGDATA"))
	if got["u"] != "https://cdn.example.com/a.png" || got["b"] != b64 || got["d"] != "data:image/png;base64,"+b64 {
		t.Fatalf("文件引用注入不符：%v", got)
	}
	if list, _ := got["list"].([]any); len(list) != 1 || list[0] != "https://cdn.example.com/a.png" {
		t.Fatalf("数组里的文件引用注入不符：%v", got["list"])
	}
}

// 文件引用的安全边界：只能引用任务输入里已填写的媒体字段；别人的素材打不开；as 非法被拒。
func TestExecutorFileRefBoundaries(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	build := func(ref string) string {
		return `module.exports = {
  buildSubmitRequest: function(ctx) { return {method: "POST", path: "/run", json: {x: ` + ref + `}}; },
  parseSubmitResponse: function() { return {immediate: {status: "succeeded", outputs: [{type: "text", text: "ok"}]}}; }
};`
	}
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("x"), 6: []byte("y")}}

	cases := []struct {
		name  string
		ref   string
		input map[string]any
		user  uint64
		fault bool // 期望插件级失败；否则期望素材不可用（宿主 terminal，非插件失败）
	}{
		{"引用不存在的字段", `{__fileRef: "input:secret"}`, map[string]any{"image": uint64(5)}, 9, true},
		{"引用没填写的字段", `{__fileRef: "input:image"}`, map[string]any{}, 9, true},
		{"没有 input: 前缀", `{__fileRef: "image"}`, map[string]any{"image": uint64(5)}, 9, true},
		{"as 不合法", `{__fileRef: "input:image", as: "raw"}`, map[string]any{"image": uint64(5)}, 9, true},
		{"素材不属于该用户", `{__fileRef: "input:image"}`, map[string]any{"image": uint64(5)}, 77, false},
		{"JSON 解码的 float64 素材 id 同样是媒体字段", `{__fileRef: "input:image"}`, map[string]any{"image": float64(5)}, 77, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := build(tc.ref)
			exec := execWith(code, func(o *plugin.Options) { o.Assets = assets })
			_, err := exec.Submit(context.Background(), imageSnap(upstream.URL, code), provider.SubmitInput{
				Task: provider.TaskRef{ID: 1, UserID: tc.user}, Input: tc.input,
			})
			var pe *provider.Error
			if !errors.As(err, &pe) || pe.Class != provider.ClassTerminal {
				t.Fatalf("应报 terminal 错误：%v", err)
			}
			if pe.PluginFault != tc.fault {
				t.Fatalf("PluginFault=%v，期望 %v：%v", pe.PluginFault, tc.fault, err)
			}
		})
	}
	if hits.Load() != 0 {
		t.Fatalf("文件引用不合规的请求不应发到上游，实际 %d 次", hits.Load())
	}
}
