package handler_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/service"
)

// fakeArchiveHandlerAssets 是打包下载 handler 测试用的素材服务：素材 101 是用户 1 的视频。
type fakeArchiveHandlerAssets struct{}

func (fakeArchiveHandlerAssets) Get(_ context.Context, userID, assetID uint64) (*model.Asset, error) {
	if userID != 1 || assetID != 101 {
		return nil, errcode.ErrAssetNotFound
	}
	return &model.Asset{ID: 101, UserID: 1, Kind: "video", MimeType: "video/mp4", ByteSize: 5, FileName: "a.mp4"}, nil
}

func (fakeArchiveHandlerAssets) Open(ctx context.Context, userID, assetID uint64) (*provider.AssetFile, error) {
	a, err := fakeArchiveHandlerAssets{}.Get(ctx, userID, assetID)
	if err != nil {
		return nil, err
	}
	return &provider.AssetFile{Asset: a, Body: io.NopCloser(strings.NewReader("video"))}, nil
}

// newArchiveRouter 装好打包下载的两条路由：签发要登录（当前用户 1），下载只认票据。
func newArchiveRouter() *gin.Engine {
	repo := &fakeCanvasRepo{items: map[uint64]*model.CanvasProject{
		1: {
			BaseModel: model.BaseModel{ID: 1}, UserID: 1, Title: "雨夜短片",
			PayloadJSON: datatypes.JSON(`{"nodes":[
				{"id":"n1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"S1","assetId":"101","mediaType":"video"}},
				{"id":"n2","type":"canvas","position":{"x":0,"y":200},"data":{"kind":"script","label":"剧本","text":"第一场 夜 外"}}
			]}`),
		},
	}}
	svc := service.NewCanvasArchiveService(service.CanvasArchiveDeps{
		Canvases: repo, Assets: fakeArchiveHandlerAssets{}, Secret: "handler-secret",
		Now: func() time.Time { return time.Date(2026, 11, 22, 21, 30, 0, 0, time.UTC) },
	})
	h := NewCanvasArchiveHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	g := r.Group("/api/v1/canvas")
	g.POST("/:id/assets/archive", h.Ticket)
	r.GET("/api/v1/canvas/:id/assets/archive", h.Download)
	return r
}

func TestCanvasArchiveHandler_TicketThenDownload(t *testing.T) {
	r := newArchiveRouter()
	canvasPath := "/api/v1/canvas/" + idcodec.Encode(1) + "/assets/archive"

	status, resp := canvasCall(t, r, http.MethodPost, canvasPath, "")
	if status != http.StatusOK || respCode(resp) != 0 {
		t.Fatalf("签发应成功：%d %v", status, resp)
	}
	data := resp["data"].(map[string]any)
	url, _ := data["url"].(string)
	if !strings.Contains(url, "token=") {
		t.Fatalf("返回地址应带票据：%v", url)
	}
	if data["count"] != float64(2) {
		t.Fatalf("文件数应为 2（一条视频 + 一份剧本），实际 %v", data["count"])
	}
	groups, _ := data["groups"].(map[string]any)
	if groups["视频"] != float64(1) || groups["剧本"] != float64(1) {
		t.Fatalf("分组统计不对：%v", groups)
	}

	// 用票据下载：不带任何请求头，模拟浏览器 <a download>
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1"+url, nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("下载应成功：%d %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("Content-Type = %q", ct)
	}
	disposition := w.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, "attachment;") || !strings.Contains(disposition, "filename*=UTF-8''") {
		t.Fatalf("Content-Disposition 不对：%q", disposition)
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("返回的不是合法 zip：%v", err)
	}
	// 解压出来直接是分组目录，不再套一层和压缩包同名的文件夹
	found := map[string]bool{}
	for _, f := range zr.File {
		found[f.Name] = true
	}
	for _, want := range []string{
		"视频/S1.mp4", "剧本/剧本.txt", "manifest.md", "manifest.csv",
	} {
		if !found[want] {
			t.Fatalf("压缩包缺少 %q，实际 %v", want, found)
		}
	}
}

func TestCanvasArchiveHandler_DownloadRejectsBadTicket(t *testing.T) {
	r := newArchiveRouter()
	canvasPath := "/api/v1/canvas/" + idcodec.Encode(1) + "/assets/archive"

	for _, query := range []string{"", "?token=bad", "?token=v1.1.1.9999999999.zzz"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, canvasPath+query, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("票据 %q 应该被拒，实际 %d", query, w.Code)
		}
	}
}

func TestCanvasArchiveHandler_DownloadRejectsOtherCanvas(t *testing.T) {
	r := newArchiveRouter()
	status, resp := canvasCall(t, r, http.MethodPost, "/api/v1/canvas/"+idcodec.Encode(1)+"/assets/archive", "")
	if status != http.StatusOK {
		t.Fatalf("签发应成功：%d %v", status, resp)
	}
	url := resp["data"].(map[string]any)["url"].(string)
	token := url[strings.Index(url, "token=")+len("token="):]

	// 拿画布 1 的票去下画布 2
	other := "/api/v1/canvas/" + idcodec.Encode(2) + "/assets/archive?token=" + token
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, other, nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("票和画布不一致应该被拒，实际 %d", w.Code)
	}
}

func TestCanvasArchiveHandler_TicketOnEmptyCanvas(t *testing.T) {
	repo := &fakeCanvasRepo{items: map[uint64]*model.CanvasProject{
		1: {BaseModel: model.BaseModel{ID: 1}, UserID: 1, Title: "空", PayloadJSON: datatypes.JSON(`{"nodes":[]}`)},
	}}
	svc := service.NewCanvasArchiveService(service.CanvasArchiveDeps{
		Canvases: repo, Assets: fakeArchiveHandlerAssets{}, Secret: "s",
	})
	h := NewCanvasArchiveHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	r.POST("/api/v1/canvas/:id/assets/archive", h.Ticket)

	status, resp := canvasCall(t, r, http.MethodPost, "/api/v1/canvas/"+idcodec.Encode(1)+"/assets/archive", "")
	if status != http.StatusBadRequest || respCode(resp) != 30022 {
		t.Fatalf("空画布应该报 30022，实际 %d %v", status, resp)
	}
}
