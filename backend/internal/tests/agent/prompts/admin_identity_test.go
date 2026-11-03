package prompts_test

import (
	"strings"
	"testing"

	"video-canvas/internal/agent/prompts"
)

// 后台助手和画布助手是同一个 Agent「小镜」：身份一致，只是知道自己此刻在后台、手里是后台的工具。
func TestAdmin_SameIdentityAsCanvas(t *testing.T) {
	got := prompts.Admin([]byte(`{}`))
	for _, want := range []string{"小镜", "连镜", "后台"} {
		if !strings.Contains(got, want) {
			t.Fatalf("后台提示词应包含 %q", want)
		}
	}
	// 不能自称另一个助手
	for _, bad := range []string{"后台配置助手", "后台模型与定价配置助手"} {
		if strings.Contains(got, bad) {
			t.Fatalf("后台提示词不应自称 %q：小镜只有一个", bad)
		}
	}
	// 身份是第一句：问「你是谁」时先说自己是小镜
	if first, _, _ := strings.Cut(strings.TrimSpace(got), "\n"); !strings.Contains(first, "小镜") {
		t.Fatalf("身份要在第一行写明，实际第一行：%s", first)
	}
}

// 后台的小镜知道画布的事归画布：用户在后台问画布操作时，引导去画布，而不是假装能做。
func TestAdmin_KnowsCanvasWorkHappensOnCanvas(t *testing.T) {
	got := prompts.Admin([]byte(`{}`))
	if !strings.Contains(got, "画布") {
		t.Fatal("后台提示词应说明画布上的事要到画布里找小镜")
	}
}

func TestAdmin_ManagementScope(t *testing.T) {
	got := prompts.Admin([]byte(`{}`))
	for _, want := range []string{"任务队列", "运行记录", "用户管理", "使用记录", "操作审计", "图片服务", "注册设置", "邮件服务", "存储设置", "admin_query", "admin_get_resource", "超级管理员", "专用表单"} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少后台管理能力说明：%s", want)
		}
	}
	if strings.Contains(got, "不操作用户、支付或存储") {
		t.Fatal("残留旧能力限制")
	}
}
