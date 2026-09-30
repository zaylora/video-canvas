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
	"video-canvas/internal/service/aiconfigfake"
)

func TestAIConfigService_Rollback(t *testing.T) {
	ctx := context.Background()

	// 准备：模型 m1 依次发布 v1、v2（正文用 deadline 区分），另有 v3 草稿
	prepare := func(t *testing.T) (*AIConfigService, *aiconfigfake.MemRepo, *aiconfigfake.Invalidator, []uint64) {
		svc, repo, inv := aicNewSvc()
		aicSeedChannel(t, svc, repo, "c1", true)
		var ids []uint64
		for i := 0; i < 2; i++ {
			body := aicModelBody("m1", "c1", fmt.Sprintf(`"deadline":"%dm"`, 10*(i+1)))
			res, err := aicSave(svc, "m1", i == 0, body)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Publish(ctx, "m1", 1); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, res.Revision.ID)
		}
		res, err := aicSave(svc, "m1", false, aicModelBody("m1", "c1", `"deadline":"30m"`))
		if err != nil {
			t.Fatal(err)
		}
		return svc, repo, inv, append(ids, res.Revision.ID)
	}

	t.Run("回滚到历史版本成功并广播", func(t *testing.T) {
		svc, repo, inv, ids := prepare(t)
		before := len(inv.Reasons)
		rev, err := svc.Rollback(ctx, "m1", ids[0], 1)
		aicWantCode(t, err, 0)
		if rev.ID != ids[0] || rev.Status != model.RevisionPublished {
			t.Fatalf("回滚结果不符合预期：%+v", rev)
		}
		pub, _ := repo.GetPublishedRevision(ctx, model.ConfigTargetModel, "m1")
		if pub.ID != ids[0] {
			t.Fatalf("指针应回到 v1")
		}
		cur, _ := repo.GetRevision(ctx, ids[1])
		if cur.Status != model.RevisionArchived {
			t.Fatalf("回滚前的版本应归档：%s", cur.Status)
		}
		if _, err := repo.GetDraft(ctx, model.ConfigTargetModel, "m1"); err != nil {
			t.Fatalf("回滚不应影响草稿：%v", err)
		}
		if len(inv.Reasons) != before+1 {
			t.Fatal("回滚应广播失效")
		}
	})

	t.Run("回滚到当前发布版本", func(t *testing.T) {
		svc, _, _, ids := prepare(t)
		_, err := svc.Rollback(ctx, "m1", ids[1], 1)
		aicWantCode(t, err, errcode.ErrConfigInvalid.Code)
	})
	t.Run("回滚到草稿", func(t *testing.T) {
		svc, _, _, ids := prepare(t)
		_, err := svc.Rollback(ctx, "m1", ids[2], 1)
		aicWantCode(t, err, errcode.ErrConfigInvalid.Code)
	})
	t.Run("revision 不存在", func(t *testing.T) {
		svc, _, _, _ := prepare(t)
		_, err := svc.Rollback(ctx, "m1", 9999, 1)
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
	})
	t.Run("revision 属于其他模型按不存在处理", func(t *testing.T) {
		svc, _, _, ids := prepare(t)
		_, err := svc.Rollback(ctx, "other", ids[0], 1)
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
	})
	t.Run("回滚目标的校验不通过", func(t *testing.T) {
		svc, repo, _, ids := prepare(t)
		// 让 v1 的正文变得不合法（模拟当年没校验就被归档的旧草稿）
		repo.Revs[0].BodyJSON = model.JSONText(aicModelBody("m1", "c1", `"bad":true`))
		_, err := svc.Rollback(ctx, "m1", ids[0], 1)
		aicWantCode(t, err, errcode.ErrConfigInvalid.Code)
	})
	t.Run("回滚目标绑定的渠道 Key 已不存在（50015）", func(t *testing.T) {
		svc, repo, _, ids := prepare(t)
		delete(repo.Secrets, model.ChannelSecretName("c1"))
		_, err := svc.Rollback(ctx, "m1", ids[0], 1)
		aicWantCode(t, err, errcode.ErrChannelSecretUnset.Code)
	})
	t.Run("回滚目标绑定的渠道已停用（50014）", func(t *testing.T) {
		svc, repo, _, ids := prepare(t)
		repo.Channels["c1"].Enabled = false
		_, err := svc.Rollback(ctx, "m1", ids[0], 1)
		aicWantCode(t, err, errcode.ErrChannelDisabled.Code)
	})
}

func TestAIConfigService_ListAndGet(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := aicNewSvc()
	aicSeedChannel(t, svc, repo, "c1", true)
	aicSeedChannel(t, svc, repo, "c2", true)
	aicPublishModel(t, svc, "m1", "c1")
	// m1 再保存一个未发布的草稿：换了展示名与渠道，列表应仍显示线上正在用的
	draft := strings.Replace(string(aicModelBody("m1", "c2", `"deadline":"12m"`)), "模型-m1", "改名后的草稿", 1)
	if _, err := aicSave(svc, "m1", false, json.RawMessage(draft)); err != nil {
		t.Fatal(err)
	}
	if _, err := aicSave(svc, "", true, aicModelBody("m2", "c2", "")); err != nil {
		t.Fatal(err)
	}

	t.Run("模型列表汇总草稿与发布状态，并带 label 与 channel", func(t *testing.T) {
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
		if m1.PublishedRevisionNo == nil || *m1.PublishedRevisionNo != 1 || m1.DraftRevisionNo == nil || *m1.DraftRevisionNo != 2 || !m1.HasUnpublishedDraft {
			t.Fatalf("m1 汇总不符合预期：%+v", m1)
		}
		if m1.Label != "模型-m1" || m1.Channel != "c1" {
			t.Fatalf("m1 应显示已发布版本的 label / channel：%+v", m1)
		}
		if m2.PublishedRevisionID != nil || m2.PublishedRevisionNo != nil || !m2.Enabled {
			t.Fatalf("m2 汇总不符合预期：%+v", m2)
		}
		if m2.Label != "模型-m2" || m2.Channel != "c2" {
			t.Fatalf("没发布过的模型应显示最新草稿的 label / channel：%+v", m2)
		}
		// 列表不含正文
		b, _ := json.Marshal(items)
		if strings.Contains(string(b), "body_json") || strings.Contains(string(b), "input_schema") {
			t.Fatalf("列表不应包含正文：%s", b)
		}
	})
	t.Run("正文损坏时 label / channel 留空，不影响其他字段", func(t *testing.T) {
		repo.Revs[len(repo.Revs)-1].BodyJSON = model.JSONText(`{oops`)
		items, err := svc.ListConfigs(ctx)
		aicWantCode(t, err, 0)
		for _, it := range items {
			if it.Key == "m2" && (it.Label != "" || it.Channel != "" || it.DraftRevisionNo == nil) {
				t.Fatalf("m2 不符合预期：%+v", it)
			}
		}
	})
	t.Run("详情包含草稿与已发布正文", func(t *testing.T) {
		d, err := svc.GetConfig(ctx, "m1")
		aicWantCode(t, err, 0)
		if d.Draft == nil || d.Published == nil || d.Draft.RevisionNo != 2 || d.Published.RevisionNo != 1 || len(d.Published.BodyJSON) == 0 {
			t.Fatalf("详情不符合预期：%+v", d)
		}
	})
	t.Run("只有草稿的详情 published 为空", func(t *testing.T) {
		d, err := svc.GetConfig(ctx, "m2")
		aicWantCode(t, err, 0)
		if d.Draft == nil || d.Published != nil {
			t.Fatalf("详情不符合预期：%+v", d)
		}
	})
	t.Run("不存在返回 404", func(t *testing.T) {
		_, err := svc.GetConfig(ctx, "none")
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
	})
	t.Run("历史版本列表", func(t *testing.T) {
		revs, err := svc.ListRevisions(ctx, "m1")
		aicWantCode(t, err, 0)
		if len(revs) != 2 || revs[0].RevisionNo != 2 || len(revs[0].BodyJSON) != 0 {
			t.Fatalf("历史列表不符合预期：%+v", revs)
		}
		_, err = svc.ListRevisions(ctx, "none")
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
	})
	t.Run("按 id 取历史版本正文并核对归属", func(t *testing.T) {
		revs, _ := svc.ListRevisions(ctx, "m1")
		r, err := svc.GetRevision(ctx, "m1", revs[1].ID)
		aicWantCode(t, err, 0)
		if len(r.BodyJSON) == 0 {
			t.Fatal("应返回正文")
		}
		_, err = svc.GetRevision(ctx, "m2", revs[1].ID)
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
	t.Run("上架已发布模型并热生效", func(t *testing.T) {
		svc, repo, inv := aicNewSvc()
		aicSeedChannel(t, svc, repo, "c1", true)
		aicPublishModel(t, svc, "m1", "c1")
		repo.Models["m1"].Enabled = false
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
	t.Run("未发布的模型不能上架", func(t *testing.T) {
		svc, repo, _ := aicNewSvc()
		aicSeedChannel(t, svc, repo, "c1", true)
		_, _ = aicSave(svc, "", true, aicModelBody("m1", "c1", ""))
		aicWantCode(t, svc.SetModelEnabled(ctx, "m1", true, 1), errcode.ErrConfigInvalid.Code)
	})
	t.Run("模型不存在", func(t *testing.T) {
		svc, _, _ := aicNewSvc()
		aicWantCode(t, svc.SetModelEnabled(ctx, "none", false, 1), errcode.ErrConfigNotFound.Code)
		aicWantCode(t, svc.SetModelSort(ctx, "none", 1, 1), errcode.ErrConfigNotFound.Code)
	})
	t.Run("修改排序立即生效", func(t *testing.T) {
		svc, repo, _ := aicNewSvc()
		aicSeedChannel(t, svc, repo, "c1", true)
		aicPublishModel(t, svc, "a", "c1")
		aicPublishModel(t, svc, "b", "c1")
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
	aicPublishModel(t, svc, "m1", "c1")
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
