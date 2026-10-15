package pluginmeta_test

import (
	"testing"

	"video-canvas/internal/provider/pluginmeta"
)

// 插件是否能承接某种模型：普通种类看它有没有声明对应端点；
// agent 不走插件钩子（网关直接用渠道 Key 请求渠道），模型不选协议，所以任何插件都不承接 agent。
func TestMeta_SupportsKind(t *testing.T) {
	withEndpoints := func(auth string, kinds ...string) *pluginmeta.Meta {
		m := &pluginmeta.Meta{Auth: pluginmeta.Auth{Type: auth}, Endpoints: map[string]pluginmeta.Endpoint{}}
		for _, k := range kinds {
			m.Endpoints[k] = pluginmeta.Endpoint{Mode: pluginmeta.ModeSync}
		}
		return m
	}
	cases := []struct {
		name string
		meta *pluginmeta.Meta
		kind string
		want bool
	}{
		{"声明了 text 端点", withEndpoints("bearer", "text"), "text", true},
		{"没声明 video 端点", withEndpoints("bearer", "text"), "video", false},
		{"agent：bearer 鉴权的插件也不承接", withEndpoints("bearer", "text"), "agent", false},
		{"agent：声明了 agent 端点也不承接", withEndpoints("bearer", "agent"), "agent", false},
		{"agent：其他鉴权同样不承接", withEndpoints("header", "text"), "agent", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.meta.SupportsKind(tc.kind); got != tc.want {
				t.Errorf("SupportsKind(%q)=%v，期望 %v", tc.kind, got, tc.want)
			}
		})
	}
}
