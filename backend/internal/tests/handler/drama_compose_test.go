package handler_test

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type composeHTTPRepo struct {
	*httpDramaRepo
	empty bool
}

func (r *composeHTTPRepo) WithTx(_ context.Context, fn func(repository.DramaTx) error) error {
	return fn(r)
}
func (r *composeHTTPRepo) Segments(_ context.Context, user, episode uint64) ([]model.DramaSegment, error) {
	if r.empty {
		return []model.DramaSegment{}, nil
	}
	chosen := uint64(20)
	return []model.DramaSegment{{BaseModel: model.BaseModel{ID: 5}, UserID: user, EpisodeID: episode, ChosenTaskID: &chosen}}, nil
}
func (r *composeHTTPRepo) SegmentTasks(_ context.Context, user, canvas uint64, node string) ([]model.GenerationTask, error) {
	return []model.GenerationTask{{ID: 20, UserID: user, CanvasProjectID: &canvas, NodeID: node, Kind: model.KindVideo, Status: model.TaskSucceeded, OutputJSON: []byte(`[{"asset_id":30,"media_type":"video"}]`)}}, nil
}
func TestDramaHandler_Compose(t *testing.T) {
	for _, tc := range []struct {
		name, body   string
		empty, busy  bool
		status, code int
	}{
		{"接受合成", `{"step":"compose","episodes":[1]}`, false, false, 202, 0},
		{"无片段", `{"step":"compose","episodes":[1]}`, true, false, 400, 67010},
		{"已有任务", `{"step":"compose","episodes":[1]}`, false, true, 409, 67004},
		{"缺少集号", `{"step":"compose"}`, false, false, 400, 10001},
		{"不接受多集", `{"step":"compose","episodes":[1,2]}`, false, false, 400, 10001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &composeHTTPRepo{httpDramaRepo: &httpDramaRepo{project: &model.DramaProject{BaseModel: model.BaseModel{ID: 7}, UserID: 1, CanvasID: 9, Revision: 1}, episode: &model.DramaEpisode{BaseModel: model.BaseModel{ID: 10}, UserID: 1, DramaID: 7, No: 1, Revision: 1}}, empty: tc.empty}
			if tc.busy {
				repo.jobs = []model.DramaJob{{UserID: 1, DramaID: 7, Status: "running"}}
			}
			h := handler.NewDramaHandler(service.NewDramaService(service.DramaDeps{Repo: repo}))
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
			r.POST("/dramas/:id/jobs", h.CreateJob)
			status, body := characterCall(t, r, "POST", "/dramas/"+idcodec.Encode(7)+"/jobs", tc.body)
			if status != tc.status || responseCode(t, body) != tc.code {
				t.Fatalf("status=%d body=%+v", status, body)
			}
			if tc.code == 0 && responseData(t, body)["result"] != nil {
				t.Fatal("排队任务 result 必须为 null")
			}
		})
	}
}
