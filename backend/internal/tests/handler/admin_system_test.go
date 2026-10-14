package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/errcode"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
)

// fakeSystemService 实现 handler.SystemService，记录 force 参数。
type fakeSystemService struct {
	force bool
	err   error
}

func (f *fakeSystemService) Version(context.Context) (*model.SystemVersionView, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &model.SystemVersionView{Version: "0.4.0", LatestMigration: "0001_baseline.sql"}, nil
}

func (f *fakeSystemService) CheckUpdate(_ context.Context, force bool) *model.UpdateCheckView {
	f.force = force
	return &model.UpdateCheckView{Enabled: true, Current: "0.4.0", Latest: "0.5.0", HasUpdate: true}
}

func (f *fakeSystemService) Migrations(context.Context) ([]model.MigrationFileView, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []model.MigrationFileView{{Filename: "0001_baseline.sql", Applied: true}}, nil
}

// fakeUpgradeService 实现 handler.UpgradeService，记录调用参数。
type fakeUpgradeService struct {
	actor    uint64
	upgraded string
	rolled   string
	err      error
}

func (f *fakeUpgradeService) Info(context.Context) (*model.UpgradeInfoView, error) {
	return &model.UpgradeInfoView{Available: true, ImageTag: "0.4.0", ActiveTasks: 2}, nil
}

func (f *fakeUpgradeService) Upgrade(_ context.Context, actor uint64, target string) error {
	f.actor, f.upgraded = actor, target
	return f.err
}

func (f *fakeUpgradeService) Rollback(_ context.Context, actor uint64, target string) error {
	f.actor, f.rolled = actor, target
	return f.err
}

func newSystemEnv(svc *fakeSystemService) *gin.Engine {
	return newSystemEnvWith(svc, &fakeUpgradeService{})
}

func newSystemEnvWith(svc *fakeSystemService, up *fakeUpgradeService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewAdminSystemHandler(svc, up)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(9)) })
	g := r.Group("/api/v1/admin/system")
	g.GET("/version", h.Version)
	g.GET("/updates", h.Updates)
	g.GET("/migrations", h.Migrations)
	g.GET("/upgrade", h.UpgradeInfo)
	g.POST("/upgrade", h.Upgrade)
	g.POST("/rollback", h.Rollback)
	return r
}

type systemResp struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

func getSystem(t *testing.T, r *gin.Engine, path string) (int, systemResp) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	var body systemResp
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

func TestAdminSystemHandler(t *testing.T) {
	t.Run("版本信息", func(t *testing.T) {
		code, body := getSystem(t, newSystemEnv(&fakeSystemService{}), "/api/v1/admin/system/version")
		var v model.SystemVersionView
		_ = json.Unmarshal(body.Data, &v)
		if code != http.StatusOK || v.Version != "0.4.0" || v.LatestMigration != "0001_baseline.sql" {
			t.Fatalf("%d %s", code, body.Data)
		}
	})

	t.Run("检查更新：默认走缓存，force=1 强制", func(t *testing.T) {
		svc := &fakeSystemService{}
		r := newSystemEnv(svc)
		code, body := getSystem(t, r, "/api/v1/admin/system/updates")
		var v model.UpdateCheckView
		_ = json.Unmarshal(body.Data, &v)
		if code != http.StatusOK || !v.HasUpdate || v.Latest != "0.5.0" || svc.force {
			t.Fatalf("%d %s force=%v", code, body.Data, svc.force)
		}
		getSystem(t, r, "/api/v1/admin/system/updates?force=1")
		if !svc.force {
			t.Fatal("force=1 应强制检查")
		}
	})

	t.Run("迁移状态", func(t *testing.T) {
		code, body := getSystem(t, newSystemEnv(&fakeSystemService{}), "/api/v1/admin/system/migrations")
		var v []model.MigrationFileView
		_ = json.Unmarshal(body.Data, &v)
		if code != http.StatusOK || len(v) != 1 || !v[0].Applied {
			t.Fatalf("%d %s", code, body.Data)
		}
	})

	t.Run("读库失败返回错误", func(t *testing.T) {
		r := newSystemEnv(&fakeSystemService{err: errors.New("db down")})
		for _, p := range []string{"/api/v1/admin/system/version", "/api/v1/admin/system/migrations"} {
			if code, body := getSystem(t, r, p); code == http.StatusOK && body.Code == 0 {
				t.Fatalf("%s 读库失败不应返回成功", p)
			}
		}
	})
}

func postSystem(t *testing.T, r *gin.Engine, path, body string) int {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w.Code
}

func TestAdminSystemHandler_Upgrade(t *testing.T) {
	t.Run("升级面板数据", func(t *testing.T) {
		code, body := getSystem(t, newSystemEnv(&fakeSystemService{}), "/api/v1/admin/system/upgrade")
		var v model.UpgradeInfoView
		_ = json.Unmarshal(body.Data, &v)
		if code != http.StatusOK || !v.Available || v.ActiveTasks != 2 {
			t.Fatalf("%d %s", code, body.Data)
		}
	})

	t.Run("升级：返回 202，带上操作人", func(t *testing.T) {
		up := &fakeUpgradeService{}
		code := postSystem(t, newSystemEnvWith(&fakeSystemService{}, up), "/api/v1/admin/system/upgrade", `{"target":"0.5.0"}`)
		if code != http.StatusAccepted || up.upgraded != "0.5.0" || up.actor != 9 {
			t.Fatalf("%d %+v", code, up)
		}
	})

	t.Run("回退", func(t *testing.T) {
		up := &fakeUpgradeService{}
		code := postSystem(t, newSystemEnvWith(&fakeSystemService{}, up), "/api/v1/admin/system/rollback", `{"target":"0.4.0"}`)
		if code != http.StatusAccepted || up.rolled != "0.4.0" {
			t.Fatalf("%d %+v", code, up)
		}
	})

	t.Run("缺少 target 时 400，不调用业务", func(t *testing.T) {
		up := &fakeUpgradeService{}
		code := postSystem(t, newSystemEnvWith(&fakeSystemService{}, up), "/api/v1/admin/system/upgrade", `{}`)
		if code != http.StatusBadRequest || up.upgraded != "" {
			t.Fatalf("%d %+v", code, up)
		}
	})

	t.Run("业务错误按错误码的状态返回", func(t *testing.T) {
		up := &fakeUpgradeService{err: errcode.ErrUpgradeRunning}
		code := postSystem(t, newSystemEnvWith(&fakeSystemService{}, up), "/api/v1/admin/system/upgrade", `{"target":"0.5.0"}`)
		if code != http.StatusConflict {
			t.Fatalf("%d", code)
		}
	})
}
