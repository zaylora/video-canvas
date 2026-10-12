package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// 渠道列表 / options 的 handler 测试：真实 service + 真实 router，覆盖路由、权限、参数校验和响应结构。

type pageResp struct {
	List     []map[string]any `json:"list"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

func seedCatalog(t *testing.T, env *aicEnv) {
	t.Helper()
	id := env.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
	env.repo.Channels["a-main"] = &model.AIChannel{Key: "a-main", Name: "主线", PluginKey: "kling", PluginVersionID: id, Enabled: true}
	env.repo.Channels["b-off"] = &model.AIChannel{Key: "b-off", Name: "停用的", PluginKey: "kling", PluginVersionID: id, Enabled: false}
}

func TestAdminCatalogHandler_ChannelList(t *testing.T) {
	env := aicNewEnv(t)
	seedCatalog(t, env)

	t.Run("admin 可读，响应是分页结构并带健康状态", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/channels", nil)
		aicWant(t, r, http.StatusOK, 0)
		var page pageResp
		if err := json.Unmarshal(r.Data, &page); err != nil || page.Total != 2 || len(page.List) != 2 || page.Page != 1 || page.PageSize != 20 {
			t.Fatalf("不是分页结构：%v %s", err, r.Raw)
		}
		health, _ := page.List[0]["health"].(map[string]any)
		if page.List[0]["key"] != "a-main" || health == nil || health["tone"] == nil {
			t.Fatalf("每行应带 health：%s", r.Raw)
		}
	})
	t.Run("q / status / kind / plugin 生效", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/channels?q=停用&status=off&kind=video&plugin=kling", nil)
		aicWant(t, r, http.StatusOK, 0)
		var page pageResp
		_ = json.Unmarshal(r.Data, &page)
		if page.Total != 1 || page.List[0]["key"] != "b-off" {
			t.Fatalf("实际 %s", r.Raw)
		}
	})
	t.Run("非法状态 / 能力 / 超长关键字 400", func(t *testing.T) {
		for _, q := range []string{"?status=bogus", "?kind=bogus", "?q=" + string(make([]byte, 0)) + repeat("a", 65)} {
			aicWant(t, env.admin(http.MethodGet, aicBase+"/channels"+q, nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		}
	})
	t.Run("普通用户 403，未登录 401", func(t *testing.T) {
		aicWant(t, env.do(http.MethodGet, aicBase+"/channels", nil, aicNormalUser), http.StatusForbidden, errcode.ErrForbidden.Code)
		aicWant(t, env.do(http.MethodGet, aicBase+"/channels", nil, 0), http.StatusUnauthorized, errcode.ErrUnauthorized.Code)
	})
}

func TestAdminCatalogHandler_ChannelOptions(t *testing.T) {
	env := aicNewEnv(t)
	seedCatalog(t, env)
	r := env.admin(http.MethodGet, aicBase+"/channels/options", nil)
	aicWant(t, r, http.StatusOK, 0)
	var opts []map[string]any
	if err := json.Unmarshal(r.Data, &opts); err != nil || len(opts) != 2 {
		t.Fatalf("options 应是不分页的数组：%v %s", err, r.Raw)
	}
	if opts[0]["key"] != "a-main" || opts[0]["plugin_key"] != "kling" || opts[0]["health"] == nil || opts[0]["kinds"] == nil {
		t.Fatalf("字段不全：%s", r.Raw)
	}
	aicWant(t, env.do(http.MethodGet, aicBase+"/channels/options", nil, aicNormalUser), http.StatusForbidden, errcode.ErrForbidden.Code)
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func TestAdminCatalogHandler_PluginList(t *testing.T) {
	env := aicNewEnv(t)
	env.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
	env.seedPlugin(t, "newapi", "1.0.0", model.PluginSourceBuiltin)

	t.Run("响应是分页结构并带版本", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/plugins", nil)
		aicWant(t, r, http.StatusOK, 0)
		var page pageResp
		if err := json.Unmarshal(r.Data, &page); err != nil || page.Total != 2 || len(page.List) != 2 {
			t.Fatalf("不是分页结构：%v %s", err, r.Raw)
		}
		if page.List[0]["versions"] == nil || page.List[0]["channel_count"] == nil || page.List[0]["latest_version"] == nil {
			t.Fatalf("每行应带 versions / channel_count / latest_version：%s", r.Raw)
		}
	})
	t.Run("q / source / status 生效", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/plugins?q=newapi&source=builtin&status=on", nil)
		aicWant(t, r, http.StatusOK, 0)
		var page pageResp
		_ = json.Unmarshal(r.Data, &page)
		if page.Total != 1 || page.List[0]["key"] != "newapi" {
			t.Fatalf("实际 %s", r.Raw)
		}
	})
	t.Run("非法来源 / 状态 400", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodGet, aicBase+"/plugins?source=bogus", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodGet, aicBase+"/plugins?status=bogus", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("options 是不分页的数组；详情按 key，不存在 404", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/plugins/options", nil)
		aicWant(t, r, http.StatusOK, 0)
		var opts []map[string]any
		if err := json.Unmarshal(r.Data, &opts); err != nil || len(opts) != 2 {
			t.Fatalf("options 应是数组：%v %s", err, r.Raw)
		}
		aicWant(t, env.admin(http.MethodGet, aicBase+"/plugins/kling", nil), http.StatusOK, 0)
		aicWant(t, env.admin(http.MethodGet, aicBase+"/plugins/ghost", nil), http.StatusNotFound, errcode.ErrPluginNotFound.Code)
	})
}

// aicPageList 取分页响应里的 list。
func aicPageList(r aicResp) ([]map[string]any, error) {
	var page pageResp
	if err := json.Unmarshal(r.Data, &page); err != nil {
		return nil, err
	}
	return page.List, nil
}

func TestAdminCatalogHandler_ModelList(t *testing.T) {
	env := aicNewEnv(t)
	seedCatalog(t, env)
	aicWant(t, env.super(http.MethodPost, aicBase+"/models", map[string]any{"body": aicModelJSON("m1", "video", "a-main")}), http.StatusOK, 0)
	aicWant(t, env.super(http.MethodPost, aicBase+"/models", map[string]any{"body": aicModelJSON("m2", "video", "b-off")}), http.StatusOK, 0)

	t.Run("响应是分页结构，每行带状态和渠道名", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/models", nil)
		aicWant(t, r, http.StatusOK, 0)
		var page pageResp
		if err := json.Unmarshal(r.Data, &page); err != nil || page.Total != 2 || len(page.List) != 2 {
			t.Fatalf("不是分页结构：%v %s", err, r.Raw)
		}
		health, _ := page.List[0]["health"].(map[string]any)
		if health == nil || health["status"] != "offline" || page.List[0]["channel_name"] != "主线" {
			t.Fatalf("没上架的模型是 offline，并带渠道名：%s", r.Raw)
		}
	})
	t.Run("按渠道和状态筛选", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/models?channel=b-off&status=offline&kind=video", nil)
		aicWant(t, r, http.StatusOK, 0)
		list, _ := aicPageList(r)
		if len(list) != 1 || list[0]["key"] != "m2" {
			t.Fatalf("实际 %s", r.Raw)
		}
	})
	t.Run("非法状态 / 类型 400", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodGet, aicBase+"/models?status=bogus", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodGet, aicBase+"/models?kind=bogus", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("options 是不分页的数组；普通用户 403", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/models/options", nil)
		aicWant(t, r, http.StatusOK, 0)
		var opts []map[string]any
		if err := json.Unmarshal(r.Data, &opts); err != nil || len(opts) != 2 {
			t.Fatalf("options 应是数组：%v %s", err, r.Raw)
		}
		aicWant(t, env.do(http.MethodGet, aicBase+"/models/options", nil, aicNormalUser), http.StatusForbidden, errcode.ErrForbidden.Code)
	})
}

func TestAdminCatalogHandler_Overview(t *testing.T) {
	env := aicNewEnv(t)
	seedCatalog(t, env)

	t.Run("admin 可读，返回汇总、待处理清单和精简渠道", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/overview", nil)
		aicWant(t, r, http.StatusOK, 0)
		var ov struct {
			Plugins  struct{ Total, Usable int } `json:"plugins"`
			Channels struct {
				Total  int `json:"total"`
				Usable int `json:"usable"`
				NoKey  int `json:"no_key"`
			} `json:"channels"`
			Models        struct{ Total, Online, Broken int } `json:"models"`
			Todos         []map[string]any                    `json:"todos"`
			ChannelBriefs []map[string]any                    `json:"channel_briefs"`
		}
		if err := json.Unmarshal(r.Data, &ov); err != nil || ov.Plugins.Total != 1 || ov.Channels.Total != 2 {
			t.Fatalf("汇总不对：%v %s", err, r.Raw)
		}
		if ov.Todos == nil || len(ov.ChannelBriefs) != 2 || ov.ChannelBriefs[0]["health"] == nil {
			t.Fatalf("应带 todos 和 channel_briefs：%s", r.Raw)
		}
	})
	t.Run("普通用户 403，未登录 401", func(t *testing.T) {
		aicWant(t, env.do(http.MethodGet, aicBase+"/overview", nil, aicNormalUser), http.StatusForbidden, errcode.ErrForbidden.Code)
		aicWant(t, env.do(http.MethodGet, aicBase+"/overview", nil, 0), http.StatusUnauthorized, errcode.ErrUnauthorized.Code)
	})
}
