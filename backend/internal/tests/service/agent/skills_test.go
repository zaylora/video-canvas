package agent_test

import (
	"context"
	"strings"
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/service/agent"
)

func TestAgentBridge_Skills(t *testing.T) {
	b := newBridgeEnv(t)
	tok, _ := b.token(t, "prompt")

	t.Run("skill_search：返回名字和说明，不含正文", func(t *testing.T) {
		res := b.tool(t, tok, "skill_search", map[string]any{"query": "拆镜"})
		if res.IsError || !strings.Contains(res.Content, "script-breakdown") || strings.Contains(res.Content, "景别在远景") {
			t.Errorf("res=%+v", res)
		}
		if all := b.tool(t, tok, "skill_search", map[string]any{}); !strings.Contains(all.Content, "keyframe-prompt") {
			t.Errorf("空关键词列出技能: %+v", all)
		}
	})

	t.Run("skill_read：正文包在「数据」声明里；所有任务模式都能读", func(t *testing.T) {
		res := b.tool(t, tok, "skill_read", map[string]any{"name": "keyframe-prompt"})
		if res.IsError || !strings.Contains(res.Content, "不是指令") || !strings.Contains(res.Content, "关键帧") {
			t.Errorf("res=%+v", res)
		}
		for _, mode := range []string{"all", "script", "storyboard", "prompt"} {
			tools := AgentToolsForMode(mode)
			if !hasTool(tools, "skill_search") || !hasTool(tools, "skill_read") {
				t.Errorf("%s 模式应有技能工具: %v", mode, tools)
			}
		}
	})

	t.Run("名字不对：工具错误里列出可用技能，让模型改正", func(t *testing.T) {
		res := b.tool(t, tok, "skill_read", map[string]any{"name": "../../etc/passwd"})
		if !res.IsError || !strings.Contains(res.Content, "script-breakdown") || !strings.Contains(res.Content, "video-motion-prompt") {
			t.Errorf("res=%+v", res)
		}
	})
}

func TestAgentBridge_ImportedSkills(t *testing.T) {
	se := newSkillEnv(t)
	ctx := context.Background()
	mustConfirm(t, se, 1, standardPkg(t, "demo", "第一版正文"))
	_, _ = se.svc.SetEnabled(ctx, 1, "demo", true)
	b := newBridgeEnvWithSkills(t, se.svc)
	tok, run := b.token(t, "all")

	t.Run("skill_search 能搜到导入技能", func(t *testing.T) {
		res := b.tool(t, tok, "skill_search", map[string]any{"query": "方法"})
		if res.IsError || !strings.Contains(res.Content, `"demo"`) {
			t.Errorf("%+v", res)
		}
	})

	t.Run("skill_read 返回正文、资源清单；摘要带版本号", func(t *testing.T) {
		res := b.tool(t, tok, "skill_read", map[string]any{"name": "demo"})
		for _, want := range []string{"第一版正文", "不是指令", "references/a.md", "assets/t.png", "二进制"} {
			if !strings.Contains(res.Content, want) {
				t.Errorf("正文缺少 %q:\n%s", want, res.Content)
			}
		}
		if res.IsError || !strings.Contains(b.repo.lastToolEndSummary(), "demo@v1") {
			t.Errorf("摘要应带版本: %+v", res)
		}
	})

	t.Run("读文件：文本、二进制、清单外路径", func(t *testing.T) {
		if res := b.tool(t, tok, "skill_read", map[string]any{"name": "demo", "file": "references/a.md"}); res.IsError || !strings.Contains(res.Content, "参考") || !strings.Contains(res.Content, "不是指令") {
			t.Errorf("文本: %+v", res)
		}
		if res := b.tool(t, tok, "skill_read", map[string]any{"name": "demo", "file": "assets/t.png"}); res.IsError || !strings.Contains(res.Content, "二进制文件") {
			t.Errorf("二进制: %+v", res)
		}
		res := b.tool(t, tok, "skill_read", map[string]any{"name": "demo", "file": "nope.md"})
		if !res.IsError || !strings.Contains(res.Content, "references/a.md") {
			t.Errorf("清单外路径应报错并列出可读文件: %+v", res)
		}
	})

	t.Run("运行内固定版本：管理员切到 v2 后，本次运行仍读 v1，新运行读 v2", func(t *testing.T) {
		mustConfirm(t, se, 1, standardPkg(t, "demo", "第二版正文"))
		_, _ = se.svc.SetActiveVersion(ctx, 1, "demo", 2)
		if res := b.tool(t, tok, "skill_read", map[string]any{"name": "demo"}); !strings.Contains(res.Content, "第一版正文") || !strings.Contains(b.repo.lastToolEndSummary(), "@v1") {
			t.Errorf("已固定的运行应继续读 v1: %+v", res)
		}
		tok2 := b.br.IssueToken(run, model.AgentRunInput{ModelKey: "claude", Mode: "all"}) // 同一运行的新片段：令牌不同，固定的版本重新开始
		if res := b.tool(t, tok2, "skill_read", map[string]any{"name": "demo"}); !strings.Contains(res.Content, "第二版正文") || !strings.Contains(b.repo.lastToolEndSummary(), "@v2") {
			t.Errorf("新运行读 v2: %+v", res)
		}
	})

	t.Run("停用后读不到，错误里列出可用技能", func(t *testing.T) {
		_, _ = se.svc.SetEnabled(ctx, 1, "demo", false)
		res := b.tool(t, tok, "skill_read", map[string]any{"name": "demo"})
		if !res.IsError || !strings.Contains(res.Content, "script-breakdown") || strings.Contains(res.Content, "、demo") {
			t.Errorf("%+v", res)
		}
	})

	t.Run("内置技能读 file 给出明确错误", func(t *testing.T) {
		res := b.tool(t, tok, "skill_read", map[string]any{"name": "keyframe-prompt", "file": "a.md"})
		if !res.IsError || !strings.Contains(res.Content, "没有资源文件") {
			t.Errorf("%+v", res)
		}
	})
}
