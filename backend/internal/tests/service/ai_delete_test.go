package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service"
	"video-canvas/internal/service/aiconfigfake"
)

// 本文件测试模型 / 渠道 / 插件的删除预检与删除。

// delWantBlockers 断言预检结果的 kind 列表（按顺序），并检查 blockers / refs 永远不是 nil。
func delWantBlockers(t *testing.T, res *model.DeleteCheck, kinds ...string) {
	t.Helper()
	if res == nil || res.Blockers == nil {
		t.Fatalf("blockers 应是数组而不是 nil：%+v", res)
	}
	if len(res.Blockers) != len(kinds) {
		t.Fatalf("期望阻断原因 %v，实际 %+v", kinds, res.Blockers)
	}
	for i, b := range res.Blockers {
		if b.Kind != kinds[i] || b.Refs == nil || b.Message == "" {
			t.Fatalf("第 %d 条阻断原因不符合预期（期望 %s）：%+v", i, kinds[i], b)
		}
	}
	// JSON 里也必须是 []
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), "null") {
		t.Fatalf("预检结果的 JSON 不应含 null：%s", raw)
	}
}

// ---------------------------------------------------------------------------
// 模型
// ---------------------------------------------------------------------------

// delModelSvc 准备：渠道 c1 就绪，模型 m1 已发布（上架中）。
func delModelSvc(t *testing.T) (*AIConfigService, *aiconfigfake.MemRepo, *aiconfigfake.Invalidator) {
	t.Helper()
	svc, repo, inv := aicNewSvc()
	svc.SetDryRunner(&aiconfigfake.DryRunner{Result: map[string]any{}})
	aicSeedChannel(t, svc, repo, "c1", true)
	aicEnableModel(t, svc, "m1", "c1")
	return svc, repo, inv
}

func TestAIConfigService_CheckModelDelete(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		key      string
		disable  bool
		wantCode int
		kinds    []string
	}{
		{"模型不存在返回 40011", "ghost", false, errcode.ErrConfigNotFound.Code, nil},
		{"上架中：model_enabled", "m1", false, 0, []string{model.BlockerModelEnabled}},
		{"已下架：没有阻断原因（空数组）", "m1", true, 0, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := delModelSvc(t)
			if tt.disable {
				if err := svc.SetModelEnabled(ctx, "m1", false, 1); err != nil {
					t.Fatal(err)
				}
			}
			res, err := svc.CheckModelDelete(ctx, tt.key)
			aicWantCode(t, err, tt.wantCode)
			if tt.wantCode == 0 {
				delWantBlockers(t, res, tt.kinds...)
			}
		})
	}
	t.Run("上架中的阻断文案", func(t *testing.T) {
		svc, _, _ := delModelSvc(t)
		res, _ := svc.CheckModelDelete(ctx, "m1")
		if res.Blockers[0].Message != "模型还在上线，先下线再删除" {
			t.Fatalf("文案不符合预期：%q", res.Blockers[0].Message)
		}
	})
}

func TestAIConfigService_DeleteModel(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		key      string
		setup    func(t *testing.T, s *AIConfigService, r *aiconfigfake.MemRepo)
		wantCode int
	}{
		{"模型不存在返回 40011", "ghost", nil, errcode.ErrConfigNotFound.Code},
		{"还在上架返回 50031", "m1", nil, errcode.ErrModelEnabled.Code},
		{"预检之后被并发重新上架：事务内复查同样返回 50031", "m1", func(t *testing.T, s *AIConfigService, r *aiconfigfake.MemRepo) {
			_ = s.SetModelEnabled(ctx, "m1", false, 1)
			r.DeleteInUse = true
		}, errcode.ErrModelEnabled.Code},
		{"已下架：删除成功", "m1", func(t *testing.T, s *AIConfigService, _ *aiconfigfake.MemRepo) {
			_ = s.SetModelEnabled(ctx, "m1", false, 1)
		}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, inv := delModelSvc(t)
			if tt.setup != nil {
				tt.setup(t, svc, repo)
			}
			before := len(inv.Reasons)
			err := svc.DeleteModel(ctx, tt.key, 1)
			aicWantCode(t, err, tt.wantCode)
			if tt.wantCode != 0 {
				if len(inv.Reasons) != before {
					t.Fatal("删除失败不应触发失效广播")
				}
				return
			}
			if len(inv.Reasons) != before+1 {
				t.Fatalf("删除成功应刷新 Registry：%v", inv.Reasons)
			}
			if _, ok := repo.Models["m1"]; ok {
				t.Fatal("指针行应已删除")
			}
			for _, rev := range repo.Revs {
				if rev.TargetKey == "m1" {
					t.Fatalf("revision 应全部删除：%+v", rev)
				}
			}
		})
	}
}

func TestAIConfigService_DeletedModelGone(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := delModelSvc(t)
	if err := svc.SetModelEnabled(ctx, "m1", false, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := aicSave(svc, "m1", false, aicModelBody("m1", "c1", "")); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteModel(ctx, "m1", 1); err != nil {
		t.Fatal(err)
	}

	t.Run("后台列表、详情、公开清单、下单快照都没有它", func(t *testing.T) {
		items, err := svc.ListConfigs(ctx)
		if err != nil || len(items) != 0 {
			t.Fatalf("列表里不应有已删除的模型：%+v %v", items, err)
		}
		_, err = svc.GetConfig(ctx, "m1")
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
		if list, err := svc.ListModels(ctx, ""); err != nil || len(list) != 0 {
			t.Fatalf("公开清单里不应有已删除的模型：%+v %v", list, err)
		}
		if _, err := svc.Snapshot(ctx, "m1"); err == nil {
			t.Fatal("已删除的模型不应能生成快照")
		}
		_, err = svc.DryRun(ctx, "m1", map[string]any{"prompt": "x"})
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
		aicWantCode(t, svc.DeleteModel(ctx, "m1", 1), errcode.ErrConfigNotFound.Code)
	})

	t.Run("同名 key 可以重新新建并启用", func(t *testing.T) {
		if _, err := aicSave(svc, "", true, aicModelBody("m1", "c1", "")); err != nil {
			t.Fatal(err)
		}
		if err := svc.SetModelEnabled(ctx, "m1", true, 1); err != nil {
			t.Fatalf("重新新建的模型应能启用：%v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// 渠道
// ---------------------------------------------------------------------------

// delChannelEnv 准备：渠道 kling-main 已创建（并设置了 Key）。
func delChannelEnv(t *testing.T) *achEnv {
	t.Helper()
	e := newAchEnv(t)
	if _, err := e.svc.Create(context.Background(), createInput()); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetSecret(context.Background(), 5, "kling-main", "sk-main"); err != nil {
		t.Fatal(err)
	}
	return e
}

// delSaveModel 直接保存一个引用渠道的模型草稿（label 为空时正文里不写 label）。
func delSaveModel(t *testing.T, e *achEnv, key, label, channel string) {
	t.Helper()
	body := `{"key":"` + key + `","kind":"video","channels":[{"channel":"` + channel + `"}]}`
	if label != "" {
		body = `{"key":"` + key + `","kind":"video","label":"` + label + `","channels":[{"channel":"` + channel + `"}]}`
	}
	if _, err := aicSave(e.cfg, "", true, json.RawMessage(body)); err != nil {
		t.Fatal(err)
	}
}

func TestAIChannelService_CheckDelete(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		key      string
		setup    func(t *testing.T, e *achEnv)
		wantCode int
		kinds    []string
	}{
		{"渠道不存在返回 50011", "ghost", nil, errcode.ErrChannelNotFound.Code, nil},
		{"没有引用：空数组", "kling-main", nil, 0, []string{}},
		{"被模型引用：channel_models", "kling-main", func(t *testing.T, e *achEnv) {
			delSaveModel(t, e, "m1", "可灵 2", "kling-main")
		}, 0, []string{model.BlockerChannelModels}},
		{"被进行中的任务引用：active_tasks", "kling-main", func(_ *testing.T, e *achEnv) {
			e.repo.ChannelTaskRefs["kling-main"] = 2
		}, 0, []string{model.BlockerActiveTasks}},
		{"两者都有：模型在前，任务在后", "kling-main", func(t *testing.T, e *achEnv) {
			delSaveModel(t, e, "m1", "", "kling-main")
			e.repo.ChannelTaskRefs["kling-main"] = 1
		}, 0, []string{model.BlockerChannelModels, model.BlockerActiveTasks}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := delChannelEnv(t)
			if tt.setup != nil {
				tt.setup(t, e)
			}
			res, err := e.svc.CheckDelete(ctx, tt.key)
			aicWantCode(t, err, tt.wantCode)
			if tt.wantCode == 0 {
				delWantBlockers(t, res, tt.kinds...)
			}
		})
	}

	t.Run("refs 列出模型 key 与展示名，没有 label 用 key；文案带数量", func(t *testing.T) {
		e := delChannelEnv(t)
		delSaveModel(t, e, "m1", "可灵 2", "kling-main")
		delSaveModel(t, e, "m2", "", "kling-main")
		delSaveModel(t, e, "m3", "别的", "other")
		e.repo.ChannelTaskRefs["kling-main"] = 3
		res, err := e.svc.CheckDelete(ctx, "kling-main")
		if err != nil {
			t.Fatal(err)
		}
		refs := res.Blockers[0].Refs
		if len(refs) != 2 || refs[0] != (model.DeleteRef{Key: "m1", Name: "可灵 2"}) || refs[1] != (model.DeleteRef{Key: "m2", Name: "m2"}) {
			t.Fatalf("refs 不符合预期：%+v", refs)
		}
		if res.Blockers[0].Message != "2 个模型在用这个渠道" || res.Blockers[1].Message != "3 个进行中的任务在用这个渠道" {
			t.Fatalf("文案不符合预期：%+v", res.Blockers)
		}
	})
}

func TestAIChannelService_Delete(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		key      string
		setup    func(t *testing.T, e *achEnv)
		wantCode int
		wantMsg  string
	}{
		{"渠道不存在返回 50011", "ghost", nil, errcode.ErrChannelNotFound.Code, ""},
		{"被模型引用返回 50016，文案带数量", "kling-main", func(t *testing.T, e *achEnv) {
			delSaveModel(t, e, "m1", "", "kling-main")
		}, errcode.ErrChannelInUse.Code, "被 1 个模型、0 个进行中的任务使用"},
		{"被进行中的任务引用返回 50016", "kling-main", func(_ *testing.T, e *achEnv) {
			e.repo.ChannelTaskRefs["kling-main"] = 4
		}, errcode.ErrChannelInUse.Code, "0 个模型、4 个进行中的任务"},
		{"预检之后被并发引用：事务内复查返回 50016", "kling-main", func(_ *testing.T, e *achEnv) {
			e.repo.DeleteInUse = true
		}, errcode.ErrChannelInUse.Code, ""},
		{"没有引用：删除成功", "kling-main", nil, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := delChannelEnv(t)
			if tt.setup != nil {
				tt.setup(t, e)
			}
			auditsBefore, notifyBefore := len(e.repo.Audits), len(e.notifier.Reasons)
			err := e.svc.Delete(ctx, 8, tt.key)
			be := adminWantCodeErr(t, err, tt.wantCode)
			if tt.wantCode != 0 {
				if tt.wantMsg != "" && !strings.Contains(be.Msg, tt.wantMsg) {
					t.Fatalf("文案应包含 %q：%q", tt.wantMsg, be.Msg)
				}
				if len(e.repo.Audits) != auditsBefore || len(e.notifier.Reasons) != notifyBefore {
					t.Fatal("删除失败不应写审计或刷新 Registry")
				}
				if _, ok := e.repo.Channels["kling-main"]; !ok && tt.key == "kling-main" {
					t.Fatal("删除失败时渠道应保留")
				}
				return
			}
			if _, ok := e.repo.Channels["kling-main"]; ok {
				t.Fatal("渠道应已删除")
			}
			if _, ok := e.repo.Secrets[model.ChannelSecretName("kling-main")]; ok {
				t.Fatal("渠道 Key 应一并删除")
			}
			// 本实例缓存里的明文也要清掉
			if _, err := e.cfg.Get(ctx, model.ChannelSecretName("kling-main")); !errors.Is(err, ErrAISecretNotSet) {
				t.Fatalf("删除后不应还能取到 Key：%v", err)
			}
			last := e.repo.Audits[len(e.repo.Audits)-1]
			if last.Action != model.AuditChannelDelete || last.ActorID != 8 || last.TargetKey != "kling-main" || last.TargetType != model.AuditTargetChannel {
				t.Fatalf("审计不符合预期：%+v", last)
			}
			if len(e.notifier.Reasons) != notifyBefore+1 {
				t.Fatalf("删除后应刷新 Registry：%v", e.notifier.Reasons)
			}
		})
	}

	t.Run("已删除模型的引用不算", func(t *testing.T) {
		e := delChannelEnv(t)
		delSaveModel(t, e, "m1", "", "kling-main")
		if err := e.cfg.SetModelEnabled(ctx, "m1", false, 1); err != nil {
			t.Fatal(err)
		}
		if err := e.cfg.DeleteModel(ctx, "m1", 1); err != nil {
			t.Fatal(err)
		}
		adminWantCode(t, e.svc.Delete(ctx, 8, "kling-main"), 0)
	})
}

// ---------------------------------------------------------------------------
// 插件
// ---------------------------------------------------------------------------

// delPluginEnv 准备：上传插件 kling（两个版本）与内置插件 core。
func delPluginEnv(t *testing.T) (*aplEnv, uint64, uint64) {
	t.Helper()
	e := newAplEnv()
	v1 := adminSeedVersion(t, e.repo, adminMetaJSON("kling", "1.0.0"), model.PluginSourceUploaded, true)
	v2 := adminSeedVersion(t, e.repo, adminMetaJSON("kling", "1.1.0"), model.PluginSourceUploaded, true)
	adminSeedVersion(t, e.repo, adminMetaJSON("core", "1.0.0"), model.PluginSourceBuiltin, true)
	return e, v1, v2
}

func TestAIPluginService_CheckDelete(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		key      string
		setup    func(e *aplEnv, v1, v2 uint64)
		wantCode int
		kinds    []string
	}{
		{"插件不存在返回 50001", "ghost", nil, errcode.ErrPluginNotFound.Code, nil},
		{"没有引用：空数组", "kling", nil, 0, []string{}},
		{"内置插件：builtin_plugin", "core", nil, 0, []string{model.BlockerBuiltinPlugin}},
		{"任一版本被渠道固定：plugin_channels", "kling", func(e *aplEnv, _, v2 uint64) {
			e.repo.Channels["c2"] = &model.AIChannel{Key: "c2", Name: "渠道二", PluginKey: "kling", PluginVersionID: v2}
		}, 0, []string{model.BlockerPluginChannels}},
		{"任一版本被进行中的任务引用：active_tasks", "kling", func(e *aplEnv, v1, _ uint64) {
			e.repo.ActiveTaskRefs[v1] = 2
		}, 0, []string{model.BlockerActiveTasks}},
		{"全都有：内置 → 渠道 → 任务", "core", func(e *aplEnv, _, _ uint64) {
			id := e.repo.Versions
			for vid, v := range id {
				if v.PluginKey == "core" {
					e.repo.Channels["cc"] = &model.AIChannel{Key: "cc", Name: "核心", PluginKey: "core", PluginVersionID: vid}
					e.repo.ActiveTaskRefs[vid] = 1
				}
			}
		}, 0, []string{model.BlockerBuiltinPlugin, model.BlockerPluginChannels, model.BlockerActiveTasks}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, v1, v2 := delPluginEnv(t)
			if tt.setup != nil {
				tt.setup(e, v1, v2)
			}
			res, err := e.svc.CheckDelete(ctx, tt.key)
			adminWantCode(t, err, tt.wantCode)
			if tt.wantCode == 0 {
				delWantBlockers(t, res, tt.kinds...)
			}
		})
	}

	t.Run("refs 列出渠道 key 与名称，文案带数量", func(t *testing.T) {
		e, v1, v2 := delPluginEnv(t)
		e.repo.Channels["c2"] = &model.AIChannel{Key: "c2", Name: "渠道二", PluginKey: "kling", PluginVersionID: v2}
		e.repo.Channels["c1"] = &model.AIChannel{Key: "c1", Name: "渠道一", PluginKey: "kling", PluginVersionID: v1}
		e.repo.ActiveTaskRefs[v1], e.repo.ActiveTaskRefs[v2] = 2, 3
		res, err := e.svc.CheckDelete(ctx, "kling")
		if err != nil {
			t.Fatal(err)
		}
		refs := res.Blockers[0].Refs
		if len(refs) != 2 || refs[0] != (model.DeleteRef{Key: "c1", Name: "渠道一"}) || refs[1] != (model.DeleteRef{Key: "c2", Name: "渠道二"}) {
			t.Fatalf("refs 不符合预期：%+v", refs)
		}
		if res.Blockers[0].Message != "2 个渠道在用这个插件" || res.Blockers[1].Message != "5 个进行中的任务在用这个插件" {
			t.Fatalf("文案不符合预期：%+v", res.Blockers)
		}
	})
}

func TestAIPluginService_Delete(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		key      string
		setup    func(e *aplEnv, v1, v2 uint64)
		wantCode int
		wantMsg  string
	}{
		{"插件不存在返回 50001", "ghost", nil, errcode.ErrPluginNotFound.Code, ""},
		{"内置插件返回 50008", "core", nil, errcode.ErrPluginBuiltinDel.Code, "内置插件不能删除，只能停用"},
		{"版本被渠道固定返回 50005，文案带数量", "kling", func(e *aplEnv, v1, _ uint64) {
			e.repo.Channels["c1"] = &model.AIChannel{Key: "c1", Name: "渠道一", PluginKey: "kling", PluginVersionID: v1}
		}, errcode.ErrPluginInUse.Code, "被 1 个渠道、0 个进行中的任务使用"},
		{"版本被进行中的任务引用返回 50005", "kling", func(e *aplEnv, _, v2 uint64) {
			e.repo.ActiveTaskRefs[v2] = 6
		}, errcode.ErrPluginInUse.Code, "0 个渠道、6 个进行中的任务"},
		{"预检之后被并发引用：事务内复查返回 50005", "kling", func(e *aplEnv, _, _ uint64) {
			e.repo.DeleteInUse = true
		}, errcode.ErrPluginInUse.Code, ""},
		{"没有引用：删除全部版本", "kling", nil, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, v1, v2 := delPluginEnv(t)
			if tt.setup != nil {
				tt.setup(e, v1, v2)
			}
			auditsBefore := len(e.repo.Audits)
			err := e.svc.Delete(ctx, 9, tt.key)
			be := adminWantCodeErr(t, err, tt.wantCode)
			if tt.wantCode != 0 {
				if tt.wantMsg != "" && !strings.Contains(be.Msg, tt.wantMsg) {
					t.Fatalf("文案应包含 %q：%q", tt.wantMsg, be.Msg)
				}
				if len(e.repo.Audits) != auditsBefore {
					t.Fatal("删除失败不应写审计")
				}
				return
			}
			if _, ok := e.repo.Plugins["kling"]; ok {
				t.Fatal("插件行应已删除")
			}
			if _, ok := e.repo.Versions[v1]; ok {
				t.Fatal("版本应已全部删除")
			}
			if _, ok := e.repo.Plugins["core"]; !ok {
				t.Fatal("其他插件不应受影响")
			}
			last := e.repo.Audits[len(e.repo.Audits)-1]
			if last.Action != model.AuditPluginDelete || last.TargetType != model.AuditTargetPlugin || last.ActorID != 9 ||
				!strings.Contains(string(last.DetailJSON), "1.0.0") || !strings.Contains(string(last.DetailJSON), "1.1.0") {
				t.Fatalf("审计不符合预期：%+v %s", last, last.DetailJSON)
			}
		})
	}
}
