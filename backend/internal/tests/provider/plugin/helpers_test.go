package plugin_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// promptCaps 是只有提示词的最小能力（文本、语音、视频/图片的查询测试用）。
var promptCaps = modelcfg.Capabilities{Prompt: modelcfg.PromptSpec{MaxLength: 10000}}

// fakeNewAPI 是通用的假上游：按 JSON 请求体回调 handler，并记录每次请求（方法、路径、Authorization、请求体）。
type fakeNewAPI struct {
	mu       sync.Mutex
	requests []recordedRequest
	handler  func(w http.ResponseWriter, r *http.Request, body map[string]any)
}

type recordedRequest struct {
	Method, Path, Auth string
	Body               map[string]any
}

func (f *fakeNewAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.requests = append(f.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Body: body})
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	f.handler(w, r, body)
}

func (f *fakeNewAPI) last() recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

// fakeSaver 模拟宿主的素材存储：读完 binary 响应体并记录，返回固定的素材。
type fakeSaver struct {
	got  provider.SaveGeneratedInput
	body string
}

func (f *fakeSaver) SaveGenerated(_ context.Context, in provider.SaveGeneratedInput) (*model.Asset, string, error) {
	raw, _ := io.ReadAll(in.Body)
	f.got, f.body = in, string(raw)
	return &model.Asset{ID: 77, MimeType: "audio/mpeg", ByteSize: int64(len(raw))}, "https://files.local/77.mp3", nil
}
