package handler_test

import (
	"context"
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

type storyboardHTTPRepo struct {
	*httpDramaRepo
	hasTakes bool
}

func (r *storyboardHTTPRepo) WithTx(_ context.Context, fn func(repository.DramaTx) error) error {
	return fn(r)
}

func (r *storyboardHTTPRepo) HasSegmentTakes(context.Context, uint64, uint64) (bool, error) {
	return r.hasTakes, nil
}

// TestDramaHandler_Storyboard 验证HTTP接受分镜步骤，区分请求参数错误、前置状态错误和已有take冲突。
func TestDramaHandler_Storyboard(t *testing.T) {
	for _, tc := range []struct {
		name          string
		body          string
		locked, takes bool
		status, code  int
	}{
		{"成功提交", `{"step":"storyboard","episodes":[1],"max_seconds":8}`, true, false, 202, 0},
		{"缺省上限", `{"step":"storyboard","episodes":[1]}`, true, false, 202, 0},
		{"上限过小", `{"step":"storyboard","episodes":[1],"max_seconds":3}`, true, false, 400, 10001},
		{"上限过大", `{"step":"storyboard","episodes":[1],"max_seconds":61}`, true, false, 400, 10001},
		{"未拆解", `{"step":"storyboard","episodes":[1]}`, false, false, 400, 67006},
		{"已有take", `{"step":"storyboard","episodes":[1]}`, true, true, 409, 67013},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			now := time.Now()
			ep := &model.DramaEpisode{BaseModel: model.BaseModel{ID: 10}, UserID: 1, DramaID: 7, No: 1, ScriptMD: "分镜正文", ScriptStatus: "ready", Revision: 1}
			if tc.locked {
				ep.LockedAt = &now
			}
			repo := &storyboardHTTPRepo{httpDramaRepo: &httpDramaRepo{project: &model.DramaProject{BaseModel: model.BaseModel{ID: 7}, UserID: 1, Revision: 1, Ratio: "9:16"}, episode: ep, credit: model.UserCredit{UserID: 1, Balance: 100}}, hasTakes: tc.takes}
			h := handler.NewDramaHandler(service.NewDramaService(service.DramaDeps{Repo: repo, Registry: httpDramaRegistry{}}))
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
			r.POST("/dramas/:id/jobs", h.CreateJob)
			status, body := characterCall(t, r, "POST", "/dramas/"+idcodec.Encode(7)+"/jobs", tc.body)
			if status != tc.status || responseCode(t, body) != tc.code {
				t.Fatalf("status=%d body=%+v", status, body)
			}
			if tc.code == 0 {
				if len(repo.jobs) != 1 || repo.jobs[0].Step != "storyboard" || repo.episode.StoryboardStatus != "generating" || repo.credit.Frozen <= 0 {
					t.Fatalf("分镜提交未持久化: jobs=%+v ep=%+v credit=%+v", repo.jobs, repo.episode, repo.credit)
				}
			} else if len(repo.jobs) != 0 || repo.credit.Frozen != 0 {
				t.Fatalf("拒绝请求不应建任务或冻结积分: %+v %+v", repo.jobs, repo.credit)
			}
		})
	}
}
