package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
)

func verifyPaidAutomationHTTP(t *testing.T, env *gtiEnv, access *service.CanvasCollaborationService, issued *service.CanvasAutomationKeyView, key model.CanvasAutomationKey, task uint64, overBudget *model.CreateGenerationTaskReq) {
	t.Helper()
	engine := router.New("test", "test-automation-generation", router.Handlers{
		Health: &handler.HealthHandler{}, CanvasShare: &handler.CanvasShareHandler{},
		CanvasCollaboration: handler.NewCanvasCollaborationHandler(access),
		GenerationTask:      handler.NewGenerationTaskHandler(env.svc),
		UserState: func(context.Context, uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Status: model.UserStatusActive, Role: "user"}, nil
		},
	})
	base := "/api/v1/canvas-automation/" + issued.CanvasID + "/collaboration/generation-tasks"
	readOptions := model.CanvasAutomationKey{CanvasID: key.CanvasID, UserID: key.UserID, Epoch: key.Epoch, Scope: model.CanvasAutomationRead}
	readOnly, err := access.IssueAutomationKey(t.Context(), readOptions, 1)
	if err != nil {
		t.Fatal(err)
	}
	limit := 10
	readOptions.Scope, readOptions.BudgetCredits = model.CanvasAutomationGenerate, &limit
	other, err := access.IssueAutomationKey(t.Context(), readOptions, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		method, path, token string
		status              int
	}{
		{http.MethodPost, base, readOnly.Token, http.StatusForbidden},
		{http.MethodGet, base + "/" + strconv.FormatUint(task, 10), issued.Token, http.StatusOK},
		{http.MethodGet, base + "/" + strconv.FormatUint(task, 10), other.Token, http.StatusNotFound},
	} {
		request := httptest.NewRequest(scenario.method, scenario.path, nil)
		request.Header.Set("Authorization", "Bearer "+scenario.token)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != scenario.status {
			t.Fatalf("生成路由 %s 应返回 %d，得到 %d: %s", scenario.method, scenario.status, response.Code, response.Body)
		}
	}
	assertPaidAutomationHTTPBudget(t, engine, base, issued.Token, overBudget)
}

func assertPaidAutomationHTTPBudget(t *testing.T, engine http.Handler, path, token string, input *model.CreateGenerationTaskReq) {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "http-budget-exhausted")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	var result struct {
		Data model.CreateGenerationTaskResp `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusAccepted || len(result.Data.Items) != 1 {
		t.Fatalf("付费接口应返回逐项结果: %d %s (%v)", response.Code, response.Body, err)
	}
	item := result.Data.Items[0]
	if item.Task != nil || item.Error == nil || item.Error.Code != errcode.ErrGenerationBudget.Code {
		t.Fatalf("凭据预算耗尽后不可创建任务: %s", response.Body)
	}
}
