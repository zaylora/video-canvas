package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

type httpDramaRegistry struct{}

func (httpDramaRegistry) ListModels(context.Context, string) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{Key: "http-text", Kind: "agent"}}, nil
}
func (httpDramaRegistry) Snapshot(context.Context, string) (*provider.Snapshot, error) {
	return &provider.Snapshot{ModelRevisionID: 1, Model: provider.ModelSnapshot{Key: "http-text", Kind: "agent", Pricing: modelcfg.Pricing{Billing: modelcfg.BillingPerCall, Unit: 2}}}, nil
}
func (r *httpDramaRepo) InsertProject(_ context.Context, p *model.DramaProject) error {
	p.ID = 7
	p.CanvasID = 9
	r.project = p
	return nil
}
func (r *httpDramaRepo) UpdateProject(_ context.Context, userID, id uint64, revision int64, fields map[string]any) error {
	if r.project == nil || r.project.UserID != userID || r.project.ID != id {
		return repository.ErrNotFound
	}
	if r.project.Revision != revision {
		return repository.ErrStateConflict
	}
	for k, v := range fields {
		switch k {
		case "title":
			r.project.Title = v.(string)
		case "outline_md":
			r.project.OutlineMD = v.(string)
		case "ratio":
			r.project.Ratio = v.(string)
		}
	}
	r.project.Revision++
	return nil
}
func (r *httpDramaRepo) DeleteProject(context.Context, uint64, uint64) error {
	r.project = nil
	r.episode = nil
	return nil
}
func (r *httpDramaRepo) SaveEpisode(_ context.Context, ep *model.DramaEpisode, revision int64) error {
	if ep.ID == 0 {
		ep.ID = 10
	} else {
		if r.episode.Revision != revision {
			return repository.ErrStateConflict
		}
		ep.Revision++
	}
	clone := *ep
	r.episode = &clone
	return nil
}
func (r *httpDramaRepo) EnsureCredit(context.Context, uint64, int) error { return nil }
func (r *httpDramaRepo) LockCredit(context.Context, uint64) (*model.UserCredit, error) {
	c := r.credit
	return &c, nil
}
func (r *httpDramaRepo) AddCredit(_ context.Context, _ uint64, balance, frozen int) error {
	r.credit.Balance += balance
	r.credit.Frozen += frozen
	return nil
}
func (r *httpDramaRepo) InsertLedger(context.Context, *model.CreditLedger) (bool, error) {
	return true, nil
}
func (r *httpDramaRepo) InsertJob(_ context.Context, j *model.DramaJob) error {
	j.ID = uint64(len(r.jobs) + 1)
	r.jobs = append(r.jobs, *j)
	return nil
}
func (r *httpDramaRepo) Job(_ context.Context, userID, dramaID, id uint64, _ bool) (*model.DramaJob, error) {
	for _, j := range r.jobs {
		if j.UserID == userID && j.DramaID == dramaID && j.ID == id {
			clone := j
			return &clone, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (r *httpDramaRepo) UpdateJob(_ context.Context, userID, id uint64, fields map[string]any) error {
	for i := range r.jobs {
		j := &r.jobs[i]
		if j.UserID != userID || j.ID != id {
			continue
		}
		for k, v := range fields {
			switch k {
			case "status":
				j.Status = v.(string)
			case "credits":
				j.Credits = v.(int)
			case "error_code":
				j.ErrorCode = v.(int)
			case "error_message":
				j.ErrorMessage = v.(string)
			case "finished_at":
				at := v.(time.Time)
				j.FinishedAt = &at
			}
		}
		return nil
	}
	return repository.ErrNotFound
}

func TestDramaHandler_EditJobsAndDelete(t *testing.T) {
	r := httpDramaRouter()
	base := "/dramas/" + idcodec.Encode(7)
	for _, tc := range []struct {
		method, path, body string
		status, code       int
	}{
		{"PATCH", base, `{"revision":99,"title":"冲突"}`, 409, 30002},
		{"PATCH", base, `{"revision":1,"title":"新剧名"}`, 200, 0},
		{"PATCH", base + "/episodes/1", `{"revision":1,"script_md":"新的完整剧本"}`, 200, 0},
		{"PATCH", base + "/episodes/1", `{"revision":1,"title":"过期版本"}`, 409, 30002},
		{"POST", base + "/estimate", `{"step":"breakdown","episodes":[1]}`, 200, 0},
		{"POST", base + "/jobs", `{"step":"breakdown","episodes":[1]}`, 202, 0},
		{"POST", base + "/jobs", `{"step":"breakdown","episodes":[1]}`, 409, 67004},
		{"POST", base + "/jobs/" + idcodec.Encode(1) + "/cancel", "", 200, 0},
		{"POST", base + "/episodes", `{"title":"第二集","synopsis":"续篇"}`, 201, 0},
		{"DELETE", base, "", 204, 0},
		{"GET", base, "", 404, 67001},
	} {
		status, body := characterCall(t, r, tc.method, tc.path, tc.body)
		if status != tc.status || (status != 204 && responseCode(t, body) != tc.code) {
			t.Fatalf("%s %s: status=%d body=%+v", tc.method, tc.path, status, body)
		}
	}
}

func TestDramaHandler_CreateMultipart(t *testing.T) {
	r := httpDramaRouter()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for key, value := range map[string]string{"text": "第1集\n母亲回家。", "title": "上传短剧", "style_key": "real_city_film"} {
		if err := form.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/dramas", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	view := responseData(t, got)
	if view["id"] != idcodec.Encode(7) || view["canvas_id"] != idcodec.Encode(9) || view["status"] != "analyzing" {
		t.Fatalf("创建契约：%+v", view)
	}
	jobs := view["active_jobs"].([]any)
	if len(jobs) != 1 || jobs[0].(map[string]any)["step"] != "analyze" {
		t.Fatalf("缺少自动研读：%+v", jobs)
	}
}
