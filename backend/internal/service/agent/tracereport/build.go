package tracereport

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"video-canvas/internal/agent/canvasgraph"
)

// 报告里各项文字的长度上限。
const (
	maxUserTextRunes = 200 // 用户原话
	maxSummaryRunes  = 80  // 时间线里一行摘要
	maxErrKeyRunes   = 60  // 判断「同一条报错」时只看前多少个字
	repeatThreshold  = 3   // 同一条工具报错重复多少次算可疑
)

// Build 按契约 8.4 的结构生成 report.md：标题与概要、可疑信号、时间线、最终画布概要。
// 同样的输入得到逐字节相同的输出；任何一节的数据缺失或损坏都只在那一节说明，不会让整份报告生成失败。
func Build(in Input) string {
	var b strings.Builder
	writeHeader(&b, in.Run, in.Spans)
	writeSignals(&b, in.Run, in.Spans)
	writeTimeline(&b, in.Spans)
	writeCanvas(&b, in.CanvasAfter)
	return b.String()
}

// writeHeader 写标题、概要行、用户原话和警告。
func writeHeader(b *strings.Builder, r Run, spans []Span) {
	var tokIn, tokOut int
	for _, s := range spans {
		if s.Kind == KindChat && s.Usage != nil {
			tokIn += s.Usage.Input
			tokOut += s.Usage.Output
		}
	}
	dur := "未结束"
	if !r.Active {
		dur = formatDuration(int(r.Duration.Milliseconds()))
	}
	model := r.Model
	if model == "" {
		model = "-"
	}
	fmt.Fprintf(b, "# Agent 运行报告 · run %s\n", r.ID)
	fmt.Fprintf(b, "> 终态 %s · prompt v%d · 模型 %s · %d 步 · token 入 %s / 出 %s · 积分 %d · 耗时 %s\n",
		r.Status, r.PromptVersion, model, r.Steps, formatTokens(tokIn), formatTokens(tokOut), r.SpentCredits, dur)
	fmt.Fprintf(b, "> 用户原话：%s\n", clip(oneLine(r.UserText), maxUserTextRunes, true))
	if r.Active {
		b.WriteString("> ⚠ 运行未结束，轨迹不完整\n")
	}
	if !r.TraceComplete {
		b.WriteString("> ⚠ 轨迹不完整：进程异常退出或原文上传失败\n")
	}
	b.WriteString("\n")
}

// writeSignals 写「可疑信号」一节：自动从轨迹里挑出值得先看的异常，没有就写「无」。
func writeSignals(b *strings.Builder, r Run, spans []Span) {
	var signals []string
	// 同一条工具报错重复出现：按「工具名 + 报错开头」归并，按第一次出现的顺序输出
	type errKey struct{ tool, msg string }
	var order []errKey
	counts := map[errKey]int{}
	summaries, truncated := 0, 0
	for _, s := range spans {
		switch {
		case s.Kind == KindExecuteTool && (s.Status == StatusError || (s.IsError != nil && *s.IsError)):
			k := errKey{s.Name, clip(oneLine(resultText(s.Result)), maxErrKeyRunes, false)}
			if counts[k] == 0 {
				order = append(order, k)
			}
			counts[k]++
		case s.Kind == KindChat && s.Purpose == "summary":
			summaries++
		}
		if s.Truncated != nil && *s.Truncated {
			truncated++
		}
	}
	for _, k := range order {
		if counts[k] >= repeatThreshold {
			signals = append(signals, fmt.Sprintf("同一条工具报错重复 %d 次：%s「%s」", counts[k], k.tool, k.msg))
		}
	}
	if summaries > 0 {
		signals = append(signals, fmt.Sprintf("摘要压缩被触发 %d 次", summaries))
	}
	switch r.Status {
	case "step_limit":
		signals = append(signals, "达到步数上限，运行已暂停")
	case "budget_exhausted":
		signals = append(signals, "本轮积分预算用尽，运行已暂停")
	}
	if truncated > 0 {
		signals = append(signals, fmt.Sprintf("%d 个 span 的原文被截断", truncated))
	}
	if !r.TraceComplete {
		signals = append(signals, "轨迹不完整：进程异常退出或原文上传失败")
	}
	b.WriteString("## 可疑信号\n")
	if len(signals) == 0 {
		b.WriteString("- 无\n")
	}
	for _, s := range signals {
		b.WriteString("- " + s + "\n")
	}
	b.WriteString("\n")
}

// writeTimeline 写时间线表。摘要里的竖线要转义、换行折成空格，否则会撑破表格。
func writeTimeline(b *strings.Builder, spans []Span) {
	b.WriteString("## 时间线\n| # | span | 名称 | 耗时 | 状态 | 摘要 |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, s := range spans {
		fmt.Fprintf(b, "| %d | %s | %s | %s | %s | %s |\n", s.Seq, s.Kind, cell(s.Name), formatDuration(s.DurationMS), s.Status,
			cell(clip(oneLine(s.Summary), maxSummaryRunes, false)))
	}
	b.WriteString("\n")
}

// cell 转义表格单元格里的竖线。
func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

// writeCanvas 写最终画布概要：组到成员的树，再列没进组的节点。尺寸用前后端共用的规则（canvasgraph.NodeSize）。
func writeCanvas(b *strings.Builder, payload json.RawMessage) {
	b.WriteString("## 最终画布概要\n")
	if len(payload) == 0 {
		b.WriteString("没有画布快照（轨迹关闭，或这一轮没有留下快照）。\n")
		return
	}
	g, err := canvasgraph.Parse(payload)
	if err != nil {
		b.WriteString("画布快照无法解析：" + clip(oneLine(err.Error()), maxSummaryRunes, false) + "\n")
		return
	}
	groups, members, loose := splitNodes(g)
	fmt.Fprintf(b, "共 %d 个节点（%d 个组）、%d 条连线\n", len(g.Nodes), len(groups), len(g.Edges))
	for _, grp := range groups {
		w, h := canvasgraph.NodeSize(grp, canvasgraph.SizeHint{})
		fmt.Fprintf(b, "- 组 `%s`%s %s×%s · %d 个成员\n", grp.ID(), quoteLabel(grp.Label()), num(w), num(h), len(members[grp.ID()]))
		for _, n := range members[grp.ID()] {
			writeNode(b, "  ", n)
		}
	}
	if len(loose) > 0 {
		b.WriteString("- 未进组的节点\n")
		for _, n := range loose {
			writeNode(b, "  ", n)
		}
	}
}

// splitNodes 把节点分成组、组的成员和未进组的节点。父组不存在的节点算未进组。
func splitNodes(g *canvasgraph.Graph) (groups []canvasgraph.Node, members map[string][]canvasgraph.Node, loose []canvasgraph.Node) {
	members = map[string][]canvasgraph.Node{}
	isGroup := map[string]bool{}
	for _, n := range g.Nodes {
		if n.IsGroup() {
			groups = append(groups, n)
			isGroup[n.ID()] = true
		}
	}
	for _, n := range g.Nodes {
		switch {
		case n.IsGroup():
		case isGroup[n.ParentID()]:
			members[n.ParentID()] = append(members[n.ParentID()], n)
		default:
			loose = append(loose, n)
		}
	}
	return groups, members, loose
}

// writeNode 写一个普通节点：种类、id、标题、尺寸、有没有提示词。
func writeNode(b *strings.Builder, indent string, n canvasgraph.Node) {
	w, h := canvasgraph.NodeSize(n, canvasgraph.SizeHint{})
	has := "无"
	if hasPrompt(n) {
		has = "有"
	}
	fmt.Fprintf(b, "%s- %s `%s`%s %s×%s · 提示词：%s\n", indent, kindLabel(n.Kind()), n.ID(), quoteLabel(n.Label()), num(w), num(h), has)
}

// hasPrompt 判断节点有没有提示词：图片、视频看 prompt（节点上或 params 里），文本节点看正文。
func hasPrompt(n canvasgraph.Node) bool {
	d := n.Data()
	if s, _ := d["prompt"].(string); strings.TrimSpace(s) != "" {
		return true
	}
	if p, _ := d["params"].(map[string]any); p != nil {
		if s, _ := p["prompt"].(string); strings.TrimSpace(s) != "" {
			return true
		}
	}
	if n.Kind() == canvasgraph.KindScript {
		if s, _ := d["text"].(string); strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// kindLabel 是节点种类的中文名，不认识的种类原样显示。
func kindLabel(kind string) string {
	switch kind {
	case canvasgraph.KindScript:
		return "文本"
	case canvasgraph.KindImage:
		return "图片"
	case canvasgraph.KindVideo:
		return "视频"
	case canvasgraph.KindAudio:
		return "音频"
	case canvasgraph.KindDirector:
		return "导演台"
	case "":
		return "节点"
	}
	return kind
}

// quoteLabel 给标题加上「」，标题为空时什么都不写。
func quoteLabel(label string) string {
	label = oneLine(label)
	if label == "" {
		return ""
	}
	return "「" + clip(label, maxSummaryRunes, false) + "」"
}

// num 把尺寸写成最短的十进制（324 而不是 324.000000）。
func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// resultText 取工具返回文本：是 JSON 字符串就解开，否则原样。
func resultText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// oneLine 把连续空白（含换行）折成一个空格。
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// clip 按字数截断；ellipsis 为真且发生了截断时加省略号。
func clip(s string, n int, ellipsis bool) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if ellipsis {
		return string(r[:n]) + "…"
	}
	return string(r[:n])
}

// formatDuration 把毫秒写成易读的时长：42ms、3.1s、41s、2m05s；0 写成「-」（没有记录耗时）。
func formatDuration(ms int) string {
	switch {
	case ms <= 0:
		return "-"
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 10_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(ms)/1000), ".0") + "s"
	case ms < 60_000:
		return fmt.Sprintf("%ds", (ms+500)/1000)
	}
	sec := (ms + 500) / 1000
	return fmt.Sprintf("%dm%02ds", sec/60, sec%60)
}

// formatTokens 把 Token 数写成 31.4k 这样的短写法。
func formatTokens(n int) string {
	if n < 1000 {
		return strconv.Itoa(n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}
