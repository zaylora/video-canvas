package pluginmeta_test

import (
	"testing"

	"video-canvas/internal/provider/pluginmeta"
)

// 插件是否能承接某种模型：普通种类看它有没有声明对应端点；
// agent 不走插件钩子（网关直接请求渠道），只要求渠道用 Bearer 鉴权。
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
		{"agent：bearer 鉴权即可，不需要端点", withEndpoints("bearer"), "agent", true},
		{"agent：声明了端点也一样", withEndpoints("bearer", "agent"), "agent", true},
		{"agent：header 鉴权不行", withEndpoints("header", "text"), "agent", false},
		{"agent：无鉴权不行", withEndpoints("none", "text"), "agent", false},
		{"agent：自定义鉴权不行", withEndpoints("custom", "text"), "agent", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.meta.SupportsKind(tc.kind); got != tc.want {
				t.Errorf("SupportsKind(%q)=%v，期望 %v", tc.kind, got, tc.want)
			}
		})
	}
}
