package handler_test

import (
	"encoding/json"
	"net/http"
	"strings"
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
	env.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
	env.repo.Channels["a-main"] = &model.AIChannel{Key: "a-main", Name: "主线", BaseURL: "https://a.example.com", Sort: 10, Enabled: true}
	env.repo.Channels["b-off"] = &model.AIChannel{Key: "b-off", Name: "停用的", BaseURL: "https://b.example.com", Sort: 20, Enabled: false}
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
		if page.List[0]["key"] != "a-main" || health == nil || health["tone"] == nil || health["label"] == nil || health["reason"] == nil {
			t.Fatalf("每行应带 health{tone,label,reason}：%s", r.Raw)
		}
		if page.List[0]["model_count"] != float64(0) || page.List[0]["online_model_count"] != float64(0) {
			t.Fatalf("每行应带 model_count / online_model_count：%s", r.Raw)
		}
		// 渠道不再绑协议：列表行里没有插件、设置项、能力这些字段
		for _, gone := range []string{"plugin_key", "plugin_version", "plugin_name", "upgrade_to", "allow_credentials", `"kinds"`, `"settings"`} {
			if strings.Contains(r.Raw, gone) {
				t.Fatalf("渠道列表不应再有 %s：%s", gone, r.Raw)
			}
		}
	})
	t.Run("q / status 生效", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/channels?q=停用&status=off", nil)
		aicWant(t, r, http.StatusOK, 0)
		var page pageResp
		_ = json.Unmarshal(r.Data, &page)
		if page.Total != 1 || page.List[0]["key"] != "b-off" {
			t.Fatalf("实际 %s", r.Raw)
		}
	})
	t.Run("kind / plugin 筛选已取消：多传的参数被忽略，不报错也不过滤", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/channels?kind=bogus&plugin=ghost", nil)
		aicWant(t, r, http.StatusOK, 0)
		var page pageResp
		_ = json.Unmarshal(r.Data, &page)
		if page.Total != 2 {
			t.Fatalf("实际 %s", r.Raw)
		}
	})
	t.Run("非法状态 / 超长关键字 400", func(t *testing.T) {
		for _, q := range []string{"?status=bogus", "?q=" + repeat("a", 65)} {
			aicWant(t, env.admin(http.MethodGet, aicBase+"/channels"+q, nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		}
	})
	t.Run("model_count / online_model_count 按绑定到该渠道的模型统计", func(t *testing.T) {
		env := aicNewEnv(t)
		seedCatalog(t, env)
		aicWant(t, env.super(http.MethodPut, aicBase+"/channels/a-main/secret", map[string]any{"value": "sk-a"}), http.StatusOK, 0)
		aicWant(t, env.super(http.MethodPost, aicBase+"/models", map[string]any{"body": aicModelJSON("m1", "video", "a-main")}), http.StatusOK, 0)
		aicWant(t, env.super(http.MethodPost, aicBase+"/models", map[string]any{"body": aicModelJSON("m2", "video", "a-main")}), http.StatusOK, 0)
		aicWant(t, env.super(http.MethodPut, aicBase+"/models/m1/enabled", map[string]any{"enabled": true}), http.StatusOK, 0)
		r := env.admin(http.MethodGet, aicBase+"/channels", nil)
		aicWant(t, r, http.StatusOK, 0)
		list, _ := aicPageList(r)
		if len(list) != 2 || list[0]["model_count"] != float64(2) || list[0]["online_model_count"] != float64(1) || list[1]["model_count"] != float64(0) {
			t.Fatalf("模型数统计不符合预期：%s", r.Raw)
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
	if opts[0]["key"] != "a-main" || opts[0]["base_url"] != "https://a.example.com" || opts[0]["health"] == nil || opts[0]["secret_set"] == nil {
		t.Fatalf("字段不全：%s", r.Raw)
	}
	for _, gone := range []string{"plugin_key", "plugin_version", `"kinds"`, `"settings"`, "allow_credentials"} {
		if strings.Contains(r.Raw, gone) {
			t.Fatalf("渠道 options 不应再有 %s：%s", gone, r.Raw)
		}
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
		if page.List[0]["versions"] == nil || page.List[0]["model_count"] == nil || page.List[0]["outdated_model_count"] == nil || page.List[0]["latest_version"] == nil {
			t.Fatalf("每行应带 versions / model_count / outdated_model_count / latest_version：%s", r.Raw)
		}
		if strings.Contains(r.Raw, "channel_count") {
			t.Fatalf("插件列表不应再有 channel_count：%s", r.Raw)
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
	t.Run("模型数：model_count 是协议用了该插件的模型，outdated_model_count 是固定在旧版本上的", func(t *testing.T) {
		env := aicNewEnv(t)
		env.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
		env.seedPlugin(t, "kling", "1.1.0", model.PluginSourceUploaded)
		env.seedProtocolModel(t, "m-old", "c1", "kling", "1.0.0")
		env.seedProtocolModel(t, "m-new", "c1", "kling", "1.1.0")
		r := env.admin(http.MethodGet, aicBase+"/plugins", nil)
		aicWant(t, r, http.StatusOK, 0)
		list, err := aicPageList(r)
		if err != nil || len(list) != 1 || list[0]["model_count"] != float64(2) || list[0]["outdated_model_count"] != float64(1) {
			t.Fatalf("模型数统计不符合预期：%v %s", err, r.Raw)
		}
		// 版本视图里的 channel_count 改名为 model_count
		if strings.Contains(r.Raw, "channel_count") || !strings.Contains(r.Raw, `"model_count"`) {
			t.Fatalf("版本视图应带 model_count 而不是 channel_count：%s", r.Raw)
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
	t.Run("每行带上游模型名和协议插件（key、版本、显示名）", func(t *testing.T) {
		list, err := aicPageList(env.admin(http.MethodGet, aicBase+"/models", nil))
		if err != nil || len(list) != 2 {
			t.Fatalf("列表不符合预期：%v", err)
		}
		if list[0]["upstream"] != "kling-v2" || list[0]["plugin"] != "kling" || list[0]["plugin_version"] != "1.0.0" || list[0]["plugin_name"] != "插件-kling" {
			t.Fatalf("协议相关字段不符合预期：%v", list[0])
		}
	})
	t.Run("待配置的模型：health.status=pending，能按 status=pending 筛，不影响其他状态的筛选", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, aicBase+"/channels/a-main/models/import", map[string]any{"ids": []string{"raw-model"}}), http.StatusOK, 0)
		r := env.admin(http.MethodGet, aicBase+"/models?status=pending", nil)
		aicWant(t, r, http.StatusOK, 0)
		list, _ := aicPageList(r)
		if len(list) != 1 || list[0]["key"] != "raw-model" || list[0]["upstream"] != "raw-model" || list[0]["plugin"] != "" || list[0]["plugin_name"] != "" {
			t.Fatalf("实际 %s", r.Raw)
		}
		if health, _ := list[0]["health"].(map[string]any); health["status"] != "pending" {
			t.Fatalf("待配置的模型状态应是 pending：%s", r.Raw)
		}
		r = env.admin(http.MethodGet, aicBase+"/models?status=offline", nil)
		list, _ = aicPageList(r)
		if len(list) != 2 {
			t.Fatalf("offline 不应包含待配置的模型：%s", r.Raw)
		}
		// 清掉，避免影响后面的 options 计数
		delete(env.repo.Models, "raw-model")
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
			Models        struct{ Total, Online, Broken, Pending int } `json:"models"`
			Todos         []map[string]any                             `json:"todos"`
			ChannelBriefs []map[string]any                             `json:"channel_briefs"`
		}
		if err := json.Unmarshal(r.Data, &ov); err != nil || ov.Plugins.Total != 1 || ov.Channels.Total != 2 {
			t.Fatalf("汇总不对：%v %s", err, r.Raw)
		}
		if ov.Todos == nil || len(ov.ChannelBriefs) != 2 || ov.ChannelBriefs[0]["health"] == nil {
			t.Fatalf("应带 todos 和 channel_briefs：%s", r.Raw)
		}
	})
	t.Run("模型汇总带待配置数", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, aicBase+"/channels/a-main/models/import", map[string]any{"ids": []string{"raw-1", "raw-2"}}), http.StatusOK, 0)
		r := env.admin(http.MethodGet, aicBase+"/overview", nil)
		aicWant(t, r, http.StatusOK, 0)
		var ov struct {
			Models struct{ Total, Online, Broken, Pending int } `json:"models"`
		}
		if err := json.Unmarshal(r.Data, &ov); err != nil || ov.Models.Total != 2 || ov.Models.Pending != 2 || ov.Models.Online != 0 {
			t.Fatalf("汇总不对：%v %s", err, r.Raw)
		}
	})
	t.Run("普通用户 403，未登录 401", func(t *testing.T) {
		aicWant(t, env.do(http.MethodGet, aicBase+"/overview", nil, aicNormalUser), http.StatusForbidden, errcode.ErrForbidden.Code)
		aicWant(t, env.do(http.MethodGet, aicBase+"/overview", nil, 0), http.StatusUnauthorized, errcode.ErrUnauthorized.Code)
	})
}
