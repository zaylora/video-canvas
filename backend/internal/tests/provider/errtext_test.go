package provider_test

import (
	"testing"

	"video-canvas/internal/provider"
)

// 上游原文给用户看之前要去掉上游自己的请求 / 追踪编号：对用户没用，排查用的是我们自己的任务编号。
func TestStripTraceIDs(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"new-api 括号里的 request id", "Unsupported input: prompt (request id: 202611131805399844957098268d9d6vyiWrRxP)", "Unsupported input: prompt"},
		{"下划线写法", "invalid size, request_id: abc-123", "invalid size"},
		{"驼峰 + 等号", "rate limited requestId=7f3a9c", "rate limited"},
		{"trace id 在中间", "bad ratio [trace_id: t-001] please retry", "bad ratio please retry"},
		{"括号里还有别的内容", "failed (code: 1, request_id: r123)", "failed (code: 1)"},
		{"X-Request-Id", "upstream error X-Request-Id: 9d6vyi", "upstream error"},
		{"中文请求 ID", "参数错误（请求ID：20261113abc）", "参数错误"},
		{"大小写混合", "oops (Request ID: req_9F)", "oops"},
		{"没有编号的原文不动", "ratio must be 9:16 or 16:9", "ratio must be 9:16 or 16:9"},
		{"只有编号时为空", "(request id: abc123)", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := provider.StripTraceIDs(tc.in); got != tc.want {
				t.Fatalf("StripTraceIDs(%q) = %q，期望 %q", tc.in, got, tc.want)
			}
		})
	}
}
