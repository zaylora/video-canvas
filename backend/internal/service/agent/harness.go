package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"video-canvas/internal/agent/prompts"
)

// maxBuildSHALen 是 agent_runs.build_sha 的列宽。
const maxBuildSHALen = 40

// Harness 是一次运行所用的 Agent 实现版本：系统提示词、工具声明和构建。
// 运行创建时记下它，之后版本对比（改了提示词有没有变好）才有依据。
type Harness struct {
	PromptVersion int    // 系统提示词版本（prompts.Version）
	ToolsHash     string // 工具声明（tool_decls.json）的哈希前缀
	BuildSHA      string // 构建的 git commit
}

// Key 把三项版本合并成 12 位十六进制短哈希，版本对比按它分组；三项都空（没记版本）时返回空串。
func (h Harness) Key() string {
	if h == (Harness{}) {
		return ""
	}
	// 字段之间加分隔，避免 (9, "1") 和 (91, "") 拼出同一个串
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s", h.PromptVersion, h.ToolsHash, h.BuildSHA)))
	return hex.EncodeToString(sum[:])[:12]
}

// CurrentHarness 返回当前代码的 Harness 版本：提示词版本取 prompts.Version，工具声明哈希取内嵌的 tool_decls.json，
// buildSHA 由调用方传入（构建时注入的 git commit，过长按列宽截断）。
func CurrentHarness(buildSHA string) Harness {
	if len(buildSHA) > maxBuildSHALen {
		buildSHA = buildSHA[:maxBuildSHALen]
	}
	sum := sha256.Sum256(toolDeclsJSON)
	return Harness{PromptVersion: prompts.Version, ToolsHash: hex.EncodeToString(sum[:])[:12], BuildSHA: buildSHA}
}
