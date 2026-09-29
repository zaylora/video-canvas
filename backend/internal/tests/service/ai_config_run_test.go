package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	. "video-canvas/internal/service"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service/aiconfigfake"
)

// aicRunSetup 准备：平台 p1（凭证 s1，已设置）已发布，模型 m1 只有草稿。
func aicRunSetup(t *testing.T) (*AIConfigService, *aiconfigfake.DryRunner, *aiconfigfake.TestTasks) {
	t.Helper()
	svc, _, _ := aicNewSvc()
	dry := &aiconfigfake.DryRunner{Result: map[string]any{"url": "https://example.com/run", "headers": map[string]any{"Authorization": "Bearer sk-s1"}}}
	tasks := &aiconfigfake.TestTasks{Views: map[uint64]aiconfigfake.TestView{}}
	svc.SetDryRunner(dry)
	svc.SetTestTaskCreator(tasks)
	aicPublishProvider(t, svc, "p1", "s1")
	if _, err := svc.SaveDraft(context.Background(), model.ConfigTargetModel, "", true, aicModelBody("m1", "p1", ""), "", 1); err != nil {
		t.Fatal(err)
	}
	return svc, dry, tasks
}

func TestAIConfigService_DryRun(t *testing.T) {
	ctx := context.Background()

	t.Run("用草稿组装快照，输入规范化后交给引擎，结果脱敏", func(t *testing.T) {
		svc, dry, _ := aicRunSetup(t)
		res, err := svc.DryRun(ctx, "m1", map[string]any{"prompt": "hi"}, false)
		aicWantCode(t, err, 0)
		if dry.Snap == nil || dry.Snap.Model.Key != "m1" || dry.Snap.Provider.Key != "p1" {
			t.Fatalf("快照不符合预期：%+v", dry.Snap)
		}
		draft, _ := svc.Repo().GetDraft(ctx, model.ConfigTargetModel, "m1")
		if dry.Snap.ModelRevisionID != draft.ID {
			t.Fatalf("应使用草稿 revision：%d vs %d", dry.Snap.ModelRevisionID, draft.ID)
		}
		if dry.Input["_normalized"] != true || dry.Input["prompt"] != "hi" {
			t.Fatalf("引擎应收到规范化后的输入：%v", dry.Input)
		}
		m, ok := res.(map[string]any)
		if !ok {
			t.Fatalf("结果应是通用 JSON 结构：%T", res)
		}
		if strings.Contains(strings.ToLower(aicJSON(m)), "sk-s1") {
			t.Fatalf("响应里不得出现凭证明文：%v", m)
		}
	})

	t.Run("没有草稿时用已发布版本", func(t *testing.T) {
		svc, dry, _ := aicRunSetup(t)
		if _, err := svc.Publish(ctx, model.ConfigTargetModel, "m1", 1); err != nil {
			t.Fatal(err)
		}
		// 发布后草稿变成 published，没有 draft 了
		if _, err := svc.Repo().GetDraft(ctx, model.ConfigTargetModel, "m1"); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal("发布后不应再有草稿")
		}
		_, err := svc.DryRun(ctx, "m1", map[string]any{}, false)
		aicWantCode(t, err, 0)
		if dry.Snap == nil {
			t.Fatal("引擎应被调用")
		}
	})

	t.Run("useProviderDraft 优先使用平台草稿", func(t *testing.T) {
		svc, dry, _ := aicRunSetup(t)
		if _, err := svc.SaveDraft(ctx, model.ConfigTargetProvider, "p1", false, aicProviderBody("p1", "s1", `"base_url_note":"draft"`), "", 1); err != nil {
			t.Fatal(err)
		}
		draftProv, _ := svc.Repo().GetDraft(ctx, model.ConfigTargetProvider, "p1")
		pubProv, _ := svc.Repo().GetPublishedRevision(ctx, model.ConfigTargetProvider, "p1")
		_, _ = svc.DryRun(ctx, "m1", map[string]any{}, false)
		if dry.Snap.ProviderRevisionID != pubProv.ID {
			t.Fatalf("默认应使用已发布的平台：%d", dry.Snap.ProviderRevisionID)
		}
		_, _ = svc.DryRun(ctx, "m1", map[string]any{}, true)
		if dry.Snap.ProviderRevisionID != draftProv.ID {
			t.Fatalf("useProviderDraft 应使用平台草稿：%d", dry.Snap.ProviderRevisionID)
		}
	})

	tests := []struct {
		name     string
		setup    func(svc *AIConfigService)
		model    string
		input    map[string]any
		wantCode int
	}{
		{"模型不存在", nil, "none", map[string]any{}, errcode.ErrConfigNotFound.Code},
		{"输入不合法", nil, "m1", map[string]any{"invalid": true}, errcode.ErrTaskInput.Code},
		{"模型配置有校验问题", func(svc *AIConfigService) {
			_, _ = svc.SaveDraft(ctx, model.ConfigTargetModel, "m1", false, aicModelBody("m1", "p1", `"bad":true`), "", 1)
		}, "m1", map[string]any{}, errcode.ErrConfigInvalid.Code},
		{"引用的平台不存在", func(svc *AIConfigService) {
			_, _ = svc.SaveDraft(ctx, model.ConfigTargetModel, "m1", false, aicModelBody("m1", "ghost", ""), "", 1)
		}, "m1", map[string]any{}, errcode.ErrConfigNotFound.Code},
		{"平台草稿有问题不影响默认使用已发布的平台", func(svc *AIConfigService) {
			_, _ = svc.SaveDraft(ctx, model.ConfigTargetProvider, "p1", false, aicProviderBody("p1", "s1", `"bad":true`), "", 1)
		}, "m1", map[string]any{}, 0}, // 已发布的平台优先，草稿有问题不影响默认路径
		{"input 为 nil 视为空对象", nil, "m1", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := aicRunSetup(t)
			if tt.setup != nil {
				tt.setup(svc)
			}
			_, err := svc.DryRun(ctx, tt.model, tt.input, false)
			aicWantCode(t, err, tt.wantCode)
		})
	}

	t.Run("引擎渲染失败返回配置错误且错误信息脱敏", func(t *testing.T) {
		svc, dry, _ := aicRunSetup(t)
		dry.Err = errors.New("render failed near sk-s1")
		_, err := svc.DryRun(ctx, "m1", map[string]any{}, false)
		aicWantCode(t, err, errcode.ErrConfigInvalid.Code)
		if strings.Contains(err.Error(), "sk-s1") {
			t.Fatalf("错误信息不得泄露凭证：%v", err)
		}
	})

	t.Run("未注入引擎", func(t *testing.T) {
		svc, _, _ := aicNewSvc()
		_, err := svc.DryRun(ctx, "m1", map[string]any{}, false)
		aicWantCode(t, err, errcode.ErrInternal.Code)
	})
}

func TestAIConfigService_TestRun(t *testing.T) {
	ctx := context.Background()

	t.Run("用草稿快照创建试跑任务", func(t *testing.T) {
		svc, _, tasks := aicRunSetup(t)
		tasks.View = &model.GenerationTaskView{ID: 42, Status: model.TaskPending}
		view, err := svc.TestRun(ctx, 9, "m1", map[string]any{"prompt": "hi"}, false)
		aicWantCode(t, err, 0)
		if view.ID != 42 || tasks.UserID != 9 || tasks.Snap == nil || tasks.Snap.Model.Key != "m1" {
			t.Fatalf("SubmitTest 调用不符合预期：view=%+v user=%d", view, tasks.UserID)
		}
		if tasks.Input["_normalized"] != true {
			t.Fatalf("应传规范化后的输入：%v", tasks.Input)
		}
	})

	tests := []struct {
		name     string
		setup    func(svc *AIConfigService, tasks *aiconfigfake.TestTasks, repo *aiconfigfake.MemRepo)
		input    map[string]any
		wantCode int
	}{
		{"凭证未设置", func(svc *AIConfigService, _ *aiconfigfake.TestTasks, repo *aiconfigfake.MemRepo) {
			delete(repo.Secrets, "s1")
		}, map[string]any{}, errcode.ErrSecretNotSet.Code},
		{"输入不合法", nil, map[string]any{"invalid": true}, errcode.ErrTaskInput.Code},
		{"任务服务返回业务错误时透传", func(_ *AIConfigService, tasks *aiconfigfake.TestTasks, _ *aiconfigfake.MemRepo) {
			tasks.Err = errcode.ErrTooManyTasks
		}, map[string]any{}, errcode.ErrTooManyTasks.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, tasks := aicRunSetup(t)
			repo := svc.Repo().(*aiconfigfake.MemRepo)
			if tt.setup != nil {
				tt.setup(svc, tasks, repo)
			}
			_, err := svc.TestRun(ctx, 1, "m1", tt.input, false)
			aicWantCode(t, err, tt.wantCode)
		})
	}

	t.Run("模型不存在", func(t *testing.T) {
		svc, _, _ := aicRunSetup(t)
		_, err := svc.TestRun(ctx, 1, "none", map[string]any{}, false)
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
	})
	t.Run("未注入任务服务", func(t *testing.T) {
		svc, _, _ := aicNewSvc()
		_, err := svc.TestRun(ctx, 1, "m1", map[string]any{}, false)
		aicWantCode(t, err, errcode.ErrInternal.Code)
	})
}

func TestAIConfigService_GetTestRun(t *testing.T) {
	ctx := context.Background()
	svc, _, tasks := aicRunSetup(t)
	tasks.Views[7] = aiconfigfake.TestView{UserID: 3, View: &model.GenerationTaskView{ID: 7, Status: model.TaskRunning}}

	t.Run("查自己的试跑任务", func(t *testing.T) {
		v, err := svc.GetTestRun(ctx, 3, 7)
		aicWantCode(t, err, 0)
		if v.ID != 7 || v.Status != model.TaskRunning {
			t.Fatalf("视图不符合预期：%+v", v)
		}
	})
	t.Run("别人的或不存在的任务统一返回不存在", func(t *testing.T) {
		_, err := svc.GetTestRun(ctx, 4, 7)
		aicWantCode(t, err, errcode.ErrTaskNotFound.Code)
		_, err = svc.GetTestRun(ctx, 3, 999)
		aicWantCode(t, err, errcode.ErrTaskNotFound.Code)
	})
	t.Run("未注入任务服务", func(t *testing.T) {
		s, _, _ := aicNewSvc()
		_, err := s.GetTestRun(ctx, 1, 1)
		aicWantCode(t, err, errcode.ErrInternal.Code)
	})
}

func TestAIRedactResult(t *testing.T) {
	res := AIRedactResult(map[string]any{
		"headers": map[string]any{"Authorization": "Bearer sk-abc\"def"},
		"list":    []any{"x sk-abc\"def y"},
	}, []string{"sk-abc\"def"})
	text := aicJSON(res)
	if strings.Contains(text, "sk-abc") || !strings.Contains(text, "***") {
		t.Fatalf("凭证应被替换：%s", text)
	}
	if AIRedactString("a sk-1 b", []string{"sk-1", ""}) != "a *** b" {
		t.Fatal("文本脱敏不正确")
	}
	if AIRedactResult(make(chan int), nil) != nil {
		t.Fatal("无法序列化的结果应丢弃而不是原样返回")
	}
}

// aicJSON 把任意值转成 JSON 文本，方便断言里查找敏感内容。
func aicJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
