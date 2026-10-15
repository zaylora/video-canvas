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
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/plugins"
)

// alibabaimageEnv 用真实 runner 预检并装载内置 alibaba-image.js，元数据取自预检结果。
type alibabaimageEnv struct {
	exec *plugin.Executor
	meta *pluginmeta.Meta
	sha  string
}

func newAlibabaImageEnv(t *testing.T, mod func(*plugin.Options)) *alibabaimageEnv {
	t.Helper()
	src, err := plugins.Source("alibaba-image")
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
		t.Fatalf("内置 alibaba-image.js 预检没有通过：%+v", pre.Issues)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	opts := plugin.Options{Runner: runner, Codes: codeStore{"code": code}, Secrets: secretResolver{value: "sk-ali"}, IPAllowed: allowLoopback}
	if mod != nil {
		mod(&opts)
	}
	return &alibabaimageEnv{exec: plugin.New(opts), meta: meta, sha: pre.SHA256}
}

func (e *alibabaimageEnv) snapshot(baseURL, kind, upstream string, caps modelcfg.Capabilities) *provider.Snapshot {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "m", Kind: kind, UpstreamModel: upstream, Capabilities: caps},
		Channel: provider.ChannelSnapshot{
			Key: "ali-main", PluginKey: "alibaba-image", PluginVersionID: 1, BaseURL: baseURL,
		},
		Plugin:          provider.PluginSnapshot{Key: "alibaba-image", Version: e.meta.Version, SHA256: e.sha, Meta: *e.meta},
		ModelRevisionID: 1,
	}
}

// alibabaimageCaps 打开提示词、参考图 / 视频 / 音频与常用生成参数，输入里的字段才不会被宿主丢掉。
func alibabaimageCaps() modelcfg.Capabilities {
	return modelcfg.Capabilities{
		Ops:    []string{modelcfg.OpT2V, modelcfg.OpI2V, modelcfg.OpOmni, modelcfg.OpT2I, modelcfg.OpI2I},
		Refs:   modelcfg.Refs{Image: modelcfg.RefSpec{On: true, Max: 10, MaxMB: 10}},
		Prompt: modelcfg.PromptSpec{MaxLength: 10000},
	}
}

type alibabaimageReq struct {
	Method, Path, Auth, Async string
	Body                      map[string]any
}

type alibabaimageUp struct {
	mu      sync.Mutex
	reqs    []alibabaimageReq
	handler func(w http.ResponseWriter, r *http.Request)
}

func (u *alibabaimageUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	u.mu.Lock()
	u.reqs = append(u.reqs, alibabaimageReq{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-DashScope-Async"), body})
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	u.handler(w, r)
}

func (u *alibabaimageUp) last() alibabaimageReq {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.reqs[len(u.reqs)-1]
}

func alibabaimageSubmit(t *testing.T, env *alibabaimageEnv, snap *provider.Snapshot, input map[string]any) (*provider.SubmitResult, error) {
	t.Helper()
	return env.exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1, UserID: 9}, Input: input})
}

func alibabaimageSub(m map[string]any, keys ...string) map[string]any {
	cur := m
	for _, k := range keys {
		next, _ := cur[k].(map[string]any)
		if next == nil {
			return map[string]any{}
		}
		cur = next
	}
	return cur
}

func alibabaimageStatic(body string) *httptest.Server {
	return httptest.NewServer(&alibabaimageUp{handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }})
}

// 图片（万相 2.7）：异步 image-generation，input.messages 里先放参考图再放文字；aspect_ratio + 1K 推算 size；结果在 output.choices。
func TestAlibabaImageImageWan27Async(t *testing.T) {
	var polls int
	up := &alibabaimageUp{}
	up.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"output":{"task_id":"img-1","task_status":"PENDING"}}`))
			return
		}
		polls++
		_, _ = w.Write([]byte(`{"output":{"task_status":"SUCCEEDED","choices":[{"message":{"content":[{"type":"image","image":"https://cdn.example.com/1.png?Expires=1"},{"image":"https://cdn.example.com/2.jpg"}]}}]}}`))
	}
	srv := httptest.NewServer(up)
	defer srv.Close()
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("a")}}
	env := newAlibabaImageEnv(t, func(o *plugin.Options) { o.Assets = assets })
	snap := env.snapshot(srv.URL, "image", "wan2.7-image", alibabaimageCaps())
	sub, err := alibabaimageSubmit(t, env, snap, map[string]any{
		"prompt": "改成水彩", "op": "i2i", "images": []uint64{5}, "count": float64(2), "aspect_ratio": "16:9", "resolution": "1K",
	})
	if err != nil || sub.ProviderTaskID != "img-1" {
		t.Fatalf("提交结果不符：%+v %v", sub, err)
	}
	got := up.last()
	if got.Path != "/api/v1/services/aigc/image-generation/generation" || got.Async != "enable" {
		t.Fatalf("请求不符：%+v", got)
	}
	msgs, _ := alibabaimageSub(got.Body, "input")["messages"].([]any)
	content, _ := msgs[0].(map[string]any)["content"].([]any)
	if len(content) != 2 || content[0].(map[string]any)["image"] != "https://cdn.example.com/a.png" || content[1].(map[string]any)["text"] != "改成水彩" {
		t.Fatalf("messages 不符：%v", msgs)
	}
	par := alibabaimageSub(got.Body, "parameters")
	if par["n"] != float64(2) || par["size"] != "1360*760" {
		t.Fatalf("parameters 不符：%v", par)
	}
	q, err := env.exec.Query(context.Background(), snap, provider.TaskRef{ID: 1, ProviderTaskID: "img-1"})
	if err != nil || q.Status != provider.StatusSucceeded || len(q.Outputs) != 2 {
		t.Fatalf("应出两张图：%+v %v", q, err)
	}
	if q.Outputs[0].MediaType != "image" || q.Outputs[0].Mime != "image/png" || q.Outputs[1].Mime != "image/jpeg" {
		t.Fatalf("产物不符：%+v", q.Outputs)
	}
	// wan2.6-image 没有参考图是插件级失败
	_, err = alibabaimageSubmit(t, env, env.snapshot(srv.URL, "image", "wan2.6-image", alibabaimageCaps()), map[string]any{"prompt": "x"})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("缺参考图应是插件级失败：%v", err)
	}
}

// 图片（旧文生图）：text2image/image-synthesis，prompt 在 input，结果在 output.results[].url；也可以预设 1K/2K 之外的分辨率直接给 size。
func TestAlibabaImageImageLegacy(t *testing.T) {
	var polled bool
	up := &alibabaimageUp{}
	up.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"output":{"task_id":"img-2","task_status":"PENDING"}}`))
			return
		}
		polled = true
		_, _ = w.Write([]byte(`{"output":{"task_status":"SUCCEEDED","results":[{"url":"https://cdn.example.com/a.png"},{"code":"DataInspectionFailed","message":"x"}]}}`))
	}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newAlibabaImageEnv(t, nil)
	snap := env.snapshot(srv.URL, "image", "wanx2.1-t2i-turbo", alibabaimageCaps())
	if _, err := alibabaimageSubmit(t, env, snap, map[string]any{"prompt": "山水", "size": "1024x1024", "negative_prompt": "模糊", "watermark": false}); err != nil {
		t.Fatal(err)
	}
	got := up.last()
	in, par := alibabaimageSub(got.Body, "input"), alibabaimageSub(got.Body, "parameters")
	if got.Path != "/api/v1/services/aigc/text2image/image-synthesis" || got.Async != "enable" ||
		in["prompt"] != "山水" || in["negative_prompt"] != "模糊" || par["size"] != "1024*1024" || par["n"] != float64(1) || par["watermark"] != false {
		t.Fatalf("请求不符：%+v", got)
	}
	q, err := env.exec.Query(context.Background(), snap, provider.TaskRef{ID: 1, ProviderTaskID: "img-2"})
	if err != nil || !polled || q.Status != provider.StatusSucceeded || len(q.Outputs) != 1 {
		t.Fatalf("应只取有地址的那张：%+v %v", q, err)
	}
	// 成功但一张图都没有：失败
	none := alibabaimageStatic(`{"output":{"task_status":"SUCCEEDED","results":[]}}`)
	defer none.Close()
	q, err = env.exec.Query(context.Background(), env.snapshot(none.URL, "image", "wanx2.1-t2i-turbo", alibabaimageCaps()), provider.TaskRef{ID: 1, ProviderTaskID: "x"})
	if err != nil || q.Status != provider.StatusFailed {
		t.Fatalf("没有图片应判失败：%+v %v", q, err)
	}
}

// 图片（千问图像）：同步 multimodal-generation，不带异步头；提交响应直接出结果；n 超过模型上限是插件级失败。
func TestAlibabaImageImageQwenSync(t *testing.T) {
	up := &alibabaimageUp{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":[{"image":"https://cdn.example.com/q.png"}]}}]},"usage":{"image_count":1}}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	env := newAlibabaImageEnv(t, nil)
	snap := env.snapshot(srv.URL, "image", "qwen-image-2.0-pro-2026-03-03", alibabaimageCaps())
	sub, err := alibabaimageSubmit(t, env, snap, map[string]any{"prompt": "一只猫", "aspect_ratio": "1:1", "resolution": "1K"})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Immediate == nil || sub.Immediate.Status != provider.StatusSucceeded || len(sub.Immediate.Outputs) != 1 || sub.Immediate.Outputs[0].URL != "https://cdn.example.com/q.png" {
		t.Fatalf("应同步出图：%+v", sub)
	}
	got := up.last()
	if got.Path != "/api/v1/services/aigc/multimodal-generation/generation" || got.Async != "" || got.Auth != "Bearer sk-ali" ||
		alibabaimageSub(got.Body, "parameters")["size"] != "1024*1024" || got.Body["model"] != "qwen-image-2.0-pro-2026-03-03" {
		t.Fatalf("请求不符：%+v", got)
	}
	_, err = alibabaimageSubmit(t, env, env.snapshot(srv.URL, "image", "qwen-image", alibabaimageCaps()), map[string]any{"prompt": "x", "count": float64(2)})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("n 超限应是插件级失败：%v", err)
	}
	// 同步响应里没有图片：failed 结果；带错误码的 200 响应按审核分类
	fail := alibabaimageStatic(`{"code":"DataInspectionFailed","message":"inappropriate content"}`)
	defer fail.Close()
	sub, err = alibabaimageSubmit(t, env, env.snapshot(fail.URL, "image", "qwen-image", alibabaimageCaps()), map[string]any{"prompt": "x"})
	if err != nil || sub.Immediate == nil || sub.Immediate.Status != provider.StatusFailed || sub.Immediate.ErrorClass != provider.ClassModeration {
		t.Fatalf("应翻译成 moderation 失败：%+v %v", sub, err)
	}
}

// 编辑模型 qwen-image-edit 没有参考图是插件级失败；有图时带在 messages 里。
func TestAlibabaImageImageQwenEdit(t *testing.T) {
	up := &alibabaimageUp{handler: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"choices":[{"message":{"content":[{"image":"https://cdn.example.com/e.png"}]}}]}}`))
	}}
	srv := httptest.NewServer(up)
	defer srv.Close()
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("a")}}
	env := newAlibabaImageEnv(t, func(o *plugin.Options) { o.Assets = assets })
	snap := env.snapshot(srv.URL, "image", "qwen-image-edit", alibabaimageCaps())
	_, err := alibabaimageSubmit(t, env, snap, map[string]any{"prompt": "x"})
	var pe *provider.Error
	if !errors.As(err, &pe) || !pe.PluginFault {
		t.Fatalf("缺参考图应是插件级失败：%v", err)
	}
	if _, err = alibabaimageSubmit(t, env, snap, map[string]any{"prompt": "换背景", "op": "i2i", "images": []uint64{5}}); err != nil {
		t.Fatal(err)
	}
	msgs, _ := alibabaimageSub(up.last().Body, "input")["messages"].([]any)
	content, _ := msgs[0].(map[string]any)["content"].([]any)
	if len(content) != 2 || content[0].(map[string]any)["image"] == nil {
		t.Fatalf("messages 不符：%v", msgs)
	}
}
