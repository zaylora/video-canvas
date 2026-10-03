package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "video-canvas/internal/handler"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

// fakeStorageAdmin 实现 handler.StorageAdminService，记录收到的参数，可注入业务错误。
type fakeStorageAdmin struct {
	err error // 非空时所有写操作与查询都返回它

	created    service.StorageCreateInput
	updated    service.StorageUpdateInput
	actor      uint64
	targetID   uint64
	secretAK   string
	secretSK   string
	defaultSet uint64
	deleted    uint64
}

func (f *fakeStorageAdmin) Presets() []storage.Preset { return storage.Presets() }
func (f *fakeStorageAdmin) List(ctx context.Context) ([]service.StorageView, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []service.StorageView{{ID: 1, Name: "本地磁盘", Provider: "local", Builtin: true, IsDefault: true}}, nil
}
func (f *fakeStorageAdmin) Get(ctx context.Context, id uint64) (*service.StorageView, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &service.StorageView{ID: id, Name: "x"}, nil
}
func (f *fakeStorageAdmin) Test(ctx context.Context, in service.StorageCreateInput) (storage.ProbeResult, error) {
	f.created = in
	if f.err != nil {
		return storage.ProbeResult{}, f.err
	}
	return storage.ProbeResult{OK: true, Steps: []storage.ProbeStep{{Index: 1, Name: "鉴权与桶", OK: true}}}, nil
}
func (f *fakeStorageAdmin) Create(ctx context.Context, actor uint64, in service.StorageCreateInput) (*service.StorageView, error) {
	f.actor, f.created = actor, in
	if f.err != nil {
		return nil, f.err
	}
	return &service.StorageView{ID: 7, Name: in.Name, Provider: in.Provider}, nil
}
func (f *fakeStorageAdmin) Update(ctx context.Context, actor, id uint64, in service.StorageUpdateInput) (*service.StorageView, error) {
	f.actor, f.targetID, f.updated = actor, id, in
	if f.err != nil {
		return nil, f.err
	}
	return &service.StorageView{ID: id, Name: in.Name, Version: in.Version + 1}, nil
}
func (f *fakeStorageAdmin) ReplaceSecret(ctx context.Context, actor, id uint64, ak, sk string) (*service.StorageView, error) {
	f.actor, f.targetID, f.secretAK, f.secretSK = actor, id, ak, sk
	if f.err != nil {
		return nil, f.err
	}
	return &service.StorageView{ID: id, SecretSet: true}, nil
}
func (f *fakeStorageAdmin) Check(ctx context.Context, actor, id uint64) (storage.ProbeResult, error) {
	f.actor, f.targetID = actor, id
	if f.err != nil {
		return storage.ProbeResult{}, f.err
	}
	return storage.ProbeResult{OK: true}, nil
}
func (f *fakeStorageAdmin) SetDefault(ctx context.Context, actor, id uint64) error {
	f.actor, f.defaultSet = actor, id
	return f.err
}
func (f *fakeStorageAdmin) DeleteCheck(ctx context.Context, id uint64) (*service.StorageDeleteCheck, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &service.StorageDeleteCheck{Deletable: false, Reason: "被 3 个素材引用", AssetCount: 3}, nil
}
func (f *fakeStorageAdmin) Delete(ctx context.Context, actor, id uint64) error {
	f.actor, f.deleted = actor, id
	return f.err
}

type storageAdminEnv struct {
	router *gin.Engine
	svc    *fakeStorageAdmin
}

func newStorageAdminEnv() *storageAdminEnv {
	gin.SetMode(gin.TestMode)
	svc := &fakeStorageAdmin{}
	h := NewAdminStorageHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(9)) })
	g := r.Group("/api/v1/admin/storages")
	g.GET("", h.List)
	g.GET("/presets", h.Presets)
	g.POST("/test", h.Test)
	g.POST("", h.Create)
	g.GET("/:id", h.Get)
	g.PUT("/default", h.SetDefault)
	g.PUT("/:id", h.Update)
	g.PUT("/:id/secret", h.ReplaceSecret)
	g.POST("/:id/check", h.Check)
	g.GET("/:id/delete-check", h.DeleteCheck)
	g.DELETE("/:id", h.Delete)
	return &storageAdminEnv{router: r, svc: svc}
}

type storageResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (e *storageAdminEnv) do(t *testing.T, method, path string, body any) (*httptest.ResponseRecorder, storageResp) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	var resp storageResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

const storageBase = "/api/v1/admin/storages"

func validCreateBody() map[string]any {
	return map[string]any{"name": "OSS 杭州", "provider": "aliyun_oss", "region": "cn-hangzhou", "bucket": "vc-bucket",
		"access_key_id": "LTAI123", "secret_key": "sk", "direct_upload": true}
}

func TestAdminStorageHandler_ListGetPresets(t *testing.T) {
	env := newStorageAdminEnv()
	w, resp := env.do(t, http.MethodGet, storageBase, nil)
	if w.Code != http.StatusOK || resp.Code != 0 || !strings.Contains(string(resp.Data), "本地磁盘") {
		t.Fatalf("列表：%d %s", w.Code, w.Body.String())
	}
	w, resp = env.do(t, http.MethodGet, storageBase+"/presets", nil)
	if w.Code != http.StatusOK || !strings.Contains(string(resp.Data), "presigned_put") || !strings.Contains(string(resp.Data), "cn-hangzhou") {
		t.Fatalf("预设应含直传方式与地域：%s", w.Body.String())
	}
	if w, resp = env.do(t, http.MethodGet, storageBase+"/5", nil); w.Code != http.StatusOK || resp.Code != 0 {
		t.Fatalf("详情：%s", w.Body.String())
	}
	t.Run("id 非法 400", func(t *testing.T) {
		if w, resp := env.do(t, http.MethodGet, storageBase+"/abc", nil); w.Code != http.StatusBadRequest || resp.Code != errcode.ErrInvalidParams.Code {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("不存在 404 / 51001", func(t *testing.T) {
		env.svc.err = errcode.ErrStorageNotFound
		if w, resp := env.do(t, http.MethodGet, storageBase+"/9", nil); w.Code != http.StatusNotFound || resp.Code != errcode.ErrStorageNotFound.Code {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
}

func TestAdminStorageHandler_Create(t *testing.T) {
	t.Run("成功：参数原样传给 service，操作人取自登录态", func(t *testing.T) {
		env := newStorageAdminEnv()
		w, resp := env.do(t, http.MethodPost, storageBase, validCreateBody())
		if w.Code != http.StatusOK || resp.Code != 0 {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
		got := env.svc.created
		if got.Provider != "aliyun_oss" || got.Bucket != "vc-bucket" || got.SecretKey != "sk" || !got.DirectUpload || env.svc.actor != 9 {
			t.Errorf("参数没传对：%+v actor=%d", got, env.svc.actor)
		}
	})
	t.Run("响应里不会出现密钥", func(t *testing.T) {
		env := newStorageAdminEnv()
		w, _ := env.do(t, http.MethodPost, storageBase, validCreateBody())
		if strings.Contains(w.Body.String(), `"sk"`) || strings.Contains(w.Body.String(), "secret_key") {
			t.Errorf("响应不应包含密钥：%s", w.Body.String())
		}
	})
	t.Run("参数校验失败 400 / 10001", func(t *testing.T) {
		tests := map[string]func(map[string]any){
			"缺少名称":         func(b map[string]any) { delete(b, "name") },
			"缺少服务商":        func(b map[string]any) { delete(b, "provider") },
			"缺少密钥":         func(b map[string]any) { delete(b, "secret_key") },
			"寻址方式非法":       func(b map[string]any) { b["addressing"] = "weird" },
			"名称超长":         func(b map[string]any) { b["name"] = strings.Repeat("a", 65) },
			"有效期是负数":       func(b map[string]any) { b["signed_ttl_sec"] = -1 },
			"use_ssl 类型错误": func(b map[string]any) { b["use_ssl"] = "yes" },
		}
		for name, mut := range tests {
			t.Run(name, func(t *testing.T) {
				env := newStorageAdminEnv()
				body := validCreateBody()
				mut(body)
				if w, resp := env.do(t, http.MethodPost, storageBase, body); w.Code != http.StatusBadRequest || resp.Code != errcode.ErrInvalidParams.Code {
					t.Fatalf("实际 %d %s", w.Code, w.Body.String())
				}
			})
		}
	})
	t.Run("业务错误：名称重复 409", func(t *testing.T) {
		env := newStorageAdminEnv()
		env.svc.err = errcode.ErrStorageNameDup
		if w, resp := env.do(t, http.MethodPost, storageBase, validCreateBody()); w.Code != http.StatusConflict || resp.Code != errcode.ErrStorageNameDup.Code {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
}

func TestAdminStorageHandler_TestUnsaved(t *testing.T) {
	env := newStorageAdminEnv()
	w, resp := env.do(t, http.MethodPost, storageBase+"/test", validCreateBody())
	if w.Code != http.StatusOK || resp.Code != 0 || !strings.Contains(string(resp.Data), `"steps"`) {
		t.Fatalf("实际 %d %s", w.Code, w.Body.String())
	}
	if w, resp = env.do(t, http.MethodPost, storageBase+"/test", map[string]any{"provider": "s3"}); w.Code != http.StatusBadRequest || resp.Code != errcode.ErrInvalidParams.Code {
		t.Fatalf("缺字段应 400：%d %s", w.Code, w.Body.String())
	}
}

func TestAdminStorageHandler_Update(t *testing.T) {
	body := func() map[string]any {
		return map[string]any{"version": 3, "name": "n", "region": "cn-hangzhou", "bucket": "vc-bucket", "signed_ttl_sec": 3600}
	}
	t.Run("成功", func(t *testing.T) {
		env := newStorageAdminEnv()
		w, resp := env.do(t, http.MethodPut, storageBase+"/4", body())
		if w.Code != http.StatusOK || resp.Code != 0 || env.svc.targetID != 4 || env.svc.updated.Version != 3 || env.svc.actor != 9 {
			t.Fatalf("实际 %d %s id=%d %+v", w.Code, w.Body.String(), env.svc.targetID, env.svc.updated)
		}
	})
	t.Run("缺少 version 400", func(t *testing.T) {
		env := newStorageAdminEnv()
		b := body()
		delete(b, "version")
		if w, resp := env.do(t, http.MethodPut, storageBase+"/4", b); w.Code != http.StatusBadRequest || resp.Code != errcode.ErrInvalidParams.Code {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("定位字段被锁 409 / 51005，文案透传", func(t *testing.T) {
		env := newStorageAdminEnv()
		env.svc.err = errcode.ErrStorageFieldLocked.WithMsg("该存储已有 5 个素材引用，不能修改：bucket")
		w, resp := env.do(t, http.MethodPut, storageBase+"/4", body())
		if w.Code != http.StatusConflict || resp.Code != errcode.ErrStorageFieldLocked.Code || !strings.Contains(resp.Msg, "bucket") {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
}

func TestAdminStorageHandler_SecretCheckDefaultDelete(t *testing.T) {
	t.Run("换密钥", func(t *testing.T) {
		env := newStorageAdminEnv()
		w, resp := env.do(t, http.MethodPut, storageBase+"/4/secret", map[string]any{"access_key_id": "AK2", "secret_key": "SK2"})
		if w.Code != http.StatusOK || resp.Code != 0 || env.svc.secretAK != "AK2" || env.svc.secretSK != "SK2" || env.svc.targetID != 4 {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "SK2") {
			t.Error("响应不应包含密钥")
		}
		if w, resp = env.do(t, http.MethodPut, storageBase+"/4/secret", map[string]any{"access_key_id": "AK2"}); w.Code != http.StatusBadRequest || resp.Code != errcode.ErrInvalidParams.Code {
			t.Errorf("缺密钥应 400：%d", w.Code)
		}
	})
	t.Run("重新测试", func(t *testing.T) {
		env := newStorageAdminEnv()
		if w, resp := env.do(t, http.MethodPost, storageBase+"/4/check", nil); w.Code != http.StatusOK || resp.Code != 0 || env.svc.targetID != 4 {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("设为默认：id 必填且 ≥1；最近测试未通过 409", func(t *testing.T) {
		env := newStorageAdminEnv()
		if w, resp := env.do(t, http.MethodPut, storageBase+"/default", map[string]any{"id": 0}); w.Code != http.StatusBadRequest || resp.Code != errcode.ErrInvalidParams.Code {
			t.Fatalf("id=0 应 400：%d %s", w.Code, w.Body.String())
		}
		if w, resp := env.do(t, http.MethodPut, storageBase+"/default", map[string]any{"id": 2}); w.Code != http.StatusOK || resp.Code != 0 || env.svc.defaultSet != 2 || env.svc.actor != 9 {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
		env.svc.err = errcode.ErrStorageNotChecked
		if w, resp := env.do(t, http.MethodPut, storageBase+"/default", map[string]any{"id": 2}); w.Code != http.StatusConflict || resp.Code != errcode.ErrStorageNotChecked.Code {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("删除预检与删除", func(t *testing.T) {
		env := newStorageAdminEnv()
		w, resp := env.do(t, http.MethodGet, storageBase+"/4/delete-check", nil)
		if w.Code != http.StatusOK || !strings.Contains(string(resp.Data), `"deletable":false`) {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
		if w, resp = env.do(t, http.MethodDelete, storageBase+"/4", nil); w.Code != http.StatusOK || env.svc.deleted != 4 {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
		env.svc.err = errcode.ErrStorageInUse.WithMsg("被 3 个素材引用")
		if w, resp = env.do(t, http.MethodDelete, storageBase+"/4", nil); w.Code != http.StatusConflict || resp.Code != errcode.ErrStorageInUse.Code {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
}
