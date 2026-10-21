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
	if len(f.Events) > 0 {
		sb.WriteString("\n## 时间线\n")
		writeTimeline(&sb, f)
	}
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

// writeTimeline 按事件序号写时间线。工具调用的参数、结果和耗时来自 agent_tool_calls，按调用 id 对上事件；
// 被拒绝的调用（选错工具、模式不允许）只有 tool.end 没有 tool.start，也要写出来。
func writeTimeline(sb *strings.Builder, f *model.AgentExportFacts) {
	calls := map[string]*model.AgentToolCall{}
	for i := range f.ToolCalls {
		calls[f.ToolCalls[i].ToolCallID] = &f.ToolCalls[i]
	}
	rendered := map[string]bool{}
	n := 0
	for _, ev := range f.Events {
		var p map[string]any
		_ = json.Unmarshal(ev.PayloadJSON, &p)
		if ev.Type == "tool.start" || ev.Type == "tool.end" {
			id := str(p["id"])
			if rendered[id] {
				continue // tool.end 的内容已经并进了对应的 tool.start
			}
			rendered[id] = true
			indent, prefix := "", ""
			if str(p["parent_id"]) != "" {
				indent, prefix = "   ", "↳ "
			}
			writeToolCall(sb, &n, indent, prefix, p, calls[id])
			continue
		}
		text, extra, ok := eventLine(ev.Type, p)
		if !ok {
			continue
		}
		n++
		fmt.Fprintf(sb, "#%d %s\n", n, text)
		if extra != "" {
			sb.WriteString(extra + "\n")
		}
	}
}

// eventLine 把一个非工具事件写成时间线上的一行（以及可选的缩进补充行）；不需要出现在时间线上的事件返回 ok=false。
func eventLine(typ string, p map[string]any) (line, extra string, ok bool) {
	switch typ {
	case "message.user":
		if b, _ := p["steer"].(bool); b {
			return "[用户插话] " + clipBytes(str(p["text"]), mdSegmentBytes), "", true
		}
		return "[用户] " + clipBytes(str(p["text"]), mdSegmentBytes), "", true
	case "message.done":
		partial := ""
		if b, _ := p["partial"].(bool); b {
			partial = "（半截回答，被中断）"
		}
		if th := str(p["thinking"]); th != "" {
			extra = "   思考：" + clipThinking(th, mdThinkingRunes)
		}
		return "[模型回复]" + partial + " " + clipBytes(str(p["text"]), mdSegmentBytes), extra, true
	case "approval.created":
		return "[审批请求] " + compactPayload(p, "kind", "status", "quote_credits"), "", true
	case "approval.decided":
		return "[审批结果] " + compactPayload(p, "kind", "status", "decision"), "", true
	case "plan.updated":
		return "[计划] " + describePlan(p), "", true
	case "run.undone":
		return "[用户撤销本轮] " + compactPayload(p), "", true
	case "feedback.set":
		return "[用户反馈] " + describeFeedbackEvent(p), "", true
	case "feedback.cleared":
		return "[用户取消了反馈]", "", true
	case "runtime.notice":
		return "[运行时提示] " + clipBytes(str(p["text"]), mdSegmentBytes), "", true
	case "run.status":
		return "[状态] " + str(p["status"]), "", true
	case "run.usage":
		return "", "", false // 用量变化太频繁，且摘要里已有合计
	}
	return "[" + typ + "] " + compactPayload(p), "", true
}

// writeToolCall 写一次工具调用：名称、耗时、成败，再接参数、结果和摘要。
func writeToolCall(sb *strings.Builder, n *int, indent, prefix string, ev map[string]any, c *model.AgentToolCall) {
	*n++
	name := str(ev["name"])
	isErr, _ := ev["is_error"].(bool)
	summary := str(ev["summary"])
	dur := ""
	if c != nil {
		name, isErr = c.Name, c.IsError
		if summary == "" {
			summary = c.Summary
		}
		if c.EndedAt != nil && c.DurationMs > 0 {
			dur = fmt.Sprintf(" · %dms", c.DurationMs)
		}
	}
	status := ""
	if isErr {
		status = " · ✘ 报错"
	}
	fmt.Fprintf(sb, "%s#%d %s[工具调用] %s%s%s\n", indent, *n, prefix, name, dur, status)
	pad := indent + "   "
	switch {
	case c == nil:
		fmt.Fprintf(sb, "%s参数和结果：未记录参数（记录这次调用时写库失败，或运行的时间早于观测上线）\n", pad)
	case c.PurgedAt != nil:
		fmt.Fprintf(sb, "%s参数和结果：已过保留期（%s 清理），摘要和耗时仍在\n", pad, c.PurgedAt.UTC().Format("2006-01-02"))
	default:
		if len(c.ArgsJSON) > 0 {
			fmt.Fprintf(sb, "%s参数：%s\n", pad, clipBytes(compactJSON(c.ArgsJSON), mdSegmentBytes))
		}
		if c.ResultText != nil {
			res, cut := TruncateBytes(*c.ResultText, mdSegmentBytes)
			note := ""
			if cut || c.ResultTruncated {
				note = "（已截断，完整内容见 JSON 导出）"
			}
			fmt.Fprintf(sb, "%s结果：%s%s\n", pad, res, note)
		}
	}
	if summary != "" {
		fmt.Fprintf(sb, "%s摘要：%s\n", pad, summary)
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
