package agent_test

import (
	"context"
	"errors"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	. "video-canvas/internal/service/agent"
)

// fakeAgentRegistry 记录 ListModels 收到的 kind，并返回预设清单。
type fakeAgentRegistry struct {
	models  []provider.ModelInfo
	err     error
	gotKind string
}

func (r *fakeAgentRegistry) ListModels(_ context.Context, kind string) ([]provider.ModelInfo, error) {
	r.gotKind = kind
	return r.models, r.err
}

func (r *fakeAgentRegistry) Snapshot(context.Context, string) (*provider.Snapshot, error) {
	return nil, errors.New("不应调用")
}

func TestRegistryAgentModels_List(t *testing.T) {
	ctx := context.Background()

	t.Run("只取 agent 种类，名字用 label，vision 取自能力", func(t *testing.T) {
		reg := &fakeAgentRegistry{models: []provider.ModelInfo{
			{Key: "claude", Kind: "agent", Label: "Claude Sonnet", Capabilities: modelcfg.Capabilities{Vision: true, Video: true}},
			{Key: "deepseek", Kind: "agent", Label: "DeepSeek V3"},
		}}
		got, err := NewRegistryAgentModels(reg).List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if reg.gotKind != "agent" {
			t.Errorf("应只查 agent 种类: %q", reg.gotKind)
		}
		if len(got) != 2 || got[0].Key != "claude" || got[0].Name != "Claude Sonnet" || !got[0].Vision || !got[0].Video || got[1].Vision || got[1].Video {
			t.Errorf("got=%+v", got)
		}
	})

	t.Run("带上下文窗口：画布据此显示上下文用量；没配窗口是 0", func(t *testing.T) {
		reg := &fakeAgentRegistry{models: []provider.ModelInfo{
			{Key: "claude", Kind: "agent", Label: "Claude", Capabilities: modelcfg.Capabilities{Context: &modelcfg.ContextSpec{Window: 200000, Output: 8192}}},
			{Key: "deepseek", Kind: "agent", Label: "DeepSeek"},
		}}
		got, err := NewRegistryAgentModels(reg).List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got[0].ContextWindow != 200000 || got[1].ContextWindow != 0 {
			t.Errorf("got=%+v", got)
		}
	})

	t.Run("带厂商 slug：画布据此显示厂商 logo；没配是空串", func(t *testing.T) {
		reg := &fakeAgentRegistry{models: []provider.ModelInfo{
			{Key: "claude", Kind: "agent", Label: "Claude", Vendor: "claude"},
			{Key: "custom", Kind: "agent", Label: "自建"},
		}}
		got, err := NewRegistryAgentModels(reg).List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Vendor != "claude" || got[1].Vendor != "" {
			t.Errorf("got=%+v", got)
		}
	})

	t.Run("没有已发布的模型：返回空清单而不是 nil", func(t *testing.T) {
		got, err := NewRegistryAgentModels(&fakeAgentRegistry{}).List(ctx)
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("got=%#v err=%v", got, err)
		}
	})

	t.Run("仓储故障原样返回", func(t *testing.T) {
		boom := errors.New("db down")
		if _, err := NewRegistryAgentModels(&fakeAgentRegistry{err: boom}).List(ctx); !errors.Is(err, boom) {
			t.Errorf("err=%v", err)
		}
	})
}
