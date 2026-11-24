package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"video-canvas/internal/handler/mcp"
	"video-canvas/internal/model"
	"video-canvas/pkg/version"
)

// TestMCPInitializeAdvertisesBeta 守住握手回包：客户端靠 serverInfo 显示这套接入的身份，
// 标题里的 Beta 是对外状态（工具集合与权限档位还会调），版本用于排查，两者都不能被悄悄删掉。
// 同时验证未知协议版本回最新的已知版本，以及会话标识头照样下发。
func TestMCPInitializeAdvertisesBeta(t *testing.T) {
	old := version.Version
	version.Version = "0.7.9-test"
	t.Cleanup(func() { version.Version = old })

	r := newMCPRouter(&mcpStubTools{}, &mcpStubCanvases{}, 7, 0, scopeSet(model.MCPScopeCanvasBrowse))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
	))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是 JSON：%s", w.Body.String())
	}
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize 没回 result：%v", out)
	}
	if got := res["protocolVersion"]; got != "2025-06-18" {
		t.Fatalf("不认识的协议版本应回最新的 2025-06-18，得到 %v", got)
	}
	info, ok := res["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("缺 serverInfo：%v", res)
	}
	if info["name"] != "video-canvas" {
		t.Errorf("服务名应保持 video-canvas，得到 %v", info["name"])
	}
	title, _ := info["title"].(string)
	if !strings.Contains(title, "Beta") {
		t.Errorf("标题要带 Beta 标记，得到 %q", title)
	}
	if info["version"] != "0.7.9-test" {
		t.Errorf("版本应取服务端构建版本，得到 %v", info["version"])
	}
	if w.Header().Get(mcp.SessionHeader) == "" {
		t.Error("initialize 必须下发会话标识头")
	}
}
