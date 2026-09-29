package response_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	. "video-canvas/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

func TestAccepted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/x", func(c *gin.Context) { Accepted(c, gin.H{"id": 1}) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", nil))

	if w.Code != http.StatusAccepted {
		t.Fatalf("期望 HTTP 202，实际 %d", w.Code)
	}
	var body Body
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != 0 || body.Msg != "success" {
		t.Fatalf("响应结构不对：%+v", body)
	}
	if data, ok := body.Data.(map[string]any); !ok || data["id"] != float64(1) {
		t.Fatalf("data 不对：%+v", body.Data)
	}
}
