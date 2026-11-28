package repository_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
	"video-canvas/migrations"
)

func TestCanvasAutomationScopeAndRevocation(t *testing.T) {
	db, repo, owner, editor, _, project := newCollaborationFixtureWithPayload(t, imageCollaborationFixture)
	ddl, err := migrations.FS.ReadFile("0074_canvas_automation_keys.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(strings.ReplaceAll(string(ddl), "ADD COLUMN source", "ADD COLUMN IF NOT EXISTS source")).Error; err != nil {
		t.Fatal(err)
	}
	budgetDDL, err := migrations.FS.ReadFile("0076_canvas_automation_budget.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(budgetDDL)).Error; err != nil {
		t.Fatal(err)
	}
	shares := repository.NewCanvasShareRepository(db)
	ctx := t.Context()
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	serviceAPI := service.NewCanvasCollaborationService(repo, shares, nil, true)
	api := newAutomationHTTP(t, serviceAPI, editor)
	base := "/api/v1/canvas/" + idcodec.Encode(project.ID)
	scoped := strings.Replace(base, "/canvas/", "/canvas-automation/", 1) + "/collaboration"
	issue := base + "/collaboration/automation-keys"
	readKey := api.issue(issue, project.CollaborationEpoch, "read")
	editKey := api.issue(issue, project.CollaborationEpoch, "edit")
	response := api.send(readKey.Token, http.MethodGet, scoped, nil, http.StatusOK)
	if response.Header().Get("Set-Cookie") != "" || bytes.Contains(response.Body.Bytes(), []byte(`"commit"`)) {
		t.Fatalf("只读凭据的能力超出范围: %s", response.Body.String())
	}
	api.send(editKey.Token, http.MethodGet, base+"/collaboration", nil, http.StatusUnauthorized)
	api.send(editKey.Token, http.MethodGet, "/api/v1/canvas-automation/"+idcodec.Encode(project.ID+1)+"/collaboration", nil, http.StatusUnauthorized)
	api.send(editKey.Token, http.MethodPost, scoped+"/compose", nil, http.StatusNotFound)
	batch := collaborationEdit(project, "scoped-edit", "data.label", `"并行制作"`)
	api.send(readKey.Token, http.MethodPost, scoped+"/ops", batch, http.StatusForbidden)
	api.send(editKey.Token, http.MethodPost, scoped+"/ops", batch, http.StatusOK)
	assertAutomationProtectedEdits(api, editKey.Token, scoped, project)
	var event model.CanvasCollaborationEvent
	if err := db.Where("canvas_id = ? AND op_id = ?", project.ID, batch.OpID).Take(&event).Error; err != nil || event.ActorID != editor || event.Source != "automation" {
		t.Fatalf("缺少自动化审计: %+v %v", event, err)
	}
	api.send(api.session, http.MethodDelete, issue+"/"+editKey.ID, nil, http.StatusNoContent)
	api.send(editKey.Token, http.MethodPost, scoped+"/ops", batch, http.StatusUnauthorized)
	if err := shares.Remove(ctx, owner, project.ID, editor); err != nil {
		t.Fatal(err)
	}
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	api.send(readKey.Token, http.MethodGet, scoped, nil, http.StatusUnauthorized)
	assertAutomationLifetime(api, repo, scoped, issue, owner, project)
}

func assertAutomationProtectedEdits(api automationHTTPFixture, token, route string, project *model.CanvasProject) {
	api.t.Helper()
	for index, change := range []repository.CanvasCollaborationChange{
		{Kind: "node", ID: "board", Action: "storyboard-shot", Value: json.RawMessage(`{"action":"rm","id":"shot"}`)},
		{Kind: "node", ID: "director", Action: "director-object", Value: json.RawMessage(`{"action":"rm","collection":"props","id":"wall"}`)},
		{Kind: "node", ID: "board", Fields: map[string]json.RawMessage{"data.storyboard": json.RawMessage(`{"shots":[]}`)}},
	} {
		batch := repository.CanvasCollaborationBatch{OpID: fmt.Sprintf("protected-%d", index), Epoch: project.CollaborationEpoch,
			BaseRevision: project.Revision, Changes: []repository.CanvasCollaborationChange{change}}
		api.send(token, http.MethodPost, route+"/ops", batch, http.StatusNotFound)
	}
}

func assertAutomationLifetime(api automationHTTPFixture, repo *repository.CanvasCollaborationRepository, scoped, issue string, owner uint64, project *model.CanvasProject) {
	api.t.Helper()
	ctx := api.t.Context()
	current := api.issue(issue, project.CollaborationEpoch, "read")
	api.send(current.Token, http.MethodGet, scoped, nil, http.StatusOK)
	view, _, err := repo.Snapshot(ctx, owner, project.ID)
	if err != nil {
		api.t.Fatal(err)
	}
	retired, err := repo.SetActive(ctx, owner, project.ID, view.Revision, false)
	if err != nil {
		api.t.Fatal(err)
	}
	reopened, err := repo.SetActive(ctx, owner, project.ID, retired.Revision, true)
	if err != nil {
		api.t.Fatal(err)
	}
	api.send(current.Token, http.MethodGet, scoped, nil, http.StatusNotFound)
	expiring := api.issue(issue, reopened.CollaborationEpoch, "read")
	api.send(expiring.Token, http.MethodGet, scoped, nil, http.StatusOK)
	id, ok := idcodec.Decode(expiring.ID)
	if !ok {
		api.t.Fatal("签发的凭据 ID 无效")
	}
	if err := repo.WithTx(ctx, func(tx *gorm.DB) error {
		return tx.Model(&model.CanvasAutomationKey{}).Where("id = ?", id).Update("expires_at", time.Now().Add(-time.Minute)).Error
	}); err != nil {
		api.t.Fatal(err)
	}
	api.send(expiring.Token, http.MethodGet, scoped, nil, http.StatusUnauthorized)
}

type automationHTTPFixture struct {
	t       *testing.T
	engine  http.Handler
	session string
}

func newAutomationHTTP(t *testing.T, serviceAPI *service.CanvasCollaborationService, editor uint64) automationHTTPFixture {
	t.Helper()
	const secret = "test-scope-auth"
	engine := router.New("test", secret, router.Handlers{Health: &handler.HealthHandler{}, CanvasShare: &handler.CanvasShareHandler{},
		CanvasCollaboration: handler.NewCanvasCollaborationHandler(serviceAPI),
		UserState: func(context.Context, uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Status: model.UserStatusActive, Role: "user"}, nil
		}})
	session, _, err := utils.GenerateToken(uint(editor), "", 0, secret, "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	return automationHTTPFixture{t: t, engine: engine, session: session}
}

func (api automationHTTPFixture) issue(route string, epoch uint64, scope string) service.CanvasAutomationKeyView {
	api.t.Helper()
	response := api.send(api.session, http.MethodPost, route, map[string]any{"epoch": epoch, "scope": scope, "hours": 1}, http.StatusOK)
	var envelope struct {
		Data service.CanvasAutomationKeyView
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || !strings.HasPrefix(envelope.Data.Token, "vca_") {
		api.t.Fatalf("签发结果无有效凭据: %v", err)
	}
	return envelope.Data
}

func (api automationHTTPFixture) send(token, method, route string, body any, status int) *httptest.ResponseRecorder {
	api.t.Helper()
	contents, err := json.Marshal(body)
	if err != nil {
		api.t.Fatal(err)
	}
	request := httptest.NewRequest(method, route, bytes.NewReader(contents))
	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.engine.ServeHTTP(response, request)
	if response.Code != status {
		var envelope struct{ Msg string }
		_ = json.Unmarshal(response.Body.Bytes(), &envelope)
		api.t.Fatalf("%s %s 得到 HTTP %d，应为 %d，原因 %s", method, route, response.Code, status, envelope.Msg)
	}
	return response
}
