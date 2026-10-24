package provider_test

import (
	"testing"
	"time"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// 快照要带上模型的「上游响应超时」，Agent 桥按快照调用网关。
func TestNewSnapshot_CarriesIdleTimeout(t *testing.T) {
	cfg := &modelcfg.ModelConfig{
		Key: "m", Kind: modelcfg.KindAgent, IdleTimeout: modelcfg.Duration(2 * time.Minute),
		Channels: []modelcfg.ChannelRef{{Channel: "ch1", UpstreamModel: "up"}},
	}
	snap := provider.NewSnapshot(cfg, &provider.ChannelRuntime{}, 1)
	if snap.Model.IdleTimeout.D() != 2*time.Minute {
		t.Errorf("IdleTimeout=%v", snap.Model.IdleTimeout.D())
	}
}
