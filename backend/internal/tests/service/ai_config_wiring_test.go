//go:build legacy

package service_test

import (
	"context"
	"strings"
	"testing"
	. "video-canvas/internal/service"

	"video-canvas/internal/model"
	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/provider/engine"
	"video-canvas/internal/repository"
	"video-canvas/internal/service/aiconfigfake"
)

// 编译期契约：本服务声明的依赖接口必须能被真实实现直接满足，接线时不需要写适配层（除了下面的两个函数适配）。
var (
	_ AIConfigRepo      = (*repository.AIConfigRepository)(nil)
	_ AIConfigRepo      = (*aiconfigfake.MemRepo)(nil)
	_ TestTaskCreator   = (*GenerationTaskService)(nil)
	_ HTTPClientFactory = HTTPClientFactoryFunc(engine.NewGuardedClient)
)

// aicEngineDryRunner 是接线时用的 DryRunner 适配器：把 engine.DryRun 的具体返回类型抹成 any。
// 注意不能直接 return engine.DryRun(...)：出错时会把带类型的 nil 指针装进 any，调用方判空会出错。
func aicEngineDryRunner() DryRunner {
	return DryRunnerFunc(func(ctx context.Context, snap *dsl.Snapshot, input map[string]any) (any, error) {
		res, err := engine.DryRun(ctx, snap, input)
		if err != nil {
			return nil, err
		}
		return res, nil
	})
}

// 端到端：真实 dsl 校验器 + 真实引擎 dry-run，走一遍“种子平台 → 导入草稿 → 保存 → dry-run”，
// 验证导入的草稿、规范化后的输入（含媒体 asset id）、引擎渲染三者互相兼容，且结果里没有凭证。
func TestAIConfigService_DryRun_RealEngine(t *testing.T) {
	ctx := context.Background()
	repo := aiconfigfake.NewMemRepo()
	svc := NewAIConfigService(repo, NewDSLValidator(), "k")
	svc.SetDryRunner(aicEngineDryRunner())
	if err := svc.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	const secret = "rh-real-secret-value-0001"
	if err := svc.SetSecret(ctx, "runninghub_api_key", secret, 1); err != nil {
		t.Fatal(err)
	}
	nodes, err := AIParseNodeInfoList([]byte(aicNodesTypical))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := AIBuildImportDraft("2093984571330498561", "video", "runninghub", nodes)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := svc.SaveDraft(ctx, model.ConfigTargetModel, "", true, draft.Draft, "", 1); err != nil || len(res.Issues) != 0 {
		t.Fatalf("保存草稿失败：%v %+v", err, res)
	}

	out, err := svc.DryRun(ctx, "rh-2093984571330498561", map[string]any{
		"prompt": "一只猫", "image": 11, "video": "12", "audio_file": 13,
	}, false)
	if err != nil {
		t.Fatalf("dry-run 失败：%v", err)
	}
	text := aicJSON(out)
	if strings.Contains(text, secret) {
		t.Fatalf("dry-run 结果不得包含凭证：%s", text)
	}
	if !strings.Contains(text, "一只猫") || !strings.Contains(text, "/openapi/v2/run/ai-app/2093984571330498561") {
		t.Fatalf("dry-run 结果不符合预期：%s", text)
	}
}
