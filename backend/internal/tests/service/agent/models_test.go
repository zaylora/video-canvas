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
			{Key: "claude", Kind: "agent", Label: "Claude Sonnet", Capabilities: modelcfg.Capabilities{Vision: true}},
			{Key: "deepseek", Kind: "agent", Label: "DeepSeek V3"},
		}}
		got, err := NewRegistryAgentModels(reg).List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if reg.gotKind != "agent" {
			t.Errorf("应只查 agent 种类: %q", reg.gotKind)
		}
		if len(got) != 2 || got[0].Key != "claude" || got[0].Name != "Claude Sonnet" || !got[0].Vision || got[1].Vision {
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
