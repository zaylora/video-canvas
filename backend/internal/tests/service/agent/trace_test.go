package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	. "video-canvas/internal/service/agent"
)

const traceKey1 = "agent-traces/2026/10/20/42/1.jsonl.gz"

// 关闭时所有方法都是空操作，nil 句柄也不会 panic：调用方不用判断有没有开轨迹。
func TestTrace_DisabledIsNoop(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, func(o *TraceOptions) { o.Enabled = false })
	if e.rec.Enabled() {
		t.Fatal("关闭时 Enabled 应为 false")
	}
	seg := e.rec.BeginSegment(ctx, e.run(), "start", startInput())
	if seg != nil {
		t.Fatal("关闭时不应开始记录")
	}
	// nil 句柄上的所有调用都不能 panic
	ct := seg.BeginChat("", "m", []byte(`{}`))
	ct.MarkFirstToken()
	_ = ct.Meta()
	_ = ct.Outcome(nil, "u", "c")
	ct.Record(nil, nil, nil)
	tt := seg.BeginTool("c1", "", "x", nil)
	tt.End(ctx, ToolEnd{})
	seg.BeginAgentTool("c2", "codemode")
	seg.EndAgentTool(ctx, "c2", false, "")
	seg.SetSkillPins(nil)
	seg.End(ctx, SegmentEnd{})
	seg.Abandon()
	var nilRec *TraceRecorder
	if nilRec.Enabled() || nilRec.BeginSegment(ctx, e.run(), "start", nil) != nil || nilRec.RecoverOrphans(ctx) != 0 {
		t.Error("nil 记录器也应是空操作")
	}
	if len(e.tempFiles(t)) != 0 || len(e.repo.spans) != 0 || len(e.stores.st.keys()) != 0 {
		t.Error("关闭时不应产生任何文件、索引或对象")
	}
}

// 运行开跑时轨迹是关着的（trace_enabled=false，比如升级前的老运行），之后再开启配置也不记。
func TestTrace_RunWithoutTraceEnabledIsSkipped(t *testing.T) {
	e := newTraceEnv(t, nil)
	run := e.run()
	run.TraceEnabled = false
	if e.rec.BeginSegment(context.Background(), run, "start", startInput()) != nil {
		t.Error("运行没开轨迹就不记")
	}
}

// 完整的一个片段：system 行、画布快照、模型调用、工具调用、片段行，压缩上传到约定的 key，写好索引，临时文件清掉。
func TestTrace_SegmentRoundTrip(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, nil)
	run := e.run()
	seg := e.rec.BeginSegment(ctx, run, "start", startInput())
	if seg == nil {
		t.Fatal("应开始记录")
	}
	if len(e.tempFiles(t)) != 1 {
		t.Fatalf("片段运行期间原文先追加到本机临时文件: %v", e.tempFiles(t))
	}
	seg.SetSkillPins(func() map[string]uint64 { return map[string]uint64{"storyboard": 9} })

	// 一次模型调用：请求体里有渠道无关的内容、图片和工具声明
	body := []byte(`{"model":"x","tools":[{"type":"function","function":{"name":"canvas_get_state"}}],"messages":[` +
		`{"role":"user","content":[{"type":"text","text":"看这张图"},{"type":"image_url","image_url":{"url":"https://cdn.example/sig?token=SECRET"}}]}]}`)
	ct := seg.BeginChat("", "claude", body)
	meta := ct.Meta()
	if meta.SpanID == "" || meta.Seq != 2 || meta.Purpose != "chat" || meta.SegmentSeq != 1 {
		t.Fatalf("计费记录要带的轨迹字段不对: %+v", meta)
	}
	e.now = e.now.Add(120 * time.Millisecond)
	ct.MarkFirstToken()
	ct.MarkFirstToken() // 只认第一次
	e.now = e.now.Add(880 * time.Millisecond)
	res := &llmgateway.Result{Text: "好的", Thinking: "想一下", FinishReason: "tool_calls", Usage: llmgateway.Usage{InputTokens: 100, OutputTokens: 20, CachedTokens: 50},
		ToolCalls: []llmgateway.ToolCall{{ID: "call_1", Name: "canvas_get_state", Arguments: `{}`}}}
	out := ct.Outcome(res, "claude-up", "ch1")
	if out.FirstTokenMS != 120 || out.DurationMS != 1000 || out.FinishReason != "tool_calls" || out.UpstreamModel != "claude-up" || out.ChannelID != "ch1" {
		t.Errorf("结算时写回的字段不对: %+v", out)
	}
	ct.Record(res, nil, &SettleResult{Credits: 2, Charged: 2})

	// 一次工具调用，关联了画布改动
	e.now = e.now.Add(time.Second)
	tt := seg.BeginTool("call_1", "", "canvas_get_state", json.RawMessage(`{"nodeIds":["n1"]}`))
	e.now = e.now.Add(50 * time.Millisecond)
	tt.End(ctx, ToolEnd{Status: model.TraceStatusOK, Content: `{"nodes":[]}`, Summary: "读取 1 个节点", MutationID: 99})

	e.now = e.now.Add(time.Second)
	seg.End(ctx, SegmentEnd{ExitCode: 0, StderrTail: "warn: x"})

	keys := e.stores.st.keys()
	if len(keys) != 1 || keys[0] != traceKey1 {
		t.Fatalf("对象 key 应是 agent-traces/{yyyy}/{mm}/{dd}/{run_id}/{segment_seq}.jsonl.gz（按运行创建日期）: %v", keys)
	}
	lines := e.stores.st.lines(t, traceKey1)
	kinds := []string{}
	for _, l := range lines {
		kinds = append(kinds, l["kind"].(string))
	}
	if strings.Join(kinds, ",") != "system,canvas,chat,execute_tool,canvas,segment" {
		t.Fatalf("行的顺序不对: %v", kinds)
	}
	sys := lines[0]
	if sys["prompt_version"].(float64) != 10 || sys["system_prompt"] != "你是画布助手" || sys["tools_hash"] == "" || len(sys["tools"].([]any)) != 1 {
		t.Errorf("system 行不对: %v", sys)
	}
	if lines[1]["phase"] != "before" || lines[4]["phase"] != "after" {
		t.Errorf("画布快照应开跑前一份、结束时一份: %v %v", lines[1]["phase"], lines[4]["phase"])
	}
	chat := lines[2]
	req := chat["request"].(map[string]any)
	if req["tools"] != sys["tools_hash"] {
		t.Errorf("请求里的工具声明应换成 tools_hash 引用: %v", req["tools"])
	}
	raw, _ := json.Marshal(chat)
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "cdn.example") || !strings.Contains(string(raw), `"asset_id"`) {
		t.Errorf("图片地址（含签名）不能进轨迹，要换成 asset_id 引用: %s", raw)
	}
	resp := chat["response"].(map[string]any)
	if resp["text"] != "好的" || resp["thinking"] != "想一下" || len(resp["tool_calls"].([]any)) != 1 || chat["credits"].(float64) != 2 {
		t.Errorf("响应、扣费不对: %v", chat)
	}
	tool := lines[3]
	if tool["tool_call_id"] != "call_1" || tool["result"] != `{"nodes":[]}` || tool["args"].(map[string]any)["nodeIds"] == nil || tool["truncated"] != false {
		t.Errorf("工具行不对: %v", tool)
	}
	segLine := lines[5]
	if segLine["launch"] != "start" || segLine["exit_code"].(float64) != 0 || segLine["stderr_tail"] != "warn: x" {
		t.Errorf("片段行不对: %v", segLine)
	}

	tools := e.repo.spansOf(model.TraceSpanExecuteTool)
	if len(tools) != 1 || tools[0].ToolCallID != "call_1" || tools[0].Status != "ok" || tools[0].Seq != 3 || tools[0].DurationMS != 50 ||
		tools[0].MutationID == nil || *tools[0].MutationID != 99 || tools[0].Bytes == 0 || tools[0].SegmentSeq != 1 {
		t.Errorf("工具索引不对: %+v", tools)
	}
	segs := e.repo.spansOf(model.TraceSpanSegment)
	if len(segs) != 1 || segs[0].Name != "start" || segs[0].ObjectKey != traceKey1 || segs[0].StorageID != 7 || segs[0].Status != "ok" || segs[0].Seq != 1 {
		t.Errorf("片段索引不对（要记下对象在哪个存储、哪个 key）: %+v", segs)
	}
	if len(e.repo.updates) != 1 {
		t.Fatalf("片段结束应更新一次运行的轨迹字段: %+v", e.repo.updates)
	}
	u := e.repo.updates[0]
	if u.AddBytes <= 0 || u.Incomplete || u.ToolsHash != sys["tools_hash"] || u.SkillPins["storyboard"] != 9 {
		t.Errorf("运行字段更新不对: %+v", u)
	}
	if len(e.tempFiles(t)) != 0 {
		t.Errorf("上传后临时文件应删除: %v", e.tempFiles(t))
	}
}

// 只有启动方式是 start 的片段才留开跑前的画布快照；续跑的片段不留（开跑前就是第一个片段开始时）。
func TestTrace_OnlyFirstSegmentKeepsBeforeSnapshotAndSeqContinues(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, nil)
	s1 := e.rec.BeginSegment(ctx, e.run(), "start", startInput())
	s1.BeginTool("c1", "", "plan_update", nil).End(ctx, ToolEnd{Status: "ok"})
	s1.End(ctx, SegmentEnd{})

	in := startInput()
	in["mode"] = "continue"
	s2 := e.rec.BeginSegment(ctx, e.run(), "approval", in)
	if s2.SegmentSeq() != 2 {
		t.Fatalf("第二个片段的序号应接着数: %d", s2.SegmentSeq())
	}
	s2.BeginTool("c2", "", "plan_update", nil).End(ctx, ToolEnd{Status: "ok"})
	s2.End(ctx, SegmentEnd{})

	second := e.stores.st.lines(t, "agent-traces/2026/10/20/42/2.jsonl.gz")
	for _, l := range second {
		if l["kind"] == "canvas" && l["phase"] == "before" {
			t.Error("续跑的片段不该再留开跑前的快照")
		}
	}
	tools := e.repo.spansOf(model.TraceSpanExecuteTool)
	if len(tools) != 2 || tools[1].Seq <= tools[0].Seq || tools[1].SegmentSeq != 2 {
		t.Errorf("运行内的序号应跨片段递增: %+v", tools)
	}
}

// codemode 的嵌套调用：parent_span_id 指向外层 Agent 工具的 span。
func TestTrace_NestedToolPointsToParentSpan(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, nil)
	seg := e.rec.BeginSegment(ctx, e.run(), "start", startInput())
	seg.BeginAgentTool("outer", "codemode")
	seg.BeginTool("inner1", "outer", "canvas_apply_ops", json.RawMessage(`{"ops":[]}`)).End(ctx, ToolEnd{Status: "ok"})
	seg.BeginTool("orphan", "unknown-parent", "canvas_apply_ops", nil).End(ctx, ToolEnd{Status: "ok"})
	seg.EndAgentTool(ctx, "outer", true, "脚本超时")
	seg.End(ctx, SegmentEnd{})

	tools := e.repo.spansOf(model.TraceSpanExecuteTool)
	by := map[string]model.AgentTraceSpan{}
	for _, s := range tools {
		by[s.ToolCallID] = s
	}
	if len(by) != 3 {
		t.Fatalf("应有外层 + 两个嵌套: %+v", tools)
	}
	if by["inner1"].ParentSpanID != by["outer"].SpanID || by["outer"].SpanID == "" {
		t.Errorf("嵌套调用应指向外层 span: %+v", by)
	}
	if by["orphan"].ParentSpanID != "" {
		t.Errorf("找不到外层时 parent_span_id 留空，不能乱指: %+v", by["orphan"])
	}
	if by["outer"].Status != "error" || by["outer"].Summary != "脚本超时" {
		t.Errorf("外层工具的结束信息不对: %+v", by["outer"])
	}
	lines := e.stores.st.lines(t, traceKey1)
	found := false
	for _, l := range lines {
		if l["tool_call_id"] == "inner1" && l["parent_tool_call_id"] == "outer" {
			found = true
		}
	}
	if !found {
		t.Error("原文里嵌套调用要带 parent_tool_call_id")
	}
}

// 单个 span 的原文超过上限：截断并标记，行不超过上限；索引照常。
func TestTrace_SpanOverLimitIsTruncated(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, func(o *TraceOptions) { o.MaxSpanBytes = 2048 })
	seg := e.rec.BeginSegment(ctx, e.run(), "start", startInput())
	seg.BeginTool("c1", "", "canvas_get_state", json.RawMessage(`{}`)).End(ctx, ToolEnd{Status: "ok", Content: strings.Repeat("长内容", 5000)})
	seg.End(ctx, SegmentEnd{})

	for _, l := range e.stores.st.lines(t, traceKey1) {
		if l["kind"] != "execute_tool" {
			continue
		}
		b, _ := json.Marshal(l)
		if len(b) > 2048 || l["truncated"] != true {
			t.Errorf("应截断到上限内并标 truncated: len=%d truncated=%v", len(b), l["truncated"])
		}
		if !strings.Contains(l["result"].(string), "已截断") {
			t.Error("截断处要写明丢了多少")
		}
	}
	if tools := e.repo.spansOf(model.TraceSpanExecuteTool); len(tools) != 1 || tools[0].Bytes == 0 || tools[0].Bytes > 2048 {
		t.Errorf("索引里的字节数是实际写入的: %+v", tools)
	}
}

// 模型请求的历史太长：先收短字符串，再丢中间的消息，保留第一条和最后一条。
func TestTrace_HugeChatRequestKeepsFirstAndLastMessage(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, func(o *TraceOptions) { o.MaxSpanBytes = 4096 })
	seg := e.rec.BeginSegment(ctx, e.run(), "start", startInput())
	var msgs []string
	for i := 0; i < 40; i++ {
		msgs = append(msgs, `{"role":"user","content":"消息`+strings.Repeat("x", 300)+`"}`)
	}
	msgs[0] = `{"role":"user","content":"最初的任务"}`
	msgs[39] = `{"role":"user","content":"最后一条"}`
	ct := seg.BeginChat("", "m", []byte(`{"messages":[`+strings.Join(msgs, ",")+`]}`))
	ct.Record(&llmgateway.Result{Text: "ok"}, nil, nil)
	seg.End(ctx, SegmentEnd{})
	for _, l := range e.stores.st.lines(t, traceKey1) {
		if l["kind"] != "chat" {
			continue
		}
		b, _ := json.Marshal(l)
		if len(b) > 4096 || l["truncated"] != true {
			t.Fatalf("应截断进上限: len=%d %v", len(b), l["truncated"])
		}
		s := string(b)
		if !strings.Contains(s, "最初的任务") || !strings.Contains(s, "最后一条") || !strings.Contains(s, "omitted_messages") {
			t.Errorf("应保留首尾消息并标出丢了几条: %s", s)
		}
	}
}

// 整轮上限用完以后只记索引：原文不再写，但索引行照写，片段摘要里说明。
func TestTrace_RunOverLimitOnlyIndexes(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, func(o *TraceOptions) { o.MaxSpanBytes = 4096; o.MaxRunBytes = 6000 })
	seg := e.rec.BeginSegment(ctx, e.run(), "start", startInput())
	for i := 0; i < 6; i++ {
		seg.BeginTool("c"+string(rune('a'+i)), "", "t", nil).End(ctx, ToolEnd{Status: "ok", Content: strings.Repeat("y", 2000)})
	}
	seg.End(ctx, SegmentEnd{})

	if got := len(e.repo.spansOf(model.TraceSpanExecuteTool)); got != 6 {
		t.Fatalf("索引行一条都不能少: %d", got)
	}
	written := 0
	for _, l := range e.stores.st.lines(t, traceKey1) {
		if l["kind"] == "execute_tool" {
			written++
		}
	}
	if written == 0 || written >= 6 {
		t.Errorf("上限用完后原文应停止写入（写了 %d 条）", written)
	}
	segs := e.repo.spansOf(model.TraceSpanSegment)
	if len(segs) != 1 || !strings.Contains(segs[0].Summary, "上限") {
		t.Errorf("片段摘要应说明超出整轮上限: %+v", segs)
	}
	zeroBytes := 0
	for _, s := range e.repo.spansOf(model.TraceSpanExecuteTool) {
		if s.Bytes == 0 {
			zeroBytes++
		}
	}
	if zeroBytes != 6-written {
		t.Errorf("没写原文的索引行 bytes 为 0: %d", zeroBytes)
	}
}

// 画布快照不受整轮上限拦截（它是复盘最需要的），但仍计入总字节数。
func TestTrace_CanvasSnapshotBypassesRunLimit(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, func(o *TraceOptions) { o.MaxSpanBytes = 2048; o.MaxRunBytes = 6000 })
	e.repo.canvas.PayloadJSON = []byte(`{"nodes":[{"id":"` + strings.Repeat("n", 3000) + `"}],"edges":[]}`)
	seg := e.rec.BeginSegment(ctx, e.run(), "start", startInput())
	seg.End(ctx, SegmentEnd{})
	phases := 0
	for _, l := range e.stores.st.lines(t, traceKey1) {
		if l["kind"] == "canvas" {
			phases++
		}
	}
	if phases != 2 {
		t.Errorf("前后两份快照都应保留: %d", phases)
	}
}

// 上传失败：这个片段的原文丢失，运行标为轨迹不完整，索引照写（没有对象 key），不影响调用方。
func TestTrace_UploadFailureMarksIncomplete(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, nil)
	e.stores.st.putErr = errors.New("存储不可用")
	seg := e.rec.BeginSegment(ctx, e.run(), "start", startInput())
	seg.BeginTool("c1", "", "t", nil).End(ctx, ToolEnd{Status: "ok"})
	seg.End(ctx, SegmentEnd{})

	segs := e.repo.spansOf(model.TraceSpanSegment)
	if len(segs) != 1 || segs[0].ObjectKey != "" || segs[0].StorageID != 0 {
		t.Errorf("上传失败的片段索引不带对象: %+v", segs)
	}
	if len(e.repo.updates) != 1 || !e.repo.updates[0].Incomplete {
		t.Errorf("应把运行标为不完整: %+v", e.repo.updates)
	}
	if len(e.tempFiles(t)) != 0 {
		t.Errorf("失败后临时文件也要清掉，免得磁盘越积越多: %v", e.tempFiles(t))
	}
}

// 写索引失败也只是标不完整，不 panic、不影响后面的记录。
func TestTrace_IndexFailureMarksIncomplete(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, nil)
	e.repo.spanErr = errors.New("数据库挂了")
	seg := e.rec.BeginSegment(ctx, e.run(), "start", startInput())
	seg.BeginTool("c1", "", "t", nil).End(ctx, ToolEnd{Status: "ok"})
	seg.End(ctx, SegmentEnd{})
	if len(e.repo.updates) != 1 || !e.repo.updates[0].Incomplete {
		t.Errorf("索引写失败应标为不完整: %+v", e.repo.updates)
	}
	if len(e.stores.st.keys()) != 1 {
		t.Error("原文照样上传")
	}
}

// 读不到画布、读不到序号：只是少一份快照 / 不记这个片段，调用方照常进行。
func TestTrace_SetupFailuresNeverBreakTheRun(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, nil)
	e.repo.canvasErr = errors.New("读画布失败")
	seg := e.rec.BeginSegment(ctx, e.run(), "start", startInput())
	if seg == nil {
		t.Fatal("读不到画布快照不该让片段起不来")
	}
	seg.End(ctx, SegmentEnd{})
	for _, l := range e.stores.st.lines(t, traceKey1) {
		if l["kind"] == "canvas" {
			t.Error("读不到就不记快照")
		}
	}

	// 临时目录建不出来（路径被一个文件占着）：不记这个片段，返回 nil
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	e2 := newTraceEnv(t, func(o *TraceOptions) { o.Dir = filepath.Join(blocked, "sub") })
	if e2.rec.BeginSegment(ctx, e2.run(), "start", startInput()) != nil {
		t.Error("临时目录不可用时应返回 nil（不记录），而不是报错")
	}
}

// 进程崩溃：已经写下的部分照样上传，片段标 aborted，运行标不完整。
func TestTrace_CrashedSegmentUploadsPartialAndMarksIncomplete(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, nil)
	seg := e.rec.BeginSegment(ctx, e.run(), "start", startInput())
	seg.BeginTool("c1", "", "t", nil).End(ctx, ToolEnd{Status: "ok"})
	seg.End(ctx, SegmentEnd{ExitCode: -1, StderrTail: "Segmentation fault", Crashed: true})

	if len(e.stores.st.keys()) != 1 {
		t.Fatal("崩溃的片段也要上传已写的部分")
	}
	segs := e.repo.spansOf(model.TraceSpanSegment)
	if len(segs) != 1 || segs[0].Status != "aborted" || segs[0].ErrorCode != "crashed" {
		t.Errorf("片段应标 aborted: %+v", segs)
	}
	if !e.repo.updates[0].Incomplete {
		t.Error("崩溃应把运行标为轨迹不完整")
	}
	last := e.stores.st.lines(t, traceKey1)
	if last[len(last)-1]["stderr_tail"] != "Segmentation fault" {
		t.Errorf("片段行应带 stderr 末尾: %v", last[len(last)-1])
	}
}

// 没有真正启动的片段（进程起不来）：放弃，不上传、不写索引、临时文件删掉。
func TestTrace_AbandonLeavesNothing(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, nil)
	seg := e.rec.BeginSegment(ctx, e.run(), "start", startInput())
	seg.Abandon()
	seg.End(ctx, SegmentEnd{}) // 放弃之后再 End 无效
	if len(e.tempFiles(t)) != 0 || len(e.repo.spans) != 0 || len(e.stores.st.keys()) != 0 || len(e.repo.updates) != 0 {
		t.Error("放弃的片段不留任何痕迹")
	}
}

// 片段结束后晚到的记录被丢弃，不会 panic，也不会写到已关闭的文件。
func TestTrace_LateRecordsAfterEndAreDropped(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, nil)
	seg := e.rec.BeginSegment(ctx, e.run(), "start", startInput())
	ct := seg.BeginChat("", "m", []byte(`{}`))
	seg.End(ctx, SegmentEnd{})
	ct.Record(&llmgateway.Result{Text: "晚到"}, nil, nil)
	seg.End(ctx, SegmentEnd{}) // 重复 End 无效
	if len(e.repo.updates) != 1 {
		t.Errorf("重复 End 不该重复上传和更新: %d", len(e.repo.updates))
	}
}

// 启动时找回上次没来得及上传的临时文件：补传、写索引、标不完整、删文件；本次启动之后新建的文件不动。
func TestTrace_RecoverOrphans(t *testing.T) {
	ctx := context.Background()
	e := newTraceEnv(t, nil)
	if err := os.MkdirAll(e.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(e.dir, "42-2-20261020-"+itoa64(e.now.Add(-time.Hour).UnixNano())+".jsonl")
	fresh := filepath.Join(e.dir, "43-1-20261020-"+itoa64(e.now.Add(time.Hour).UnixNano())+".jsonl")
	junk := filepath.Join(e.dir, "garbage.jsonl")
	for _, p := range []string{old, fresh, junk} {
		if err := os.WriteFile(p, []byte(`{"kind":"system"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.rec.RecoverOrphans(ctx); n != 1 {
		t.Fatalf("应只补传 1 个孤儿: %d", n)
	}
	keys := e.stores.st.keys()
	if len(keys) != 1 || keys[0] != "agent-traces/2026/10/20/42/2.jsonl.gz" {
		t.Errorf("补传的 key: %v", keys)
	}
	segs := e.repo.spansOf(model.TraceSpanSegment)
	if len(segs) != 1 || segs[0].RunID != 42 || segs[0].SegmentSeq != 2 || segs[0].Status != "aborted" || segs[0].ObjectKey != keys[0] {
		t.Errorf("补传后应有一条 aborted 的片段索引: %+v", segs)
	}
	if len(e.repo.updates) != 1 || !e.repo.updates[0].Incomplete {
		t.Errorf("补传后应把运行标为不完整: %+v", e.repo.updates)
	}
	left := e.tempFiles(t)
	if len(left) != 1 || !strings.HasPrefix(left[0], "43-1-") {
		t.Errorf("孤儿和垃圾文件要删，本次启动后新建的要留: %v", left)
	}
}

func itoa64(n int64) string { b, _ := json.Marshal(n); return string(b) }
