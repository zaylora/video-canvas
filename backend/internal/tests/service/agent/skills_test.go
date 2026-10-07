package agent_test

import (
	"strings"
	"testing"

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
