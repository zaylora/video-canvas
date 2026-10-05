package netguard_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"video-canvas/internal/provider/netguard"
)

// TestConfig_CheckHost 覆盖 SMTP 等非 HTTP 出站目标的主机校验：字面量 IP 与 DNS 解析后的 IP 都不能落在内网 / 回环 / 链路本地。
func TestConfig_CheckHost(t *testing.T) {
	cfg := &netguard.Config{Resolver: mapResolver{
		"smtp.public.com": {net.ParseIP("8.8.8.8")},
		"evil.com":        {net.ParseIP("8.8.8.8"), net.ParseIP("10.0.0.5")},
		"meta.com":        {net.ParseIP("169.254.169.254")},
		"empty.com":       {},
	}}
	cfg.ApplyDefaults()
	tests := []struct {
		host    string
		blocked bool
	}{
		{"8.8.8.8", false}, {"127.0.0.1", true}, {"10.1.2.3", true}, {"192.168.0.1", true}, {"169.254.169.254", true}, {"::1", true},
		{"smtp.public.com", false},
		{"evil.com", true},  // 公网 + 内网混合：整体拒绝
		{"meta.com", true},  // 解析到链路本地
		{"empty.com", true}, // 没有解析结果
		{"nx.com", true},    // 解析失败
	}
	for _, tt := range tests {
		err := cfg.CheckHost(context.Background(), tt.host)
		if tt.blocked != (err != nil) {
			t.Errorf("%s：期望 blocked=%v，实际 err=%v", tt.host, tt.blocked, err)
		}
	}
	if err := cfg.CheckHost(context.Background(), "127.0.0.1"); !errors.Is(err, netguard.ErrBlockedAddress) {
		t.Errorf("内网地址应返回 ErrBlockedAddress：%v", err)
	}
}
