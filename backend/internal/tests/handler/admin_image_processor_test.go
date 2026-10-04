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

	"video-canvas/internal/imageproc"
	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/service"
)

// fakeProcessorAdmin 实现 handler.ImageProcessorAdminService，记录收到的参数，可注入业务错误。
type fakeProcessorAdmin struct {
	err error

	created   service.ProcessorCreateInput
	updated   service.ProcessorUpdateInput
	actor     uint64
	targetID  uint64
	published int
	calls     []string
}

func (f *fakeProcessorAdmin) view(id uint64) *service.ProcessorView {
	return &service.ProcessorView{ID: id, Name: "R2 处理", Vendor: imageproc.VendorCloudflare, StorageID: 2, Status: "draft", Version: 1}
}

func (f *fakeProcessorAdmin) Presets() []imageproc.Preset { return imageproc.Presets() }
func (f *fakeProcessorAdmin) List(context.Context) ([]service.ProcessorView, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []service.ProcessorView{*f.view(1)}, nil
}
func (f *fakeProcessorAdmin) Get(_ context.Context, id uint64) (*service.ProcessorView, error) {
	f.targetID = id
	if f.err != nil {
		return nil, f.err
	}
	return f.view(id), nil
}
func (f *fakeProcessorAdmin) Create(_ context.Context, actor uint64, in service.ProcessorCreateInput) (*service.ProcessorView, error) {
	f.actor, f.created = actor, in
	if f.err != nil {
		return nil, f.err
	}
	return f.view(7), nil
}
func (f *fakeProcessorAdmin) Update(_ context.Context, actor, id uint64, in service.ProcessorUpdateInput) (*service.ProcessorView, error) {
	f.actor, f.targetID, f.updated = actor, id, in
	if f.err != nil {
		return nil, f.err
	}
	return f.view(id), nil
}
func (f *fakeProcessorAdmin) Check(_ context.Context, actor, id uint64) (*service.ProcessorView, error) {
	f.actor, f.targetID = actor, id
	f.calls = append(f.calls, "check")
	if f.err != nil {
		return nil, f.err
	}
	return f.view(id), nil
}
func (f *fakeProcessorAdmin) Publish(_ context.Context, actor, id uint64, version int) (*service.ProcessorView, error) {
	f.actor, f.targetID, f.published = actor, id, version
	f.calls = append(f.calls, "publish")
	if f.err != nil {
		return nil, f.err
	}
	return f.view(id), nil
}
func (f *fakeProcessorAdmin) Rollback(_ context.Context, actor, id uint64) (*service.ProcessorView, error) {
	f.actor, f.targetID = actor, id
	f.calls = append(f.calls, "rollback")
	if f.err != nil {
		return nil, f.err
	}
	return f.view(id), nil
}
func (f *fakeProcessorAdmin) Disable(_ context.Context, actor, id uint64) (*service.ProcessorView, error) {
	f.actor, f.targetID = actor, id
	f.calls = append(f.calls, "disable")
	if f.err != nil {
		return nil, f.err
	}
	return f.view(id), nil
}
func (f *fakeProcessorAdmin) Delete(_ context.Context, actor, id uint64) error {
	f.actor, f.targetID = actor, id
	f.calls = append(f.calls, "delete")
	return f.err
}

type processorEnv struct {
	router *gin.Engine
	svc    *fakeProcessorAdmin
}

const processorBase = "/api/v1/admin/image-processors"

func newProcessorEnv() *processorEnv {
	gin.SetMode(gin.TestMode)
	svc := &fakeProcessorAdmin{}
	h := NewAdminImageProcessorHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(9)) })
	g := r.Group(processorBase)
	g.GET("", h.List)
	g.GET("/presets", h.Presets)
	g.POST("", h.Create)
	g.GET("/:id", h.Get)
	g.PUT("/:id", h.Update)
	g.POST("/:id/check", h.Check)
	g.POST("/:id/publish", h.Publish)
	g.POST("/:id/rollback", h.Rollback)
	g.POST("/:id/disable", h.Disable)
	g.DELETE("/:id", h.Delete)
	return &processorEnv{router: r, svc: svc}
}

func (e *processorEnv) do(t *testing.T, method, path string, body any) (*httptest.ResponseRecorder, storageResp) {
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

func TestAdminImageProcessor_ListGetPresets(t *testing.T) {
	env := newProcessorEnv()
	w, resp := env.do(t, http.MethodGet, processorBase+"/presets", nil)
	if w.Code != http.StatusOK || !strings.Contains(string(resp.Data), "tencent_cos") || !strings.Contains(string(resp.Data), "aliyun_oss_img") || strings.Contains(string(resp.Data), "aws") {
		t.Fatalf("预设应含腾讯云、阿里云、Cloudflare 且不含 AWS：%s", w.Body.String())
	}
	if w, resp = env.do(t, http.MethodGet, processorBase, nil); w.Code != http.StatusOK || !strings.Contains(string(resp.Data), "R2 处理") {
		t.Fatalf("列表：%d %s", w.Code, w.Body.String())
	}
	if w, _ = env.do(t, http.MethodGet, processorBase+"/5", nil); w.Code != http.StatusOK || env.svc.targetID != 5 {
		t.Fatalf("详情：%d %s", w.Code, w.Body.String())
	}
	t.Run("id 非法 400", func(t *testing.T) {
		if w, resp := env.do(t, http.MethodGet, processorBase+"/abc", nil); w.Code != http.StatusBadRequest || resp.Code != errcode.ErrInvalidParams.Code {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("不存在 404 / 52001", func(t *testing.T) {
		env.svc.err = errcode.ErrProcessorNotFound
		defer func() { env.svc.err = nil }()
		if w, resp := env.do(t, http.MethodGet, processorBase+"/5", nil); w.Code != http.StatusNotFound || resp.Code != 52001 {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
}

func TestAdminImageProcessor_Create(t *testing.T) {
	body := func() map[string]any {
		return map[string]any{"name": "R2 处理", "vendor": "cloudflare", "storage_id": 2,
			"config": map[string]any{"domain": "assets.example.com", "width": 512, "format": "auto", "time_sec": 0.5, "quality": 75, "on_error_redirect": true}}
	}

	t.Run("成功：参数原样交给 service，操作人来自登录态", func(t *testing.T) {
		env := newProcessorEnv()
		w, resp := env.do(t, http.MethodPost, processorBase, body())
		if w.Code != http.StatusOK || resp.Code != 0 {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
		in := env.svc.created
		if env.svc.actor != 9 || in.Name != "R2 处理" || in.Vendor != "cloudflare" || in.StorageID != 2 ||
			in.Config.Domain != "assets.example.com" || in.Config.TimeSec != 0.5 || in.Config.Quality != 75 || in.Config.OnErrorRedirect == nil || !*in.Config.OnErrorRedirect {
			t.Fatalf("参数没有原样传递：actor=%d %+v", env.svc.actor, in)
		}
	})

	t.Run("参数校验失败 400 / 10001", func(t *testing.T) {
		for name, mut := range map[string]func(map[string]any){
			"缺名称":      func(m map[string]any) { delete(m, "name") },
			"缺厂商":      func(m map[string]any) { delete(m, "vendor") },
			"缺存储":      func(m map[string]any) { delete(m, "storage_id") },
			"名称过长":     func(m map[string]any) { m["name"] = strings.Repeat("a", 65) },
			"存储 id 非正": func(m map[string]any) { m["storage_id"] = 0 },
		} {
			t.Run(name, func(t *testing.T) {
				env := newProcessorEnv()
				b := body()
				mut(b)
				w, resp := env.do(t, http.MethodPost, processorBase, b)
				if w.Code != http.StatusBadRequest || resp.Code != errcode.ErrInvalidParams.Code {
					t.Fatalf("实际 %d %s", w.Code, w.Body.String())
				}
				if env.svc.created.Name != "" {
					t.Error("校验失败不应调用 service")
				}
			})
		}
	})

	t.Run("业务错误透传：厂商与存储不匹配 400 / 52005", func(t *testing.T) {
		env := newProcessorEnv()
		env.svc.err = errcode.ErrProcessorStorageMismatch.WithMsg("这是「Cloudflare R2」存储")
		w, resp := env.do(t, http.MethodPost, processorBase, body())
		if w.Code != http.StatusBadRequest || resp.Code != 52005 || !strings.Contains(resp.Msg, "Cloudflare R2") {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
}

func TestAdminImageProcessor_Update(t *testing.T) {
	env := newProcessorEnv()
	b := map[string]any{"version": 3, "name": "新名字", "config": map[string]any{"domain": "a.example.com", "width": 256, "format": "webp"}}
	w, resp := env.do(t, http.MethodPut, processorBase+"/4", b)
	if w.Code != http.StatusOK || resp.Code != 0 || env.svc.targetID != 4 || env.svc.updated.Version != 3 || env.svc.updated.Name != "新名字" || env.svc.updated.Config.Width != 256 {
		t.Fatalf("实际 %d %s %+v", w.Code, w.Body.String(), env.svc.updated)
	}
	t.Run("缺 version 400", func(t *testing.T) {
		w, resp := env.do(t, http.MethodPut, processorBase+"/4", map[string]any{"name": "x", "config": map[string]any{}})
		if w.Code != http.StatusBadRequest || resp.Code != errcode.ErrInvalidParams.Code {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("版本冲突 409 / 52004", func(t *testing.T) {
		env.svc.err = errcode.ErrProcessorVersionConflict
		defer func() { env.svc.err = nil }()
		if w, resp := env.do(t, http.MethodPut, processorBase+"/4", b); w.Code != http.StatusConflict || resp.Code != 52004 {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
}

func TestAdminImageProcessor_Lifecycle(t *testing.T) {
	env := newProcessorEnv()
	for _, step := range []struct{ path, want string }{
		{"/6/check", "check"}, {"/6/rollback", "rollback"}, {"/6/disable", "disable"},
	} {
		if w, resp := env.do(t, http.MethodPost, processorBase+step.path, nil); w.Code != http.StatusOK || resp.Code != 0 || env.svc.targetID != 6 || env.svc.actor != 9 {
			t.Fatalf("%s：%d %s", step.path, w.Code, w.Body.String())
		}
	}
	if strings.Join(env.svc.calls, ",") != "check,rollback,disable" {
		t.Fatalf("调用顺序不对：%v", env.svc.calls)
	}

	t.Run("发布要带 version", func(t *testing.T) {
		if w, resp := env.do(t, http.MethodPost, processorBase+"/6/publish", map[string]any{"version": 4}); w.Code != http.StatusOK || resp.Code != 0 || env.svc.published != 4 {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
		if w, resp := env.do(t, http.MethodPost, processorBase+"/6/publish", map[string]any{}); w.Code != http.StatusBadRequest || resp.Code != errcode.ErrInvalidParams.Code {
			t.Fatalf("缺 version 应 400：%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("没通过校验不能发布 409 / 52007", func(t *testing.T) {
		env.svc.err = errcode.ErrProcessorNotChecked
		defer func() { env.svc.err = nil }()
		if w, resp := env.do(t, http.MethodPost, processorBase+"/6/publish", map[string]any{"version": 4}); w.Code != http.StatusConflict || resp.Code != 52007 {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("删除", func(t *testing.T) {
		if w, resp := env.do(t, http.MethodDelete, processorBase+"/6", nil); w.Code != http.StatusOK || resp.Code != 0 {
			t.Fatalf("实际 %d %s", w.Code, w.Body.String())
		}
		env.svc.err = errcode.ErrProcessorPublished
		defer func() { env.svc.err = nil }()
		if w, resp := env.do(t, http.MethodDelete, processorBase+"/6", nil); w.Code != http.StatusConflict || resp.Code != 52008 {
			t.Fatalf("已发布不能删：%d %s", w.Code, w.Body.String())
		}
	})
}
