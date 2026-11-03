package agent_test

import (
	"net/http"
	"strings"
	"testing"

	"video-canvas/internal/pkg/errcode"
	agentsvc "video-canvas/internal/service/agent"
)

func TestPlatformAdminProposeIdempotencyAndErrors(t *testing.T) {
	for _, kind := range []string{"admin_secret", "admin_test_run"} {
		api := &fakePlatform{proposal: &agentsvc.ApprovalProposal{Content: "等待审批", Payload: []byte(`{"reason":"测试"}`)}}
		headers := map[string]string{"X-Agent-Context": ctxHeader(`{"run_id":"1","user_id":"1","session_id":"1","canvas_id":"0","mode":"admin"}`), "X-Idempotency-Key": "durable.task.123"}
		w := platformCall(newPlatformRouter(api), http.MethodPost, approvalsPath+"propose", `{"kind":"`+kind+`","args":{}}`, headers)
		if w.Code != 200 || api.idemKey != "durable.task.123" || api.kind != kind {
			t.Fatalf("未透传二期审批幂等键：%d %s %+v", w.Code, w.Body, api)
		}
		headers["X-Idempotency-Key"] = "invalid/key"
		w = platformCall(newPlatformRouter(api), http.MethodPost, approvalsPath+"propose", `{"kind":"`+kind+`"}`, headers)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":10001`) {
			t.Fatalf("非法幂等键：%d %s", w.Code, w.Body)
		}
		delete(headers, "X-Idempotency-Key")
		api.err = errcode.ErrForbidden
		w = platformCall(newPlatformRouter(api), http.MethodPost, approvalsPath+"propose", `{"kind":"`+kind+`"}`, headers)
		if w.Code != 403 {
			t.Fatalf("权限错误未透传：%d %s", w.Code, w.Body)
		}
	}
}
