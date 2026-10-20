package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/service/agent/tracereport"
)

// 轨迹包的组装：把索引（数据库）和原文（对象存储）、事件、画布改动拼成契约 8.4 规定的七个文件。

// 轨迹包里的文件名，顺序即 zip 里的顺序。
const (
	fileReport       = "report.md"
	fileTrace        = "trace.jsonl"
	fileEvents       = "events.jsonl"
	fileMutations    = "mutations.jsonl"
	fileCanvasBefore = "canvas.before.json"
	fileCanvasAfter  = "canvas.after.json"
	fileMeta         = "meta.json"
)

// 一行摘要里正文的最大字数。
const chatSummaryRunes = 60

// buildBundle 取齐一轮运行的全部数据并组装轨迹包。
func (s *TraceAdminService) buildBundle(ctx context.Context, actorID uint64, run *model.AgentRun) (*TraceBundle, error) {
	spans, err := s.repo.ListTraceSpans(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	calls, err := s.repo.ListModelCallsByRun(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	events, err := s.repo.ListRunEvents(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	muts, err := s.repo.ListMutations(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	approvals, err := s.repo.ListApprovals(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	obj := s.readObjects(ctx, spans)

	rows := mergeSpans(spans, calls, events, approvals, obj)
	sys := systemRow(run, obj)
	now := s.opt.Now()

	var trace bytes.Buffer
	writeJSONLine(&trace, sys)
	truncated := false
	for i := range rows {
		writeJSONLine(&trace, &rows[i])
		if rows[i].Truncated != nil && *rows[i].Truncated {
			truncated = true
		}
	}

	complete := run.TraceComplete && !obj.unreadable
	report := tracereport.Build(tracereport.Input{Run: reportRun(run, rows, complete), Spans: rows, CanvasAfter: obj.canvasAfter})
	files := []TraceFile{
		{fileReport, []byte(report)},
		{fileTrace, trace.Bytes()},
		{fileEvents, eventsJSONL(events)},
		{fileMutations, mutationsJSONL(muts)},
	}
	// 画布快照不存在（轨迹关闭、快照读取失败）时文件缺省，而不是给一个空文件
	if len(obj.canvasBefore) > 0 {
		files = append(files, TraceFile{fileCanvasBefore, obj.canvasBefore})
	}
	if len(obj.canvasAfter) > 0 {
		files = append(files, TraceFile{fileCanvasAfter, obj.canvasAfter})
	}
	files = append(files, TraceFile{fileMeta, metaJSON(run, complete, truncated, now, actorID)})
	return &TraceBundle{Files: files, ExportedAt: now}, nil
}

// writeJSONLine 写一行 JSON。结构体里只有 JSON 能表示的值，序列化不会失败。
func writeJSONLine(buf *bytes.Buffer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		b = []byte("{}")
	}
	buf.Write(b)
	buf.WriteByte('\n')
}

// systemRow 是 trace.jsonl 的第一行。原文里有 system 行就用它（那是当时真正发给模型的）；
// 没有（轨迹没记下来）就用运行行上的版本快照拼一个，工具声明和提示词留空。
func systemRow(run *model.AgentRun, obj *rawObject) *tracereport.SystemRow {
	row := &tracereport.SystemRow{Seq: 0, Kind: tracereport.KindSystem, PromptVersion: run.PromptVersion, ToolsHash: run.ToolsHash, Tools: json.RawMessage("[]")}
	if m := obj.system; m != nil {
		_ = json.Unmarshal(m["prompt_version"], &row.PromptVersion)
		_ = json.Unmarshal(m["tools_hash"], &row.ToolsHash)
		_ = json.Unmarshal(m["system_prompt"], &row.SystemPrompt)
		if len(m["tools"]) > 0 {
			row.Tools = m["tools"]
		}
	}
	return row
}

// mergeSpans 把各处的 span 合成一个按时间排好、从 1 编号的列表：
// 模型调用来自计费记录，工具和片段来自索引表，审批来自事件；原文行按 span_id 补上请求、响应、参数、返回等。
// 没有原文的 span（超出整轮上限、对象丢失）照样列出，并标 truncated。
func mergeSpans(spans []model.AgentTraceSpan, calls []model.AgentModelCall, events []model.AgentEvent, approvals []model.AgentApproval, obj *rawObject) []tracereport.Span {
	type item struct {
		span tracereport.Span
		seq  int // 记录时的序号，开始时间相同时用它排先后
	}
	var items []item
	for i := range calls {
		items = append(items, item{chatSpan(&calls[i], obj.lines[calls[i].SpanID]), calls[i].Seq})
	}
	for i := range spans {
		sp := &spans[i]
		switch sp.Kind {
		case model.TraceSpanExecuteTool:
			items = append(items, item{toolSpan(sp, obj.lines[sp.SpanID]), sp.Seq})
		case model.TraceSpanSegment:
			items = append(items, item{segmentSpan(sp, obj.lines[sp.SpanID]), sp.Seq})
		}
	}
	for _, a := range approvalSpans(events, approvals) {
		items = append(items, item{a, 1 << 30}) // 审批没有记录序号，同一时刻时排在后面
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].span.StartedAt.Equal(items[j].span.StartedAt) {
			return items[i].span.StartedAt.Before(items[j].span.StartedAt)
		}
		return items[i].seq < items[j].seq
	})
	out := make([]tracereport.Span, len(items))
	for i := range items {
		out[i] = items[i].span
		out[i].Seq = i + 1
	}
	return out
}

// chatSpan 由一条模型调用记录和它的原文行（可为 nil）组成 chat span。
func chatSpan(c *model.AgentModelCall, raw map[string]json.RawMessage) tracereport.Span {
	spanID := c.SpanID
	if spanID == "" {
		spanID = "call" + strconv.FormatUint(c.ID, 10) // 记录器没能记下这次调用的 span 时，用计费记录的 id 顶上
	}
	first := c.FirstTokenMS
	credits := c.Charged
	sp := tracereport.Span{SpanID: spanID, Kind: tracereport.KindChat, Name: c.ModelKey, Status: tracereport.StatusOK, StartedAt: c.CreatedAt,
		DurationMS: c.DurationMS, Purpose: firstNonEmpty(c.Purpose, chatPurposeDefault), UpstreamModel: c.UpstreamModel, ChannelID: c.ChannelID,
		FirstTokenMS: &first, FinishReason: c.FinishReason, Credits: &credits,
		Usage: &tracereport.Usage{Input: c.InputTokens, Output: c.OutputTokens, Cached: c.CachedTokens}}
	switch {
	case c.Status == model.ModelCallPending:
		sp.Status = tracereport.StatusPending
	case c.Error == "已取消":
		sp.Status, sp.ErrorCode = tracereport.StatusAborted, "canceled"
	case c.Error != "":
		sp.Status, sp.ErrorCode = tracereport.StatusError, "model_error"
	}
	truncated := raw == nil
	if raw != nil {
		sp.Request, sp.Response = raw["request"], raw["response"]
		truncated = rawBool(raw["truncated"])
	}
	sp.Truncated = &truncated
	sp.Summary = chatSummary(c, sp.Response)
	return sp
}

// chatSummary 是模型调用的一行摘要：出错写错误原因，否则写回复开头和发起的工具调用。
func chatSummary(c *model.AgentModelCall, response json.RawMessage) string {
	if c.Error != "" {
		return c.Error
	}
	if len(response) == 0 {
		return "（原文未记录）"
	}
	var r struct {
		Text      string `json:"text"`
		ToolCalls []struct {
			Name string `json:"name"`
		} `json:"tool_calls"`
	}
	if json.Unmarshal(response, &r) != nil {
		return ""
	}
	var parts []string
	if t := strings.TrimSpace(r.Text); t != "" {
		parts = append(parts, truncateRunes(strings.Join(strings.Fields(t), " "), chatSummaryRunes))
	}
	if len(r.ToolCalls) > 0 {
		names := make([]string, len(r.ToolCalls))
		for i, tc := range r.ToolCalls {
			names[i] = tc.Name
		}
		parts = append(parts, "调用 "+strings.Join(names, "、"))
	}
	return strings.Join(parts, " · ")
}

// toolSpan 由一条工具索引行和原文行组成 execute_tool span。
func toolSpan(sp *model.AgentTraceSpan, raw map[string]json.RawMessage) tracereport.Span {
	out := tracereport.Span{SpanID: sp.SpanID, Kind: tracereport.KindExecuteTool, Name: sp.Name, Status: sp.Status, Summary: sp.Summary,
		StartedAt: sp.StartedAt, DurationMS: sp.DurationMS, ErrorCode: sp.ErrorCode, ToolCallID: sp.ToolCallID}
	if sp.ParentSpanID != "" {
		p := sp.ParentSpanID
		out.ParentSpanID = &p
	}
	if sp.MutationID != nil && *sp.MutationID != 0 {
		out.MutationID = idcodec.Encode(*sp.MutationID)
	}
	truncated := raw == nil
	isErr := sp.Status != model.TraceStatusOK
	if raw != nil {
		out.Args, out.Result = raw["args"], raw["result"]
		truncated = rawBool(raw["truncated"])
		if len(raw["is_error"]) > 0 {
			isErr = rawBool(raw["is_error"])
		}
	}
	out.IsError, out.Truncated = &isErr, &truncated
	return out
}

// segmentSpan 由一条片段索引行和原文里的片段行组成 segment span。
func segmentSpan(sp *model.AgentTraceSpan, raw map[string]json.RawMessage) tracereport.Span {
	out := tracereport.Span{SpanID: sp.SpanID, Kind: tracereport.KindSegment, Name: sp.Name, Status: sp.Status, Summary: sp.Summary,
		StartedAt: sp.StartedAt, DurationMS: sp.DurationMS, ErrorCode: sp.ErrorCode, Launch: sp.Name}
	if raw != nil {
		var code int
		if json.Unmarshal(raw["exit_code"], &code) == nil {
			out.ExitCode = &code
		}
		_ = json.Unmarshal(raw["stderr_tail"], &out.StderrTail)
	}
	return out
}

// rawBool 读一个 JSON 布尔；缺失或不是布尔按 false。
func rawBool(raw json.RawMessage) bool {
	var b bool
	_ = json.Unmarshal(raw, &b)
	return b
}

// approvalEvent 是 approval.created / approval.decided 事件的内容（AgentApprovalView 序列化的样子）。
type approvalEvent struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Status  string          `json:"status"`
	Payload json.RawMessage `json:"payload"`
	Quote   int             `json:"quote_credits"`
	Decide  json.RawMessage `json:"decision"`
}

// approvalSpans 从 approval.created / approval.decided 事件拼出审批 span：创建事件定开始，决定事件定结束和结果。
// 只有创建事件的审批是 pending（还在等，或被停止、过期而没有决定事件——后者用审批表里的终态补上）。
func approvalSpans(events []model.AgentEvent, approvals []model.AgentApproval) []tracereport.Span {
	type pair struct {
		created, decided *model.AgentEvent
		cv, dv           approvalEvent
	}
	byID := map[string]*pair{}
	var order []string
	for i := range events {
		e := &events[i]
		if e.Type != "approval.created" && e.Type != "approval.decided" {
			continue
		}
		var v approvalEvent
		if json.Unmarshal(e.PayloadJSON, &v) != nil || v.ID == "" {
			continue
		}
		p := byID[v.ID]
		if p == nil {
			p = &pair{}
			byID[v.ID] = p
			order = append(order, v.ID)
		}
		if e.Type == "approval.created" {
			p.created, p.cv = e, v
		} else {
			p.decided, p.dv = e, v
		}
	}
	table := map[uint64]*model.AgentApproval{}
	for i := range approvals {
		table[approvals[i].ID] = &approvals[i]
	}
	var out []tracereport.Span
	for _, id := range order {
		p := byID[id]
		if p.created == nil {
			continue // 创建事件被清理了，开始时间不知道，不列
		}
		quote := p.cv.Quote
		sp := tracereport.Span{SpanID: "a" + id[:min(len(id), 16)], Kind: tracereport.KindApproval, Name: p.cv.Kind, Status: tracereport.StatusPending,
			StartedAt: p.created.CreatedAt, ApprovalKind: p.cv.Kind, Quote: &quote}
		var payload struct {
			Question string `json:"question"`
			Kind     string `json:"kind"`
		}
		_ = json.Unmarshal(p.cv.Payload, &payload)
		if p.cv.Kind == model.ApprovalAsk {
			sp.Question, sp.AskKind = payload.Question, payload.Kind
			sp.Summary = truncateRunes(payload.Question, chatSummaryRunes)
		}
		final := ""
		if uid, ok := idcodec.Decode(id); ok {
			if a := table[uid]; a != nil {
				sp.ToolCallID = a.ToolCallID
				if len(a.ResultJSON) > 2 { // "{}" 是还没有结果
					sp.Result = json.RawMessage(a.ResultJSON)
				}
				if p.decided == nil && a.Status != model.ApprovalPending {
					final = a.Status // 没有决定事件却已不是等待中：被停止或过期
				}
			}
		}
		if p.decided != nil {
			final = p.dv.Status
			sp.Decision = p.dv.Decide
			sp.DurationMS = int(p.decided.CreatedAt.Sub(p.created.CreatedAt).Milliseconds())
		}
		if final != "" {
			sp.Status = approvalStatus(final)
			if sp.Summary == "" {
				sp.Summary = approvalLabel(p.cv.Kind) + "：" + final
			}
			if len(sp.Result) == 0 {
				sp.Result = json.RawMessage(`{"status":` + strconv.Quote(final) + `}`)
			}
		} else if sp.Summary == "" {
			sp.Summary = approvalLabel(p.cv.Kind) + "：等待决定"
		}
		out = append(out, sp)
	}
	return out
}

// approvalStatus 把审批状态映射成 span 状态：批准和已执行是 ok，拒绝是 denied，失败是 error，过期或被停止是 aborted。
func approvalStatus(s string) string {
	switch s {
	case model.ApprovalApproved, model.ApprovalPartial, model.ApprovalExecuted:
		return tracereport.StatusOK
	case model.ApprovalRejected:
		return tracereport.StatusDenied
	case model.ApprovalFailed:
		return tracereport.StatusError
	case model.ApprovalExpired:
		return tracereport.StatusAborted
	}
	return tracereport.StatusPending
}

// approvalLabel 是审批种类的中文名，写进一行摘要。
func approvalLabel(kind string) string {
	switch kind {
	case model.ApprovalGenerate:
		return "申请生成"
	case model.ApprovalDelete:
		return "申请删除"
	case model.ApprovalAsk:
		return "向用户提问"
	}
	return kind
}

// reportRun 把运行行转成报告用的概况。
func reportRun(run *model.AgentRun, rows []tracereport.Span, complete bool) tracereport.Run {
	r := tracereport.Run{ID: idcodec.Encode(run.ID), Status: run.Status, Active: model.IsActiveRun(run.Status), PromptVersion: run.PromptVersion,
		Model: run.AgentModelKey, Steps: run.Steps, SpentCredits: run.SpentCredits, TraceComplete: complete}
	if run.EndedAt != nil {
		r.Duration = run.EndedAt.Sub(run.CreatedAt)
	}
	var in struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(run.InputJSON, &in)
	r.UserText = in.Message
	return r
}

// eventsJSONL 是 events.jsonl：这一轮的 agent_events，一行一个，按会话内序号升序。
func eventsJSONL(events []model.AgentEvent) []byte {
	var buf bytes.Buffer
	for _, e := range events {
		writeJSONLine(&buf, map[string]any{"session_id": idcodec.ID(e.SessionID), "run_id": idcodec.ID(e.RunID), "seq": e.Seq, "type": e.Type,
			"data": json.RawMessage(emptyObj(e.PayloadJSON)), "created_at": e.CreatedAt})
	}
	return buf.Bytes()
}

// mutationsJSONL 是 mutations.jsonl：这一轮的 agent_mutations（画布改动和 revision），一行一个。
func mutationsJSONL(muts []model.AgentMutation) []byte {
	var buf bytes.Buffer
	for _, m := range muts {
		writeJSONLine(&buf, map[string]any{"id": idcodec.ID(m.ID), "run_id": idcodec.ID(m.RunID), "seq": m.Seq, "tool_call_id": m.ToolCallID, "kind": m.Kind,
			"changes": json.RawMessage(emptyObj(m.ChangesJSON)), "revision_before": m.RevisionBefore, "revision_after": m.RevisionAfter,
			"undone_at": m.UndoneAt, "created_at": m.CreatedAt})
	}
	return buf.Bytes()
}

// emptyObj 把空 JSON 补成 {}，免得序列化 RawMessage 时报错。
func emptyObj(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

// metaJSON 是 meta.json，字段见契约 8.4。
func metaJSON(run *model.AgentRun, complete, truncated bool, exportedAt time.Time, actorID uint64) []byte {
	pins := json.RawMessage(emptyObj(run.SkillPins))
	meta := map[string]any{
		"run_id": idcodec.ID(run.ID), "user_id": idcodec.ID(run.UserID), "canvas_id": idcodec.ID(run.CanvasID), "mode": run.Mode, "status": run.Status,
		"prompt_version": run.PromptVersion, "agent_model_key": run.AgentModelKey, "tools_hash": run.ToolsHash, "skill_pins": pins,
		"harness_case": run.HarnessCase, "started_at": run.CreatedAt, "ended_at": run.EndedAt, "trace_complete": complete, "truncated": truncated,
		"exported_at": exportedAt, "exported_by": idcodec.ID(actorID),
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return []byte("{}")
	}
	return append(b, '\n')
}
