package modelcfg_test

import (
	"strings"
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

func TestParseModel_PendingPricing(t *testing.T) {
	for _, tc := range []struct {
		name      string
		enabled   bool
		pricing   map[string]any
		wantValid bool
	}{
		{"未定价草稿可保存", false, map[string]any{"billing": "per_call", "pending": true}, true},
		{"未定价不能启用", true, map[string]any{"billing": "per_call", "pending": true}, false},
		{"未知售价不能同时写占位价格", false, map[string]any{"billing": "per_call", "pending": true, "unit": 1}, false},
		{"没有草稿标识仍须正常定价", false, map[string]any{"billing": "per_call"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := mcLoadFixture(t, "model_video.json")
			body["enabled"], body["pricing"] = tc.enabled, tc.pricing
			_, issues := modelcfg.ParseModel(mcMarshal(t, body))
			if (len(issues) == 0) != tc.wantValid {
				t.Fatalf("校验结果：%+v", issues)
			}
			if !tc.wantValid && !strings.Contains(issues[0].Path, "pricing") {
				t.Fatalf("应明确指出定价问题：%+v", issues)
			}
		})
	}
}
