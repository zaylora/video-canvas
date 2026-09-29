package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	. "video-canvas/internal/service"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/repository"
	"video-canvas/internal/service/aiconfigfake"
)

// aicNewSvc 组装一个使用内存 fake 的配置服务，带主密钥。
func aicNewSvc() (*AIConfigService, *aiconfigfake.MemRepo, *aiconfigfake.Invalidator) {
	repo := aiconfigfake.NewMemRepo()
	svc := NewAIConfigService(repo, &aiconfigfake.Validator{}, "test-master-key")
	inv := &aiconfigfake.Invalidator{}
	svc.SetInvalidator(inv)
	return svc, repo, inv
}

// aicProviderBody 生成平台配置正文；secret 为空表示 auth.type=none。
func aicProviderBody(key, secret string, extra string) json.RawMessage {
	auth := `{"type":"none"}`
	if secret != "" {
		auth = fmt.Sprintf(`{"type":"bearer","secret":%q}`, secret)
	}
	if extra != "" {
		extra = "," + extra
	}
	return json.RawMessage(fmt.Sprintf(`{"dsl":1,"key":%q,"name":"平台-%s","base_url":"https://example.com","allowed_hosts":["example.com"],"auth":%s%s}`, key, key, auth, extra))
}

// aicModelBody 生成模型配置正文。
func aicModelBody(key, provider string, extra string) json.RawMessage {
	if extra != "" {
		extra = "," + extra
	}
	return json.RawMessage(fmt.Sprintf(`{"key":%q,"kind":"video","provider":%q,"label":"模型-%s","hint":"提示","credits":5,"enabled":true,"sort":10,`+
		`"params":{"webappId":"123"},"input_schema":{"prompt":{"type":"text","label":"提示词","required":true}},"mapping":{"secretish":"x"}%s}`, key, provider, key, extra))
}

// aicWantCode 断言错误是指定业务错误码；wantCode=0 表示期望成功。
func aicWantCode(t *testing.T, err error, wantCode int) {
	t.Helper()
	if wantCode == 0 {
		if err != nil {
			t.Fatalf("期望成功，实际返回错误：%v", err)
		}
		return
	}
	var e *errcode.Error
	if !errors.As(err, &e) || e.Code != wantCode {
		t.Fatalf("期望错误码 %d，实际：%v", wantCode, err)
	}
}

// aicPublishProvider 保存并发布一个平台（先设置好凭证）。
func aicPublishProvider(t *testing.T, svc *AIConfigService, key, secret string) {
	t.Helper()
	ctx := context.Background()
	if secret != "" {
		if err := svc.SetSecret(ctx, secret, "sk-"+secret, 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.SaveDraft(ctx, model.ConfigTargetProvider, "", true, aicProviderBody(key, secret, ""), "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, model.ConfigTargetProvider, key, 1); err != nil {
		t.Fatal(err)
	}
}

// aicPublishModel 保存并发布一个模型。
func aicPublishModel(t *testing.T, svc *AIConfigService, key, provider string) {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.SaveDraft(ctx, model.ConfigTargetModel, "", true, aicModelBody(key, provider, ""), "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, model.ConfigTargetModel, key, 1); err != nil {
		t.Fatal(err)
	}
}

func TestAIConfigService_SaveDraft(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		key      string
		create   bool
		body     json.RawMessage
		setup    func(svc *AIConfigService)
		wantCode int
		wantIss  int
	}{
		{"新建平台草稿成功", model.ConfigTargetProvider, "", true, aicProviderBody("p1", "s1", ""), nil, 0, 0},
		{"新建模型草稿成功", model.ConfigTargetModel, "", true, aicModelBody("m1", "p1", ""), nil, 0, 0},
		{"key 已存在不能再新建", model.ConfigTargetProvider, "", true, aicProviderBody("p1", "s1", ""), func(s *AIConfigService) {
			_, _ = s.SaveDraft(context.Background(), model.ConfigTargetProvider, "", true, aicProviderBody("p1", "s1", ""), "", 1)
		}, errcode.ErrConfigInvalid.Code, 0},
		{"更新不存在的配置返回 404", model.ConfigTargetProvider, "nope", false, aicProviderBody("nope", "s1", ""), nil, errcode.ErrConfigNotFound.Code, 0},
		{"路径 key 与正文不一致", model.ConfigTargetProvider, "p2", false, aicProviderBody("p1", "s1", ""), nil, errcode.ErrInvalidParams.Code, 0},
		{"正文不是 JSON 对象", model.ConfigTargetProvider, "", true, json.RawMessage(`[1,2]`), nil, errcode.ErrInvalidParams.Code, 0},
		{"正文缺少 key", model.ConfigTargetProvider, "", true, json.RawMessage(`{"name":"x"}`), nil, errcode.ErrInvalidParams.Code, 0},
		{"key 含非法字符", model.ConfigTargetProvider, "", true, json.RawMessage(`{"key":"a/b"}`), nil, errcode.ErrInvalidParams.Code, 0},
		{"平台 key 超过 64 字符", model.ConfigTargetProvider, "", true, aicProviderBody(strings.Repeat("a", 65), "", ""), nil, errcode.ErrInvalidParams.Code, 0},
		{"target 非法", "other", "", true, aicProviderBody("p1", "", ""), nil, errcode.ErrInvalidParams.Code, 0},
		{"校验有问题时仍保存并返回 Issue", model.ConfigTargetProvider, "", true, aicProviderBody("p1", "s1", `"bad":true`), nil, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := aicNewSvc()
			if tt.setup != nil {
				tt.setup(svc)
			}
			res, err := svc.SaveDraft(context.Background(), tt.target, tt.key, tt.create, tt.body, "备注", 9)
			aicWantCode(t, err, tt.wantCode)
			if tt.wantCode != 0 {
				return
			}
			if len(res.Issues) != tt.wantIss || res.Issues == nil {
				t.Fatalf("Issue 数量期望 %d（且非 nil），实际 %v", tt.wantIss, res.Issues)
			}
			if res.Revision.Status != model.RevisionDraft || res.Revision.CreatedBy != 9 || res.Revision.Note != "备注" {
				t.Fatalf("revision 不符合预期：%+v", res.Revision)
			}
			if tt.wantIss > 0 {
				// 有问题也已经落库
				if _, err := repo.GetDraft(context.Background(), tt.target, res.Revision.TargetKey); err != nil {
					t.Fatalf("草稿应已保存：%v", err)
				}
			}
		})
	}
}

func TestAIConfigService_SaveDraft_Revisions(t *testing.T) {
	svc, repo, _ := aicNewSvc()
	ctx := context.Background()
	if _, err := svc.SaveDraft(ctx, model.ConfigTargetModel, "", true, aicModelBody("m1", "p1", ""), "", 1); err != nil {
		t.Fatal(err)
	}
	// 上下架状态由接口独占：先改 enabled，再保存草稿不应被正文覆盖
	repo.Models["m1"].Enabled = false
	repo.Models["m1"].Sort = 77
	res, err := svc.SaveDraft(ctx, model.ConfigTargetModel, "m1", false, aicModelBody("m1", "p2", ""), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Revision.RevisionNo != 2 {
		t.Fatalf("revision_no 期望 2，实际 %d", res.Revision.RevisionNo)
	}
	if repo.Models["m1"].Enabled || repo.Models["m1"].Sort != 77 {
		t.Fatalf("已存在的指针行不应被正文覆盖 enabled/sort：%+v", repo.Models["m1"])
	}
	if repo.Models["m1"].ProviderKey != "p2" {
		t.Fatalf("provider 应与正文同步：%+v", repo.Models["m1"])
	}
	drafts := 0
	for _, r := range repo.Revs {
		if r.Status == model.RevisionDraft {
			drafts++
		}
	}
	if drafts != 1 {
		t.Fatalf("同一目标只应保留 1 个草稿，实际 %d", drafts)
	}
}

func TestAIConfigService_NewModelInitialValues(t *testing.T) {
	svc, repo, _ := aicNewSvc()
	if _, err := svc.SaveDraft(context.Background(), model.ConfigTargetModel, "", true, aicModelBody("m1", "p1", ""), "", 1); err != nil {
		t.Fatal(err)
	}
	m := repo.Models["m1"]
	if !m.Enabled || m.Sort != 10 || m.Kind != "video" || m.ProviderKey != "p1" {
		t.Fatalf("新建模型的指针行应取正文初始值：%+v", m)
	}
}

func TestAIConfigService_Validate(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := aicNewSvc()
	if _, err := svc.SaveDraft(ctx, model.ConfigTargetProvider, "", true, aicProviderBody("p1", "", `"bad":true`), "", 1); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		target    string
		key       string
		body      json.RawMessage
		wantCode  int
		wantValid bool
	}{
		{"校验已保存的草稿：有问题", model.ConfigTargetProvider, "p1", nil, 0, false},
		{"校验传入的正文：通过", model.ConfigTargetProvider, "p1", aicProviderBody("p1", "", ""), 0, true},
		{"传入正文的 key 与目标不一致", model.ConfigTargetProvider, "p1", aicProviderBody("other", "", ""), 0, false},
		{"传入正文不是对象", model.ConfigTargetProvider, "p1", json.RawMessage(`"x"`), 0, false},
		{"没有草稿", model.ConfigTargetProvider, "none", nil, errcode.ErrConfigNoDraft.Code, false},
		{"target 非法", "x", "p1", nil, errcode.ErrInvalidParams.Code, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := svc.Validate(ctx, tt.target, tt.key, tt.body)
			aicWantCode(t, err, tt.wantCode)
			if tt.wantCode != 0 {
				return
			}
			if res.Valid != tt.wantValid || res.Issues == nil || (!res.Valid && len(res.Issues) == 0) {
				t.Fatalf("校验结果不符合预期：%+v", res)
			}
		})
	}
}

func TestAIConfigService_Publish(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		setup    func(t *testing.T, svc *AIConfigService)
		target   string
		key      string
		wantCode int
	}{
		{"没有草稿", nil, model.ConfigTargetProvider, "p1", errcode.ErrConfigNoDraft.Code},
		{"草稿有校验问题不能发布", func(t *testing.T, s *AIConfigService) {
			_, _ = s.SaveDraft(ctx, model.ConfigTargetProvider, "", true, aicProviderBody("p1", "", `"bad":true`), "", 1)
		}, model.ConfigTargetProvider, "p1", errcode.ErrConfigInvalid.Code},
		{"平台引用的凭证未设置", func(t *testing.T, s *AIConfigService) {
			_, _ = s.SaveDraft(ctx, model.ConfigTargetProvider, "", true, aicProviderBody("p1", "s1", ""), "", 1)
		}, model.ConfigTargetProvider, "p1", errcode.ErrSecretNotSet.Code},
		{"平台凭证已设置，发布成功", func(t *testing.T, s *AIConfigService) {
			_ = s.SetSecret(ctx, "s1", "sk-1", 1)
			_, _ = s.SaveDraft(ctx, model.ConfigTargetProvider, "", true, aicProviderBody("p1", "s1", ""), "", 1)
		}, model.ConfigTargetProvider, "p1", 0},
		{"auth.type=none 不需要凭证", func(t *testing.T, s *AIConfigService) {
			_, _ = s.SaveDraft(ctx, model.ConfigTargetProvider, "", true, aicProviderBody("p1", "", ""), "", 1)
		}, model.ConfigTargetProvider, "p1", 0},
		{"模型引用的平台未发布", func(t *testing.T, s *AIConfigService) {
			_, _ = s.SaveDraft(ctx, model.ConfigTargetProvider, "", true, aicProviderBody("p1", "", ""), "", 1)
			_, _ = s.SaveDraft(ctx, model.ConfigTargetModel, "", true, aicModelBody("m1", "p1", ""), "", 1)
		}, model.ConfigTargetModel, "m1", errcode.ErrConfigInvalid.Code},
		{"模型引用的平台已发布，发布成功", func(t *testing.T, s *AIConfigService) {
			aicPublishProvider(t, s, "p1", "")
			_, _ = s.SaveDraft(ctx, model.ConfigTargetModel, "", true, aicModelBody("m1", "p1", ""), "", 1)
		}, model.ConfigTargetModel, "m1", 0},
		{"模型草稿有校验问题", func(t *testing.T, s *AIConfigService) {
			aicPublishProvider(t, s, "p1", "")
			_, _ = s.SaveDraft(ctx, model.ConfigTargetModel, "", true, aicModelBody("m1", "p1", `"bad":true`), "", 1)
		}, model.ConfigTargetModel, "m1", errcode.ErrConfigInvalid.Code},
		{"target 非法", nil, "x", "p1", errcode.ErrInvalidParams.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, inv := aicNewSvc()
			if tt.setup != nil {
				tt.setup(t, svc)
			}
			before := len(inv.Reasons)
			rev, err := svc.Publish(ctx, tt.target, tt.key, 1)
			aicWantCode(t, err, tt.wantCode)
			if tt.wantCode != 0 {
				if len(inv.Reasons) != before {
					t.Fatal("发布失败不应触发失效广播")
				}
				return
			}
			if rev.Status != model.RevisionPublished {
				t.Fatalf("状态应为 published：%+v", rev)
			}
			pub, err := repo.GetPublishedRevision(ctx, tt.target, tt.key)
			if err != nil || pub.ID != rev.ID {
				t.Fatalf("指针未指向新发布的版本：%v", err)
			}
			if len(inv.Reasons) != before+1 {
				t.Fatalf("发布成功应广播一次失效，实际 %v", inv.Reasons)
			}
		})
	}
}

// aicRacyRepo 在发布前偷偷保存一个新草稿，模拟“校验之后草稿被别人改掉”。
type aicRacyRepo struct{ *aiconfigfake.MemRepo }

func (r aicRacyRepo) PublishDraft(ctx context.Context, ptr repository.ConfigPointer, id uint64) (*model.AIConfigRevision, error) {
	_, _ = r.MemRepo.SaveDraft(ctx, repository.SaveDraftInput{Pointer: ptr, Body: aicProviderBody(ptr.Key, "", `"x":1`)})
	return r.MemRepo.PublishDraft(ctx, ptr, id)
}

func TestAIConfigService_Publish_DraftChangedConcurrently(t *testing.T) {
	ctx := context.Background()
	mem := aiconfigfake.NewMemRepo()
	svc := NewAIConfigService(aicRacyRepo{mem}, &aiconfigfake.Validator{}, "k")
	if _, err := svc.SaveDraft(ctx, model.ConfigTargetProvider, "", true, aicProviderBody("p1", "", ""), "", 1); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Publish(ctx, model.ConfigTargetProvider, "p1", 1)
	aicWantCode(t, err, errcode.ErrConfigNoDraft.Code)
	if _, err := mem.GetPublishedRevision(ctx, model.ConfigTargetProvider, "p1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal("冲突时不应发布任何版本")
	}
}

func TestAIConfigService_Rollback(t *testing.T) {
	ctx := context.Background()

	// 准备：平台 p1 依次发布 v1、v2，另有 v3 草稿
	prepare := func(t *testing.T) (*AIConfigService, *aiconfigfake.MemRepo, *aiconfigfake.Invalidator, []uint64) {
		svc, repo, inv := aicNewSvc()
		var ids []uint64
		for i := 0; i < 2; i++ {
			// 第一次新建（路径 key 为空），之后都是更新
			key := ""
			if i > 0 {
				key = "p1"
			}
			res, err := svc.SaveDraft(ctx, model.ConfigTargetProvider, key, i == 0, aicProviderBody("p1", "", fmt.Sprintf(`"v":%d`, i+1)), "", 1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Publish(ctx, model.ConfigTargetProvider, "p1", 1); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, res.Revision.ID)
		}
		res, err := svc.SaveDraft(ctx, model.ConfigTargetProvider, "p1", false, aicProviderBody("p1", "", `"v":3`), "", 1)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.Revision.ID)
		return svc, repo, inv, ids
	}

	t.Run("回滚到历史版本成功并广播", func(t *testing.T) {
		svc, repo, inv, ids := prepare(t)
		before := len(inv.Reasons)
		rev, err := svc.Rollback(ctx, model.ConfigTargetProvider, "p1", ids[0], 1)
		aicWantCode(t, err, 0)
		if rev.ID != ids[0] || rev.Status != model.RevisionPublished {
			t.Fatalf("回滚结果不符合预期：%+v", rev)
		}
		pub, _ := repo.GetPublishedRevision(ctx, model.ConfigTargetProvider, "p1")
		if pub.ID != ids[0] {
			t.Fatalf("指针应回到 v1")
		}
		cur, _ := repo.GetRevision(ctx, ids[1])
		if cur.Status != model.RevisionArchived {
			t.Fatalf("回滚前的版本应归档：%s", cur.Status)
		}
		if _, err := repo.GetDraft(ctx, model.ConfigTargetProvider, "p1"); err != nil {
			t.Fatalf("回滚不应影响草稿：%v", err)
		}
		if len(inv.Reasons) != before+1 {
			t.Fatal("回滚应广播失效")
		}
	})

	t.Run("回滚到当前发布版本", func(t *testing.T) {
		svc, _, _, ids := prepare(t)
		_, err := svc.Rollback(ctx, model.ConfigTargetProvider, "p1", ids[1], 1)
		aicWantCode(t, err, errcode.ErrConfigInvalid.Code)
	})
	t.Run("回滚到草稿", func(t *testing.T) {
		svc, _, _, ids := prepare(t)
		_, err := svc.Rollback(ctx, model.ConfigTargetProvider, "p1", ids[2], 1)
		aicWantCode(t, err, errcode.ErrConfigInvalid.Code)
	})
	t.Run("revision 不存在", func(t *testing.T) {
		svc, _, _, _ := prepare(t)
		_, err := svc.Rollback(ctx, model.ConfigTargetProvider, "p1", 9999, 1)
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
	})
	t.Run("revision 属于其他目标按不存在处理", func(t *testing.T) {
		svc, _, _, ids := prepare(t)
		_, err := svc.Rollback(ctx, model.ConfigTargetProvider, "other", ids[0], 1)
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
	})
	t.Run("回滚目标的校验不通过", func(t *testing.T) {
		svc, repo, _, ids := prepare(t)
		// 让 v1 的正文变成不合法（模拟当年没校验就被归档的旧草稿）
		repo.Revs[0].BodyJSON = model.JSONText(aicProviderBody("p1", "", `"bad":true`))
		_, err := svc.Rollback(ctx, model.ConfigTargetProvider, "p1", ids[0], 1)
		aicWantCode(t, err, errcode.ErrConfigInvalid.Code)
	})
	t.Run("回滚目标引用的凭证已不存在", func(t *testing.T) {
		svc, repo, _, ids := prepare(t)
		repo.Revs[0].BodyJSON = model.JSONText(aicProviderBody("p1", "gone", ""))
		_, err := svc.Rollback(ctx, model.ConfigTargetProvider, "p1", ids[0], 1)
		aicWantCode(t, err, errcode.ErrSecretNotSet.Code)
	})
}

func TestAIConfigService_ListAndGet(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := aicNewSvc()
	aicPublishProvider(t, svc, "p1", "")
	aicPublishModel(t, svc, "m1", "p1")
	// m1 再保存一个未发布的草稿
	if _, err := svc.SaveDraft(ctx, model.ConfigTargetModel, "m1", false, aicModelBody("m1", "p1", `"x":1`), "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveDraft(ctx, model.ConfigTargetModel, "", true, aicModelBody("m2", "p1", ""), "", 1); err != nil {
		t.Fatal(err)
	}

	t.Run("模型列表汇总草稿与发布状态", func(t *testing.T) {
		items, err := svc.ListConfigs(ctx, model.ConfigTargetModel)
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
		if m2.PublishedRevisionID != nil || m2.PublishedRevisionNo != nil || m2.Enabled == nil {
			t.Fatalf("m2 汇总不符合预期：%+v", m2)
		}
		// 列表不含正文
		b, _ := json.Marshal(items)
		if json.Valid(b) && aicContainsAny(string(b), "body_json", "mapping") {
			t.Fatalf("列表不应包含正文：%s", b)
		}
	})
	t.Run("平台列表", func(t *testing.T) {
		items, err := svc.ListConfigs(ctx, model.ConfigTargetProvider)
		aicWantCode(t, err, 0)
		if len(items) != 1 || items[0].Name != "平台-p1" || items[0].HasUnpublishedDraft {
			t.Fatalf("平台列表不符合预期：%+v", items)
		}
	})
	t.Run("详情包含草稿与已发布正文", func(t *testing.T) {
		d, err := svc.GetConfig(ctx, model.ConfigTargetModel, "m1")
		aicWantCode(t, err, 0)
		if d.Draft == nil || d.Published == nil || d.Draft.RevisionNo != 2 || d.Published.RevisionNo != 1 || len(d.Published.BodyJSON) == 0 {
			t.Fatalf("详情不符合预期：%+v", d)
		}
	})
	t.Run("只有草稿的详情 published 为空", func(t *testing.T) {
		d, err := svc.GetConfig(ctx, model.ConfigTargetModel, "m2")
		aicWantCode(t, err, 0)
		if d.Draft == nil || d.Published != nil {
			t.Fatalf("详情不符合预期：%+v", d)
		}
	})
	t.Run("不存在返回 404", func(t *testing.T) {
		_, err := svc.GetConfig(ctx, model.ConfigTargetModel, "none")
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
	})
	t.Run("历史版本列表", func(t *testing.T) {
		revs, err := svc.ListRevisions(ctx, model.ConfigTargetModel, "m1")
		aicWantCode(t, err, 0)
		if len(revs) != 2 || revs[0].RevisionNo != 2 || len(revs[0].BodyJSON) != 0 {
			t.Fatalf("历史列表不符合预期：%+v", revs)
		}
		_, err = svc.ListRevisions(ctx, model.ConfigTargetModel, "none")
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
	})
	t.Run("按 id 取历史版本正文并核对归属", func(t *testing.T) {
		revs, _ := svc.ListRevisions(ctx, model.ConfigTargetModel, "m1")
		r, err := svc.GetRevision(ctx, model.ConfigTargetModel, "m1", revs[1].ID)
		aicWantCode(t, err, 0)
		if len(r.BodyJSON) == 0 {
			t.Fatal("应返回正文")
		}
		_, err = svc.GetRevision(ctx, model.ConfigTargetModel, "m2", revs[1].ID)
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
	})
}

func TestAIConfigService_ModelEnabledAndSort(t *testing.T) {
	ctx := context.Background()
	t.Run("上架已发布模型并热生效", func(t *testing.T) {
		svc, repo, inv := aicNewSvc()
		aicPublishProvider(t, svc, "p1", "")
		aicPublishModel(t, svc, "m1", "p1")
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
		svc, _, _ := aicNewSvc()
		_, _ = svc.SaveDraft(ctx, model.ConfigTargetModel, "", true, aicModelBody("m1", "p1", ""), "", 1)
		aicWantCode(t, svc.SetModelEnabled(ctx, "m1", true, 1), errcode.ErrConfigInvalid.Code)
	})
	t.Run("模型不存在", func(t *testing.T) {
		svc, _, _ := aicNewSvc()
		aicWantCode(t, svc.SetModelEnabled(ctx, "none", false, 1), errcode.ErrConfigNotFound.Code)
		aicWantCode(t, svc.SetModelSort(ctx, "none", 1, 1), errcode.ErrConfigNotFound.Code)
	})
	t.Run("修改排序立即生效", func(t *testing.T) {
		svc, _, _ := aicNewSvc()
		aicPublishProvider(t, svc, "p1", "")
		aicPublishModel(t, svc, "a", "p1")
		aicPublishModel(t, svc, "b", "p1")
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
	if !json.Valid(b) {
		t.Fatalf("应返回合法 JSON：%s", b)
	}
	_, err = svc.JSONSchema("x")
	aicWantCode(t, err, errcode.ErrInvalidParams.Code)
}

func TestNewDSLValidator_JSONSchema(t *testing.T) {
	v := NewDSLValidator()
	for _, target := range []string{"provider", "model"} {
		b, err := v.JSONSchema(target)
		if err != nil || !json.Valid(b) {
			t.Fatalf("%s 应返回合法的 JSON Schema：%v", target, err)
		}
	}
	if _, err := v.JSONSchema("other"); err == nil {
		t.Fatal("未知 target 应返回错误")
	}
}

func TestAIFormatIssues(t *testing.T) {
	var issues []dsl.Issue
	for i := 0; i < 12; i++ {
		issues = append(issues, dsl.Issue{Path: fmt.Sprintf("p%d", i), Message: "m"})
	}
	got := AIFormatIssues(issues)
	if !aicContainsAny(got, "p0：m", "共 12 条") || aicContainsAny(got, "p11") {
		t.Fatalf("最多列 10 条并提示总数：%s", got)
	}
	if AIFormatIssues([]dsl.Issue{{Message: "只有消息"}}) != "只有消息" {
		t.Fatal("没有 path 时只输出消息")
	}
}

// aicContainsAny 判断 s 是否包含任意一个子串。
func aicContainsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
