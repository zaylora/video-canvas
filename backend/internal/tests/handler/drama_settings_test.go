package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type settingsHTTPRepo struct {
	*httpDramaRepo
	drafts  map[string]model.DramaSourceDraft
	units   []model.DramaUnit
	prompts map[uint64]model.DramaPrompt
}

func (r *settingsHTTPRepo) WithTx(_ context.Context, fn func(repository.DramaTx) error) error {
	return fn(r)
}
func (r *settingsHTTPRepo) CreateDraft(_ context.Context, d *model.DramaSourceDraft) error {
	r.drafts[d.ID] = *d
	return nil
}
func (r *settingsHTTPRepo) Draft(_ context.Context, user uint64, token string, _ bool) (*model.DramaSourceDraft, error) {
	d, ok := r.drafts[token]
	if !ok || d.UserID != user || !d.ExpiresAt.After(time.Now()) {
		return nil, repository.ErrNotFound
	}
	return &d, nil
}
func (r *settingsHTTPRepo) DeleteDraft(_ context.Context, _ uint64, token string) error {
	delete(r.drafts, token)
	return nil
}
func (r *settingsHTTPRepo) InsertUnits(_ context.Context, units []model.DramaUnit) error {
	r.units = append(r.units, units...)
	return nil
}
func (r *settingsHTTPRepo) Units(_ context.Context, user, id uint64, from, to int) ([]model.DramaUnit, error) {
	out := []model.DramaUnit{}
	for _, u := range r.units {
		if u.UserID == user && u.DramaID == id && u.No >= from && (to == 0 || u.No <= to) {
			out = append(out, u)
		}
	}
	return out, nil
}
func (r *settingsHTTPRepo) Prompts(_ context.Context, user uint64, step string) ([]model.DramaPrompt, error) {
	out := []model.DramaPrompt{}
	for _, p := range r.prompts {
		if p.UserID == user && (step == "" || p.Step == step) {
			out = append(out, p)
		}
	}
	return out, nil
}
func (r *settingsHTTPRepo) Prompt(_ context.Context, user, id uint64) (*model.DramaPrompt, error) {
	p, ok := r.prompts[id]
	if !ok || p.UserID != user {
		return nil, repository.ErrNotFound
	}
	return &p, nil
}
func (r *settingsHTTPRepo) SavePrompt(_ context.Context, p *model.DramaPrompt) error {
	for _, old := range r.prompts {
		if old.UserID == p.UserID && old.Step == p.Step && old.Name == p.Name && old.ID != p.ID {
			return repository.ErrDuplicate
		}
	}
	if p.ID == 0 {
		p.ID = uint64(len(r.prompts) + 1)
	} else if r.prompts[p.ID].Revision != p.Revision {
		return repository.ErrStateConflict
	} else {
		p.Revision++
	}
	r.prompts[p.ID] = *p
	return nil
}
func (r *settingsHTTPRepo) DeletePrompt(_ context.Context, user, id uint64) error {
	if _, e := r.Prompt(context.TODO(), user, id); e != nil {
		return e
	}
	delete(r.prompts, id)
	return nil
}

func settingsRouter() (*gin.Engine, *settingsHTTPRepo) {
	repo := &settingsHTTPRepo{httpDramaRepo: &httpDramaRepo{credit: model.UserCredit{UserID: 1, Balance: 10000}}, drafts: map[string]model.DramaSourceDraft{}, prompts: map[uint64]model.DramaPrompt{}}
	h := handler.NewDramaHandler(service.NewDramaService(service.DramaDeps{Repo: repo, Settings: repo, Registry: httpDramaRegistry{}}))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	r.POST("/dramas", h.Create)
	r.POST("/drama-sources/preflight", h.Preflight)
	r.GET("/drama-skills", h.Skills)
	r.GET("/dramas/:id/units", h.Units)
	r.GET("/dramas/:id/units/:no", h.Unit)
	r.POST("/dramas/:id/planning/pause", h.PlanningPause)
	r.POST("/dramas/:id/planning/resume", h.PlanningResume)
	r.POST("/dramas/:id/planning/range", h.PlanningRange)
	r.POST("/dramas/:id/planning/estimate", h.PlanningEstimate)
	r.POST("/dramas/:id/planning/retry-builtin", h.PlanningRetryBuiltin)
	r.GET("/drama-prompt-templates", h.PromptTemplates)
	r.GET("/drama-prompts", h.ListPrompts)
	r.POST("/drama-prompts", h.CreatePrompt)
	r.PUT("/drama-prompts/:id", h.UpdatePrompt)
	r.DELETE("/drama-prompts/:id", h.DeletePrompt)
	r.POST("/drama-sources/:token/estimate", h.EstimateSource)
	return r, repo
}

func TestDramaHandler_PreflightEstimateAndUnits(t *testing.T) {
	r, repo := settingsRouter()
	for _, text := range []string{"", "第1章 开始\n正文\n第2章 结束\n结尾"} {
		var buf bytes.Buffer
		form := multipart.NewWriter(&buf)
		if err := form.WriteField("text", text); err != nil {
			t.Fatal(err)
		}
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/drama-sources/preflight", &buf)
		req.Header.Set("Content-Type", form.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if text == "" {
			if w.Code != 400 || responseCode(t, body) != 67002 {
				t.Fatalf("%d %+v", w.Code, body)
			}
			continue
		}
		if w.Code != 200 {
			t.Fatalf("%d %+v", w.Code, body)
		}
		v := responseData(t, body)
		token := v["source_token"].(string)
		units := v["units"].([]any)
		if _, ok := units[0].(map[string]any)["text"]; ok {
			t.Fatal("预检泄露原文")
		}
		status, estimate := characterCall(t, r, "POST", "/drama-sources/"+token+"/estimate", `{"source_kind":"novel","range_end":1}`)
		if status != 200 || responseData(t, estimate)["episodes_exact"] != false {
			t.Fatalf("%d %+v", status, estimate)
		}
		status, created := characterCall(t, r, "POST", "/dramas", `{"source_token":"`+token+`","source_kind":"novel","ratio":"9:16","style_key":"real_city_film"}`)
		if status != 201 {
			t.Fatalf("%d %+v", status, created)
		}
	}
	base := "/dramas/" + idcodec.Encode(7)
	for _, tc := range []struct {
		method, path, body string
		status, code       int
	}{
		{"GET", base + "/units?from=1&to=2", "", 200, 0},
		{"GET", base + "/units/1", "", 200, 0},
		{"GET", base + "/units?from=2&to=1", "", 400, 10001},
		{"GET", base + "/units/99", "", 404, 67001},
		{"POST", "/drama-sources/expired/estimate", `{"source_kind":"novel"}`, 404, 67014},
		{"POST", "/drama-sources/expired/estimate", `{"source_kind":"unknown"}`, 400, 10001},
		{"GET", "/drama-skills?step=assets", "", 200, 0},
		{"GET", "/drama-skills?step=bad", "", 400, 10001},
	} {
		status, body := characterCall(t, r, tc.method, tc.path, tc.body)
		if status != tc.status || responseCode(t, body) != tc.code {
			t.Fatalf("%s %d %+v", tc.path, status, body)
		}
	}
	if len(repo.units) != 2 {
		t.Fatal("建剧未保存单元")
	}
}

func TestDramaHandler_PlanningStateValidation(t *testing.T) {
	r, repo := settingsRouter()
	repo.project = &model.DramaProject{BaseModel: model.BaseModel{ID: 7}, UserID: 1, Revision: 1, Phase: "running", UnitsTotal: 2, RangeEnd: 1}
	base := "/dramas/" + idcodec.Encode(7) + "/planning/"
	for _, tc := range []struct {
		action, body string
		status, code int
	}{
		{"pause", "", 200, 0},
		{"pause", "", 409, 67019},
		{"range", `{"range_end":3}`, 400, 10001},
		{"range", `{"range_end":2}`, 409, 67019},
		{"retry-builtin", `{"step":"assets"}`, 409, 67019},
		{"retry-builtin", `{"step":"bad"}`, 400, 10001},
	} {
		status, body := characterCall(t, r, "POST", base+tc.action, tc.body)
		if status != tc.status || responseCode(t, body) != tc.code {
			t.Fatalf("%s %d %+v", tc.action, status, body)
		}
	}
}

func TestDramaHandler_JSONCreateAndExpiredDraft(t *testing.T) {
	r, repo := settingsRouter()
	status, body := characterCall(t, r, "POST", "/dramas", `{"source_token":"expired","source_kind":"novel","ratio":"9:16","style_key":"real_city_film"}`)
	if status != 404 || responseCode(t, body) != 67014 {
		t.Fatalf("%d %+v", status, body)
	}
	repo.drafts["valid"] = model.DramaSourceDraft{ID: "valid", UserID: 1, Text: "第1章 开始\n正文\n第2章 结尾\n结局", ExpiresAt: time.Now().Add(time.Hour)}
	status, body = characterCall(t, r, "POST", "/dramas", `{"source_token":"valid","source_kind":"novel","ratio":"9:16","style_key":"real_city_film"}`)
	if status != 201 {
		t.Fatalf("%d %+v", status, body)
	}
	v := responseData(t, body)
	if v["phase"] != "running" || len(repo.units) != 2 || repo.jobs[0].Lane != "pipeline" || len(repo.drafts) != 0 {
		t.Fatalf("%+v units=%d jobs=%+v", v, len(repo.units), repo.jobs)
	}
}

func TestDramaHandler_PromptCRUD(t *testing.T) {
	r, _ := settingsRouter()
	for _, tc := range []struct {
		method, path, body string
		status, code       int
	}{
		{"POST", "/drama-prompts", `{"step":"adapt","name":"我的","bodies":{"plan":"保留对白"}}`, 201, 0},
		{"POST", "/drama-prompts", `{"step":"adapt","name":"空","bodies":{"plan":""}}`, 400, 67017},
		{"POST", "/drama-prompts", `{"step":"bad","name":"坏","bodies":{}}`, 400, 10001},
		{"POST", "/drama-prompts", `{"step":"adapt","name":"我的","bodies":{"plan":"重复"}}`, 400, 67017},
		{"GET", "/drama-prompts?step=adapt", "", 200, 0},
		{"PUT", "/drama-prompts/" + idcodec.Encode(1), `{"step":"adapt","revision":1,"name":"新","bodies":{"scripts":"台词"}}`, 200, 0},
		{"PUT", "/drama-prompts/" + idcodec.Encode(1), `{"step":"adapt","revision":1,"name":"新","bodies":{"scripts":"台词"}}`, 409, 30002},
		{"DELETE", "/drama-prompts/" + idcodec.Encode(1), "", 204, 0},
		{"DELETE", "/drama-prompts/" + idcodec.Encode(1), "", 404, 67016},
	} {
		status, body := characterCall(t, r, tc.method, tc.path, tc.body)
		if status != tc.status || status != 204 && responseCode(t, body) != tc.code {
			t.Fatalf("%s %s: %d %+v", tc.method, tc.path, status, body)
		}
	}
	status, body := characterCall(t, r, "GET", "/drama-prompt-templates", "")
	raw, _ := json.Marshal(body)
	if status != 200 || len(responseData(t, body)["items"].([]any)) != 4 {
		t.Fatalf("%s", raw)
	}
}

func (r *settingsHTTPRepo) CleanupDrafts(context.Context) error { return nil }
func (r *settingsHTTPRepo) RecentDramaDurations(context.Context, uint64, string) ([]int, error) {
	return nil, nil
}

func (r *httpDramaRepo) Segments(context.Context, uint64, uint64) ([]model.DramaSegment, error) {
	return nil, nil
}
