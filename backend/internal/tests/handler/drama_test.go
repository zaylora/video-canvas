package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// httpDramaRepo 只提供 HTTP 场景需要的读取原语，其他调用会暴露为测试失败。
type httpDramaRepo struct {
	repository.DramaTx
	project *model.DramaProject
	episode *model.DramaEpisode
	jobs    []model.DramaJob
	credit  model.UserCredit
}

func (r *httpDramaRepo) WithTx(_ context.Context, fn func(repository.DramaTx) error) error {
	return fn(r)
}
func (r *httpDramaRepo) List(_ context.Context, userID uint64, keyword string, _, _ int) ([]model.DramaProject, int64, error) {
	if r.project == nil || r.project.UserID != userID || !strings.Contains(r.project.Title, keyword) {
		return []model.DramaProject{}, 0, nil
	}
	return []model.DramaProject{*r.project}, 1, nil
}
func (r *httpDramaRepo) Project(_ context.Context, userID, id uint64, _ bool) (*model.DramaProject, error) {
	if r.project == nil || r.project.ID != id || r.project.UserID != userID {
		return nil, repository.ErrNotFound
	}
	p := *r.project
	return &p, nil
}
func (r *httpDramaRepo) Episodes(_ context.Context, userID, id uint64) ([]model.DramaEpisode, error) {
	if r.episode != nil && r.episode.UserID == userID && r.episode.DramaID == id {
		return []model.DramaEpisode{*r.episode}, nil
	}
	return []model.DramaEpisode{}, nil
}
func (r *httpDramaRepo) Jobs(_ context.Context, userID, dramaID uint64, active bool) ([]model.DramaJob, error) {
	rows := []model.DramaJob{}
	for _, j := range r.jobs {
		if j.UserID == userID && j.DramaID == dramaID && (!active || j.Status == "queued" || j.Status == "running") {
			rows = append(rows, j)
		}
	}
	return rows, nil
}

func httpDramaRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	repo := &httpDramaRepo{project: &model.DramaProject{BaseModel: model.BaseModel{ID: 7}, UserID: 1, Title: "母亲", CanvasID: 9, StyleKey: "real_city_film", Ratio: "9:16", Revision: 1}, episode: &model.DramaEpisode{BaseModel: model.BaseModel{ID: 10}, UserID: 1, DramaID: 7, No: 1, Title: "归来", ScriptMD: "正文", ScriptStatus: "ready", Revision: 1}, credit: model.UserCredit{UserID: 1, Balance: 100}}
	h := handler.NewDramaHandler(service.NewDramaService(service.DramaDeps{Repo: repo, Registry: httpDramaRegistry{}}))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	r.GET("/drama-styles", h.Styles)
	r.GET("/dramas", h.List)
	r.POST("/dramas", h.Create)
	r.GET("/dramas/:id", h.Get)
	r.PATCH("/dramas/:id", h.Update)
	r.DELETE("/dramas/:id", h.Delete)
	r.GET("/dramas/:id/episodes/:no", h.GetEpisode)
	r.POST("/dramas/:id/episodes", h.CreateEpisode)
	r.PATCH("/dramas/:id/episodes/:no", h.UpdateEpisode)
	r.POST("/dramas/:id/estimate", h.Estimate)
	r.POST("/dramas/:id/jobs", h.CreateJob)
	r.GET("/dramas/:id/jobs", h.ListJobs)
	r.POST("/dramas/:id/jobs/:job_id/cancel", h.CancelJob)
	return r
}

func TestDramaHandler_ReadAndIsolation(t *testing.T) {
	r := httpDramaRouter()
	id := idcodec.Encode(7)
	for _, tc := range []struct {
		path         string
		status, code int
	}{
		{"/dramas/" + id, 200, 0},
		{"/dramas/" + id + "/episodes/1", 200, 0},
		{"/dramas/" + id + "/jobs?status=active", 200, 0},
		{"/dramas?keyword=母&page=1&page_size=20", 200, 0},
		{"/dramas/" + idcodec.Encode(99), 404, 67001},
		{"/dramas/" + id + "/episodes/2", 404, 67001},
	} {
		t.Run(tc.path, func(t *testing.T) {
			status, body := characterCall(t, r, http.MethodGet, tc.path, "")
			if status != tc.status || responseCode(t, body) != tc.code {
				t.Fatalf("status=%d body=%+v", status, body)
			}
		})
	}
	status, body := characterCall(t, r, http.MethodGet, "/dramas/"+id+"/episodes/1", "")
	if status != 200 || responseData(t, body)["script_md"] != "正文" {
		t.Fatalf("单集正文丢失：%+v", body)
	}
	_, body = characterCall(t, r, http.MethodGet, "/dramas/"+id, "")
	episodes := responseData(t, body)["episodes"].([]any)
	if _, hasScript := episodes[0].(map[string]any)["script_md"]; hasScript {
		t.Fatal("项目详情不应包含分集正文")
	}
}

func TestDramaHandler_Validation(t *testing.T) {
	r := httpDramaRouter()
	base := "/dramas/" + idcodec.Encode(7)
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/dramas/not-an-id", ""},
		{"GET", base + "/episodes/0", ""},
		{"GET", base + "/episodes/abc", ""},
		{"GET", "/dramas?page=no", ""},
		{"PATCH", base, `{"title":"缺版本"}`},
		{"PATCH", base + "/episodes/1", `{"revision":0}`},
		{"POST", base + "/episodes", `{"title":1}`},
		{"POST", base + "/estimate", `{"step":"compose"}`},
		{"POST", base + "/jobs", `{"step":"breakdown","episodes":[0]}`},
		{"POST", base + "/jobs/invalid/cancel", ""},
		{"GET", base + "/jobs?status=typo", ""},
	} {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			status, body := characterCall(t, r, tc.method, tc.path, tc.body)
			if status != 400 || responseCode(t, body) != 10001 {
				t.Fatalf("status=%d body=%+v", status, body)
			}
		})
	}
}

func TestDramaHandler_Styles(t *testing.T) {
	status, body := characterCall(t, httpDramaRouter(), "GET", "/drama-styles", "")
	data := responseData(t, body)
	if status != 200 || len(data["genres"].([]any)) != 12 || len(data["styles"].([]any)) != 11 {
		t.Fatalf("风格契约：%+v", body)
	}
}

func TestDramaHandler_CreateSourceExclusive(t *testing.T) {
	for _, both := range []bool{false, true} {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		if err := form.WriteField("style_key", "real_city_film"); err != nil {
			t.Fatal(err)
		}
		if both {
			if err := form.WriteField("text", "原文"); err != nil {
				t.Fatal(err)
			}
			part, err := form.CreateFormFile("file", "剧本.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write([]byte("原文")); err != nil {
				t.Fatal(err)
			}
		}
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/dramas", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		w := httptest.NewRecorder()
		httpDramaRouter().ServeHTTP(w, req)
		var got struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if w.Code != 400 || got.Code != 67002 {
			t.Fatalf("二选一原文：status=%d body=%s", w.Code, w.Body.String())
		}
	}
}
