package repository_test

import (
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
)

type collaborationHTTPSender func(method, route, body string, actor uint64, status int) *httptest.ResponseRecorder

func TestCanvasCollaborationHTTP(t *testing.T) {
	db, shared, _ := canvasSeed(t)
	if err := db.Create(&model.User{BaseModel: model.BaseModel{ID: 42}, Username: "owner", Email: "owner@test.example"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.CanvasCollaborationRecord{}, &model.CanvasCollaborationReceipt{}, &model.CanvasCollaborationEvent{}, &model.CanvasCollaborationComment{}, &model.CanvasCollaborationCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	shareRepo := repository.NewCanvasShareRepository(db)
	repo := repository.NewCanvasCollaborationRepository(db.Session(&gorm.Session{
		NowFunc: func() time.Time { return time.Date(2026, time.November, 28, 8, 0, 0, 123456789, time.UTC) },
	}))
	const secret = "collaboration-http-test-secret"
	engine := router.New("test", secret, router.Handlers{
		Health: &handler.HealthHandler{}, CanvasShare: handler.NewCanvasShareHandler(shared),
		CanvasCollaboration: handler.NewCanvasCollaborationHandler(service.NewCanvasCollaborationService(repo, shareRepo, nil, true).WithUsernameDirectory(repository.NewUserRepository(db))),
		UserState: func(context.Context, uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Status: model.UserStatusActive, Role: "user", TokenVersion: 1}, nil
		},
	})
	base := "/api/v1/canvas/" + idcodec.Encode(8) + "/collaboration"
	send := func(method, route, body string, actor uint64, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, base+route, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		token, _, err := utils.GenerateToken(uint(actor), "", 1, secret, "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res := httptest.NewRecorder()
		engine.ServeHTTP(res, req)
		if res.Code != status {
			t.Fatalf("%s %s 用户 %d: 应为 %d，得到 %d %s", method, route, actor, status, res.Code, res.Body.String())
		}
		return res
	}
	send(http.MethodPut, "/mode", `{"active":true,"revision":1}`, 1, http.StatusNotFound)
	confirmCollaborationHTTPMode(t, send)
	send(http.MethodGet, "", "", 1, http.StatusNotFound)
	send(http.MethodPut, "/member", `{"email":"recv@test.example","role":"editor"}`, 42, http.StatusNoContent)
	joined := send(http.MethodGet, "", "", 1, http.StatusOK)
	if body := joined.Body.String(); !strings.Contains(body, `"role":"editor"`) || strings.Contains(body, "/files/source") || !strings.Contains(body, "/api/v1/canvas-media/") || len(joined.Result().Cookies()) != 1 {
		t.Fatalf("编辑成员快照应含当前角色、受保护的媒体地址和访问票据: %s", body)
	}
	op := `{"opId":"change","epoch":1,"baseRevision":2,"changes":[{"kind":"node","id":"n","fields":{"data.label":"协作成员"}}]}`
	send(http.MethodPost, "/ops", op, 1, http.StatusOK)
	assertCanvasCollaborationReceipt(t, send)
	send(http.MethodGet, "/events?epoch=1&after=2", "", 42, http.StatusOK)
	assertCanvasCollaborationHistory(t, send)
	assertCanvasCollaborationComments(t, send)
	assertCanvasCollaborationCommentReplay(t, send)
	memberGeneration := "/member/" + idcodec.Encode(1) + "/generation"
	send(http.MethodPut, memberGeneration, `{"enabled":true}`, 1, http.StatusNotFound)
	send(http.MethodPut, memberGeneration, `{"enabled":true}`, 42, http.StatusNoContent)
	if body := send(http.MethodGet, "/events?epoch=1&after=4", "", 1, http.StatusOK).Body.String(); !strings.Contains(body, `"canGenerate":true`) {
		t.Fatalf("编辑成员应通过同一增量视图看到独立生成授权: %s", body)
	}
	send(http.MethodPut, "/member", `{"email":"recv@test.example","role":"viewer"}`, 42, http.StatusNoContent)
	send(http.MethodPut, memberGeneration, `{"enabled":true}`, 42, http.StatusNotFound)
	send(http.MethodPost, "/ops", op, 1, http.StatusNotFound)
	send(http.MethodGet, "/ops/change?epoch=1", "", 1, http.StatusNotFound)
	send(http.MethodPost, "/comments", `{"epoch":1,"nodeId":"n","opId":"viewer","body":"no"}`, 1, http.StatusNotFound)
	send(http.MethodGet, "/comments?epoch=1&nodeId=n", "", 1, http.StatusNotFound)
	send(http.MethodGet, "/comments/events?epoch=1&after=0", "", 1, http.StatusNotFound)
	send(http.MethodGet, "/history?epoch=1", "", 1, http.StatusNotFound)
}

func confirmCollaborationHTTPMode(t *testing.T, send collaborationHTTPSender) {
	t.Helper()
	send(http.MethodGet, "/mode", "", 42, http.StatusBadRequest)
	send(http.MethodGet, "/mode?active=true", "", 1, http.StatusNotFound)
	send(http.MethodGet, "/mode?active=false", "", 42, http.StatusOK)
	res := send(http.MethodGet, "/mode?active=true", "", 42, http.StatusOK)
	var envelope struct {
		Data repository.CanvasModePreview `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil || !envelope.Data.Active || envelope.Data.Revision != 1 || len(envelope.Data.Preview) != 64 {
		t.Fatalf("应返回开启预览: %s, %v", res.Body.String(), err)
	}
	if res.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("模式预览不得被缓存")
	}
	send(http.MethodPut, "/mode", fmt.Sprintf(`{"active":false,"revision":1,"preview":%q}`, envelope.Data.Preview), 42, http.StatusConflict)
	send(http.MethodPut, "/mode", fmt.Sprintf(`{"active":true,"revision":1,"preview":%q}`, envelope.Data.Preview), 42, http.StatusOK)
}

func assertCanvasCollaborationCommentReplay(t *testing.T, send collaborationHTTPSender) {
	t.Helper()
	const path = "/comments/events?epoch=1"
	send(http.MethodGet, path+"&after=invalid!", "", 1, http.StatusBadRequest)
	send(http.MethodGet, "/comments/events?epoch=3&after=0", "", 1, http.StatusConflict)
	send(http.MethodGet, path+"&after=0", "", 2, http.StatusNotFound)
	read := func(route string) (string, []json.RawMessage) {
		t.Helper()
		var page struct {
			Cursor string            `json:"cursor"`
			Items  []json.RawMessage `json:"items"`
		}
		data := canvasHTTPData(t, send(http.MethodGet, route, "", 1, http.StatusOK))
		if err := json.Unmarshal(data, &page); err != nil || page.Cursor == "" || page.Items == nil {
			t.Fatalf("评论回放页无效: %s, %v", data, err)
		}
		return page.Cursor, page.Items
	}
	heads, items := read(path)
	if len(items) != 0 || heads == "0" {
		t.Fatalf("默认游标应位于评论末尾: %s, %v", heads, items)
	}
	for range 2 {
		send(http.MethodPost, "/comments", `{"epoch":1,"nodeId":"n","opId":"follow-up","body":"第二条评论"}`, 42, http.StatusOK)
	}
	cursor, items := read(path + "&after=" + heads)
	if cursor == heads || len(items) != 1 || !strings.Contains(string(items[0]), `"nodeId":"n"`) || !strings.Contains(string(items[0]), `"authorUsername":"owner"`) || !strings.Contains(string(items[0]), "第二条评论") {
		t.Fatalf("应按游标返回一条带节点和用户名的评论: %s, %s", cursor, items)
	}
	if next, items := read(path + "&after=" + cursor); next != cursor || len(items) != 0 {
		t.Fatalf("重复回放应无新增内容: %s, %s", next, items)
	}
}

func assertCanvasCollaborationReceipt(t *testing.T, send collaborationHTTPSender) {
	t.Helper()
	result := send(http.MethodGet, "/ops/change?epoch=1", "", 1, http.StatusOK)
	if body := result.Body.String(); !strings.Contains(body, `"result":"applied"`) || !strings.Contains(body, `"revision":3`) || strings.Contains(body, "协作成员") {
		t.Fatalf("提交者只能读取收据摘要: %s", body)
	}
	result = send(http.MethodGet, "/ops/change?epoch=1", "", 42, http.StatusOK)
	if !strings.Contains(result.Body.String(), `"result":"missing"`) {
		t.Fatalf("其他编辑者不应取得提交人的收据: %s", result.Body.String())
	}
	send(http.MethodGet, "/ops/change", "", 1, http.StatusBadRequest)
	send(http.MethodGet, "/ops/change?epoch=2", "", 1, http.StatusConflict)
	send(http.MethodGet, "/ops/change?epoch=1", "", 2, http.StatusNotFound)
}

func assertCanvasCollaborationHistory(t *testing.T, send collaborationHTTPSender) {
	t.Helper()
	history := send(http.MethodGet, "/history?epoch=1", "", 1, http.StatusOK)
	if body := history.Body.String(); !strings.Contains(body, `"revision":3`) || !strings.Contains(body, `"node"`) || !strings.Contains(body, `"authorUsername":"recipient"`) || strings.Contains(body, "协作成员") || strings.Contains(body, "@test.example") {
		t.Fatalf("版本活动只暴露记录标识和变化类型: %s", body)
	}
	send(http.MethodGet, "/history?epoch=1", "", 2, http.StatusNotFound)
	send(http.MethodPost, "/ops", `{"opId":"later","epoch":1,"baseRevision":3,"changes":[{"kind":"node","id":"n","fields":{"data.label":"后来修改"}}]}`, 42, http.StatusOK)
	send(http.MethodGet, "/history/2?epoch=1", "", 1, http.StatusNotFound)
	baseline := send(http.MethodGet, "/history/2?epoch=1", "", 42, http.StatusOK)
	if body := baseline.Body.String(); !strings.Contains(body, `"revision":2`) || strings.Contains(body, "协作成员") {
		t.Fatalf("起始历史版本不得掺入后来编辑: %s", body)
	}
	edit := send(http.MethodGet, "/history/3?epoch=1", "", 42, http.StatusOK)
	if body := edit.Body.String(); !strings.Contains(body, `"revision":3`) || !strings.Contains(body, "协作成员") || strings.Contains(body, "后来修改") {
		t.Fatalf("历史预览应重放已确认编辑: %s", body)
	}
	const copyRequest = `{"epoch":1,"opId":"history-copy"}`
	send(http.MethodPost, "/history/3/copy", copyRequest, 1, http.StatusNotFound)
	first := send(http.MethodPost, "/history/3/copy", copyRequest, 42, http.StatusOK)
	repeat := send(http.MethodPost, "/history/3/copy", copyRequest, 42, http.StatusOK)
	if first.Body.String() == "" || !strings.Contains(first.Body.String(), "协作成员") || strings.Contains(first.Body.String(), "后来修改") ||
		!jsonEqual(canvasHTTPData(t, first), string(canvasHTTPData(t, repeat))) {
		t.Fatalf("从历史另存必须使用确认版本并支持幂等重试: %s %s", first.Body, repeat.Body)
	}
	send(http.MethodPost, "/history/2/copy", copyRequest, 42, http.StatusConflict)
}

func canvasHTTPData(t *testing.T, reply *httptest.ResponseRecorder) []byte {
	t.Helper()
	var response struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

func assertCanvasCollaborationComments(t *testing.T, send collaborationHTTPSender) {
	t.Helper()
	const route = "/comments?epoch=1&nodeId=n"
	send(http.MethodGet, route, "", 2, http.StatusNotFound)
	send(http.MethodGet, "/comments?epoch=3&nodeId=n", "", 1, http.StatusConflict)
	send(http.MethodPost, "/comments", `{"epoch":1,"nodeId":"n","opId":"empty","body":"  "}`, 1, http.StatusBadRequest)
	comment := fmt.Sprintf(`{"epoch":1,"nodeId":"n","opId":"note","body":"镜头交接","mentions":[%q]}`, idcodec.Encode(1))
	for range 2 {
		if body := send(http.MethodPost, "/comments", comment, 42, http.StatusOK).Body.String(); !strings.Contains(body, `"authorUsername":"owner"`) {
			t.Fatalf("发送成功应返回作者用户名: %s", body)
		}
	}
	send(http.MethodPost, "/comments", strings.Replace(comment, "镜头交接", "更换正文", 1), 42, http.StatusConflict)
	private := fmt.Sprintf(`{"epoch":1,"nodeId":"n","opId":"outsider","body":"no","mentions":[%q]}`, idcodec.Encode(2))
	send(http.MethodPost, "/comments", private, 1, http.StatusBadRequest)
	result := send(http.MethodGet, route, "", 1, http.StatusOK)
	if body := result.Body.String(); strings.Count(body, "镜头交接") != 1 || !strings.Contains(body, fmt.Sprintf(`%q:"recipient"`, idcodec.Encode(1))) || !strings.Contains(body, fmt.Sprintf(`%q:"owner"`, idcodec.Encode(42))) || strings.Contains(body, "@test.example") {
		t.Fatalf("应返回一条完整评论和相关账号的用户名: %s", body)
	}
	assertCanvasCollaborationMentions(t, send)
}

func assertCanvasCollaborationMentions(t *testing.T, send collaborationHTTPSender) {
	t.Helper()
	const path = "/mentions?epoch=1"
	res := send(http.MethodGet, path, "", 1, http.StatusOK)
	var page struct {
		Items []struct {
			ID     string `json:"id"`
			NodeID string `json:"nodeId"`
		} `json:"items"`
	}
	if err := json.Unmarshal(canvasHTTPData(t, res), &page); err != nil || len(page.Items) != 1 || page.Items[0].NodeID != "n" || page.Items[0].ID == "" {
		t.Fatalf("被提及的成员应收到一次节点提醒: %s, %v", res.Body.String(), err)
	}
	if strings.Contains(res.Body.String(), "镜头交接") {
		t.Fatal("提醒列表无需返回评论正文")
	}
	for _, viewer := range []struct {
		path string
		user uint64
	}{{path, 42}, {path + "&after=" + page.Items[0].ID, 1}} {
		if data := canvasHTTPData(t, send(http.MethodGet, viewer.path, "", viewer.user, http.StatusOK)); string(data) != `{"items":[]}` {
			t.Fatalf("无需显示未提及或已读的提醒: %s", data)
		}
	}
	send(http.MethodGet, path, "", 2, http.StatusNotFound)
}
