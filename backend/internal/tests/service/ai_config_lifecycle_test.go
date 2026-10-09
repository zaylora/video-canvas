package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/modelcfg"
	. "video-canvas/internal/service"
)

func TestAIConfigService_ListAndGet(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := aicNewSvc()
	aicSeedChannel(t, svc, repo, "c1", true)
	aicSeedChannel(t, svc, repo, "c2", true)
	aicEnableModel(t, svc, "m1", "c1")
	// m1 改了展示名与渠道再保存：已启用的模型保存即生效，列表显示新内容
	renamed := strings.Replace(string(aicModelBody("m1", "c2", `"deadline":"12m"`)), "模型-m1", "改名后", 1)
	if _, err := aicSave(svc, "m1", false, json.RawMessage(renamed)); err != nil {
		t.Fatal(err)
	}
	if _, err := aicSave(svc, "", true, aicModelBody("m2", "c2", "")); err != nil {
		t.Fatal(err)
	}

	t.Run("模型列表带启用状态、label 与 channel", func(t *testing.T) {
		items, err := svc.ListConfigs(ctx)
		aicWantCode(t, err, 0)
		if len(items) != 2 {
			t.Fatalf("期望 2 项，实际 %d", len(items))
		}
		byKey := map[string]ConfigListItem{}
		for _, it := range items {
			byKey[it.Key] = it
		}
		m1, m2 := byKey["m1"], byKey["m2"]
		if !m1.Enabled || m1.Label != "改名后" || m1.Channel != "c2" {
			t.Fatalf("m1 应显示保存后的 label / channel：%+v", m1)
		}
		if m2.Enabled || m2.Label != "模型-m2" || m2.Channel != "c2" {
			t.Fatalf("m2 保存后默认未启用，label / channel 取自配置：%+v", m2)
		}
		// 列表不含正文与版本信息
		b, _ := json.Marshal(items)
		for _, bad := range []string{"body", "capabilities", "revision", "draft"} {
			if strings.Contains(string(b), bad) {
				t.Fatalf("列表不应包含 %q：%s", bad, b)
			}
		}
	})
	t.Run("正文损坏时 label / channel 留空，不影响其他字段", func(t *testing.T) {
		repo.Revs[len(repo.Revs)-1].BodyJSON = model.JSONText(`{oops`)
		items, err := svc.ListConfigs(ctx)
		aicWantCode(t, err, 0)
		for _, it := range items {
			if it.Key == "m2" && (it.Label != "" || it.Channel != "" || it.Kind != "video") {
				t.Fatalf("m2 不符合预期：%+v", it)
			}
		}
	})
	t.Run("详情带配置正文与启用状态", func(t *testing.T) {
		d, err := svc.GetConfig(ctx, "m1")
		aicWantCode(t, err, 0)
		if !d.Enabled || len(d.Body) == 0 || !strings.Contains(string(d.Body), "改名后") {
			t.Fatalf("详情不符合预期：%+v", d)
		}
		b, _ := json.Marshal(d)
		if strings.Contains(string(b), "revision") || strings.Contains(string(b), "draft") || strings.Contains(string(b), "published") {
			t.Fatalf("详情不应带版本信息：%s", b)
		}
	})
	t.Run("不存在返回 404", func(t *testing.T) {
		_, err := svc.GetConfig(ctx, "none")
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
	})
	t.Run("没有任何模型时列表是空数组", func(t *testing.T) {
		empty, _, _ := aicNewSvc()
		items, err := empty.ListConfigs(ctx)
		if err != nil || items == nil || len(items) != 0 {
			t.Fatalf("结果不符合预期：%#v %v", items, err)
		}
	})
}

func TestAIConfigService_ModelEnabledAndSort(t *testing.T) {
	ctx := context.Background()
	t.Run("启用已保存的模型并热生效", func(t *testing.T) {
		svc, repo, inv := aicNewSvc()
		aicSeedChannel(t, svc, repo, "c1", true)
		aicEnableModel(t, svc, "m1", "c1")
		aicWantCode(t, svc.SetModelEnabled(ctx, "m1", false, 1), 0)
		before := len(inv.Reasons)
		aicWantCode(t, svc.SetModelEnabled(ctx, "m1", true, 1), 0)
		if !repo.Models["m1"].Enabled || len(inv.Reasons) != before+1 {
			t.Fatal("上架未生效或未广播")
		}
		list, _ := svc.ListModels(ctx, "")
		if len(list) != 1 {
			t.Fatalf("上架后应出现在清单里：%v", list)
		}
		aicWantCode(t, svc.SetModelEnabled(ctx, "m1", false, 1), 0)
		if list, _ := svc.ListModels(ctx, ""); len(list) != 0 {
			t.Fatalf("下架后应立即从清单消失：%v", list)
		}
	})
	t.Run("配置有校验问题的模型不能启用", func(t *testing.T) {
		svc, repo, _ := aicNewSvc()
		aicSeedChannel(t, svc, repo, "c1", true)
		_, _ = aicSave(svc, "", true, aicModelBody("m1", "c1", `"bad":true`))
		aicWantCode(t, svc.SetModelEnabled(ctx, "m1", true, 1), errcode.ErrConfigInvalid.Code)
	})
	t.Run("保存后默认未启用，用户看不到，启用后才可见", func(t *testing.T) {
		svc, repo, _ := aicNewSvc()
		aicSeedChannel(t, svc, repo, "c1", true)
		_, _ = aicSave(svc, "", true, aicModelBody("m1", "c1", ""))
		if list, _ := svc.ListModels(ctx, ""); len(list) != 0 {
			t.Fatalf("保存后未启用，不应出现在用户清单里：%v", list)
		}
		aicWantCode(t, svc.SetModelEnabled(ctx, "m1", true, 1), 0)
		if list, _ := svc.ListModels(ctx, ""); len(list) != 1 {
			t.Fatalf("启用后应出现在清单里：%v", list)
		}
	})
	t.Run("模型不存在", func(t *testing.T) {
		svc, _, _ := aicNewSvc()
		aicWantCode(t, svc.SetModelEnabled(ctx, "none", false, 1), errcode.ErrConfigNotFound.Code)
		aicWantCode(t, svc.SetModelSort(ctx, "none", 1, 1), errcode.ErrConfigNotFound.Code)
	})
	t.Run("修改排序立即生效", func(t *testing.T) {
		svc, repo, _ := aicNewSvc()
		aicSeedChannel(t, svc, repo, "c1", true)
		aicEnableModel(t, svc, "a", "c1")
		aicEnableModel(t, svc, "b", "c1")
		list, _ := svc.ListModels(ctx, "")
		if len(list) != 2 || list[0].Key != "a" {
			t.Fatalf("同 sort 按 key 排序：%v", list)
		}
		aicWantCode(t, svc.SetModelSort(ctx, "b", 1, 1), 0)
		list, _ = svc.ListModels(ctx, "")
		if list[0].Key != "b" {
			t.Fatalf("b 应排到最前：%v", list)
		}
	})
}

func TestAIConfigService_JSONSchema(t *testing.T) {
	svc, _, _ := aicNewSvc()
	b, err := svc.JSONSchema(model.ConfigTargetModel)
	aicWantCode(t, err, 0)
	if !json.Valid(b) || !strings.Contains(string(b), "channels") {
		t.Fatalf("应返回带 channels 的合法 JSON Schema：%s", b)
	}
	// 平台协议已改为插件，没有 provider 的 schema
	for _, target := range []string{"provider", "x", ""} {
		_, err = svc.JSONSchema(target)
		aicWantCode(t, err, errcode.ErrInvalidParams.Code)
	}
}

func TestAIConfigService_NotifyChanged(t *testing.T) {
	ctx := context.Background()
	svc, repo, inv := aicNewSvc()
	aicSeedChannel(t, svc, repo, "c1", true)
	aicEnableModel(t, svc, "m1", "c1")
	// 插件 / 渠道服务在改动后调用它：停用渠道后清单里立刻不再有这个模型，并广播给其他实例
	if list, _ := svc.ListModels(ctx, ""); len(list) != 1 {
		t.Fatalf("前置：应有 1 个模型：%v", list)
	}
	repo.Channels["c1"].Enabled = false
	before := len(inv.Reasons)
	svc.NotifyChanged(ctx, "channel update c1")
	if list, _ := svc.ListModels(ctx, ""); len(list) != 0 {
		t.Fatalf("渠道停用并通知后清单应立即为空：%v", list)
	}
	if len(inv.Reasons) != before+1 || inv.Reasons[before] != "channel update c1" {
		t.Fatalf("应广播失效：%v", inv.Reasons)
	}
}

func TestAIFormatIssues(t *testing.T) {
	var issues []modelcfg.Issue
	for i := 0; i < 12; i++ {
		issues = append(issues, modelcfg.Issue{Path: fmt.Sprintf("p%d", i), Message: "m"})
	}
	got := AIFormatIssues(issues)
	if !strings.Contains(got, "p0：m") || !strings.Contains(got, "共 12 条") || strings.Contains(got, "p11") {
		t.Fatalf("最多列 10 条并提示总数：%s", got)
	}
	if AIFormatIssues([]modelcfg.Issue{{Message: "只有消息"}}) != "只有消息" {
		t.Fatal("没有 path 时只输出消息")
	}
}
