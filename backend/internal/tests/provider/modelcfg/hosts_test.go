// 本文件：hosts.go 的单元测试：allowed_hosts 的格式校验与匹配。

package modelcfg_test

import (
	"strings"
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

func TestValidateHostPattern(t *testing.T) {
	tests := []struct {
		pattern string
		wantErr string // 空表示应该通过；否则是错误信息里应包含的片段
	}{
		{"www.runninghub.cn", ""},
		{"*.runninghub.cn", ""},
		{"*.aliyuncs.com", ""},
		{"*.oss-cn-hangzhou.aliyuncs.com", ""},
		{"*.cloudfront.net", ""},   // 私有后缀：云厂商托管结果文件常用，允许
		{"*.s3.amazonaws.com", ""}, // 同上
		{"*.example.com.cn", ""},
		{"*.com", "过宽"},
		{"*.com.cn", "公共后缀"},
		{"*.co.uk", "公共后缀"},
		{"com.cn", ""}, // 非通配的精确域名不受影响
		{"*.internal", "过宽"},
		{"", "不能为空"},
		{"A.com", "小写"},
		{"a.com:8080", "端口"},
		{"https://a.com", "协议"},
		{"10.0.0.1", "IP"},
		{"*.10.0.0.1", "IP"},
		{"a_b.com", "格式"},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			err := modelcfg.ValidateHostPattern(tt.pattern)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("期望通过，实际：%v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("期望错误包含 %q，实际：%v", tt.wantErr, err)
			}
		})
	}
}

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
