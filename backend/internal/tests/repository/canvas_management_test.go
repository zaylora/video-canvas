package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
)

func TestCanvasCollaborationManagement(t *testing.T) {
	db, repo, owner, editor, viewer, project := newCollaborationFixture(t)
	ctx := t.Context()
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	rename := model.CanvasManagementReq{Revision: project.Revision, Title: "正式分镜"}
	for _, req := range []model.CanvasManagementReq{
		{Revision: project.Revision, Title: " \t"},
		{Revision: project.Revision, Title: rename.Title, Archive: true},
		{Revision: project.Revision, Title: "分镜\x00"},
	} {
		if _, err := repo.Manage(ctx, owner, project.ID, req); !errors.Is(err, repository.ErrCanvasCollaborationInvalidBatch) {
			t.Fatalf("无效管理请求应被拒绝: %v", err)
		}
	}
	if _, err := repo.Manage(ctx, editor, project.ID, rename); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("仅所有者可以重命名: %v", err)
	}
	current, err := repo.Manage(ctx, owner, project.ID, rename)
	if err != nil || current.Revision != project.Revision+1 || current.Title != rename.Title {
		t.Fatalf("重命名应确认新版本: %+v %v", current, err)
	}
	page, err := repo.Replay(ctx, editor, project.ID, project.CollaborationEpoch, project.Revision, 10)
	if err != nil || page.Title != rename.Title || page.Cursor != current.Revision || len(page.Events) != 1 {
		t.Fatalf("成员应收到标题和连续版本: %+v %v", page, err)
	}
	preview, err := repo.ArchivePreview(ctx, owner, project.ID)
	if err != nil || preview.Editors != 1 || preview.Viewers != 0 || preview.Title != rename.Title {
		t.Fatalf("预览应包含标题和成员范围: %+v %v", preview, err)
	}
	archive := model.CanvasManagementReq{Revision: project.Revision, Archive: true, Preview: preview.Preview}
	if _, err := repo.Manage(ctx, owner, project.ID, archive); !errors.Is(err, repository.ErrRevisionConflict) {
		t.Fatalf("过期预览不能归档画布: %v", err)
	}
	archive.Revision = current.Revision
	if err := shares.GrantRole(ctx, owner, project.ID, viewer, model.CanvasRoleViewer); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Manage(ctx, owner, project.ID, archive); !errors.Is(err, repository.ErrRevisionConflict) {
		t.Fatalf("版本相同但成员范围变化时应重新确认: %v", err)
	}
	preview, err = repo.ArchivePreview(ctx, owner, project.ID)
	if err != nil || preview.Viewers != 1 || preview.Preview == archive.Preview {
		t.Fatalf("预览应反映新成员: %+v %v", preview, err)
	}
	archive.Preview = preview.Preview
	verifyCanvasArchiveBusy(t, db, repo, project, owner, editor, archive)
	retired, err := repo.Manage(ctx, owner, project.ID, archive)
	if err != nil || retired.Title != rename.Title || retired.Revision != current.Revision {
		t.Fatalf("归档收据应保留所确认的标题和版本: %+v %v", retired, err)
	}
	for _, actor := range []uint64{owner, editor, viewer} {
		if _, _, err := repo.Snapshot(ctx, actor, project.ID); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("归档后应撤销访问: %v", err)
		}
	}
}

func verifyCanvasArchiveBusy(t *testing.T, db *gorm.DB, repo *repository.CanvasCollaborationRepository, project *model.CanvasProject, owner, editor uint64, archive model.CanvasManagementReq) {
	t.Helper()
	ctx := t.Context()
	job := &model.CanvasComposeJob{CanvasID: project.ID, UserID: editor, Epoch: project.CollaborationEpoch,
		Status: model.TaskRunning, Inputs: []byte(`[]`)}
	if err := db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Manage(ctx, owner, project.ID, archive); !errors.Is(err, repository.ErrInUse) {
		t.Fatalf("在途成片应阻止归档: %v", err)
	}
	if _, err := repo.ArchivePreview(ctx, owner, project.ID); !errors.Is(err, repository.ErrInUse) {
		t.Fatalf("在途成片应阻止打开归档确认: %v", err)
	}
	if err := db.Model(job).Update("status", model.TaskCanceled).Error; err != nil {
		t.Fatal(err)
	}
}

func TestCanvasCollaborationManagementHTTP(t *testing.T) {
	db, repo, owner, editor, _, project := newCollaborationFixture(t)
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(t.Context(), owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	const secret = "canvas-management-test-secret"
	engine := router.New("test", secret, router.Handlers{
		Health:              &handler.HealthHandler{},
		CanvasCollaboration: handler.NewCanvasCollaborationHandler(service.NewCanvasCollaborationService(repo, shares, nil, true)),
		UserState: func(context.Context, uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Status: model.UserStatusActive, Role: "user", TokenVersion: 1}, nil
		},
	})
	base := "/api/v1/canvas/" + idcodec.Encode(project.ID)
	send := func(method, path, payload string, actor uint64, status int, contains string) []byte {
		t.Helper()
		request := httptest.NewRequest(method, base+path, strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		token, _, err := utils.GenerateToken(uint(actor), "", 1, secret, "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		reply := httptest.NewRecorder()
		engine.ServeHTTP(reply, request)
		if reply.Code != status || !strings.Contains(reply.Body.String(), contains) {
			t.Fatalf("%s %s: 应为 %d 且包含 %q，得到 %d %s", method, path, status, contains, reply.Code, reply.Body.String())
		}
		return reply.Body.Bytes()
	}
	rename := fmt.Sprintf(`{"revision":%d,"title":"正式分镜"}`, project.Revision)
	send(http.MethodPost, "/management", `{"title":"漏传版本"}`, owner, http.StatusBadRequest, `"code":10001`)
	send(http.MethodPost, "/management", rename, editor, http.StatusNotFound, `"code":30001`)
	send(http.MethodPost, "/management", rename, owner, http.StatusOK, `"title":"正式分镜"`)
	send(http.MethodPost, "/management", rename, owner, http.StatusConflict, `"code":30002`)
	query := fmt.Sprintf("/collaboration/events?epoch=%d&after=%d", project.CollaborationEpoch, project.Revision)
	send(http.MethodGet, query, "", editor, http.StatusOK, `"title":"正式分镜"`)
	send(http.MethodGet, "/management", "", editor, http.StatusNotFound, `"code":30001`)
	previewReply := send(http.MethodGet, "/management", "", owner, http.StatusOK, `"editors":1`)
	var envelope struct {
		Data model.CanvasArchivePreview `json:"data"`
	}
	if err := json.Unmarshal(previewReply, &envelope); err != nil {
		t.Fatal(err)
	}
	archive := fmt.Sprintf(`{"revision":%d,"archive":true,"preview":%q}`, envelope.Data.Revision, envelope.Data.Preview)
	send(http.MethodPost, "/management", archive, owner, http.StatusOK, `"archived":true`)
	send(http.MethodGet, query, "", editor, http.StatusNotFound, `"code":30001`)
}
