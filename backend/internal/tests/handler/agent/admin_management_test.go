package agent_test

import (
	"net/http"
	"strings"
	"testing"

	"video-canvas/internal/pkg/errcode"
	agentsvc "video-canvas/internal/service/agent"
)

func TestPlatformAdminManagementTools(t *testing.T) {
	for _, name := range []string{"admin_query", "admin_get_resource"} {
		t.Run(name, func(t *testing.T) {
			headers := map[string]string{"X-Agent-Context": ctxHeader(`{"run_id":"1","user_id":"1","session_id":"1","canvas_id":"0","mode":"admin"}`)}
			api := &fakePlatform{res: &agentsvc.ToolResult{Content: `{"body":{}}`}}
			path := "/internal/platform/v1/tools/" + name
			w := platformCall(newPlatformRouter(api), http.MethodPost, path, `{"tool_call_id":"m1","args":{"resource":"users"}}`, headers)
			if w.Code != 200 || api.name != name || api.rc.Mode != "admin" {
				t.Fatalf("成功调用：%d %s", w.Code, w.Body)
			}
			w = platformCall(newPlatformRouter(api), http.MethodPost, path, `{"args":`, headers)
			if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":10001`) {
				t.Fatalf("参数校验：%d %s", w.Code, w.Body)
			}
			api.err = errcode.ErrForbidden
			w = platformCall(newPlatformRouter(api), http.MethodPost, path, `{"args":{}}`, headers)
			if w.Code != 403 {
				t.Fatalf("权限错误：%d %s", w.Code, w.Body)
			}
		})
	}
}
