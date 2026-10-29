package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/drama"
	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/service"
)

// deleteEpisodeCanvas 是删集接口测试用的画布写入者：第 3 集模拟「有任务在跑」，其余成功并记下集号。
type deleteEpisodeCanvas struct{ deleted []int }

func (c *deleteEpisodeCanvas) ApplyBreakdown(context.Context, uint64, uint64, uint64, uint64, []int, []drama.Asset) ([]service.DramaCanvasAsset, error) {
	return nil, nil
}

func (c *deleteEpisodeCanvas) ApplyPrompts(context.Context, uint64, uint64, uint64, []drama.AssetPrompt) error {
	return nil
}

func (c *deleteEpisodeCanvas) DeleteEpisode(_ context.Context, _, _, _ uint64, no int) error {
	if no == 3 {
		return errcode.ErrDramaJobBusy
	}
	c.deleted = append(c.deleted, no)
	return nil
}

func TestDramaHandler_DeleteEpisode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &httpDramaRepo{project: &model.DramaProject{BaseModel: model.BaseModel{ID: 7}, UserID: 1, CanvasID: 9, Revision: 1}}
	canvas := &deleteEpisodeCanvas{}
	h := handler.NewDramaHandler(service.NewDramaService(service.DramaDeps{Repo: repo, Registry: httpDramaRegistry{}, Canvas: canvas}))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	r.DELETE("/dramas/:id/episodes/:no", h.DeleteEpisode)

	base := "/dramas/" + idcodec.Encode(7) + "/episodes/"
	for _, tc := range []struct {
		path         string
		status, code int
	}{
		{base + "2", 204, 0},
		{base + "3", 409, errcode.ErrDramaJobBusy.Code},
		{base + "0", 400, 10001},
		{"/dramas/" + idcodec.Encode(99) + "/episodes/2", 404, 67001},
	} {
		t.Run(tc.path, func(t *testing.T) {
			status, body := characterCall(t, r, http.MethodDelete, tc.path, "")
			if status != tc.status || (status != 204 && responseCode(t, body) != tc.code) {
				t.Fatalf("status=%d body=%+v", status, body)
			}
		})
	}
	if len(canvas.deleted) != 1 || canvas.deleted[0] != 2 {
		t.Fatalf("只应删掉第 2 集：%+v", canvas.deleted)
	}
}
