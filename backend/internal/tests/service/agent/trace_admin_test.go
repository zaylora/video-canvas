package agent_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service/agent"
)

// fakeAudit 记下写入的审计，可以让写入失败。
type fakeAudit struct {
	mu   sync.Mutex
	logs []*model.AdminAuditLog
	err  error
	// order 记录审计写入和读存储的先后，验证「先写审计再传输」
	order *[]string
}

func (f *fakeAudit) Insert(_ context.Context, l *model.AdminAuditLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.order != nil {
		*f.order = append(*f.order, "audit")
	}
	if f.err != nil {
		return f.err
	}
	f.logs = append(f.logs, l)
	return nil
}

type adminEnv struct {
	*traceEnv
	audit *fakeAudit
	svc   *TraceAdminService
	now   time.Time
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	te := newTraceEnv(t, nil)
	a := &adminEnv{traceEnv: te, audit: &fakeAudit{}, now: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	a.svc = NewTraceAdminService(te.repo, te.stores, a.audit, TraceAdminOptions{RetentionDays: 14, Now: func() time.Time { return a.now }})
	return a
}

// recordedRun 真实地走一遍记录器：一个片段、一次模型调用、一次工具调用，运行落到仓储里，返回运行。
func (a *adminEnv) recordedRun(t *testing.T) *model.AgentRun {
	t.Helper()
	ctx := context.Background()
	run := a.traceEnv.run()
	run.Status = model.RunSucceeded
	run.AgentModelKey = "claude"
	run.ToolsHash = "abcd"
	run.TraceComplete = true
	end := run.CreatedAt.Add(41 * time.Second)
	run.EndedAt = &end
	run.InputJSON = []byte(`{"message":"把剧本拆成分镜"}`)
	run.Steps, run.SpentCredits = 2, 3
	a.repo.runs[run.ID] = run

	seg := a.rec.BeginSegment(ctx, run, "start", startInput())
	if seg == nil {
		t.Fatal("应开始记录")
	}
	ct := seg.BeginChat("", "claude", []byte(`{"messages":[{"role":"user","content":"拆分镜"}]}`))
	meta := ct.Meta()
	a.repo.calls = append(a.repo.calls, model.AgentModelCall{RunID: run.ID, ModelKey: "claude", Status: model.ModelCallSettled, InputTokens: 100, OutputTokens: 20,
		Charged: 3, SpanID: meta.SpanID, Seq: meta.Seq, Purpose: meta.Purpose, SegmentSeq: meta.SegmentSeq, DurationMS: 900, FinishReason: "stop", CreatedAt: a.traceEnv.now})
	ct.Record(nil, nil, &SettleResult{Credits: 3, Charged: 3})
	a.traceEnv.now = a.traceEnv.now.Add(time.Second)
	tt := seg.BeginTool("tc1", "", "canvas_apply_ops", json.RawMessage(`{"ops":[]}`))
	tt.End(ctx, ToolEnd{Status: model.TraceStatusError, ErrorCode: "validation", IsError: true, Content: "第 1 项不合法", Summary: "不合法"})
	seg.End(ctx, SegmentEnd{})
	return run
}

func (a *adminEnv) export(t *testing.T, runID uint64, format string) bundle {
	t.Helper()
	b, err := a.svc.ExportTrace(context.Background(), 9, runID, format)
	if err != nil {
		t.Fatalf("ExportTrace: %v", err)
	}
	return bundle{b}
}

// bundle 给轨迹包加上按文件名取内容的方法（TraceBundle 是别的包的类型，测试包里不能给它加方法）。
type bundle struct{ *TraceBundle }

func (b bundle) file(t *testing.T, name string) string {
	t.Helper()
	for _, f := range b.Files {
		if f.Name == name {
			return string(f.Content)
		}
	}
	t.Fatalf("轨迹包里没有 %s", name)
	return ""
}

func (b bundle) has(name string) bool {
	for _, f := range b.Files {
		if f.Name == name {
			return true
		}
	}
	return false
}

// 轨迹包里的文件、顺序、内容与契约 8.4 一致。
func TestExportTrace_BundleFiles(t *testing.T) {
	a := newAdminEnv(t)
	run := a.recordedRun(t)
	b := a.export(t, run.ID, "json")

	var names []string
	for _, f := range b.Files {
		names = append(names, f.Name)
	}
	want := "report.md,trace.jsonl,events.jsonl,mutations.jsonl,canvas.before.json,canvas.after.json,meta.json"
	if strings.Join(names, ",") != want {
		t.Fatalf("文件清单不对: %v", names)
	}
	report := b.file(t, "report.md")
	rid := idcodec.Encode(run.ID)
	for _, w := range []string{"# Agent 运行报告 · run " + rid, "> 终态 succeeded · prompt v10 · 模型 claude · 2 步", "> 用户原话：把剧本拆成分镜", "## 时间线", "validation", "## 最终画布概要"} {
		if w == "validation" {
			continue // 错误分类在索引里，报告里只有摘要
		}
		if !strings.Contains(report, w) {
			t.Errorf("报告缺少 %q:\n%s", w, report)
		}
	}
	if strings.Contains(report, "⚠") {
		t.Errorf("完整且已结束的运行没有警告:\n%s", report)
	}

	lines := strings.Split(strings.TrimSpace(b.file(t, "trace.jsonl")), "\n")
	var rows []map[string]any
	for _, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("trace.jsonl 里有不是 JSON 的行: %v", err)
		}
		rows = append(rows, m)
	}
	if rows[0]["kind"] != "system" || rows[0]["seq"].(float64) != 0 || rows[0]["system_prompt"] != "你是画布助手" {
		t.Errorf("第一行应是 system 行: %v", rows[0])
	}
	var kinds []string
	for i, r := range rows[1:] {
		kinds = append(kinds, r["kind"].(string))
		if r["seq"].(float64) != float64(i+1) {
			t.Errorf("seq 应从 1 连续编号: %v", r)
		}
	}
	if strings.Join(kinds, ",") != "segment,chat,execute_tool" && strings.Join(kinds, ",") != "chat,execute_tool,segment" {
		t.Errorf("span 种类: %v", kinds)
	}
	var tool map[string]any
	for _, r := range rows {
		if r["kind"] == "execute_tool" {
			tool = r
		}
	}
	if tool["status"] != "error" || tool["error_code"] != "validation" || tool["is_error"] != true || tool["result"] != "第 1 项不合法" || tool["parent_span_id"] != nil {
		t.Errorf("工具行: %v", tool)
	}

	var meta map[string]any
	if err := json.Unmarshal([]byte(b.file(t, "meta.json")), &meta); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"run_id", "user_id", "canvas_id", "mode", "status", "prompt_version", "agent_model_key", "tools_hash", "skill_pins", "harness_case",
		"started_at", "ended_at", "trace_complete", "truncated", "exported_at", "exported_by"} {
		if _, ok := meta[k]; !ok {
			t.Errorf("meta.json 缺少 %s: %v", k, meta)
		}
	}
	if meta["run_id"] != rid || meta["exported_by"] != idcodec.Encode(9) || meta["status"] != "succeeded" || meta["trace_complete"] != true {
		t.Errorf("meta.json 内容: %v", meta)
	}
	if !strings.Contains(b.file(t, "canvas.before.json"), "nodes") || !strings.Contains(b.file(t, "canvas.after.json"), "nodes") {
		t.Error("画布快照应取自原文对象")
	}
}

// zip 和 json 两种格式的文件一致；zip 能解开。
func TestExportTrace_ZipMatchesJSON(t *testing.T) {
	a := newAdminEnv(t)
	run := a.recordedRun(t)
	b := a.export(t, run.ID, "zip")
	data, err := b.Zip()
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	texts := b.TextFiles()
	if len(zr.File) != len(texts) {
		t.Fatalf("zip 里 %d 个文件，json 里 %d 个", len(zr.File), len(texts))
	}
	for _, f := range zr.File {
		rc, _ := f.Open()
		got, _ := io.ReadAll(rc)
		rc.Close()
		if string(got) != texts[f.Name] {
			t.Errorf("%s 在 zip 与 json 里不一致", f.Name)
		}
	}
}

// 轨迹包里不能有渠道 Key、请求头。
func TestExportTrace_NoSecrets(t *testing.T) {
	a := newAdminEnv(t)
	run := a.recordedRun(t)
	b := a.export(t, run.ID, "json")
	for _, f := range b.Files {
		low := strings.ToLower(string(f.Content))
		if strings.Contains(low, "sk-test") || strings.Contains(low, "authorization") || strings.Contains(low, "bearer ") {
			t.Errorf("%s 里出现了密钥或请求头", f.Name)
		}
	}
}

func TestExportTrace_Errors(t *testing.T) {
	ctx := context.Background()
	a := newAdminEnv(t)
	_, err := a.svc.ExportTrace(ctx, 9, 12345, "zip")
	wantAgentCode(t, err, errcode.ErrAgentRunMissing)

	off := &model.AgentRun{ID: 50, CreatedAt: a.now.Add(-time.Hour), TraceEnabled: false}
	a.repo.runs[50] = off
	_, err = a.svc.ExportTrace(ctx, 9, 50, "zip")
	wantAgentCode(t, err, errcode.ErrAgentTraceDisabled)

	old := &model.AgentRun{ID: 51, CreatedAt: a.now.AddDate(0, 0, -15), TraceEnabled: true}
	a.repo.runs[51] = old
	_, err = a.svc.ExportTrace(ctx, 9, 51, "zip")
	wantAgentCode(t, err, errcode.ErrAgentTraceExpired)

	if len(a.audit.logs) != 0 {
		t.Errorf("导出被拒绝时不应留下导出审计: %+v", a.audit.logs)
	}
	if errcode.ErrAgentTraceExpired.Code == errcode.ErrAgentTraceDisabled.Code || errcode.ErrAgentTraceExpired.Code == errcode.ErrAgentRunMissing.Code {
		t.Error("过期、关闭、不存在要有各自明确的错误码")
	}
}

// 审计在开始传输之前写入；审计写不进去就不导出。
func TestExportTrace_AuditWrittenFirst(t *testing.T) {
	ctx := context.Background()
	a := newAdminEnv(t)
	run := a.recordedRun(t)
	b := a.export(t, run.ID, "zip")
	if b.TraceBundle == nil || len(a.audit.logs) != 1 {
		t.Fatalf("每次导出写一条审计: %+v", a.audit.logs)
	}
	l := a.audit.logs[0]
	if l.Action != "agent.trace.export" || l.ActorID != 9 || l.TargetID != run.ID || l.TargetType != model.AdminAuditTargetAgentRun {
		t.Errorf("审计内容: %+v", l)
	}
	if strings.Contains(string(l.DetailJSON), "拆分镜") {
		t.Errorf("审计里不能带轨迹内容: %s", l.DetailJSON)
	}
	a.audit.err = errors.New("db down")
	if _, err := a.svc.ExportTrace(ctx, 9, run.ID, "zip"); err == nil {
		t.Error("审计写失败时不应继续导出：没有留下记录就不能把用户的对话原文给出去")
	}
}

// 画布快照缺失（读不到）时，对应文件缺省而不是空文件；报告里也要说明。
func TestExportTrace_MissingSnapshotFilesOmitted(t *testing.T) {
	a := newAdminEnv(t)
	a.repo.canvasErr = errors.New("画布读取失败")
	run := a.recordedRun(t)
	b := a.export(t, run.ID, "json")
	if b.has("canvas.before.json") || b.has("canvas.after.json") {
		t.Errorf("没有快照时文件应缺省: %v", b.TextFiles())
	}
	if !strings.Contains(b.file(t, "report.md"), "没有画布快照") {
		t.Error("报告的画布概要要说明没有快照")
	}
	if !b.has("meta.json") || !b.has("trace.jsonl") {
		t.Error("其余文件照常导出")
	}
}

// 原文对象丢了：索引还在，导出照常，报告和 meta 标不完整。
func TestExportTrace_ObjectMissingMarksIncomplete(t *testing.T) {
	a := newAdminEnv(t)
	run := a.recordedRun(t)
	for _, k := range a.stores.st.keys() {
		_ = a.stores.st.Delete(context.Background(), k)
	}
	b := a.export(t, run.ID, "json")
	if !strings.Contains(b.file(t, "report.md"), "> ⚠ 轨迹不完整") {
		t.Errorf("原文读不出来要提示不完整:\n%s", b.file(t, "report.md"))
	}
	var meta map[string]any
	_ = json.Unmarshal([]byte(b.file(t, "meta.json")), &meta)
	if meta["trace_complete"] != false {
		t.Errorf("meta: %v", meta)
	}
	if !strings.Contains(b.file(t, "trace.jsonl"), `"kind":"execute_tool"`) {
		t.Error("索引里的工具调用仍应列在时间线上")
	}
}

// 审批 span 从 agent_events 的 approval.created / approval.decided 拼出。
func TestExportTrace_ApprovalSpansFromEvents(t *testing.T) {
	a := newAdminEnv(t)
	run := a.recordedRun(t)
	aid := uint64(77)
	enc := idcodec.Encode(aid)
	t0 := run.CreatedAt.Add(10 * time.Second)
	created, _ := json.Marshal(map[string]any{"id": enc, "kind": "ask", "status": "pending", "quote_credits": 0,
		"payload": map[string]any{"question": "画幅选哪个？", "kind": "choice", "options": []string{"9:16", "16:9"}}, "decision": map[string]any{}})
	decided, _ := json.Marshal(map[string]any{"id": enc, "kind": "ask", "status": "approved", "decision": map[string]any{"answer": "9:16"}})
	a.repo.events = []model.AgentEvent{
		{RunID: run.ID, SessionID: run.SessionID, Seq: 5, Type: "approval.created", PayloadJSON: created, CreatedAt: t0},
		{RunID: run.ID, SessionID: run.SessionID, Seq: 6, Type: "approval.decided", PayloadJSON: decided, CreatedAt: t0.Add(5 * time.Second)},
	}
	a.repo.approvals = []model.AgentApproval{{ID: aid, RunID: run.ID, ToolCallID: "tc-ask", ResultJSON: []byte(`{}`), Status: model.ApprovalApproved}}
	b := a.export(t, run.ID, "json")
	var ap map[string]any
	for _, l := range strings.Split(strings.TrimSpace(b.file(t, "trace.jsonl")), "\n") {
		var m map[string]any
		_ = json.Unmarshal([]byte(l), &m)
		if m["kind"] == "approval" {
			ap = m
		}
	}
	if ap == nil {
		t.Fatalf("应有审批 span:\n%s", b.file(t, "trace.jsonl"))
	}
	if ap["approval_kind"] != "ask" || ap["ask_kind"] != "choice" || ap["question"] != "画幅选哪个？" || ap["status"] != "ok" || ap["duration_ms"].(float64) != 5000 ||
		ap["decision"].(map[string]any)["answer"] != "9:16" || ap["tool_call_id"] != "tc-ask" || ap["name"] != "ask" {
		t.Errorf("审批 span 字段不对: %v", ap)
	}
	if !strings.Contains(b.file(t, "events.jsonl"), "approval.created") {
		t.Error("events.jsonl 应含这一轮的事件")
	}
}

// ---- 列表 ----

func TestListRuns_MapsFieldsAndTraceState(t *testing.T) {
	ctx := context.Background()
	a := newAdminEnv(t)
	end := a.now.Add(-time.Hour)
	create := end.Add(-41 * time.Second)
	mk := func(id uint64, mut func(*model.AgentRun)) repository.AdminRunRow {
		r := model.AgentRun{ID: id, UserID: 1, CanvasID: 7, SessionID: 3, Status: model.RunSucceeded, Mode: "storyboard", Steps: 18, SpentCredits: 2, PromptVersion: 9,
			CreatedAt: create, EndedAt: &end, TraceEnabled: true, TraceComplete: true}
		mut(&r)
		return repository.AdminRunRow{AgentRun: r, UserEmail: "alice@studio.cn", CanvasTitle: "雨夜追凶", ToolErrors: 3}
	}
	a.repo.listRows = []repository.AdminRunRow{
		mk(1, func(*model.AgentRun) {}),
		mk(2, func(r *model.AgentRun) { r.HarnessCase = "sb-9x16" }),
		mk(3, func(r *model.AgentRun) { r.TraceEnabled = false }),
		mk(4, func(r *model.AgentRun) {
			r.CreatedAt = a.now.AddDate(0, 0, -20)
			r.EndedAt = nil
			r.Status = model.RunRunning
		}),
	}
	page, err := a.svc.ListRuns(ctx, &model.ListAgentRunsReq{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(page)
	var out struct {
		List     []map[string]any `json:"list"`
		Total    float64          `json:"total"`
		Page     float64          `json:"page"`
		PageSize float64          `json:"page_size"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Page != 1 || out.PageSize != 20 || out.Total != 4 || len(out.List) != 4 {
		t.Fatalf("分页默认 1 / 20: %+v", out)
	}
	first := out.List[0]
	for _, k := range []string{"id", "created_at", "ended_at", "user_id", "user_email", "canvas_id", "canvas_title", "mode", "status", "steps", "tool_errors",
		"spent_credits", "duration_ms", "prompt_version", "source", "harness_case", "trace", "trace_complete"} {
		if _, ok := first[k]; !ok {
			t.Errorf("列表项缺少 %s: %v", k, first)
		}
	}
	if first["id"] != idcodec.Encode(1) || first["duration_ms"].(float64) != 41000 || first["source"] != "user" || first["trace"] != "available" || first["tool_errors"].(float64) != 3 {
		t.Errorf("第一项: %v", first)
	}
	if out.List[1]["source"] != "harness" || out.List[1]["harness_case"] != "sb-9x16" {
		t.Errorf("harness 用例: %v", out.List[1])
	}
	if out.List[2]["trace"] != "disabled" || out.List[2]["tool_errors"].(float64) != 0 {
		t.Errorf("没开轨迹: %v", out.List[2])
	}
	if out.List[3]["trace"] != "expired" || out.List[3]["ended_at"] != nil || out.List[3]["duration_ms"] != nil || out.List[3]["status"] != "running" {
		t.Errorf("过期且未结束: %v", out.List[3])
	}
}

func TestListRuns_Filters(t *testing.T) {
	ctx := context.Background()
	a := newAdminEnv(t)
	cvs, run := idcodec.Encode(7), idcodec.Encode(42)
	_, err := a.svc.ListRuns(ctx, &model.ListAgentRunsReq{User: "alice", CanvasID: cvs, RunID: run, Status: "failed", Source: "harness",
		From: "2026-10-01T00:00:00Z", To: "2026-10-10T00:00:00Z", Page: 2, PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	f := a.repo.gotFilter
	if f.CanvasID != 7 || f.RunID != 42 || f.Status != "failed" || f.Harness == nil || !*f.Harness || f.UserLike != "alice" || f.UserID != 0 {
		t.Errorf("筛选条件: %+v", f)
	}
	if f.Limit != 100 || f.Offset != 100 {
		t.Errorf("page_size 最大 100，第 2 页偏移 100: limit=%d offset=%d", f.Limit, f.Offset)
	}
	if !f.From.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) || f.To == nil {
		t.Errorf("时间范围: %+v", f)
	}

	// 用户可以是编码后的用户 id
	_, _ = a.svc.ListRuns(ctx, &model.ListAgentRunsReq{User: idcodec.Encode(5)})
	if a.repo.gotFilter.UserID != 5 {
		t.Errorf("编码后的用户 id 应按 id 匹配: %+v", a.repo.gotFilter)
	}
	// 默认最近 7 天
	_, _ = a.svc.ListRuns(ctx, &model.ListAgentRunsReq{})
	if got := a.now.Sub(a.repo.gotFilter.From); got != 7*24*time.Hour {
		t.Errorf("默认应是最近 7 天: %v", got)
	}
	_, _ = a.svc.ListRuns(ctx, &model.ListAgentRunsReq{Source: "user"})
	if h := a.repo.gotFilter.Harness; h == nil || *h {
		t.Errorf("source=user 只看真实用户: %+v", h)
	}

	for name, req := range map[string]*model.ListAgentRunsReq{
		"画布 id 格式错误": {CanvasID: "zzz"},
		"运行 id 格式错误": {RunID: "zzz"},
		"开始时间格式错误":   {From: "昨天"},
		"结束时间格式错误":   {To: "明天"},
		"开始不早于结束":    {From: "2026-10-10T00:00:00Z", To: "2026-10-01T00:00:00Z"},
	} {
		_, err := a.svc.ListRuns(ctx, req)
		wantAgentCode(t, err, errcode.ErrInvalidParams)
		_ = name
	}
}

// ---- 清理 ----

func TestTraceCleaner_RemovesExpiredObjectsSpansAndEvents(t *testing.T) {
	ctx := context.Background()
	te := newTraceEnv(t, nil)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	old, fresh := now.AddDate(0, 0, -15), now.AddDate(0, 0, -1)
	put := func(key string) {
		_ = te.stores.st.Put(ctx, key, strings.NewReader("x"), 1, "")
	}
	put("agent-traces/old/1.jsonl.gz")
	put("agent-traces/new/1.jsonl.gz")
	put("agent-traces/gone/1.jsonl.gz")
	add := func(s model.AgentTraceSpan) { _ = te.repo.CreateTraceSpan(ctx, &s) }
	add(model.AgentTraceSpan{RunID: 1, SpanID: "s1", Kind: "segment", StartedAt: old, StorageID: 7, ObjectKey: "agent-traces/old/1.jsonl.gz"})
	add(model.AgentTraceSpan{RunID: 1, SpanID: "t1", Kind: "execute_tool", StartedAt: old})
	add(model.AgentTraceSpan{RunID: 2, SpanID: "s2", Kind: "segment", StartedAt: fresh, StorageID: 7, ObjectKey: "agent-traces/new/1.jsonl.gz"})
	add(model.AgentTraceSpan{RunID: 3, SpanID: "s3", Kind: "segment", StartedAt: old, StorageID: 99, ObjectKey: "agent-traces/gone/1.jsonl.gz"})
	te.stores.missing[99] = true // 存储配置已被删除
	te.repo.events = []model.AgentEvent{{Type: "message.user", CreatedAt: old}, {Type: "message.user", CreatedAt: fresh}}

	c := NewTraceCleaner(te.repo, te.stores, 14, func() time.Time { return now })
	res, err := c.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Objects != 2 || res.Spans != 3 || res.Events != 1 {
		t.Errorf("清理数量: %+v", res)
	}
	keys := te.stores.st.keys()
	if len(keys) != 2 { // 新的 + 存储配置已删除的那个（对象无从删起，只清索引）
		t.Errorf("只应删掉过期的对象: %v", keys)
	}
	for _, k := range keys {
		if strings.Contains(k, "old") {
			t.Errorf("过期对象还在: %s", k)
		}
	}
	left := te.repo.spansOf("segment")
	if len(left) != 1 || left[0].SpanID != "s2" || len(te.repo.spansOf("execute_tool")) != 0 {
		t.Errorf("索引应只剩没过期的: %+v", te.repo.spans)
	}
	if len(te.repo.events) != 1 {
		t.Errorf("agent_events 同样按保留期清理: %+v", te.repo.events)
	}
}

// 对象删失败时保留索引行，下一轮重试（先删行会让对象成为永远清不掉的孤儿）。
func TestTraceCleaner_KeepsRowWhenObjectDeleteFails(t *testing.T) {
	ctx := context.Background()
	te := newTraceEnv(t, nil)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	_ = te.stores.st.Put(ctx, "agent-traces/o/1.jsonl.gz", strings.NewReader("x"), 1, "")
	_ = te.repo.CreateTraceSpan(ctx, &model.AgentTraceSpan{RunID: 1, SpanID: "s1", Kind: "segment", StartedAt: now.AddDate(0, 0, -30), StorageID: 7, ObjectKey: "agent-traces/o/1.jsonl.gz"})
	te.stores.st.delErr = errors.New("存储不可用")
	c := NewTraceCleaner(te.repo, te.stores, 14, func() time.Time { return now })
	if _, err := c.Cleanup(ctx); err == nil {
		t.Error("对象删不掉要返回错误，让上层记日志")
	}
	if len(te.repo.spansOf("segment")) != 1 || len(te.stores.st.keys()) != 1 {
		t.Error("对象没删成，索引行必须留着")
	}
	te.stores.st.delErr = nil
	if res, err := c.Cleanup(ctx); err != nil || res.Objects != 1 || len(te.repo.spansOf("segment")) != 0 {
		t.Errorf("存储恢复后下一轮应清掉: res=%+v err=%v", res, err)
	}
}

func TestTraceCleaner_BatchesAndStopsOnCancel(t *testing.T) {
	te := newTraceEnv(t, nil)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for i := 0; i < TraceCleanupBatch*2+10; i++ {
		te.repo.events = append(te.repo.events, model.AgentEvent{Type: "tool.end", CreatedAt: now.AddDate(0, 0, -30)})
	}
	c := NewTraceCleaner(te.repo, te.stores, 14, func() time.Time { return now })
	res, err := c.Cleanup(context.Background())
	if err != nil || res.Events != int64(TraceCleanupBatch*2+10) || len(te.repo.events) != 0 {
		t.Errorf("应分批删完: res=%+v err=%v left=%d", res, err, len(te.repo.events))
	}

	te.repo.events = append(te.repo.events, model.AgentEvent{Type: "x", CreatedAt: now.AddDate(0, 0, -30)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Cleanup(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("ctx 取消后应立即停止: %v", err)
	}
	if len(te.repo.events) != 1 {
		t.Error("取消后不应再删")
	}
}
