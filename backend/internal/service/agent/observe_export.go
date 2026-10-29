package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
)

// Markdown 导出的截断规则：单段最多 2KB，思考摘要取前 600 字。完整内容在同一运行的 JSON 导出里。
const (
	mdSegmentBytes  = 2 << 10
	mdThinkingRunes = 600
	mdPayloadBytes  = 600 // 审批、计划等其他事件的载荷摘要
)

// feedbackReasonLabels 是点踩原因标签的中文名，与前端展示一致。
var feedbackReasonLabels = map[string]string{
	model.FeedbackReasonMisread:   "没理解我的需求",
	model.FeedbackReasonWrongEdit: "画布改错了",
	model.FeedbackReasonSlow:      "步骤太多或太慢",
	model.FeedbackReasonBadOutput: "生成结果不好",
	model.FeedbackReasonOther:     "其他",
}

// RenderRunMarkdown 把一次运行渲染成给 AI 读的 Markdown：
// 头部（运行、Harness 版本、信号）→ 用户请求 → 上下文 → 时间线（步骤编号 #n，判定时可以引用）→ 画布改动 → 结束。
// 顺序按「用户要什么 → Agent 怎么做 → 结果 → 用户满不满意」。没有内容的章节不输出；工具参数和结果每段最多 2KB。
func RenderRunMarkdown(f *model.AgentExportFacts) string {
	var sb strings.Builder
	writeMarkdownHeader(&sb, f)

	if req := firstUserRequest(f.Events); req != "" {
		sb.WriteString("\n## 用户请求\n" + req + "\n")
	}
	if len(f.Context) > 0 {
		sb.WriteString("\n## 上下文（同一会话里更早的轮次）\n")
		for _, ev := range f.Context {
			text := eventText(ev)
			if text == "" {
				continue
			}
			who := "用户"
			if ev.Type == "message.done" {
				who = "Agent"
			}
			sb.WriteString("- " + who + "：" + clipBytes(text, mdSegmentBytes) + "\n")
		}
	}
	if steps := BuildTimeline(f); len(steps) > 0 {
		sb.WriteString("\n## 时间线\n")
		writeTimeline(&sb, steps)
	}
	writeModelCalls(&sb, f)
	if len(f.Mutations) > 0 {
		sb.WriteString("\n## 画布改动\n")
		for _, m := range f.Mutations {
			sb.WriteString(describeMutation(m) + "\n")
		}
	}
	sb.WriteString("\n## 结束\n" + describeEnding(f) + "\n")
	return sb.String()
}

func writeMarkdownHeader(sb *strings.Builder, f *model.AgentExportFacts) {
	run, d := &f.Run, &f.Digest
	harness := fmt.Sprintf("{ prompt_version: %d, harness_key: %s, tools_hash: %s, build_sha: %s, skills: %s }",
		run.PromptVersion, run.HarnessKey, run.ToolsHash, run.BuildSHA, skillsInline(d.Skills))
	sb.WriteString("---\n")
	fmt.Fprintf(sb, "run_id: %s\n", idcodec.Encode(run.ID))
	fmt.Fprintf(sb, "time: %s\n", run.CreatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(sb, "user: %d\n", run.UserID)
	fmt.Fprintf(sb, "mode: %s\n", run.Mode)
	fmt.Fprintf(sb, "model: %s\n", d.ModelKey)
	fmt.Fprintf(sb, "harness: %s\n", harness)
	// 摘要还没生成时（运行刚创建）步数和积分取运行本身的
	dur := ""
	if run.EndedAt != nil {
		dur = run.EndedAt.Sub(run.CreatedAt).Round(time.Second).String()
		dur = strings.NewReplacer("h0m0s", "h", "m0s", "m").Replace(dur)
	}
	fmt.Fprintf(sb, "status: %s\nsteps: %d\ncredits: %d\nduration: %s\n", run.Status, run.Steps, run.SpentCredits, dur)
	fb, reasons := 0, []string{}
	if f.Feedback != nil {
		fb = f.Feedback.Rating
		reasons = decodeReasons(f.Feedback.Reasons)
	}
	fmt.Fprintf(sb, "signals: { feedback: %d, reasons: [%s], undone: %t, approvals_rejected: %d, steer_count: %d, tool_errors: %d }\n",
		fb, strings.Join(reasons, ", "), d.Undone, d.ApprovalsRejected, d.SteerCount, d.ToolErrors)
	sb.WriteString("---\n")
}

// skillsInline 把摘要里的技能版本写成 { a: v3, b: builtin }。
func skillsInline(raw []byte) string {
	var m map[string]string
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil || len(m) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + m[k]
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func decodeReasons(raw []byte) []string {
	var out []string
	_ = json.Unmarshal(raw, &out)
	return out
}

// firstUserRequest 取本轮第一条非插话的用户消息。
func firstUserRequest(events []model.AgentEvent) string {
	for _, ev := range events {
		if ev.Type != "message.user" {
			continue
		}
		var p struct {
			Text  string `json:"text"`
			Steer bool   `json:"steer"`
		}
		if json.Unmarshal(ev.PayloadJSON, &p) == nil && !p.Steer && p.Text != "" {
			return p.Text
		}
	}
	return ""
}

// eventText 取 message.user / message.done 事件里的正文。
func eventText(ev model.AgentEvent) string {
	var p struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(ev.PayloadJSON, &p)
	return p.Text
}

// TimelineStep 是时间线上的一步：Markdown 导出和后台详情抽屉都从同一份步骤列表生成，所以两边的内容、编号、截断规则天然一致。
type TimelineStep struct {
	N        int           `json:"n"`                  // 步骤编号 #n，分析时用它引用证据
	Kind     string        `json:"kind"`               // 见 timelineKind*
	Label    string        `json:"label"`              // 这一步的名称，如「用户」「模型回复」「工具调用」
	Text     string        `json:"text"`               // 正文（已按 2KB 截断）；工具调用为空，内容在 Tool 里
	Thinking string        `json:"thinking,omitempty"` // 模型回复的思考摘要，前 600 字
	Partial  bool          `json:"partial,omitempty"`  // 半截回答，被中断
	Nested   bool          `json:"nested,omitempty"`   // codemode 脚本里发起的子调用
	At       *time.Time    `json:"at,omitempty"`       // 事件时间
	Tool     *TimelineTool `json:"tool,omitempty"`     // 工具调用的细节
}

// 时间线步骤的种类，前端据此决定图标和颜色。
const (
	TimelineUser     = "user"      // 用户消息
	TimelineSteer    = "steer"     // 用户插话
	TimelineAssist   = "assistant" // 模型回复
	TimelineKindTool = "tool"      // 工具调用
	TimelineApproval = "approval"  // 审批请求和结果
	TimelinePlan     = "plan"      // 计划更新
	TimelineUndo     = "undo"      // 用户撤销本轮
	TimelineFeedback = "feedback"  // 用户反馈
	TimelineNotice   = "notice"    // 运行时提示
	TimelineStatus   = "status"    // 运行状态
	TimelineOther    = "other"     // 其他事件
)

// TimelineTool 是工具调用这一步的细节。参数和结果每段最多 2KB，完整内容在 JSON 导出的 tool_calls 里。
type TimelineTool struct {
	Name       string     `json:"name"`                  // 工具名
	DurationMs int        `json:"duration_ms,omitempty"` // 耗时（毫秒），没有记录为 0
	IsError    bool       `json:"is_error"`              // 工具没做成
	Summary    string     `json:"summary,omitempty"`     // 一行摘要
	Args       string     `json:"args,omitempty"`        // 参数（压缩 JSON，已截断）
	Result     string     `json:"result,omitempty"`      // 返回给模型的文本（已截断）
	Truncated  bool       `json:"truncated,omitempty"`   // 参数或结果被截断过
	Recorded   bool       `json:"recorded"`              // 有这次调用的记录；为 false 表示观测写库失败过，或运行早于观测上线
	PurgedAt   *time.Time `json:"purged_at,omitempty"`   // 参数和结果已过保留期被清理的时间
}

// BuildTimeline 把一次运行的落库事件整理成时间线步骤。工具调用的参数、结果和耗时来自 agent_tool_calls，按调用 id 对上事件；
// 被拒绝的调用（选错工具、模式不允许）只有 tool.end 没有 tool.start，也要出现。
func BuildTimeline(f *model.AgentExportFacts) []TimelineStep {
	calls := map[string]*model.AgentToolCall{}
	for i := range f.ToolCalls {
		calls[f.ToolCalls[i].ToolCallID] = &f.ToolCalls[i]
	}
	seen := map[string]bool{}
	steps := make([]TimelineStep, 0, len(f.Events))
	for i := range f.Events {
		ev := &f.Events[i]
		var p map[string]any
		_ = json.Unmarshal(ev.PayloadJSON, &p)
		var step TimelineStep
		if ev.Type == "tool.start" || ev.Type == "tool.end" {
			id := str(p["id"])
			if seen[id] {
				continue // tool.end 的内容已经并进了对应的 tool.start
			}
			seen[id] = true
			step = toolStep(p, calls[id])
		} else {
			var ok bool
			if step, ok = eventStep(ev.Type, p); !ok {
				continue
			}
		}
		at := ev.CreatedAt
		if !at.IsZero() {
			step.At = &at
		}
		step.N = len(steps) + 1
		steps = append(steps, step)
	}
	return steps
}

// eventStep 把一个非工具事件转成时间线上的一步；不需要出现在时间线上的事件返回 ok=false。
func eventStep(typ string, p map[string]any) (TimelineStep, bool) {
	switch typ {
	case "message.user":
		if b, _ := p["steer"].(bool); b {
			return TimelineStep{Kind: TimelineSteer, Label: "用户插话", Text: clipBytes(str(p["text"]), mdSegmentBytes)}, true
		}
		return TimelineStep{Kind: TimelineUser, Label: "用户", Text: clipBytes(str(p["text"]), mdSegmentBytes)}, true
	case "message.done":
		partial, _ := p["partial"].(bool)
		st := TimelineStep{Kind: TimelineAssist, Label: "模型回复", Text: clipBytes(str(p["text"]), mdSegmentBytes), Partial: partial}
		if th := str(p["thinking"]); th != "" {
			st.Thinking = clipThinking(th, mdThinkingRunes)
		}
		return st, true
	case "approval.created":
		return TimelineStep{Kind: TimelineApproval, Label: "审批请求", Text: compactPayload(p, "kind", "status", "quote_credits")}, true
	case "approval.decided":
		return TimelineStep{Kind: TimelineApproval, Label: "审批结果", Text: compactPayload(p, "kind", "status", "decision")}, true
	case "plan.updated":
		return TimelineStep{Kind: TimelinePlan, Label: "计划", Text: describePlan(p)}, true
	case "run.undone":
		return TimelineStep{Kind: TimelineUndo, Label: "用户撤销本轮", Text: compactPayload(p)}, true
	case "feedback.set":
		return TimelineStep{Kind: TimelineFeedback, Label: "用户反馈", Text: describeFeedbackEvent(p)}, true
	case "feedback.cleared":
		return TimelineStep{Kind: TimelineFeedback, Label: "用户取消了反馈"}, true
	case "runtime.notice":
		return TimelineStep{Kind: TimelineNotice, Label: "运行时提示", Text: clipBytes(str(p["text"]), mdSegmentBytes)}, true
	case "run.status":
		return TimelineStep{Kind: TimelineStatus, Label: "状态", Text: str(p["status"])}, true
	case "run.usage":
		return TimelineStep{}, false // 用量变化太频繁，且摘要里已有合计
	case "watch.updated":
		return TimelineStep{}, false // 后台等待的进度每个任务结束都变一次，只是界面状态；交付给 Agent 的那一刻由 tasks.settled 标出
	case "tasks.settled":
		return TimelineStep{Kind: TimelineNotice, Label: "系统唤醒（生成全部结束）", Text: compactPayload(p, "batch_ids", "steer")}, true
	}
	return TimelineStep{Kind: TimelineOther, Label: typ, Text: compactPayload(p)}, true
}

// toolStep 把一次工具调用转成时间线上的一步：名称、耗时、成败、参数、结果和摘要。
func toolStep(ev map[string]any, c *model.AgentToolCall) TimelineStep {
	name := str(ev["name"])
	isErr, _ := ev["is_error"].(bool)
	t := &TimelineTool{Name: name, IsError: isErr, Summary: str(ev["summary"]), Recorded: c != nil}
	if c != nil {
		t.Name, t.IsError = c.Name, c.IsError
		if t.Summary == "" {
			t.Summary = c.Summary
		}
		if c.EndedAt != nil && c.DurationMs > 0 {
			t.DurationMs = c.DurationMs
		}
		t.PurgedAt = c.PurgedAt
		if c.PurgedAt == nil {
			if len(c.ArgsJSON) > 0 {
				args, cut := TruncateBytes(compactJSON(c.ArgsJSON), mdSegmentBytes)
				t.Args, t.Truncated = args, cut
			}
			if c.ResultText != nil {
				res, cut := TruncateBytes(*c.ResultText, mdSegmentBytes)
				t.Result = res
				t.Truncated = t.Truncated || cut || c.ResultTruncated
			}
		}
	}
	return TimelineStep{Kind: TimelineKindTool, Label: "工具调用", Nested: str(ev["parent_id"]) != "", Tool: t}
}

// writeTimeline 把时间线步骤写成 Markdown。
func writeTimeline(sb *strings.Builder, steps []TimelineStep) {
	for _, st := range steps {
		if st.Tool != nil {
			writeToolStep(sb, st)
			continue
		}
		line := "[" + st.Label + "]"
		if st.Partial {
			line += "（半截回答，被中断）"
		}
		if st.Text != "" {
			line += " " + st.Text
		}
		fmt.Fprintf(sb, "#%d %s\n", st.N, line)
		if st.Thinking != "" {
			fmt.Fprintf(sb, "   思考：%s\n", st.Thinking)
		}
	}
}

// writeToolStep 写一次工具调用：名称、耗时、成败，再接参数、结果和摘要。
func writeToolStep(sb *strings.Builder, st TimelineStep) {
	t := st.Tool
	indent, prefix := "", ""
	if st.Nested {
		indent, prefix = "   ", "↳ "
	}
	dur, status := "", ""
	if t.DurationMs > 0 {
		dur = fmt.Sprintf(" · %dms", t.DurationMs)
	}
	if t.IsError {
		status = " · ✘ 报错"
	}
	fmt.Fprintf(sb, "%s#%d %s[工具调用] %s%s%s\n", indent, st.N, prefix, t.Name, dur, status)
	pad := indent + "   "
	switch {
	case !t.Recorded:
		fmt.Fprintf(sb, "%s参数和结果：未记录参数（记录这次调用时写库失败，或运行的时间早于观测上线）\n", pad)
	case t.PurgedAt != nil:
		fmt.Fprintf(sb, "%s参数和结果：已过保留期（%s 清理），摘要和耗时仍在\n", pad, t.PurgedAt.UTC().Format("2006-01-02"))
	default:
		if t.Args != "" {
			fmt.Fprintf(sb, "%s参数：%s\n", pad, t.Args)
		}
		if t.Result != "" || t.Truncated {
			note := ""
			if t.Truncated {
				note = "（已截断，完整内容见 JSON 导出）"
			}
			fmt.Fprintf(sb, "%s结果：%s%s\n", pad, t.Result, note)
		}
	}
	if t.Summary != "" {
		fmt.Fprintf(sb, "%s摘要：%s\n", pad, t.Summary)
	}
}

// describeMutation 写一条画布改动：种类、改动条数、是否被撤销。
func describeMutation(m model.AgentMutation) string {
	if m.Kind == model.MutationUndo {
		return fmt.Sprintf("- #%d undo · 撤销本轮的改动", m.Seq)
	}
	var changes []json.RawMessage
	_ = json.Unmarshal(m.ChangesJSON, &changes)
	s := fmt.Sprintf("- #%d %s · %d 处改动", m.Seq, m.Kind, len(changes))
	if m.ToolCallID != "" {
		s += " · 来自 " + m.ToolCallID
	}
	if m.UndoneAt != nil {
		s += " · 已撤销"
	}
	return s
}

// describeEnding 写「结束」一节：终态、错误、最后一条回复和用户反馈。
func describeEnding(f *model.AgentExportFacts) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "状态 %s", f.Run.Status)
	if f.Run.ErrorCode != "" || f.Run.ErrorMessage != "" {
		fmt.Fprintf(&sb, "；错误 %s %s", f.Run.ErrorCode, f.Run.ErrorMessage)
	}
	for i := len(f.Events) - 1; i >= 0; i-- {
		if f.Events[i].Type == "message.done" {
			if t := eventText(f.Events[i]); t != "" {
				sb.WriteString("\nAgent 最后的回复：" + clipBytes(t, mdSegmentBytes))
			}
			break
		}
	}
	sb.WriteString("\n用户反馈：")
	if f.Feedback == nil {
		sb.WriteString("无")
		return sb.String()
	}
	sb.WriteString(describeFeedback(f.Feedback.Rating, decodeReasons(f.Feedback.Reasons), f.Feedback.Comment))
	return sb.String()
}

func describeFeedbackEvent(p map[string]any) string {
	rating := 0
	if v, ok := p["rating"].(float64); ok {
		rating = int(v)
	}
	var reasons []string
	if arr, ok := p["reasons"].([]any); ok {
		for _, r := range arr {
			reasons = append(reasons, str(r))
		}
	}
	return describeFeedback(rating, reasons, str(p["comment"]))
}

// describeFeedback 把评价写成「👎 没理解我的需求、步骤太多或太慢；「原话」」。
func describeFeedback(rating int, reasons []string, comment string) string {
	if rating == model.FeedbackUp {
		if comment != "" {
			return "👍；「" + comment + "」"
		}
		return "👍"
	}
	s := "👎"
	labels := make([]string, 0, len(reasons))
	for _, r := range reasons {
		if l, ok := feedbackReasonLabels[r]; ok {
			labels = append(labels, l)
		} else {
			labels = append(labels, r)
		}
	}
	if len(labels) > 0 {
		s += " " + strings.Join(labels, "、")
	}
	if comment != "" {
		s += "；「" + comment + "」"
	}
	return s
}

// describePlan 把计划写成「N 步：标题1 / 标题2」。
func describePlan(p map[string]any) string {
	steps, _ := p["steps"].([]any)
	titles := make([]string, 0, len(steps))
	for _, s := range steps {
		m, _ := s.(map[string]any)
		t := str(m["title"])
		if st := str(m["status"]); st != "" {
			t += "(" + st + ")"
		}
		titles = append(titles, t)
	}
	return fmt.Sprintf("%d 步：%s", len(steps), strings.Join(titles, " / "))
}

// compactPayload 把载荷里指定的键写成 k=v 一行；不指定键就写整个载荷的压缩 JSON。
func compactPayload(p map[string]any, keys ...string) string {
	if len(keys) == 0 {
		return clipBytes(compactJSONAny(p), mdPayloadBytes)
	}
	var parts []string
	for _, k := range keys {
		if v, ok := p[k]; ok && v != nil {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	return clipBytes(strings.Join(parts, " "), mdPayloadBytes)
}

func compactJSON(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	return compactJSONAny(v)
}

func compactJSONAny(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// clipBytes 把文本截到 n 字节以内，被截断时在末尾加省略标记。
func clipBytes(s string, n int) string {
	out, cut := TruncateBytes(s, n)
	if cut {
		return out + "…（已截断）"
	}
	return out
}

// clipThinking 把文本截到 n 个字，被截断时加省略号。
func clipThinking(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// mdParamsBytes 是 Markdown 里每次请求的参数摘要最多保留的字节数。
const mdParamsBytes = 300

// writeModelCalls 列出每次模型调用和它实际发给上游的请求摘要：模型当时看到了多少消息和工具、带了什么参数。
// 完整请求体不进 Markdown（动辄几十 KB），要看用 agent-trace pull --with-requests。没有模型调用不输出。
func writeModelCalls(sb *strings.Builder, f *model.AgentExportFacts) {
	if len(f.ModelCalls) == 0 {
		return
	}
	requests := make(map[uint64]*model.AgentModelCallBody, len(f.Requests))
	for i := range f.Requests {
		requests[f.Requests[i].CallID] = &f.Requests[i]
	}
	sb.WriteString("\n## 模型调用\n")
	for i := range f.ModelCalls {
		m := &f.ModelCalls[i]
		purpose := "对话"
		switch m.Purpose {
		case ModelPurposeSummary:
			purpose = "摘要"
		case ModelPurposeTitle:
			purpose = "命名"
		case ModelPurposeActivity:
			purpose = "活动说明"
		}
		fmt.Fprintf(sb, "- 第 %d 次（%s）· %s · 输入 %d / 输出 %d token", i+1, purpose, firstNonEmpty(m.UpstreamModel, m.ModelKey), m.InputTokens, m.OutputTokens)
		if m.StopReason != "" {
			sb.WriteString(" · stop=" + m.StopReason)
		}
		b := requests[m.ID]
		if b == nil {
			sb.WriteString(" · 请求体未留存（过了保留期，或这次调用发生时还没有留存）\n")
			continue
		}
		v := requestSummary(b)
		fmt.Fprintf(sb, " · 请求：消息 %d 条、工具 %d 个、%dKB", v.Messages, v.Tools, (v.Bytes+1023)/1024)
		if len(v.Params) > 0 {
			sb.WriteString("，参数 " + clipBytes(string(v.Params), mdParamsBytes))
		}
		if !v.Kept {
			sb.WriteString("（超过上限，只留摘要）")
		}
		sb.WriteString("\n")
	}
}
