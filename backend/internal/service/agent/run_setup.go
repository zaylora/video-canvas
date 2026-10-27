package agent

import (
	"context"
	"encoding/json"
	"slices"

	"go.uber.org/zap"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/agent/prompts"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
)

// RunModel 是运行用的模型的规格：Agent 据此设置上下文窗口和输出上限、决定要不要带图片。
type RunModel struct {
	Name          string `json:"name"`
	ContextWindow int    `json:"context_window"`
	MaxTokens     int    `json:"max_tokens"`
	Vision        bool   `json:"vision"`
}

// RunSetup 是一次运行（或续跑）需要的、由平台决定的全部内容：模型、系统提示词、可用的工具、版本。
// 令牌模式下它们是启动参数的一部分（runtime.go），独立的 Agent 服务通过 PrepareRun 取。
type RunSetup struct {
	Model         RunModel   `json:"model"`
	SystemPrompt  string     `json:"system_prompt"`
	Tools         []ToolDecl `json:"tools"`
	DisabledTools []string   `json:"disabled_tools,omitempty"` // 配置里停用的工具名（Agent 自己的工具靠它决定启用与否）
	// FirstMessage 是带画布目录和运行参数的用户消息；请求里没带 message（续跑）时为空
	FirstMessage string      `json:"first_message,omitempty"`
	Harness      HarnessInfo `json:"harness"`
}

// HarnessInfo 是运行创建时记下的版本：提示词版本、工具声明哈希、构建号，以及合并成的短哈希（版本对比按它分组）。
type HarnessInfo struct {
	PromptVersion int    `json:"prompt_version"`
	ToolsHash     string `json:"tools_hash"`
	BuildSHA      string `json:"build_sha"`
	Key           string `json:"key"`
}

// PrepareRequest 是运行准备的请求。
type PrepareRequest struct {
	Message  string `json:"message"`   // 用户的消息；不传表示续跑，不需要第一条消息
	MaxSteps int    `json:"max_steps"` // 写进第一条消息的步数上限，不传取默认 40
}

// runSetup 组装模型、系统提示词和工具声明。模型不可用、渠道 Key 没设置、模型配置里没有上下文规格都按「Agent 模型不可用」处理。
// 技能目录读不出来（数据库故障）不拦住运行，退化成没有目录，模型仍能 skill_search / skill_read。
func (b *AgentBridge) runSetup(ctx context.Context, run *model.AgentRun, modelKey, mode string, disabled []string) (*RunSetup, error) {
	snap, _, err := b.resolveModel(ctx, modelKey)
	if err != nil {
		return nil, err
	}
	caps := snap.Model.Capabilities
	if caps.Context == nil {
		return nil, errcode.ErrAgentModelNA
	}
	var catalog []prompts.SkillItem
	if brief, err := b.skillLib().Enabled(ctx); err != nil {
		logger.Warn("读取技能目录失败，本次运行的提示词不带技能目录", zap.Error(err), zap.Uint64("run_id", run.ID))
	} else {
		for _, sk := range brief {
			catalog = append(catalog, prompts.SkillItem{Name: sk.Name, Description: sk.Description})
		}
	}
	tools := AgentToolDecls(mode, caps.Vision, caps.Video)
	if len(disabled) > 0 {
		tools = slices.DeleteFunc(tools, func(d ToolDecl) bool { return slices.Contains(disabled, d.Name) })
	}
	h := b.d.Agent.harness
	return &RunSetup{
		Model:         RunModel{Name: snap.Model.Label, ContextWindow: caps.Context.Window, MaxTokens: caps.Context.Output, Vision: caps.Vision},
		SystemPrompt:  prompts.SystemWithSkills(mode, catalog),
		Tools:         tools,
		DisabledTools: slices.Clone(disabled),
		Harness:       HarnessInfo{PromptVersion: h.PromptVersion, ToolsHash: h.ToolsHash, BuildSHA: h.BuildSHA, Key: h.Key()},
	}, nil
}

// PrepareRun 给 Agent 服务一次运行（或续跑）需要的、由平台决定的内容。不读写任何记录、不发事件。
// 带 message 时还会读画布目录（选中的节点优先）拼出第一条用户消息，规则和令牌模式的 Start 一致。
func (b *AgentBridge) PrepareRun(ctx context.Context, rc RunContext, req PrepareRequest) (*RunSetup, error) {
	if err := rc.validate(); err != nil {
		return nil, err
	}
	if req.MaxSteps < 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("max_steps 不能是负数")
	}
	run := rc.run()
	setup, err := b.runSetup(ctx, run, rc.ModelKey, firstNonEmpty(rc.Mode, model.AgentModeAll), b.disabledTools)
	if err != nil {
		return nil, err
	}
	if req.Message == "" {
		return setup, nil
	}
	cat, err := b.d.Canvas.Catalog(ctx, run, canvasgraph.CatalogOptions{Priority: rc.Selection, Selected: rc.Selection})
	if err != nil {
		return nil, err
	}
	setup.FirstMessage, err = firstUserMessage(req.Message, cat, orDefault(req.MaxSteps, defaultRunMaxSteps))
	return setup, err
}

func orDefault(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

// firstUserMessage 拼出带画布目录和运行参数的用户消息（令牌模式的 Start 和 PrepareRun 共用）。
func firstUserMessage(text string, cat *canvasgraph.Catalog, maxSteps int) (string, error) {
	catJSON, err := json.Marshal(cat)
	if err != nil {
		return "", err
	}
	return prompts.User(text, string(catJSON), prompts.RunInfo{MaxSteps: maxSteps}), nil
}
