package agent_test

import (
	"context"
	"strings"
	"testing"

	. "video-canvas/internal/service/agent"
)

func TestAgentBridge_Skills(t *testing.T) {
	b := newBridgeEnv(t)
	tok, _ := b.token(t, "prompt")

	t.Run("没有任何技能：skill_search 返回空列表，不报错", func(t *testing.T) {
		for _, q := range []string{"拆镜", ""} {
			res := b.tool(t, tok, "skill_search", map[string]any{"query": q})
			if res.IsError || !strings.Contains(res.Content, `"skills":[]`) {
				t.Errorf("搜 %q 应得到空列表: %+v", q, res)
			}
		}
	})

	t.Run("所有任务模式都有技能工具", func(t *testing.T) {
		for _, mode := range []string{"all", "script", "storyboard", "prompt"} {
			tools := AgentToolsForMode(mode)
			if !hasTool(tools, "skill_search") || !hasTool(tools, "skill_read") {
				t.Errorf("%s 模式应有技能工具: %v", mode, tools)
			}
		}
	})

	t.Run("名字不对：工具错误里说明没有可用技能，不留一个空的列表", func(t *testing.T) {
		res := b.tool(t, tok, "skill_read", map[string]any{"name": "../../etc/passwd"})
		if !res.IsError || !strings.Contains(res.Content, "没有叫") || !strings.Contains(res.Content, "当前没有可用的技能") || strings.HasSuffix(res.Content, "：") {
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
		if res.IsError || !strings.Contains(toolEndSummary(res), "demo@v1") {
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
		if res := b.tool(t, tok, "skill_read", map[string]any{"name": "demo"}); !strings.Contains(res.Content, "第一版正文") || !strings.Contains(toolEndSummary(res), "@v1") {
			t.Errorf("已固定的运行应继续读 v1: %+v", res)
		}
		b.br.EndPlatformRun(run.ID) // 运行结束：固定的版本随之清掉，下一个运行重新固定
		if res := b.tool(t, tok, "skill_read", map[string]any{"name": "demo"}); !strings.Contains(res.Content, "第二版正文") || !strings.Contains(toolEndSummary(res), "@v2") {
			t.Errorf("新运行读 v2: %+v", res)
		}
	})

	t.Run("停用后读不到，错误里不再列出它", func(t *testing.T) {
		_, _ = se.svc.SetEnabled(ctx, 1, "demo", false)
		res := b.tool(t, tok, "skill_read", map[string]any{"name": "demo"})
		if !res.IsError || !strings.Contains(res.Content, "没有叫") || strings.Contains(res.Content, "、demo") || strings.Contains(res.Content, "：demo") {
			t.Errorf("%+v", res)
		}
	})
}

func TestAgentBridge_ImportedSkillWithScripts(t *testing.T) {
	se := newSkillEnv(t)
	ctx := context.Background()
	mustConfirm(t, se, 1, zipOf(t, map[string]string{
		"SKILL.md":       skillMD("scripty", "带脚本的校验方法", "出稿前先对照脚本里的规则检查。"),
		"scripts/run.py": "def check(text):\n    return len(text) <= 100\n",
	}))
	_, _ = se.svc.SetEnabled(ctx, 1, "scripty", true)
	b := newBridgeEnvWithSkills(t, se.svc)
	tok, _ := b.token(t, "all")

	t.Run("skill_read 列出脚本，并明确说不能执行、不要假装运行过", func(t *testing.T) {
		res := b.tool(t, tok, "skill_read", map[string]any{"name": "scripty"})
		for _, want := range []string{"scripts/run.py", "不能执行脚本", "不要声称"} {
			if res.IsError || !strings.Contains(res.Content, want) {
				t.Errorf("正文缺少 %q:\n%+v", want, res)
			}
		}
	})

	t.Run("脚本源码可以当文本读，包在「数据」声明里", func(t *testing.T) {
		res := b.tool(t, tok, "skill_read", map[string]any{"name": "scripty", "file": "scripts/run.py"})
		if res.IsError || !strings.Contains(res.Content, "def check(text)") || !strings.Contains(res.Content, "不是指令") {
			t.Errorf("%+v", res)
		}
	})
}
