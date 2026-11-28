package repository_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

func TestCanvasCollaborationTaskSummaries(t *testing.T) {
	ctx := t.Context()
	db, repo, owner, editor, viewer, project := newCollaborationFixtureWithPayload(t, `{"nodes":[{"id":"board","type":"canvas","data":{"kind":"storyboard"}},{"id":"cast","type":"canvas","data":{"kind":"character"}}],"edges":[]}`)
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	jobs := []model.StoryboardSplitJob{
		{UserID: owner, CanvasID: project.ID, NodeID: "board", CollaborationEpoch: project.CollaborationEpoch, Status: model.SplitJobRunning, Script: "private script", Credits: 37},
		{UserID: owner, CanvasID: project.ID, NodeID: "board", CollaborationEpoch: project.CollaborationEpoch + 1, Status: model.SplitJobFailed, Error: "private error"},
		{UserID: owner, CanvasID: project.ID, NodeID: "deleted-node", CollaborationEpoch: project.CollaborationEpoch, Status: model.SplitJobSucceeded},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	tasks := []model.GenerationTask{
		{UserID: owner, CanvasProjectID: &project.ID, NodeID: "voice:cast", CollaborationEpoch: project.CollaborationEpoch, Kind: model.KindAudio, Status: model.TaskSucceeded, InputJSON: datatypes.JSON(`{"prompt":"private prompt"}`), ConfigSnapshot: datatypes.JSON(`{}`)},
		{UserID: owner, CanvasProjectID: &project.ID, NodeID: "cast", CollaborationEpoch: project.CollaborationEpoch, Kind: model.KindImage, Status: model.TaskSucceeded, InputJSON: datatypes.JSON(`{}`), ConfigSnapshot: datatypes.JSON(`{}`)},
	}
	if err := db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.NewCanvasCollaborationService(repo, shares, nil, true).WithUsernameDirectory(repository.NewUserRepository(db))
	items, err := svc.TaskSummaries(ctx, editor, project.ID, project.CollaborationEpoch, 30)
	if err != nil || len(items) != 2 {
		t.Fatalf("只应含本代有效节点上的两类专项任务: %+v %v", items, err)
	}
	raw, err := json.Marshal(items)
	if err != nil || strings.Contains(string(raw), "private") || strings.Contains(string(raw), "credits") {
		t.Fatalf("共享摘要泄露私有字段: %s %v", raw, err)
	}
	_, err = svc.TaskSummaries(ctx, viewer, project.ID, project.CollaborationEpoch, 30)
	assertDraftError(t, err, errcode.ErrCanvasNotFound.Code)
	if err := shares.Remove(ctx, owner, project.ID, editor); err != nil {
		t.Fatal(err)
	}
	_, err = svc.TaskSummaries(ctx, editor, project.ID, project.CollaborationEpoch, 30)
	assertDraftError(t, err, errcode.ErrCanvasNotFound.Code)
}
