package agent_test

import (
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/service/agent"
)

func TestAdminPricingRound(t *testing.T) {
	for _, tc := range []struct {
		raw  float64
		want int
	}{{0.14 * 6.6529 * 7.5, 7}, {2 * 1.1, 3}, {2 * 0.9, 2}, {7.0000000001, 7}, {7.000001, 8}} {
		if got := AdminPriceCeil(tc.raw); got != tc.want {
			t.Fatalf("ceil(%v)=%d want %d", tc.raw, got, tc.want)
		}
	}
}

func TestRunContextAdminCanvas(t *testing.T) {
	for _, tc := range []struct {
		mode   string
		canvas uint64
		valid  bool
	}{{"admin", 0, true}, {"admin", 1, false}, {"all", 0, false}, {"all", 1, true}, {"", 1, true}, {"x", 1, false}} {
		rc := RunContext{RunID: 1, UserID: 1, SessionID: 1, CanvasID: tc.canvas, Mode: tc.mode}
		if err := ValidateRunContextForTest(rc); (err == nil) != tc.valid {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	if !model.ValidAgentMode("admin") {
		t.Fatal("admin 模式未注册")
	}
	for _, name := range AgentToolsForMode("admin") {
		if name != "web_search" && name != "web_fetch" && name != "plan_update" && name != "show_card" && len(name) > 0 && name[:6] != "admin_" {
			t.Fatalf("泄漏工具 %s", name)
		}
	}
}
