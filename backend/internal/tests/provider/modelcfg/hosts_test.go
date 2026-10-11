// 本文件：hosts.go 的单元测试：域名白名单匹配。

package modelcfg_test

import (
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

func TestMatchHost(t *testing.T) {
	patterns := []string{"www.runninghub.cn", "*.runninghub.ai"}
	tests := []struct {
		host string
		want bool
	}{
		{"www.runninghub.cn", true},
		{"WWW.RunningHub.CN", true},
		{"www.runninghub.cn.", true},
		{"runninghub.cn", false},
		{"evil-www.runninghub.cn", false},
		{"a.runninghub.ai", true},
		{"a.b.runninghub.ai", true},
		{"runninghub.ai", false},
		{"xrunninghub.ai", false},
		{"", false},
		{"127.0.0.1", false},
	}
	for _, tt := range tests {
		if got := modelcfg.MatchHost(patterns, tt.host); got != tt.want {
			t.Errorf("modelcfg.MatchHost(%q) = %v, 期望 %v", tt.host, got, tt.want)
		}
	}
}
