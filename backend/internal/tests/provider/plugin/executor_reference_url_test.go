package plugin_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
)

type fileURLPreparerFunc func(context.Context, provider.TaskRef, *provider.AssetFile) (string, error)

func (f fileURLPreparerFunc) PrepareURL(ctx context.Context, task provider.TaskRef, asset *provider.AssetFile) (string, error) {
	return f(ctx, task, asset)
}

const mixedLocalRefPlugin = `
module.exports = {
  buildSubmitRequest: function(ctx) {
    var ref = ctx.input.images[0];
    return {method: "POST", path: "/run", json: {
      url: {__fileRef: ref, as: "url"},
      duplicate: {__fileRef: ref},
      inline: {__fileRef: ref, as: "dataUrl"}
    }};
  },
  parseSubmitResponse: function(ctx) { return {immediate: {status: "succeeded", outputs: [{type: "url", url: ctx.channel.baseUrl + "/o.png"}]}}; }
};`

func TestExecutorPreparesOnlyLocalURLReferences(t *testing.T) {
	const signed = "https://private.example.com/ref-cache/x?X-Amz-Signature=temporary"
	requests := make(chan map[string]string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer upstream.Close()
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("PNGDATA")}}
	var prepares atomic.Int32
	exec := execWith(mixedLocalRefPlugin, func(o *plugin.Options) {
		o.Assets = assets
		o.ReferenceURLs = fileURLPreparerFunc(func(_ context.Context, task provider.TaskRef, file *provider.AssetFile) (string, error) {
			if task.ID != 17 || task.UserID != 9 || file.Asset.ID != 5 {
				t.Errorf("准备器收到意外的任务或素材: %+v asset=%d", task, file.Asset.ID)
			}
			prepares.Add(1)
			return signed, nil
		})
	})
	submit := provider.SubmitInput{
		Task:  provider.TaskRef{ID: 17, UserID: 9},
		Input: map[string]any{"prompt": "x", "op": "i2i", "images": []uint64{5}},
	}
	ctx, trace := provider.WithTrace(t.Context())
	if _, err := exec.Submit(ctx, imageSnap(upstream.URL, mixedLocalRefPlugin), submit); err != nil {
		t.Fatal(err)
	}
	got := <-requests
	if got["url"] != signed || got["duplicate"] != signed || got["inline"] != "data:image/png;base64,UE5HREFUQQ==" {
		t.Fatalf("URL 与内联引用解析不符: %v", got)
	}
	traced, err := json.Marshal(trace.Steps())
	if err != nil || strings.Contains(string(traced), "X-Amz-Signature") || strings.Contains(string(traced), "UE5HREFUQQ==") {
		t.Fatalf("追踪不得保存回显的签名 URL 或内联内容，marshal err=%v", err)
	}
	if prepares.Load() != 1 || assets.opened.Load() != 2 {
		t.Fatalf("同一次提交应只准备一次 URL，且只为 URL 和内联各打开一次: prepares=%d opens=%d", prepares.Load(), assets.opened.Load())
	}
}

func TestExecutorLocalURLPreparationFailureStaysBeforeSubmit(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("{}"))
	}))
	defer upstream.Close()
	const multipartRef = `multipart: {fields: {url: {__fileRef: ref, as: "url"}}, parts: [{name: "file", fileRef: ref}]}`
	code := strings.Replace(mixedLocalRefPlugin,
		`json: {
      url: {__fileRef: ref, as: "url"},
      duplicate: {__fileRef: ref},
      inline: {__fileRef: ref, as: "dataUrl"}
    }`,
		multipartRef, 1)
	exec := execWith(code, func(o *plugin.Options) {
		o.Assets = &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("PNGDATA")}}
		o.ReferenceURLs = fileURLPreparerFunc(func(context.Context, provider.TaskRef, *provider.AssetFile) (string, error) {
			return "", &provider.Error{Class: provider.ClassRetryable, Code: "reference_url_timeout", Message: "临时参考素材准备超时"}
		})
	})
	_, err := exec.Submit(t.Context(), imageSnap(upstream.URL, code), provider.SubmitInput{
		Task:  provider.TaskRef{ID: 17, UserID: 9},
		Input: map[string]any{"prompt": "x", "op": "i2i", "images": []uint64{5}},
	})
	if provider.ClassOf(err) != provider.ClassRetryable || requests.Load() != 0 {
		t.Fatalf("准备失败不应发送生成请求: err=%v requests=%d", err, requests.Load())
	}
}

func TestExecutorAsyncVideoCanFetchReferenceAfterSubmit(t *testing.T) {
	const code = `
module.exports = {
  buildSubmitRequest: function(ctx) {
    return {method: "POST", path: "/submit", json: {image: {__fileRef: ctx.input.images[0], as: "url"}}};
  },
  parseSubmitResponse: function() { return {providerTaskId: "video-queued"}; }
};`
	const content = "video-reference"
	bucket := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ref-cache/video.png" || r.URL.Query().Get("X-Amz-Signature") != "test" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, content)
	}))
	defer bucket.Close()
	var submittedURL = make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		submittedURL <- body["image"]
		_, _ = io.WriteString(w, "{}")
	}))
	defer upstream.Close()
	snap := imageSnap(upstream.URL, code)
	snap.Model.Kind = modelcfg.KindVideo
	snap.Model.Capabilities.Ops = []string{modelcfg.OpI2V}
	snap.Plugin.Meta.Endpoints = map[string]pluginmeta.Endpoint{modelcfg.KindVideo: {Mode: pluginmeta.ModeAsync}}
	exec := execWith(code, func(o *plugin.Options) {
		o.Assets = &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte(content)}}
		o.ReferenceURLs = fileURLPreparerFunc(func(context.Context, provider.TaskRef, *provider.AssetFile) (string, error) {
			return bucket.URL + "/ref-cache/video.png?X-Amz-Signature=test", nil
		})
	})
	result, err := exec.Submit(t.Context(), snap, provider.SubmitInput{
		Task:  provider.TaskRef{ID: 17, UserID: 9},
		Input: map[string]any{"prompt": "x", "op": "i2v", "images": []uint64{5}},
	})
	if err != nil || result.ProviderTaskID != "video-queued" {
		t.Fatalf("视频提交未被受理：%v", err)
	}
	assertAsyncReferenceGET(t, bucket.Client(), <-submittedURL, content)
}

func assertAsyncReferenceGET(t *testing.T, client *http.Client, signed, want string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, signed, nil)
	if err != nil {
		t.Fatal("外链不可用")
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal("视频受理后的外链下载失败")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != http.StatusOK || string(body) != want {
		t.Fatalf("视频受理后的下载内容错误：HTTP %d，%d 字节", res.StatusCode, len(body))
	}
}
