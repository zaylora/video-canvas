package agent

import (
	"context"
	"encoding/json"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// 报备字段的长度上限：它们来自 Node，只用来显示，截短免得被撑爆。
const (
	maxReportLabelRunes   = 20
	maxReportVerbRunes    = 10
	maxReportSummaryRunes = 200
	maxReportIDLen        = 128
	maxReportNameLen      = 64
)

// AgentToolEvent 是 Agent 自己的工具（不经桥的 /tool，如 codemode）开始或结束时的报备。
// 工具名、界面名称、动词都由 Agent 带来，Go 不查注册表，只原样记进事件。
type AgentToolEvent struct {
	Phase      string // start 或 end
	ToolCallID string
	Name       string
	Label      string
	Verb       string
	IsError    bool   // 仅 end
	Summary    string // 仅 end，时间线上显示的一行摘要

	Args   json.RawMessage // 仅 start，工具的参数（如 codemode 的脚本），留存给事后分析；可为空
	Result string          // 仅 end，工具返回给模型的文本，留存给事后分析；可为空
}

// AgentToolEventResult 是 start 的结果：不允许时 Agent 不能运行这个工具。
type AgentToolEventResult struct {
	Allowed   bool   `json:"allowed"`
	Content   string `json:"content,omitempty"`
	Terminate bool   `json:"terminate"` // 让循环停下（步数用尽，运行已暂停）
}

// ReportAgentTool 记录 Agent 工具的开始和结束，让前端时间线里有这一步。开始时计 1 步、检查步数上限（和普通工具一样）。
// 工具名不能和 Go 的工具重名：否则 Agent 能借报备冒充 Go 的工具。
func (b *AgentBridge) ReportAgentTool(ctx context.Context, token string, ev AgentToolEvent) (*AgentToolEventResult, error) {
	sess, err := b.session(token)
	if err != nil {
		return nil, err
	}
	run, err := b.activeRun(ctx, sess)
	if err != nil {
		return nil, err
	}
	if ev.ToolCallID == "" || len(ev.ToolCallID) > maxReportIDLen || ev.Name == "" || len(ev.Name) > maxReportNameLen {
		return nil, errcode.ErrInvalidParams.WithMsg("工具调用 id 和工具名不能为空，也不能太长")
	}
	if findTool(ev.Name) != nil {
		return nil, errcode.ErrInvalidParams.WithMsg("工具名和 Go 下发的工具重名")
	}
	data := map[string]any{"id": ev.ToolCallID, "name": ev.Name, "label": truncateRunes(ev.Label, maxReportLabelRunes), "verb": truncateRunes(ev.Verb, maxReportVerbRunes)}
	switch ev.Phase {
	case "start":
		if run.Steps >= run.MaxSteps {
			b.pauseRun(ctx, run, model.RunStepLimit)
			return &AgentToolEventResult{Allowed: false, Terminate: true, Content: "已达到本轮的步数上限，运行已暂停，等待用户继续。"}, nil
		}
		if err := b.d.Repo.AddRunUsage(ctx, run.ID, 1, 0); err != nil {
			return nil, err
		}
		b.d.Agent.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "tool.start", data)
		b.recordToolStart(ctx, run, ev.ToolCallID, "", ev.Name, ev.Args, b.d.Now())
	case "end":
		data["is_error"], data["summary"] = ev.IsError, truncateRunes(ev.Summary, maxReportSummaryRunes)
		b.d.Agent.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "tool.end", data)
		// 耗时留给存储按开始时间推算：这里不知道 Node 那边的开始时刻
		b.recordToolEnd(ctx, run, ev.ToolCallID, &ToolResult{Content: ev.Result, IsError: ev.IsError, summary: data["summary"].(string)}, b.d.Now())
	default:
		return nil, errcode.ErrInvalidParams.WithMsg("phase 只能是 start 或 end")
	}
	return &AgentToolEventResult{Allowed: true}, nil
}
