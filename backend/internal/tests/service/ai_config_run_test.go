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
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/service/aiconfigfake"
)

// aicRunSetup 准备：渠道 c1（Key 已设置）就绪，模型 m1 只有草稿。
func aicRunSetup(t *testing.T) (*AIConfigService, *aiconfigfake.MemRepo, *aiconfigfake.DryRunner, *aiconfigfake.TestTasks) {
	t.Helper()
	svc, repo, _ := aicNewSvc()
	dry := &aiconfigfake.DryRunner{Result: map[string]any{"url": "https://example.com/run", "headers": map[string]any{"X-Echo": "Bearer sk-c1"}}}
	tasks := &aiconfigfake.TestTasks{Views: map[uint64]aiconfigfake.TestView{}}
	svc.SetDryRunner(dry)
	svc.SetTestTaskCreator(tasks)
	aicSeedChannel(t, svc, repo, "c1", true)
	if _, err := aicSave(svc, "", true, aicModelBody("m1", "c1", "")); err != nil {
		t.Fatal(err)
	}
	return svc, repo, dry, tasks
}

func TestAIConfigService_DryRun(t *testing.T) {
	ctx := context.Background()

	t.Run("用草稿组装快照（渠道 + 固定的插件版本），输入规范化后交给宿主，结果脱敏", func(t *testing.T) {
		svc, repo, dry, _ := aicRunSetup(t)
		res, err := svc.DryRun(ctx, "m1", map[string]any{"prompt": "hi"})
		aicWantCode(t, err, 0)
		if dry.Snap == nil || dry.Snap.Model.Key != "m1" || dry.Snap.Channel.Key != "c1" || dry.Snap.Plugin.Version != "1.0.0" {
			t.Fatalf("快照不符合预期：%+v", dry.Snap)
		}
		draft, _ := repo.GetDraft(ctx, model.ConfigTargetModel, "m1")
		if dry.Snap.ModelRevisionID != draft.ID {
			t.Fatalf("应使用草稿 revision：%d vs %d", dry.Snap.ModelRevisionID, draft.ID)
		}
		if dry.Input["prompt"] != "hi" {
			t.Fatalf("宿主应收到规范化后的输入：%v", dry.Input)
		}
		m, ok := res.(map[string]any)
		if !ok {
			t.Fatalf("结果应是通用 JSON 结构：%T", res)
		}
		if strings.Contains(strings.ToLower(aicJSON(m)), "sk-c1") || !strings.Contains(aicJSON(m), "***") {
			t.Fatalf("响应里不得出现凭证明文：%v", m)
		}
	})

	t.Run("没有草稿时用已发布版本", func(t *testing.T) {
		svc, repo, dry, _ := aicRunSetup(t)
		if _, err := svc.Publish(ctx, "m1", 1); err != nil {
			t.Fatal(err)
		}
		// 发布后草稿变成 published，没有 draft 了
		if _, err := repo.GetDraft(ctx, model.ConfigTargetModel, "m1"); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal("发布后不应再有草稿")
		}
		_, err := svc.DryRun(ctx, "m1", map[string]any{"prompt": "x"})
		aicWantCode(t, err, 0)
		if dry.Snap == nil {
			t.Fatal("宿主应被调用")
		}
	})

	tests := []struct {
		name     string
		setup    func(svc *AIConfigService, repo *aiconfigfake.MemRepo)
		model    string
		input    map[string]any
		wantCode int
	}{
		{"模型不存在", nil, "none", map[string]any{}, errcode.ErrConfigNotFound.Code},
		{"输入不合法（缺必填 prompt）", nil, "m1", map[string]any{}, errcode.ErrTaskInput.Code},
		{"input 为 nil 视为空对象，同样缺必填", nil, "m1", nil, errcode.ErrTaskInput.Code},
		{"模型配置有校验问题", func(svc *AIConfigService, _ *aiconfigfake.MemRepo) {
			_, _ = aicSave(svc, "m1", false, aicModelBody("m1", "c1", `"bad":true`))
		}, "m1", map[string]any{"prompt": "x"}, errcode.ErrConfigInvalid.Code},
		{"绑定的渠道不存在", func(svc *AIConfigService, _ *aiconfigfake.MemRepo) {
			_, _ = aicSave(svc, "m1", false, aicModelBody("m1", "ghost", ""))
		}, "m1", map[string]any{"prompt": "x"}, errcode.ErrChannelNotFound.Code},
		{"渠道已停用", func(_ *AIConfigService, repo *aiconfigfake.MemRepo) { repo.Channels["c1"].Enabled = false }, "m1", map[string]any{"prompt": "x"}, errcode.ErrChannelDisabled.Code},
		{"渠道用的插件已停用", func(_ *AIConfigService, repo *aiconfigfake.MemRepo) { repo.Plugins["kling"].Enabled = false }, "m1", map[string]any{"prompt": "x"}, errcode.ErrPluginDisabled.Code},
		{"dry-run 不要求渠道 Key 已设置（宿主里 Key 用 *** 占位）", func(_ *AIConfigService, repo *aiconfigfake.MemRepo) { delete(repo.Secrets, "channel:c1") }, "m1", map[string]any{"prompt": "x"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _, _ := aicRunSetup(t)
			if tt.setup != nil {
				tt.setup(svc, repo)
			}
			_, err := svc.DryRun(ctx, tt.model, tt.input)
			aicWantCode(t, err, tt.wantCode)
		})
	}

	t.Run("runner 不可用返回 503（50021）", func(t *testing.T) {
		svc, _, dry, _ := aicRunSetup(t)
		dry.Err = &provider.Error{Class: provider.ClassRetryable, Code: provider.CodeRunnerUnavailable, Message: "down"}
		_, err := svc.DryRun(ctx, "m1", map[string]any{"prompt": "x"})
		aicWantCode(t, err, errcode.ErrRunnerUnavailable.Code)
	})

	t.Run("宿主渲染失败返回配置错误且错误信息脱敏", func(t *testing.T) {
		svc, _, dry, _ := aicRunSetup(t)
		dry.Err = errors.New("render failed near sk-c1")
		_, err := svc.DryRun(ctx, "m1", map[string]any{"prompt": "x"})
		aicWantCode(t, err, errcode.ErrConfigInvalid.Code)
		if strings.Contains(err.Error(), "sk-c1") {
			t.Fatalf("错误信息不得泄露凭证：%v", err)
		}
	})

	t.Run("未注入宿主", func(t *testing.T) {
		svc, _, _ := aicNewSvc()
		_, err := svc.DryRun(ctx, "m1", map[string]any{})
		aicWantCode(t, err, errcode.ErrInternal.Code)
	})
}

func TestAIConfigService_TestRun(t *testing.T) {
	ctx := context.Background()

	t.Run("用草稿快照创建试跑任务", func(t *testing.T) {
		svc, _, _, tasks := aicRunSetup(t)
		tasks.View = &model.GenerationTaskView{ID: 42, Status: model.TaskPending}
		view, err := svc.TestRun(ctx, 9, "m1", map[string]any{"prompt": "hi"})
		aicWantCode(t, err, 0)
		if view.ID != 42 || tasks.UserID != 9 || tasks.Snap == nil || tasks.Snap.Model.Key != "m1" || tasks.Snap.Channel.Key != "c1" {
			t.Fatalf("SubmitTest 调用不符合预期：view=%+v user=%d", view, tasks.UserID)
		}
		if tasks.Input["prompt"] != "hi" {
			t.Fatalf("应传规范化后的输入：%v", tasks.Input)
		}
	})

	tests := []struct {
		name     string
		setup    func(tasks *aiconfigfake.TestTasks, repo *aiconfigfake.MemRepo)
		input    map[string]any
		wantCode int
	}{
		{"渠道 Key 未设置（50015）", func(_ *aiconfigfake.TestTasks, repo *aiconfigfake.MemRepo) { delete(repo.Secrets, "channel:c1") }, map[string]any{"prompt": "x"}, errcode.ErrChannelSecretUnset.Code},
		{"输入不合法", nil, map[string]any{}, errcode.ErrTaskInput.Code},
		{"渠道已停用", func(_ *aiconfigfake.TestTasks, repo *aiconfigfake.MemRepo) { repo.Channels["c1"].Enabled = false }, map[string]any{"prompt": "x"}, errcode.ErrChannelDisabled.Code},
		{"任务服务返回业务错误时透传", func(tasks *aiconfigfake.TestTasks, _ *aiconfigfake.MemRepo) { tasks.Err = errcode.ErrTooManyTasks }, map[string]any{"prompt": "x"}, errcode.ErrTooManyTasks.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _, tasks := aicRunSetup(t)
			if tt.setup != nil {
				tt.setup(tasks, repo)
			}
			_, err := svc.TestRun(ctx, 1, "m1", tt.input)
			aicWantCode(t, err, tt.wantCode)
		})
	}

	t.Run("模型不存在", func(t *testing.T) {
		svc, _, _, _ := aicRunSetup(t)
		_, err := svc.TestRun(ctx, 1, "none", map[string]any{})
		aicWantCode(t, err, errcode.ErrConfigNotFound.Code)
	})
	t.Run("未注入任务服务", func(t *testing.T) {
		svc, _, _ := aicNewSvc()
		_, err := svc.TestRun(ctx, 1, "m1", map[string]any{})
		aicWantCode(t, err, errcode.ErrInternal.Code)
	})
}

func TestAIConfigService_GetTestRun(t *testing.T) {
	ctx := context.Background()
	svc, _, _, tasks := aicRunSetup(t)
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

func TestAIConfigService_GetTestTrace(t *testing.T) {
	ctx := context.Background()
	svc, _, _, tasks := aicRunSetup(t)
	tasks.Views[7] = aiconfigfake.TestView{UserID: 3, View: &model.GenerationTaskView{ID: 7, Status: model.TaskSucceeded}}

	t.Run("还没有追踪时 steps 是空数组而不是 null", func(t *testing.T) {
		tr, err := svc.GetTestTrace(ctx, 3, 7)
		aicWantCode(t, err, 0)
		b, _ := json.Marshal(tr)
		if tr.Steps == nil || string(b) != `{"steps":[]}` {
			t.Fatalf("应输出空数组：%s", b)
		}
	})
	t.Run("返回宿主写下的每一步", func(t *testing.T) {
		tasks.Trace = []provider.TraceStep{{Name: "submit", Kind: "hook"}, {Name: "submit", Kind: "http"}}
		tr, err := svc.GetTestTrace(ctx, 3, 7)
		aicWantCode(t, err, 0)
		if len(tr.Steps) != 2 || tr.Steps[1].Kind != "http" {
			t.Fatalf("追踪不符合预期：%+v", tr)
		}
	})
	t.Run("别人的或不存在的任务统一返回不存在", func(t *testing.T) {
		_, err := svc.GetTestTrace(ctx, 4, 7)
		aicWantCode(t, err, errcode.ErrTaskNotFound.Code)
		_, err = svc.GetTestTrace(ctx, 3, 999)
		aicWantCode(t, err, errcode.ErrTaskNotFound.Code)
	})
	t.Run("未注入任务服务", func(t *testing.T) {
		s, _, _ := aicNewSvc()
		_, err := s.GetTestTrace(ctx, 1, 1)
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
