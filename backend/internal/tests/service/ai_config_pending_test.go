package service_test

import (
	"context"
	"encoding/json"
	"testing"

	"video-canvas/internal/pkg/errcode"
)

func TestAIConfigService_PendingPricingDraftTrialAndPublish(t *testing.T) {
	svc, repo, dry, tasks := aicRunSetup(t)
	var body map[string]any
	_ = json.Unmarshal(aicModelBody("m1", "c1", ""), &body)
	body["enabled"] = false
	body["pricing"] = map[string]any{"billing": "per_call", "pending": true}
	raw, _ := json.Marshal(body)
	saved, err := aicSave(svc, "m1", false, raw)
	aicWantCode(t, err, 0)
	if len(saved.Issues) != 0 {
		t.Fatalf("合法草稿不应有校验问题：%+v", saved.Issues)
	}
	_, err = svc.DryRun(context.Background(), 9, "m1", map[string]any{"prompt": "test"})
	aicWantCode(t, err, 0)
	if dry.Snap == nil {
		t.Fatal("未执行离线试跑")
	}
	view, err := svc.TestRun(context.Background(), 9, "m1", map[string]any{"prompt": "test"})
	aicWantCode(t, err, 0)
	if view == nil || view.ID == 0 || tasks.Snap == nil {
		t.Fatal("未创建管理员试跑")
	}
	aicWantCode(t, svc.SetModelEnabled(context.Background(), "m1", true, 9), errcode.ErrConfigInvalid.Code)
	if repo.Models["m1"].Enabled {
		t.Fatal("未定价模型被上架")
	}
	body["pricing"] = map[string]any{"billing": "per_call", "unit": 5}
	raw, _ = json.Marshal(body)
	_, err = aicSave(svc, "m1", false, raw)
	aicWantCode(t, err, 0)
	aicWantCode(t, svc.SetModelEnabled(context.Background(), "m1", true, 9), 0)
}
