package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/service"
)

type hybridAdminFake struct {
	view service.StorageHybridView
	err  error
}

func (f *hybridAdminFake) Status(context.Context) (service.StorageHybridView, error) {
	return f.view, f.err
}

func (f *hybridAdminFake) Enable(_ context.Context, limit int64) (service.StorageHybridView, error) {
	f.view.State, f.view.LimitBytes = model.StorageHybridPreparing, limit
	return f.view, f.err
}

func (f *hybridAdminFake) Disable(context.Context) (service.StorageHybridView, error) {
	f.view.State = model.StorageHybridOff
	return f.view, f.err
}

func TestAdminStorageHybridEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	fake := &hybridAdminFake{view: service.StorageHybridView{HasCloud: true}}
	admin := handler.NewAdminStorageHandler(nil).WithHybrid(fake)
	router.GET("/hybrid", admin.Hybrid)
	router.PUT("/hybrid", admin.SetHybrid)

	for _, tt := range []struct {
		method, request, want string
		status                int
	}{
		{http.MethodGet, "", `"has_cloud":true`, http.StatusOK},
		{http.MethodPut, `{"enabled":true,"limit_bytes":250}`, `"state":"preparing"`, http.StatusOK},
		{http.MethodPut, `{"enabled":false}`, `"state":"off"`, http.StatusOK},
		{http.MethodPut, `{"enabled":true,"limit_bytes":-1}`, "10001", http.StatusBadRequest},
		{http.MethodPut, `{"limit_bytes":0}`, "10001", http.StatusBadRequest},
	} {
		assertHybridHTTP(t, router, tt.method, tt.request, tt.want, tt.status)
	}
	fake.err = errcode.ErrStorageHybridLocked
	assertHybridHTTP(t, router, http.MethodPut, `{"enabled":false}`, "51013", http.StatusConflict)
}

func assertHybridHTTP(t *testing.T, router *gin.Engine, method, request, want string, status int) {
	t.Helper()
	req := httptest.NewRequest(method, "/hybrid", strings.NewReader(request))
	req.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	if out.Code != status || !strings.Contains(out.Body.String(), want) {
		t.Fatalf("响应不符: %s %s => %d %s", method, request, out.Code, out.Body.String())
	}
}
