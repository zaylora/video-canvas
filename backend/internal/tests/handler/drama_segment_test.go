package handler_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/service"
	"video-canvas/internal/service/dramasegmentfake"
)

func segmentHTTP() (*gin.Engine, *dramasegmentfake.Repo) {
	gin.SetMode(gin.TestMode)
	repo := dramasegmentfake.New()
	h := handler.NewDramaSegmentHandler(service.NewDramaSegmentService(service.DramaSegmentDeps{Repo: repo, Tasks: repo, Assets: repo, Broadcaster: repo}))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	r.GET("/dramas/:id/episodes/:no/segments", h.List)
	r.POST("/dramas/:id/episodes/:no/segments", h.Create)
	r.PUT("/dramas/:id/episodes/:no/segments/order", h.Order)
	r.PATCH("/drama-segments/:sid", h.Update)
	r.DELETE("/drama-segments/:sid", h.Delete)
	r.POST("/drama-segments/:sid/generate", h.Generate)
	r.POST("/drama-segments/:sid/keyframe", h.Keyframe)
	return r, repo
}

func TestDramaSegmentHandler_Contracts(t *testing.T) {
	r, _ := segmentHTTP()
	projectSegments := "/dramas/" + idcodec.Encode(7) + "/episodes/1/segments"
	status, body := characterCall(t, r, "POST", projectSegments, `{}`)
	if status != 201 || responseCode(t, body) != 0 || responseData(t, body)["status"] != "idle" {
		t.Fatalf("创建：status=%d body=%+v", status, body)
	}
	id := responseData(t, body)["id"].(string)
	base := "/drama-segments/" + id
	for _, tc := range []struct {
		method, path, body string
		status, code       int
	}{
		{"GET", projectSegments, "", 200, 0},
		{"PATCH", base, `{"revision":1,"prompt":"新提示词"}`, 200, 0},
		{"PATCH", base, `{"revision":1,"prompt":"旧版本"}`, 409, errcode.ErrCanvasConflict.Code},
		{"POST", base + "/generate", `{"model_id":"video","input":{"prompt":"生成"}}`, 202, 0},
		{"POST", base + "/keyframe", `{"model_id":"image","input":{"prompt":"首帧"}}`, 202, 0},
		{"PUT", projectSegments + "/order", `{"ids":[]}`, 400, 67012},
		{"PATCH", base, `{"revision":2,"chosen_task_id":"` + idcodec.Encode(255) + `"}`, 400, 67011},
		{"PATCH", base, `{"prompt":"缺版本"}`, 400, 10001},
		{"POST", base + "/generate", `{"input":{}}`, 400, 10001},
		{"POST", base + "/keyframe", `{"model_id":"image"}`, 400, 10001},
		{"POST", projectSegments, `{"seconds":0}`, 400, 10001},
		{"POST", "/dramas/" + idcodec.Encode(7) + "/episodes/0/segments", `{}`, 400, 10001},
		{"PATCH", "/drama-segments/invalid", `{"revision":1}`, 400, 10001},
		{"DELETE", "/drama-segments/" + idcodec.Encode(255), "", 404, 67009},
		{"DELETE", base, "", 204, 0},
	} {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			gotStatus, gotBody := characterCall(t, r, tc.method, tc.path, tc.body)
			if gotStatus != tc.status || (gotStatus != 204 && responseCode(t, gotBody) != tc.code) {
				t.Fatalf("status=%d body=%+v", gotStatus, gotBody)
			}
		})
	}
}

func TestDramaSegmentHandler_IdempotencyAndEmptyView(t *testing.T) {
	r, repo := segmentHTTP()
	_, body := characterCall(t, r, "POST", "/dramas/"+idcodec.Encode(7)+"/episodes/1/segments", `{}`)
	v := responseData(t, body)
	if v["chosen_task_id"] != "" || v["keyframe"] != nil || len(v["takes"].([]any)) != 0 || len(v["shots"].([]any)) != 0 {
		t.Fatalf("空片段视图契约：%+v", v)
	}
	req := httptest.NewRequest("POST", "/drama-segments/"+v["id"].(string)+"/generate", strings.NewReader(`{"model_id":"video","input":{"prompt":"镜头"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "segment-request")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 202 || repo.IdempotencyKey != "segment-request" || repo.Created.Kind != "video" || repo.Created.CanvasID != 9 {
		t.Fatalf("幂等头未正确传递：status=%d key=%s req=%+v", w.Code, repo.IdempotencyKey, repo.Created)
	}
}
