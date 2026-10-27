package service_test

import (
	"context"
	"testing"

	"video-canvas/internal/provider"
)

func TestCatalog_ModelLimits(t *testing.T) {
	ctx := context.Background()

	t.Run("认识的模型：返回内置快照里的窗口和最大输出，并标明来源", func(t *testing.T) {
		got := newCatEnv(t).cat.ModelLimits(ctx, "openai/gpt-4o", "")
		if !got.Found || got.ContextWindow != 128000 || got.MaxOutput != 16384 || got.Source != "builtin" {
			t.Fatalf("实际 %+v", got)
		}
	})
	t.Run("不认识的模型：不算错误，Found=false 且数值为 0", func(t *testing.T) {
		got := newCatEnv(t).cat.ModelLimits(ctx, "my-private-model-v7", "")
		if got.Found || got.ContextWindow != 0 || got.MaxOutput != 0 || got.Source != "" {
			t.Fatalf("实际 %+v", got)
		}
	})
	t.Run("带渠道：上游给了规格就优先用上游的", func(t *testing.T) {
		e := newCatEnv(t)
		e.channel(t, "a-main", "主线", true, true)
		e.lister.Limits = map[string]provider.UpstreamLimits{"gpt-4o": {Window: 300000, Output: 80000}}
		got := e.cat.ModelLimits(ctx, "gpt-4o", "a-main")
		if !got.Found || got.ContextWindow != 300000 || got.MaxOutput != 80000 || got.Source != "upstream" {
			t.Fatalf("实际 %+v", got)
		}
	})
	t.Run("带渠道：上游只给了窗口、没给输出，用内置的", func(t *testing.T) {
		e := newCatEnv(t)
		e.channel(t, "a-main", "主线", true, true)
		e.lister.Limits = map[string]provider.UpstreamLimits{"gpt-4o": {Window: 300000}}
		if got := e.cat.ModelLimits(ctx, "gpt-4o", "a-main"); got.Source != "builtin" || got.ContextWindow != 128000 {
			t.Fatalf("实际 %+v", got)
		}
	})
	t.Run("带渠道：上游的规格不合法（输出不小于窗口），用内置的", func(t *testing.T) {
		e := newCatEnv(t)
		e.channel(t, "a-main", "主线", true, true)
		e.lister.Limits = map[string]provider.UpstreamLimits{"gpt-4o": {Window: 1000, Output: 5000}}
		if got := e.cat.ModelLimits(ctx, "gpt-4o", "a-main"); got.Source != "builtin" {
			t.Fatalf("实际 %+v", got)
		}
	})
	t.Run("带渠道：上游失败、渠道没设 Key 或渠道不存在，都悄悄退回内置，不报错", func(t *testing.T) {
		e := newCatEnv(t)
		e.channel(t, "a-main", "主线", true, true)
		e.channel(t, "b-nokey", "没设Key", true, false)
		e.lister.Err = errBoom
		for _, ch := range []string{"a-main", "b-nokey", "no-such"} {
			if got := e.cat.ModelLimits(ctx, "gpt-4o", ch); !got.Found || got.Source != "builtin" {
				t.Errorf("渠道 %s：实际 %+v", ch, got)
			}
		}
	})
}

func TestCatalog_ModelLimitsModalities(t *testing.T) {
	got := newCatEnv(t).cat.ModelLimits(context.Background(), "google/gemini-2.5-flash", "")
	if got.Video == nil || !*got.Video || got.Vision == nil || !*got.Vision {
		t.Fatalf("应附上视频和图片建议: %+v", got)
	}
	unknown := newCatEnv(t).cat.ModelLimits(context.Background(), "private-model-no-match", "")
	if unknown.Video != nil || unknown.Vision != nil {
		t.Fatalf("未知模型不能猜测能力: %+v", unknown)
	}
}
