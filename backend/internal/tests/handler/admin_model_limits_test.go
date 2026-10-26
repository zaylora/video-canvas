package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"video-canvas/internal/pkg/errcode"
)

// GET /admin/ai/models/limits：按上游模型名查内置的上下文规格，给新建 Agent / 文本模型时自动填写用。
func TestAdminCatalogHandler_ModelLimits(t *testing.T) {
	env := aicNewEnv(t)

	t.Run("认识的模型", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/models/limits?name=openai/gpt-4o", nil)
		aicWant(t, r, http.StatusOK, 0)
		var got struct {
			Found         bool   `json:"found"`
			ContextWindow int    `json:"context_window"`
			MaxOutput     int    `json:"max_output"`
			Source        string `json:"source"`
		}
		if err := json.Unmarshal(r.Data, &got); err != nil {
			t.Fatal(err)
		}
		if !got.Found || got.ContextWindow != 128000 || got.MaxOutput != 16384 || got.Source != "builtin" {
			t.Fatalf("实际 %s", r.Raw)
		}
	})
	t.Run("不认识的模型也是 200，found=false", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/models/limits?name=my-private-model-v7", nil)
		aicWant(t, r, http.StatusOK, 0)
		var got struct {
			Found bool `json:"found"`
		}
		_ = json.Unmarshal(r.Data, &got)
		if got.Found {
			t.Fatalf("不该认识：%s", r.Raw)
		}
	})
	t.Run("channel 可选：渠道不存在或上游不可用时悄悄退回内置，仍是 200", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/models/limits?name=gpt-4o&channel=no-such", nil)
		aicWant(t, r, http.StatusOK, 0)
		var got struct {
			Found  bool   `json:"found"`
			Source string `json:"source"`
		}
		_ = json.Unmarshal(r.Data, &got)
		if !got.Found || got.Source != "builtin" {
			t.Fatalf("实际 %s", r.Raw)
		}
	})
	t.Run("缺 name 返回 400", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodGet, aicBase+"/models/limits", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("普通用户不可读", func(t *testing.T) {
		aicWant(t, env.do(http.MethodGet, aicBase+"/models/limits?name=gpt-4o", nil, aicNormalUser), http.StatusForbidden, errcode.ErrForbidden.Code)
	})
}
