package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	. "video-canvas/internal/service/agent"
)

type diagnosticHistory struct{ filter repository.AdminRequestFilter }

func (f *diagnosticHistory) RecentRequests(_ context.Context, filter repository.AdminRequestFilter) ([]repository.AdminRequestRecord, error) {
	f.filter = filter
	return []repository.AdminRequestRecord{{ID: 7, ModelKey: "sample", ChannelKey: "main", IsTest: true, Status: model.TaskFailed, ErrorCode: "401", ErrorMessage: "Unauthorized", CreatedAt: time.Now()}}, nil
}

type diagnosticTrials struct{ *phaseTwoFake }

func (f diagnosticTrials) GetTestTrace(context.Context, uint64, uint64) (*service.TestTraceView, error) {
	return &service.TestTraceView{Steps: []provider.TraceStep{{Name: "submit", Kind: "http", Request: &provider.TraceRequest{Method: "POST", URL: "https://example.invalid/run?api_key=DO-NOT-EXPOSE", Headers: map[string]string{"Authorization": "DO-NOT-EXPOSE"}}, Response: &provider.TraceResponse{Status: 401, Body: `{"api_key":"DO-NOT-EXPOSE","message":"Unauthorized"}`}}}}, nil
}
func TestAdminRecentErrors_FilterAndRedaction(t *testing.T) {
	f, _, rc := phaseTwoEnv()
	history := &diagnosticHistory{}
	svc := NewAdminAssistantService(AdminAssistantDeps{Models: f, Users: f, Channels: f, Audit: f, Trials: diagnosticTrials{f}, History: history})
	bridge := NewAgentBridge(BridgeDeps{Admin: svc})
	res, err := bridge.ExecutePlatformTool(context.Background(), rc, "diagnose", "admin_get_recent_errors", json.RawMessage(`{"channel_key":"main","limit":3}`))
	if err != nil || res.IsError {
		t.Fatalf("诊断查询失败：%+v %v", res, err)
	}
	if history.filter.UserID != rc.UserID || history.filter.ChannelKey != "main" || history.filter.Limit != 3 {
		t.Fatalf("过滤条件错误：%+v", history.filter)
	}
	if strings.Contains(res.Content, "DO-NOT-EXPOSE") || !strings.Contains(res.Content, "401") || !strings.Contains(res.Content, "/run") {
		t.Fatalf("诊断结果未正确脱敏：%s", res.Content)
	}
	res, err = bridge.ExecutePlatformTool(context.Background(), rc, "bad", "admin_get_recent_errors", json.RawMessage(`{"limit":1000}`))
	if err != nil || !res.IsError {
		t.Fatalf("未限制查询数量：%+v %v", res, err)
	}
	rc.Mode = "all"
	rc.CanvasID = 1
	res, err = bridge.ExecutePlatformTool(context.Background(), rc, "canvas", "admin_get_recent_errors", json.RawMessage(`{}`))
	if err != nil || !res.IsError {
		t.Fatalf("画布取得了后台诊断权限：%+v %v", res, err)
	}
}
