package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	. "video-canvas/internal/handler"

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

func newSystemEnv(svc *fakeSystemService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewAdminSystemHandler(svc)
	r := gin.New()
	g := r.Group("/api/v1/admin/system")
	g.GET("/version", h.Version)
	g.GET("/updates", h.Updates)
	g.GET("/migrations", h.Migrations)
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
