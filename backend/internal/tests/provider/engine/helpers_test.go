package engine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	. "video-canvas/internal/provider/engine"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/model"
)

const engSecret = "sk-SECRET-1234567890abcdef"

// engSecrets 是测试用的凭证解析器；calls 记录被解析的次数，DryRun 测试要求它为 0。
type engSecrets struct {
	mu    sync.Mutex
	m     map[string]string
	calls int
}

func (s *engSecrets) Get(ctx context.Context, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	v, ok := s.m[name]
	if !ok {
		return "", errors.New("凭证不存在")
	}
	return v, nil
}

func engDefaultSecrets() *engSecrets {
	return &engSecrets{m: map[string]string{"runninghub_api_key": engSecret}}
}

// engAssets 是测试用的素材存储。
type engAssets struct {
	content []byte
	url     string
	opened  []uint64
}

func (a *engAssets) Get(ctx context.Context, userID, assetID uint64) (*model.Asset, error) {
	return &model.Asset{ID: assetID, UserID: userID, Kind: "image"}, nil
}

func (a *engAssets) Open(ctx context.Context, userID, assetID uint64) (*provider.AssetFile, error) {
	if userID != 7 {
		return nil, provider.ErrAssetNotFound
	}
	a.opened = append(a.opened, assetID)
	return &provider.AssetFile{
		Asset: &model.Asset{ID: assetID, UserID: userID, Kind: "image", MimeType: "image/png", FileName: "first-frame.png"},
		Body:  io.NopCloser(bytes.NewReader(a.content)),
		URL:   a.url,
	}, nil
}

// engRH 模拟一个 RunningHub 风格的平台：上传 / 提交 / 查询 / 下载。
type engRH struct {
	t   *testing.T
	srv *httptest.Server

	mu           sync.Mutex
	uploadName   string
	uploadMime   string
	uploadBody   []byte
	uploadAuth   string
	submitBody   map[string]any
	submitAuth   string
	submitURI    string
	queryBody    map[string]any
	queryCount   int
	cancelCalled bool

	onSubmit func(w http.ResponseWriter, r *http.Request) // 覆盖默认提交行为
	onQuery  func(w http.ResponseWriter, r *http.Request)
	onCancel func(w http.ResponseWriter, r *http.Request)
	extra    map[string]http.HandlerFunc // 额外路由（下载 / 重定向等）
}

func engNewRH(t *testing.T) *engRH {
	rh := &engRH{t: t, extra: map[string]http.HandlerFunc{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/openapi/v2/media/upload/binary", rh.handleUpload)
	mux.HandleFunc("/openapi/v2/run/ai-app/", rh.handleSubmit)
	mux.HandleFunc("/openapi/v2/query", rh.handleQuery)
	mux.HandleFunc("/openapi/v2/cancel", rh.handleCancel)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		rh.mu.Lock()
		h := rh.extra[r.URL.Path]
		rh.mu.Unlock()
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	})
	rh.srv = httptest.NewServer(mux)
	t.Cleanup(rh.srv.Close)
	return rh
}

func (rh *engRH) setExtra(path string, h http.HandlerFunc) {
	rh.mu.Lock()
	rh.extra[path] = h
	rh.mu.Unlock()
}

func engJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (rh *engRH) handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		engJSON(w, 400, map[string]any{"code": 1, "msg": err.Error()})
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		engJSON(w, 400, map[string]any{"code": 1, "msg": "缺少 file 字段"})
		return
	}
	defer f.Close()
	body, _ := io.ReadAll(f)
	rh.mu.Lock()
	rh.uploadName, rh.uploadMime, rh.uploadBody, rh.uploadAuth = hdr.Filename, hdr.Header.Get("Content-Type"), body, r.Header.Get("Authorization")
	rh.mu.Unlock()
	engJSON(w, 200, map[string]any{"code": 0, "data": map[string]any{"fileName": "uploaded/abc.png"}})
}

func (rh *engRH) handleSubmit(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	rh.mu.Lock()
	rh.submitBody, rh.submitAuth, rh.submitURI = body, r.Header.Get("Authorization"), r.RequestURI
	h := rh.onSubmit
	rh.mu.Unlock()
	if h != nil {
		h(w, r)
		return
	}
	engJSON(w, 200, map[string]any{"taskId": "T-1001", "status": "QUEUED"})
}

func (rh *engRH) handleQuery(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	rh.mu.Lock()
	rh.queryBody = body
	rh.queryCount++
	h := rh.onQuery
	rh.mu.Unlock()
	if h != nil {
		h(w, r)
		return
	}
	engJSON(w, 200, map[string]any{
		"status": "SUCCESS",
		"results": []any{
			map[string]any{"url": rh.srv.URL + "/files/out.mp4", "nodeId": "9", "outputType": "mp4"},
			map[string]any{"url": rh.srv.URL + "/files/cover.png", "nodeId": "10", "outputType": "png"},
		},
		"usage": map[string]any{"consumeMoney": 0.25, "taskCostTime": "12"},
	})
}

func (rh *engRH) handleCancel(w http.ResponseWriter, r *http.Request) {
	rh.mu.Lock()
	rh.cancelCalled = true
	h := rh.onCancel
	rh.mu.Unlock()
	if h != nil {
		h(w, r)
		return
	}
	engJSON(w, 200, map[string]any{"code": 0})
}

// engSnapshot 用设计文档的 RunningHub 夹具构造快照，并把 base_url 指向测试服务器。
func engSnapshot(t *testing.T, baseURL string) *dsl.Snapshot {
	t.Helper()
	dir := filepath.Join("..", "dsl", "testdata")
	pb, err := os.ReadFile(filepath.Join(dir, "runninghub_provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, issues := dsl.ParseProvider(pb)
	if len(issues) > 0 {
		t.Fatalf("provider 夹具不合法：%v", issues)
	}
	mb, err := os.ReadFile(filepath.Join(dir, "runninghub_model.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, issues := dsl.ParseModel(mb, p)
	if len(issues) > 0 {
		t.Fatalf("model 夹具不合法：%v", issues)
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	p.BaseURL = baseURL
	p.AllowedHosts = []string{u.Hostname()}
	return &dsl.Snapshot{Provider: *p, Model: *m}
}

// engInput 是通过校验的示例输入（image 是 asset id）。
func engInput() map[string]any {
	return map[string]any{"prompt": "一只在奔跑的猫", "image": uint64(12), "duration": 10.0}
}

func engSubmitInput() provider.SubmitInput {
	return provider.SubmitInput{Task: provider.TaskRef{ID: 55, UserID: 7}, Input: engInput(), WebhookURL: "https://app.example/hook/abc"}
}

// engNew 创建允许访问回环地址的测试引擎（生产默认拒绝内网）。
func engNew(assets provider.AssetStore, secrets provider.SecretResolver, mut func(*Options)) provider.Executor {
	o := Options{Secrets: secrets, Assets: assets, AllowPrivate: true}
	if mut != nil {
		mut(&o)
	}
	return New(o)
}

func engClass(t *testing.T, err error) provider.ErrorClass {
	t.Helper()
	if err == nil {
		t.Fatal("期望返回错误")
	}
	var e *provider.Error
	if !errors.As(err, &e) {
		t.Fatalf("期望 *provider.Error，实际 %T：%v", err, err)
	}
	return e.Class
}
