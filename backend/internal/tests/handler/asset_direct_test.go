package handler_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "video-canvas/internal/handler"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/config"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

// directLocalStore 是“支持直传的对象存储”的替身：底层用真实的本地目录，额外提供直传、Stat、Range。
type directLocalStore struct{ *storage.LocalStorage }

func (d directLocalStore) DirectMethod() string { return storage.DirectPostPolicy }

func (d directLocalStore) DirectUpload(ctx context.Context, req storage.DirectUploadRequest) (*storage.DirectUpload, error) {
	return &storage.DirectUpload{Method: "post", URL: "https://bucket.test/", Fields: map[string]string{"key": req.Key}}, nil
}

func (d directLocalStore) Stat(ctx context.Context, key string) (storage.ObjectInfo, error) {
	fi, err := os.Stat(filepath.Join(d.Dir(), filepath.FromSlash(key)))
	if err != nil {
		return storage.ObjectInfo{}, storage.ErrNotFound
	}
	return storage.ObjectInfo{Size: fi.Size()}, nil
}

func (d directLocalStore) OpenRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	rc, err := d.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	_, _ = io.CopyN(io.Discard, rc, offset)
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(rc, length), rc}, nil
}

type handlerIntentRepo struct {
	rows   map[uint64]*model.AssetUploadIntent
	nextID uint64
}

func (r *handlerIntentRepo) Create(ctx context.Context, in *model.AssetUploadIntent) error {
	r.nextID++
	in.ID = r.nextID
	cp := *in
	r.rows[in.ID] = &cp
	return nil
}
func (r *handlerIntentRepo) GetByID(ctx context.Context, userID, id uint64) (*model.AssetUploadIntent, error) {
	in, ok := r.rows[id]
	if !ok || in.UserID != userID {
		return nil, repository.ErrNotFound
	}
	cp := *in
	return &cp, nil
}
func (r *handlerIntentRepo) Complete(ctx context.Context, id uint64, at time.Time) error {
	in, ok := r.rows[id]
	if !ok || in.CompletedAt != nil {
		return repository.ErrNotFound
	}
	in.CompletedAt = &at
	return nil
}
func (r *handlerIntentRepo) ListExpired(context.Context, time.Time, int) ([]model.AssetUploadIntent, error) {
	return nil, nil
}
func (r *handlerIntentRepo) Delete(ctx context.Context, id uint64) error {
	delete(r.rows, id)
	return nil
}

// newDirectAssetEnv 组装带直传能力的素材路由；direct 为 false 时模拟管理员没开直传。
func newDirectAssetEnv(t *testing.T, direct bool) (*gin.Engine, *directLocalStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	local, err := storage.NewLocal(config.LocalStorage{Dir: filepath.Join(t.TempDir(), "assets")})
	if err != nil {
		t.Fatal(err)
	}
	st := directLocalStore{local}
	repo := &fakeAssetHandlerRepo{rows: map[uint64]*model.Asset{}}
	svc := service.NewAssetService(repo, handlerRegistry{&storage.Handle{ID: 2, Provider: storage.ProviderS3, Storage: st, DirectUpload: direct}}, config.Storage{MaxUpload: 1 << 20})
	svc.SetUploadIntents(&handlerIntentRepo{rows: map[uint64]*model.AssetUploadIntent{}})
	h := NewAssetHandler(svc)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)) })
	r.POST("/api/v1/assets/upload-intents", h.CreateUploadIntent)
	r.POST("/api/v1/assets/upload-intents/:id/complete", h.CompleteUpload)
	return r, &st
}

func intentReq(over map[string]any) map[string]any {
	b := map[string]any{"file_name": "a.png", "size": 1234, "mime_type": "image/png"}
	for k, v := range over {
		b[k] = v
	}
	return b
}

func postJSON(t *testing.T, r http.Handler, path string, body any) (int, assetResp) {
	t.Helper()
	b, _ := json.Marshal(body)
	w, resp := doAssetRequest(t, r, http.MethodPost, path, strings.NewReader(string(b)), "application/json")
	return w.Code, resp
}

func TestAssetHandler_UploadIntent(t *testing.T) {
	const base = "/api/v1/assets/upload-intents"

	t.Run("直传：返回上传凭证", func(t *testing.T) {
		r, _ := newDirectAssetEnv(t, true)
		code, resp := postJSON(t, r, base, intentReq(nil))
		if code != http.StatusOK || resp.Code != 0 {
			t.Fatalf("实际 %d %+v", code, resp)
		}
		var v service.UploadIntentView
		_ = json.Unmarshal(resp.Data, &v)
		if v.Mode != "direct" || v.Method != "post" || v.IntentID == 0 || v.Fields["key"] == "" {
			t.Fatalf("实际 %+v", v)
		}
	})

	t.Run("管理员没开直传：告诉客户端走后端中转", func(t *testing.T) {
		r, _ := newDirectAssetEnv(t, false)
		code, resp := postJSON(t, r, base, intentReq(nil))
		var v service.UploadIntentView
		_ = json.Unmarshal(resp.Data, &v)
		if code != http.StatusOK || v.Mode != "proxy" {
			t.Fatalf("实际 %d %+v", code, v)
		}
	})

	t.Run("参数校验失败 400 / 10001", func(t *testing.T) {
		bad := map[string]map[string]any{
			"缺少文件名":  {"file_name": ""},
			"缺少类型":   {"mime_type": ""},
			"大小为 0":  {"size": 0},
			"大小是负数":  {"size": -5},
			"大小类型错误": {"size": "big"},
		}
		for name, over := range bad {
			t.Run(name, func(t *testing.T) {
				r, _ := newDirectAssetEnv(t, true)
				code, resp := postJSON(t, r, base, intentReq(over))
				if code != http.StatusBadRequest || resp.Code != errcode.ErrInvalidParams.Code {
					t.Fatalf("实际 %d %+v", code, resp)
				}
			})
		}
	})

	t.Run("业务错误：超过大小上限 413；类型不支持 400", func(t *testing.T) {
		r, _ := newDirectAssetEnv(t, true)
		if code, resp := postJSON(t, r, base, intentReq(map[string]any{"size": 5 << 20})); code != http.StatusRequestEntityTooLarge || resp.Code != errcode.ErrAssetTooLarge.Code {
			t.Errorf("超限：%d %+v", code, resp)
		}
		if code, resp := postJSON(t, r, base, intentReq(map[string]any{"mime_type": "application/x-msdownload"})); code != http.StatusBadRequest || resp.Code != errcode.ErrAssetInvalid.Code {
			t.Errorf("类型：%d %+v", code, resp)
		}
	})
}

func TestAssetHandler_CompleteUpload(t *testing.T) {
	const base = "/api/v1/assets/upload-intents"
	png := assetTestPNGBytes(t, 30, 20)

	t.Run("申请 -> 浏览器直传 -> 登记，返回稳定地址的素材视图", func(t *testing.T) {
		r, st := newDirectAssetEnv(t, true)
		_, resp := postJSON(t, r, base, intentReq(map[string]any{"size": len(png)}))
		var iv service.UploadIntentView
		_ = json.Unmarshal(resp.Data, &iv)
		// 模拟浏览器把文件传进了桶
		if err := st.Put(context.Background(), iv.Fields["key"], strings.NewReader(string(png)), int64(len(png)), "image/png"); err != nil {
			t.Fatal(err)
		}

		code, resp := postJSON(t, r, base+"/"+jsonID(iv.IntentID)+"/complete", nil)
		if code != http.StatusOK || resp.Code != 0 {
			t.Fatalf("实际 %d %+v", code, resp)
		}
		var v model.AssetView
		_ = json.Unmarshal(resp.Data, &v)
		if v.Kind != "image" || v.Width != 30 || v.Height != 20 || !strings.HasPrefix(v.URL, "/files/u1/") {
			t.Fatalf("视图不对：%+v", v)
		}
	})

	t.Run("文件还没传上去 400", func(t *testing.T) {
		r, _ := newDirectAssetEnv(t, true)
		_, resp := postJSON(t, r, base, intentReq(nil))
		var iv service.UploadIntentView
		_ = json.Unmarshal(resp.Data, &iv)
		code, resp := postJSON(t, r, base+"/"+jsonID(iv.IntentID)+"/complete", nil)
		if code != http.StatusBadRequest || resp.Code != errcode.ErrAssetInvalid.Code {
			t.Fatalf("实际 %d %+v", code, resp)
		}
	})

	t.Run("意图不存在 404 / 51021；id 非法 400", func(t *testing.T) {
		r, _ := newDirectAssetEnv(t, true)
		if code, resp := postJSON(t, r, base+"/999/complete", nil); code != http.StatusNotFound || resp.Code != errcode.ErrUploadIntentNotFound.Code {
			t.Errorf("不存在：%d %+v", code, resp)
		}
		if code, resp := postJSON(t, r, base+"/abc/complete", nil); code != http.StatusBadRequest || resp.Code != errcode.ErrInvalidParams.Code {
			t.Errorf("id 非法：%d %+v", code, resp)
		}
	})
}

func jsonID(id uint64) string {
	b, _ := json.Marshal(id)
	return string(b)
}
