package handler_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	. "video-canvas/internal/handler"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/config"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

// fakeAssetHandlerRepo 实现 service.AssetRepo，按 user_id 校验归属。
type fakeAssetHandlerRepo struct {
	rows   map[uint64]*model.Asset
	nextID uint64
}

func (f *fakeAssetHandlerRepo) Create(ctx context.Context, a *model.Asset) error {
	f.nextID++
	a.ID = f.nextID
	cp := *a
	f.rows[a.ID] = &cp
	return nil
}

func (f *fakeAssetHandlerRepo) GetByID(ctx context.Context, userID, id uint64) (*model.Asset, error) {
	a, ok := f.rows[id]
	if !ok || a.UserID != userID {
		return nil, repository.ErrNotFound
	}
	cp := *a
	return &cp, nil
}

type assetTestEnv struct {
	router *gin.Engine
	repo   *fakeAssetHandlerRepo
	store  *storage.LocalStorage
}

// newAssetTestEnv 用真实 service + 真实本地存储（临时目录）+ fake repo 组装路由，userID 固定为 1。
func newAssetTestEnv(t *testing.T, maxUpload int64) *assetTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, err := storage.NewLocal(config.LocalStorage{Dir: filepath.Join(t.TempDir(), "assets")})
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeAssetHandlerRepo{rows: map[uint64]*model.Asset{}}
	svc := service.NewAssetService(repo, store, config.Storage{MaxUpload: maxUpload, SignedTTL: time.Hour})
	h := NewAssetHandler(svc)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)) })
	r.POST("/api/v1/assets", h.Upload)
	r.GET("/api/v1/assets/:id", h.Get)
	return &assetTestEnv{router: r, repo: repo, store: store}
}

func assetTestPNGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// assetMultipart 构造 multipart 请求体，files 的 key 是字段名。
func assetMultipart(t *testing.T, field, fileName string, content []byte, extraFields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range extraFields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if field != "" {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="`+field+`"; filename="`+fileName+`"`)
		h.Set("Content-Type", "image/png") // 客户端声明的类型，服务端不应信任
		part, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(content)
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

type assetResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func doAssetRequest(t *testing.T, r http.Handler, method, path string, body io.Reader, contentType string) (*httptest.ResponseRecorder, assetResp) {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp assetResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

func TestAssetHandler_Upload(t *testing.T) {
	pngBytes := assetTestPNGBytes(t, 30, 20)

	t.Run("上传成功返回素材视图", func(t *testing.T) {
		env := newAssetTestEnv(t, 1<<20)
		body, ct := assetMultipart(t, "file", "海报.png", pngBytes, map[string]string{"foo": "bar"})
		w, resp := doAssetRequest(t, env.router, http.MethodPost, "/api/v1/assets", body, ct)
		if w.Code != http.StatusOK || resp.Code != 0 {
			t.Fatalf("状态码 %d，响应 %s", w.Code, w.Body.String())
		}
		var v model.AssetView
		if err := json.Unmarshal(resp.Data, &v); err != nil {
			t.Fatal(err)
		}
		if v.ID == 0 || v.Kind != "image" || v.MimeType != "image/png" || v.Width != 30 || v.Height != 20 || v.FileName != "海报.png" || v.ByteSize != int64(len(pngBytes)) {
			t.Fatalf("视图不对：%+v", v)
		}
		if !strings.HasPrefix(v.URL, "/files/u1/") || !strings.HasSuffix(v.URL, ".png") {
			t.Fatalf("URL 不对：%q", v.URL)
		}
		// 内容确实写进了存储
		row := env.repo.rows[v.ID]
		rc, err := env.store.Open(context.Background(), row.StorageKey)
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		if got, _ := io.ReadAll(rc); !bytes.Equal(got, pngBytes) {
			t.Fatal("存储内容不一致")
		}
		// 响应 JSON 的字段名（前端据此映射）
		var raw map[string]any
		_ = json.Unmarshal(resp.Data, &raw)
		for _, k := range []string{"id", "kind", "url", "mime_type", "byte_size", "width", "height", "duration_ms", "file_name"} {
			if _, ok := raw[k]; !ok {
				t.Errorf("响应缺少字段 %s", k)
			}
		}
	})

	t.Run("file 字段前面有其他字段也能找到", func(t *testing.T) {
		env := newAssetTestEnv(t, 1<<20)
		body, ct := assetMultipart(t, "file", "a.png", pngBytes, map[string]string{"a": "1", "b": "2"})
		w, resp := doAssetRequest(t, env.router, http.MethodPost, "/api/v1/assets", body, ct)
		if w.Code != http.StatusOK || resp.Code != 0 {
			t.Fatalf("响应 %s", w.Body.String())
		}
	})

	tests := []struct {
		name       string
		maxUpload  int64
		build      func(t *testing.T) (io.Reader, string)
		wantStatus int
		wantCode   int
	}{
		{
			name: "缺少 file 字段返回 400", maxUpload: 1 << 20,
			build: func(t *testing.T) (io.Reader, string) {
				return assetMultipart(t, "other", "a.png", pngBytes, map[string]string{"x": "y"})
			},
			wantStatus: http.StatusBadRequest, wantCode: 10001,
		},
		{
			name: "file 是普通文本字段而不是文件返回 400", maxUpload: 1 << 20,
			build: func(t *testing.T) (io.Reader, string) {
				return assetMultipart(t, "", "", nil, map[string]string{"file": "not a file"})
			},
			wantStatus: http.StatusBadRequest, wantCode: 10001,
		},
		{
			name: "不是 multipart 返回 400", maxUpload: 1 << 20,
			build: func(t *testing.T) (io.Reader, string) {
				return strings.NewReader(`{"file":"x"}`), "application/json"
			},
			wantStatus: http.StatusBadRequest, wantCode: 10001,
		},
		{
			name: "multipart 损坏返回 400", maxUpload: 1 << 20,
			build: func(t *testing.T) (io.Reader, string) {
				return strings.NewReader("--xx\r\nbroken"), "multipart/form-data; boundary=xx"
			},
			wantStatus: http.StatusBadRequest, wantCode: 10001,
		},
		{
			name: "文件类型不合法返回 400 与素材错误码", maxUpload: 1 << 20,
			build: func(t *testing.T) (io.Reader, string) {
				return assetMultipart(t, "file", "a.png", []byte("<html>not an image</html>"), nil)
			},
			wantStatus: http.StatusBadRequest, wantCode: 40008,
		},
		{
			name: "文件超过上限返回 413", maxUpload: 1024,
			build: func(t *testing.T) (io.Reader, string) {
				return assetMultipart(t, "file", "a.png", append(append([]byte{}, pngBytes...), make([]byte, 2048)...), nil)
			},
			wantStatus: http.StatusRequestEntityTooLarge, wantCode: 40009,
		},
		{
			name: "请求体远超上限返回 413", maxUpload: 1024,
			build: func(t *testing.T) (io.Reader, string) {
				return assetMultipart(t, "file", "a.png", make([]byte, 3<<20), nil)
			},
			wantStatus: http.StatusRequestEntityTooLarge, wantCode: 40009,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newAssetTestEnv(t, tt.maxUpload)
			body, ct := tt.build(t)
			w, resp := doAssetRequest(t, env.router, http.MethodPost, "/api/v1/assets", body, ct)
			if w.Code != tt.wantStatus || resp.Code != tt.wantCode {
				t.Fatalf("期望 %d/%d，实际 %d/%d：%s", tt.wantStatus, tt.wantCode, w.Code, resp.Code, w.Body.String())
			}
			if len(env.repo.rows) != 0 {
				t.Fatalf("失败不应入库：%d 行", len(env.repo.rows))
			}
		})
	}

	t.Run("未流式读完超大 Content-Length 时不读 body 直接 413", func(t *testing.T) {
		env := newAssetTestEnv(t, 1024)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/assets", strings.NewReader("x"))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=xx")
		req.ContentLength = 100 << 20
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("期望 413，实际 %d：%s", w.Code, w.Body.String())
		}
	})
}

func TestAssetHandler_Get(t *testing.T) {
	env := newAssetTestEnv(t, 1<<20)
	env.repo.rows[9] = &model.Asset{ID: 9, UserID: 1, Kind: "image", StorageKey: "u1/202609/a.png", MimeType: "image/png", ByteSize: 5, Width: 3, Height: 4, FileName: "a.png"}
	env.repo.rows[10] = &model.Asset{ID: 10, UserID: 2, Kind: "image", StorageKey: "u2/202609/b.png", MimeType: "image/png"}

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantCode   int
	}{
		{"本人素材成功", "/api/v1/assets/9", http.StatusOK, 0},
		{"他人素材返回 404", "/api/v1/assets/10", http.StatusNotFound, 40007},
		{"不存在返回 404", "/api/v1/assets/999", http.StatusNotFound, 40007},
		{"id 不是数字返回 400", "/api/v1/assets/abc", http.StatusBadRequest, 10001},
		{"id 为 0 返回 400", "/api/v1/assets/0", http.StatusBadRequest, 10001},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, resp := doAssetRequest(t, env.router, http.MethodGet, tt.path, nil, "")
			if w.Code != tt.wantStatus || resp.Code != tt.wantCode {
				t.Fatalf("期望 %d/%d，实际 %d/%d：%s", tt.wantStatus, tt.wantCode, w.Code, resp.Code, w.Body.String())
			}
			if tt.wantCode == 0 {
				var v model.AssetView
				if err := json.Unmarshal(resp.Data, &v); err != nil {
					t.Fatal(err)
				}
				if v.ID != 9 || v.Width != 3 || v.URL != "/files/u1/202609/a.png" {
					t.Fatalf("视图不对：%+v", v)
				}
			}
		})
	}
}

// 真实 HTTP 服务器上验证：全局 ReadTimeout 很短时，上传路由通过 ResponseController 放宽后，慢速上传仍能成功。
func TestAssetHandler_Upload_RelaxedReadDeadline(t *testing.T) {
	env := newAssetTestEnv(t, 1<<20)
	srv := httptest.NewUnstartedServer(env.router)
	srv.Config.ReadTimeout = 300 * time.Millisecond
	srv.Start()
	defer srv.Close()

	pngBytes := assetTestPNGBytes(t, 10, 10)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("file", "slow.png")
	half := len(pngBytes) / 2
	_, _ = part.Write(pngBytes[:half])
	head := append([]byte{}, buf.Bytes()...) // multipart 头 + 前半截文件
	buf.Reset()
	_, _ = part.Write(pngBytes[half:])
	_ = mw.Close()
	tail := buf.Bytes()

	// 手写 HTTP 请求：先发头和前半截 body，停顿超过 ReadTimeout 再发后半截
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	total := len(head) + len(tail)
	reqHead := "POST /api/v1/assets HTTP/1.1\r\nHost: test\r\nContent-Type: " + mw.FormDataContentType() +
		"\r\nContent-Length: " + strconv.Itoa(total) + "\r\nConnection: close\r\n\r\n"
	_, _ = conn.Write([]byte(reqHead))
	_, _ = conn.Write(head)
	time.Sleep(700 * time.Millisecond) // 超过 ReadTimeout
	_, _ = conn.Write(tail)

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("读取响应失败（上传被超时掐断？）：%v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"code":0`) {
		t.Fatalf("期望上传成功，实际 %d：%s", resp.StatusCode, b)
	}
}
