package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
)

// TestMCPProjectReqAcceptsEveryScope 权限档位有几档，请求体就要能一次全选。
//
// 项数上限写死在 binding 标签里时，档位表一扩就会出事：短剧拆成读 / 改 / 跑三档以后
// 总档位到 10，而标签还停在 8，用户在界面上全选就被 422 拦住，提示只说「最多 8 项」，
// 看不出是代码里的常量落后于能力表。这条用例把上限和档位表绑在一起。
func TestMCPProjectReqAcceptsEveryScope(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"name":   "全选",
		"scopes": model.MCPScopeOrder,
	})
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	var got *model.MCPProjectReq
	var bindErr error
	r.POST("/", func(c *gin.Context) {
		var req model.MCPProjectReq
		bindErr = c.ShouldBindJSON(&req)
		got = &req
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if bindErr != nil {
		t.Fatalf("全选 %d 个档位应当通过校验，实际被拒：%v", len(model.MCPScopeOrder), bindErr)
	}
	if len(got.Scopes) != len(model.MCPScopeOrder) {
		t.Fatalf("档位数对不上：提交 %d 个，解析出 %d 个", len(model.MCPScopeOrder), len(got.Scopes))
	}
}
