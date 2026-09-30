package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/service/aiconfigfake"
)

// 本文件是插件 / 渠道服务测试共用的小工具。

// adminMetaJSON 生成一份 meta（bearer 鉴权，支持 video 与 text，两个渠道设置项：region 有默认值，tenant 必填）。
// tenant 必填是为了让“漏填必填设置项”的用例有目标；需要更宽松的版本用 adminMetaLoose。
func adminMetaJSON(key, version string) json.RawMessage {
	return json.RawMessage(`{"apiVersion":1,"key":"` + key + `","name":"插件-` + key + `","version":"` + version + `",` +
		`"auth":{"type":"bearer"},"endpoints":{"video":{"mode":"async"},"text":{"mode":"sync"}},` +
		`"channelSettings":{"region":{"type":"enum","label":"区域","options":["cn","global"],"default":"cn"},` +
		`"tenant":{"type":"string","label":"租户","required":true},"burst":{"type":"number","label":"突发"},"debug":{"type":"boolean","label":"调试"}},` +
		`"import":{"args":{"prefix":{"type":"string","label":"前缀","default":""},"limit":{"type":"number","label":"数量","required":true}}}}`)
}

// adminMetaLoose 是没有任何渠道设置项、鉴权为 none 的 meta。
func adminMetaLoose(key, version string) json.RawMessage {
	return json.RawMessage(`{"apiVersion":1,"key":"` + key + `","name":"插件-` + key + `","version":"` + version + `",` +
		`"auth":{"type":"none"},"endpoints":{"video":{"mode":"async"}}}`)
}

// adminPass 是“预检通过”的结果。
func adminPass(meta json.RawMessage) *provider.PrecheckResult {
	return &provider.PrecheckResult{OK: true, Meta: meta}
}

// adminSeedVersion 直接往 fake 仓储里放一个插件版本（不经过服务），返回版本 id。
func adminSeedVersion(t *testing.T, repo *aiconfigfake.MemRepo, meta json.RawMessage, source string, enabled bool) uint64 {
	t.Helper()
	var m struct {
		Key     string `json:"key"`
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(meta, &m); err != nil {
		t.Fatal(err)
	}
	p := &model.AIPlugin{Key: m.Key, Name: m.Name, Source: source, Enabled: enabled}
	v := &model.AIPluginVersion{PluginKey: m.Key, Version: m.Version, SHA256: "seed", Code: "code", MetaJSON: model.JSONText(meta)}
	if err := repo.SaveVersion(context.Background(), p, v); err != nil {
		t.Fatal(err)
	}
	// SaveVersion 只在插件行不存在时才采用传入的 enabled；已存在时（多版本）不动它
	if !enabled {
		_ = repo.SetPluginEnabled(context.Background(), m.Key, false)
	}
	return v.ID
}

// adminWantCode 断言错误是指定业务错误码；wantCode=0 表示期望成功。
func adminWantCode(t *testing.T, err error, wantCode int) {
	t.Helper()
	_ = adminWantCodeErr(t, err, wantCode)
}

// adminWantCodeErr 同 adminWantCode，并返回业务错误（成功时为 nil），需要检查 msg 的用例用它。
func adminWantCodeErr(t *testing.T, err error, wantCode int) *errcode.Error {
	t.Helper()
	if wantCode == 0 {
		if err != nil {
			t.Fatalf("期望成功，实际返回错误：%v", err)
		}
		return nil
	}
	var e *errcode.Error
	if !errors.As(err, &e) {
		t.Fatalf("期望业务错误 %d，实际 %v", wantCode, err)
	}
	if e.Code != wantCode {
		t.Fatalf("期望业务错误码 %d，实际 %d（%s）", wantCode, e.Code, e.Msg)
	}
	return e
}
