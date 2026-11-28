package repository_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"video-canvas/internal/config"
	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

func TestCanvasMediaDraftHTTP(t *testing.T) {
	db, repo, owner, editor, viewer, project := newCollaborationFixtureWithPayload(t, imageCollaborationFixture)
	if err := db.AutoMigrate(&model.Asset{}, &model.CanvasMediaDraft{}); err != nil {
		t.Fatal(err)
	}
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(t.Context(), owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	assetID := seedMediaDraftSource(t, db, repo, project, owner, "source.png")
	assets := service.NewAssetService(repository.NewAssetRepository(db), storage.NewRegistry(canvasImportStorage{dir: t.TempDir()}, 0), config.Storage{})
	svc := service.NewCanvasCollaborationService(repo, shares, nil, true).WithMediaDrafts(assets)
	base := "/api/v1/canvas/" + idcodec.Encode(project.ID) + "/collaboration/media/drafts/processed"
	send := mediaDraftHTTP(t, svc, base)
	send(editor, "", "application/json", strings.NewReader(`{}`), http.StatusBadRequest, `"code":10001`)
	query := fmt.Sprintf("?epoch=%d&revision=%d&sourceNode=node&sourceAsset=%d", project.CollaborationEpoch, project.Revision, assetID)
	for _, req := range []struct {
		actor  uint64
		status int
		check  string
	}{{viewer, http.StatusNotFound, `"code":30001`}, {editor, http.StatusOK, `"draftId":"processed"`}} {
		body, mime := mediaDraftMultipart(t)
		send(req.actor, query, mime, body, req.status, req.check)
	}
	attach := fmt.Sprintf(`{"epoch":%d}`, project.CollaborationEpoch)
	send(owner, "/attach", "application/json", strings.NewReader(attach), http.StatusNotFound, `"code":30001`)
	first := send(editor, "/attach", "application/json", strings.NewReader(attach), http.StatusOK, `"opId":"processed"`)
	retry := send(editor, "/attach", "application/json", strings.NewReader(attach), http.StatusOK, `"opId":"processed"`)
	if draftResponseData(t, first) != draftResponseData(t, retry) || !strings.Contains(first, "/api/v1/canvas-media/") {
		t.Fatalf("确认重试应返回一致且含授权媒体地址的收据: %s %s", first, retry)
	}
}

func draftResponseData(t *testing.T, body string) string {
	t.Helper()
	var envelope struct{ Data json.RawMessage }
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatal(err)
	}
	return string(envelope.Data)
}

func mediaDraftHTTP(t *testing.T, svc *service.CanvasCollaborationService, base string) func(uint64, string, string, io.Reader, int, string) string {
	t.Helper()
	const secret = "test-media-draft-auth"
	engine := router.New("test", secret, router.Handlers{
		Health: &handler.HealthHandler{}, CanvasShare: &handler.CanvasShareHandler{},
		CanvasCollaboration: handler.NewCanvasCollaborationHandler(svc),
		UserState: func(context.Context, uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Status: model.UserStatusActive, Role: "user", TokenVersion: 1}, nil
		},
	})
	return func(actor uint64, route, mime string, body io.Reader, status int, check string) string {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, base+route, body)
		request.Header.Set("Content-Type", mime)
		token, _, err := utils.GenerateToken(uint(actor), "", 1, secret, "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		text := response.Body.String()
		if response.Code != status || !strings.Contains(text, check) {
			t.Fatalf("预期状态 %d 和 %s，收到 %d %s", status, check, response.Code, text)
		}
		return text
	}
}

func mediaDraftMultipart(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	file, err := writer.CreateFormFile("file", "mark.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(draftPNG(t)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body, writer.FormDataContentType()
}
