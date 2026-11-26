package repository_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
)

func TestCanvasShareHTTPScopedMedia(t *testing.T) {
	db, svc, assets := canvasSeed(t)
	const secret = "canvas-share-http-test-secret"
	handlers := router.Handlers{Health: &handler.HealthHandler{}, CanvasProject: handler.NewCanvasProjectHandler(service.NewCanvasProjectService(repository.NewCanvasProjectRepository(db))), CanvasShare: handler.NewCanvasShareHandler(svc), UserState: func(context.Context, uint64) (*middleware.UserState, error) {
		return &middleware.UserState{Status: "active", Role: "user", TokenVersion: 1}, nil
	}}
	engine := router.New("test", secret, handlers)
	send := func(method, path, body string, user uint64, cookie *http.Cookie, ranges ...string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if user != 0 {
			token, _, err := utils.GenerateToken(uint(user), "", 1, secret, "test", 1)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if len(ranges) > 0 {
			req.Header.Set("Range", ranges[0])
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}
	canvas := "/canvas/" + idcodec.Encode(8)
	media := "/canvas-media/" + idcodec.Encode(8) + "/" + idcodec.Encode(10)
	if w := send("POST", canvas+"/shares", `{"mode":"view","email":"recv@test.example"}`, 42, nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := send("GET", canvas, "", 1, nil); w.Code != 404 {
		t.Fatal("普通详情未隔离", w.Code)
	}
	reader := send("GET", canvas+"/reader", "", 1, nil)
	cookies := reader.Result().Cookies()
	if reader.Code != 200 || len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("缺少限定作用域的媒体凭据", reader.Code)
	}
	if w := send("GET", canvas+"/reader?revision=1", "", 1, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"unchanged":true`) || strings.Contains(w.Body.String(), `"payload_json"`) || len(w.Result().Cookies()) != 1 {
		t.Fatal("同版本检查应只续签媒体凭据并返回未变更", w.Code, w.Body.String())
	}
	if w := send("PUT", canvas, `{"title":"viewer-edit","revision":1}`, 1, nil); w.Code != 404 {
		t.Fatal("查看者不能保存原作", w.Code, w.Body.String())
	}
	if w := send("PUT", canvas, `{"title":"owner-edit","revision":1}`, 42, nil); w.Code != 200 {
		t.Fatal("作者应能保存新版本", w.Code, w.Body.String())
	}
	if w := send("GET", canvas+"/reader?revision=1", "", 1, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"title":"owner-edit"`) || !strings.Contains(w.Body.String(), `"revision":2`) || !strings.Contains(w.Body.String(), `"payload_json"`) {
		t.Fatal("持旧版本的查看者应收到作者保存后的投影", w.Code, w.Body.String())
	}
	if w := send("GET", media, "", 0, cookies[0]); w.Code != 200 || w.Body.String() != "sample" || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("媒体读取失败", w.Code)
	}
	if w := send("GET", media, "", 0, cookies[0], "bytes=1-3"); w.Code != 206 || w.Body.String() != "amp" || w.Header().Get("Content-Range") != "bytes 1-3/6" {
		t.Fatal("远程媒体分段读取失败", w.Code, w.Body.String())
	}
	if w := send("GET", media, "", 0, cookies[0], "bytes=-2"); w.Code != 206 || w.Body.String() != "le" {
		t.Fatal("远程媒体尾段读取失败", w.Code, w.Body.String())
	}
	if w := send("GET", media, "", 0, cookies[0], "bytes=7-"); w.Code != 416 || w.Header().Get("Content-Range") != "bytes */6" {
		t.Fatal("超范围的媒体请求应返回 416", w.Code)
	}
	if w := send("HEAD", media, "", 0, cookies[0]); w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "6" {
		t.Fatal("媒体元数据读取失败", w.Code, w.Body.String())
	}
	poster := media + "?v=poster"
	if w := send("GET", poster, "", 0, cookies[0]); w.Code != 200 || w.Body.String() != "poster" || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("Location") != "" || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("应在共享范围内流式返回封面", w.Code, w.Body.String())
	}
	if w := send("HEAD", poster, "", 0, cookies[0]); w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "6" {
		t.Fatal("封面元数据读取失败", w.Code, w.Body.String())
	}
	if w := send("GET", "/canvas-media/"+idcodec.Encode(8)+"/"+idcodec.Encode(99)+"?v=poster", "", 0, cookies[0]); w.Code != 404 || assets.posterReads != 2 {
		t.Fatal("范围外的素材不应请求封面", w.Code, assets.posterReads)
	}
	if w := send("GET", "/canvas-media/"+idcodec.Encode(9)+"/"+idcodec.Encode(10), "", 0, cookies[0]); w.Code != 401 {
		t.Fatal("跨画布票据应失效", w.Code)
	}
	if w := send("DELETE", canvas+"/shares/"+idcodec.Encode(1), "", 42, nil); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if w := send("GET", canvas+"/reader?revision=2", "", 1, nil); w.Code != 404 || len(w.Result().Cookies()) != 0 {
		t.Fatal("撤权后版本检查不能续签媒体凭据", w.Code)
	}
	if w := send("GET", media, "", 0, cookies[0], "bytes=1-3"); w.Code != 404 {
		t.Fatal("已撤权的票据仍可读", w.Code)
	}
	if w := send("GET", poster, "", 0, cookies[0]); w.Code != 404 || assets.posterReads != 2 {
		t.Fatal("已撤权的票据不应请求封面", w.Code, assets.posterReads)
	}
}
