package handler_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// 本文件测试模型 / 渠道 / 插件的删除预检与删除接口。

// aicLongKey 超过路径参数 key 的长度上限（128），用来触发参数校验失败。
var aicLongKey = strings.Repeat("k", 129)

// aicBlockers 解析 delete-check 的 data，并确认 blockers 与 refs 都是数组而不是 null。
func aicBlockers(t *testing.T, r aicResp) []model.DeleteBlocker {
	t.Helper()
	aicWant(t, r, http.StatusOK, 0)
	if strings.Contains(string(r.Data), "null") {
		t.Fatalf("预检结果不应含 null：%s", r.Data)
	}
	var res model.DeleteCheck
	if err := json.Unmarshal(r.Data, &res); err != nil || res.Blockers == nil {
		t.Fatalf("预检结果格式不符合预期：%v %s", err, r.Raw)
	}
	return res.Blockers
}

func TestAdminAIHandler_DeleteModel(t *testing.T) {
	env := aicNewEnv(t)
	env.enableAll(t) // m1 已发布并上架

	t.Run("预检：上架中返回 model_enabled；参数校验失败 400；不存在 404", func(t *testing.T) {
		bs := aicBlockers(t, env.admin(http.MethodGet, aicBase+"/models/m1/delete-check", nil))
		if len(bs) != 1 || bs[0].Kind != model.BlockerModelEnabled || bs[0].Message != "模型还在上线，先下线再删除" {
			t.Fatalf("阻断原因不符合预期：%+v", bs)
		}
		aicWant(t, env.admin(http.MethodGet, aicBase+"/models/"+aicLongKey+"/delete-check", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodGet, aicBase+"/models/ghost/delete-check", nil), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
	})
	t.Run("删除：上架中 409（50031）；参数校验失败 400；不存在 404", func(t *testing.T) {
		r := env.admin(http.MethodDelete, aicBase+"/models/m1", nil)
		aicWant(t, r, http.StatusConflict, errcode.ErrModelEnabled.Code)
		if r.Msg != "模型还在上线，先下线再删除" {
			t.Fatalf("文案不符合预期：%q", r.Msg)
		}
		aicWant(t, env.admin(http.MethodDelete, aicBase+"/models/"+aicLongKey, nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodDelete, aicBase+"/models/ghost", nil), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
	})
	t.Run("下架后预检为空数组，admin 删除成功，之后处处看不到它", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/m1/enabled", map[string]any{"enabled": false}), http.StatusOK, 0)
		if bs := aicBlockers(t, env.admin(http.MethodGet, aicBase+"/models/m1/delete-check", nil)); len(bs) != 0 {
			t.Fatalf("下架后不应有阻断原因：%+v", bs)
		}
		aicWant(t, env.admin(http.MethodDelete, aicBase+"/models/m1", nil), http.StatusOK, 0)
		aicWant(t, env.admin(http.MethodGet, aicBase+"/models/m1", nil), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
		r := env.admin(http.MethodGet, aicBase+"/models", nil)
		aicWant(t, r, http.StatusOK, 0)
		if strings.Contains(string(r.Data), `"m1"`) {
			t.Fatalf("后台列表不应有已删除的模型：%s", r.Data)
		}
		r = env.do(http.MethodGet, "/api/v1/models", nil, aicNormalUser)
		aicWant(t, r, http.StatusOK, 0)
		if strings.Contains(string(r.Data), `"m1"`) {
			t.Fatalf("公开清单不应有已删除的模型：%s", r.Data)
		}
	})
	t.Run("同名 key 可以重新新建，并且是全新的（未启用）", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models", map[string]any{"body": aicModelJSON("m1", "video", "kling-main")}), http.StatusOK, 0)
		r := env.admin(http.MethodGet, aicBase+"/models/m1", nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"enabled":false`) {
			t.Fatalf("重新新建的模型应未启用：%s", r.Raw)
		}
	})
}

func TestAdminChannelHandler_Delete(t *testing.T) {
	env := aicNewEnv(t)
	env.enableAll(t) // 渠道 kling-main 被模型 m1 引用

	t.Run("权限：admin 调用预检与删除都是 403", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodGet, aicBase+"/channels/kling-main/delete-check", nil), http.StatusForbidden, errcode.ErrForbidden.Code)
		aicWant(t, env.admin(http.MethodDelete, aicBase+"/channels/kling-main", nil), http.StatusForbidden, errcode.ErrForbidden.Code)
	})
	t.Run("预检：channel_models 列出模型 key 与展示名；参数校验失败 400；不存在 404", func(t *testing.T) {
		bs := aicBlockers(t, env.super(http.MethodGet, aicBase+"/channels/kling-main/delete-check", nil))
		if len(bs) != 1 || bs[0].Kind != model.BlockerChannelModels || bs[0].Message != "1 个模型在用这个渠道" ||
			len(bs[0].Refs) != 1 || bs[0].Refs[0] != (model.DeleteRef{Key: "m1", Name: "模型-m1"}) {
			t.Fatalf("阻断原因不符合预期：%+v", bs)
		}
		aicWant(t, env.super(http.MethodGet, aicBase+"/channels/"+aicLongKey+"/delete-check", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.super(http.MethodGet, aicBase+"/channels/ghost/delete-check", nil), http.StatusNotFound, errcode.ErrChannelNotFound.Code)
	})
	t.Run("删除：被引用 409（50016）；参数校验失败 400；不存在 404", func(t *testing.T) {
		r := env.super(http.MethodDelete, aicBase+"/channels/kling-main", nil)
		aicWant(t, r, http.StatusConflict, errcode.ErrChannelInUse.Code)
		if !strings.Contains(r.Msg, "1 个模型") {
			t.Fatalf("文案应带数量：%q", r.Msg)
		}
		aicWant(t, env.super(http.MethodDelete, aicBase+"/channels/"+aicLongKey, nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.super(http.MethodDelete, aicBase+"/channels/ghost", nil), http.StatusNotFound, errcode.ErrChannelNotFound.Code)
	})
	t.Run("模型删除后渠道可删，Key 一并删除", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/m1/enabled", map[string]any{"enabled": false}), http.StatusOK, 0)
		aicWant(t, env.admin(http.MethodDelete, aicBase+"/models/m1", nil), http.StatusOK, 0)
		if bs := aicBlockers(t, env.super(http.MethodGet, aicBase+"/channels/kling-main/delete-check", nil)); len(bs) != 0 {
			t.Fatalf("不应再有阻断原因：%+v", bs)
		}
		aicWant(t, env.super(http.MethodDelete, aicBase+"/channels/kling-main", nil), http.StatusOK, 0)
		aicWant(t, env.super(http.MethodGet, aicBase+"/channels/kling-main", nil), http.StatusNotFound, errcode.ErrChannelNotFound.Code)
		if _, ok := env.repo.Secrets[model.ChannelSecretName("kling-main")]; ok {
			t.Fatal("渠道 Key 应一并删除")
		}
	})
}

func TestAdminPluginHandler_DeletePlugin(t *testing.T) {
	env := aicNewEnv(t)
	id := env.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
	env.seedPlugin(t, "kling", "1.1.0", model.PluginSourceUploaded)
	env.seedPlugin(t, "newapi", "1.0.0", model.PluginSourceBuiltin)
	env.repo.Channels["c1"] = &model.AIChannel{Key: "c1", Name: "渠道一", PluginKey: "kling", PluginVersionID: id}

	t.Run("权限：admin 调用预检与删除都是 403", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodGet, aicBase+"/plugins/kling/delete-check", nil), http.StatusForbidden, errcode.ErrForbidden.Code)
		aicWant(t, env.admin(http.MethodDelete, aicBase+"/plugins/kling", nil), http.StatusForbidden, errcode.ErrForbidden.Code)
	})
	t.Run("预检：plugin_channels / builtin_plugin；参数校验失败 400；不存在 404", func(t *testing.T) {
		bs := aicBlockers(t, env.super(http.MethodGet, aicBase+"/plugins/kling/delete-check", nil))
		if len(bs) != 1 || bs[0].Kind != model.BlockerPluginChannels || bs[0].Refs[0] != (model.DeleteRef{Key: "c1", Name: "渠道一"}) {
			t.Fatalf("阻断原因不符合预期：%+v", bs)
		}
		bs = aicBlockers(t, env.super(http.MethodGet, aicBase+"/plugins/newapi/delete-check", nil))
		if len(bs) != 1 || bs[0].Kind != model.BlockerBuiltinPlugin {
			t.Fatalf("内置插件应返回 builtin_plugin：%+v", bs)
		}
		aicWant(t, env.super(http.MethodGet, aicBase+"/plugins/"+aicLongKey+"/delete-check", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.super(http.MethodGet, aicBase+"/plugins/ghost/delete-check", nil), http.StatusNotFound, errcode.ErrPluginNotFound.Code)
	})
	t.Run("删除：被渠道引用 409（50005）；内置 409（50008）；参数校验失败 400；不存在 404", func(t *testing.T) {
		aicWant(t, env.super(http.MethodDelete, aicBase+"/plugins/kling", nil), http.StatusConflict, errcode.ErrPluginInUse.Code)
		aicWant(t, env.super(http.MethodDelete, aicBase+"/plugins/newapi", nil), http.StatusConflict, errcode.ErrPluginBuiltinDel.Code)
		aicWant(t, env.super(http.MethodDelete, aicBase+"/plugins/"+aicLongKey, nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.super(http.MethodDelete, aicBase+"/plugins/ghost", nil), http.StatusNotFound, errcode.ErrPluginNotFound.Code)
	})
	t.Run("渠道移走后删除成功，全部版本一起删", func(t *testing.T) {
		delete(env.repo.Channels, "c1")
		if bs := aicBlockers(t, env.super(http.MethodGet, aicBase+"/plugins/kling/delete-check", nil)); len(bs) != 0 {
			t.Fatalf("不应再有阻断原因：%+v", bs)
		}
		aicWant(t, env.super(http.MethodDelete, aicBase+"/plugins/kling", nil), http.StatusOK, 0)
		if _, ok := env.repo.Plugins["kling"]; ok || len(env.repo.Versions) != 1 {
			t.Fatalf("插件与全部版本应已删除：%d 个版本", len(env.repo.Versions))
		}
	})
}
