package agent

import (
	"context"
	"encoding/json"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
)

// maxToolArgsBytes 是单次工具调用参数完整留存的上限；超过的只留一个带 _truncated 标记的摘要。
// 参数里的大头通常是模型整段写出的剧本或提示词，上限放宽到 64KB 足够覆盖正常使用。
const maxToolArgsBytes = 64 << 10

// toolArgsHeadBytes 是超大参数的摘要里保留的开头长度。
const toolArgsHeadBytes = 2 << 10

// ToolCallResult 是一次工具调用结束时要补记的内容，见 model.AgentToolCallResult。
type ToolCallResult = model.AgentToolCallResult

// ToolCallRecorder 逐条留存工具调用的参数、结果和耗时，供事后分析。
// 它是观测数据：实现出错时调用方只记日志，不能影响用户的运行。
type ToolCallRecorder interface {
	// StartToolCall 记一条刚开始的调用；同一运行里 tool_call_id 重复时不覆盖，也不是错误。
	StartToolCall(ctx context.Context, c *model.AgentToolCall) error
	// FinishToolCall 补记调用的结果；找不到对应的开始记录时不是错误。
	FinishToolCall(ctx context.Context, runID uint64, toolCallID string, res ToolCallResult) error
}

// SetToolRecorder 绑定工具调用记录器。装配阶段调用，运行开始前完成，之后不再改；不绑定就不记录。
func (b *AgentBridge) SetToolRecorder(r ToolCallRecorder) { b.toolRec = r }

// TruncateToolResult 把工具结果截到 model.MaxToolResultBytes 以内（不切坏 UTF-8 字符），返回截断后的文本和是否截断过。
func TruncateToolResult(s string) (string, bool) {
	if len(s) <= model.MaxToolResultBytes {
		return s, false
	}
	cut := model.MaxToolResultBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut-- // 回退到字符边界
	}
	return s[:cut], true
}

// ToolArgsForRecord 把工具参数转成可以写进 jsonb 列的形式：
// 没有参数或不是合法 JSON 记 NULL；超过 maxToolArgsBytes 的换成 {"_truncated":true,"_bytes":N,"_head":"开头"}，完整内容不留。
func ToolArgsForRecord(args []byte) datatypes.JSON {
	if len(args) == 0 || !json.Valid(args) {
		return nil
	}
	if len(args) <= maxToolArgsBytes {
		return datatypes.JSON(args)
	}
	head, _ := TruncateBytes(string(args), toolArgsHeadBytes)
	out, err := json.Marshal(map[string]any{"_truncated": true, "_bytes": len(args), "_head": head})
	if err != nil {
		return nil // 摘要都编码不了就不记，参数本来就是观测数据
	}
	return out
}

// TruncateBytes 把字符串截到 n 字节以内（不切坏 UTF-8 字符），返回截断后的文本和是否截断过。
func TruncateBytes(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}

// recordToolStart 记一条刚开始的工具调用。失败只记日志：观测数据丢一条，不该让用户的运行失败。
func (b *AgentBridge) recordToolStart(ctx context.Context, run *model.AgentRun, id, parentID, name string, args []byte, at time.Time) {
	if b.toolRec == nil {
		return
	}
	c := &model.AgentToolCall{RunID: run.ID, SessionID: run.SessionID, ToolCallID: id, ParentID: parentID, Name: name, ArgsJSON: ToolArgsForRecord(args), StartedAt: at}
	if err := b.toolRec.StartToolCall(ctx, c); err != nil {
		logger.Warn("记录工具调用失败", zap.Error(err), zap.Uint64("run_id", run.ID), zap.String("tool", name))
	}
}

// recordToolEnd 补记工具调用的结果。用不会被取消的上下文：用户停止运行时请求的上下文已取消，结束时间照样要记下。
func (b *AgentBridge) recordToolEnd(ctx context.Context, run *model.AgentRun, id string, res *ToolResult, started time.Time) {
	if b.toolRec == nil || res == nil {
		return
	}
	now := b.d.Now()
	text, cut := TruncateToolResult(res.Content)
	summary := truncateRunes(firstNonEmpty(res.summary, summarizeError(res)), maxToolSummaryRunes)
	r := ToolCallResult{ResultText: text, Truncated: cut, IsError: res.IsError, Summary: summary, EndedAt: now, DurationMs: int(now.Sub(started).Milliseconds())}
	if err := b.toolRec.FinishToolCall(context.WithoutCancel(ctx), run.ID, id, r); err != nil {
		logger.Warn("补记工具调用结果失败", zap.Error(err), zap.Uint64("run_id", run.ID), zap.String("call_id", id))
	}
}

// maxToolSummaryRunes 是 agent_tool_calls.summary 的列宽（字符数）。
const maxToolSummaryRunes = 255
