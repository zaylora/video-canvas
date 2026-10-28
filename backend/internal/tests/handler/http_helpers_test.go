package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// characterCall 发一次请求并解出统一响应体；204 要求正文为空。
// 名字沿用角色卡下线前的叫法（短剧 handler 测试都在用），与角色卡已无关系。
func characterCall(t *testing.T, r *gin.Engine, method, path, body string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code == http.StatusNoContent {
		if w.Body.Len() != 0 {
			t.Fatalf("204 响应不应有正文：%s", w.Body.String())
		}
		return w.Code, nil
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是 JSON：%s", w.Body.String())
	}
	return w.Code, resp
}

// responseCode 取统一响应里的业务码。
func responseCode(t *testing.T, resp map[string]any) int {
	t.Helper()
	code, ok := resp["code"].(float64)
	if !ok {
		t.Fatalf("响应缺少 code：%+v", resp)
	}
	return int(code)
}

// responseData 取统一响应里的 data。
func responseData(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("响应缺少 data：%+v", resp)
	}
	return data
}
