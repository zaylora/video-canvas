package agent

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/agent/prompts"
	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// 轨迹记录器：把每轮运行的模型请求 / 响应与工具调用记下来，设计见 docs/design/Agent调优Harness/Agent调优Harness.md 第 6.B.2 节。
//
// 原文以「每个运行片段一个对象」存放：片段运行期间一行一行追加到后端本机的临时文件，片段结束时压缩上传到素材所用的对象存储，
// key 是 agent-traces/{yyyy}/{mm}/{dd}/{run_id}/{segment_seq}.jsonl.gz；索引放数据库（工具和片段在 agent_trace_spans，
// 模型调用在 agent_model_calls）。
//
// 铁律：记录器的任何失败都只打 warn 日志，绝不能影响用户的运行。所以对外的方法都不返回错误；
// *TraceRecorder、*SegmentTrace、*ChatTrace、*ToolTrace 的方法全部支持 nil 接收者（等于空操作），
// 调用方不用判断有没有开轨迹。

// 轨迹记录的固定参数。
const (
	traceKeyPrefix     = "agent-traces"     // 对象存储里的 key 前缀；/files 公开路由只认有素材记录的 key，读不到它
	traceContentType   = "application/gzip" // 原文对象的类型
	traceDBTimeout     = 5 * time.Second    // 单次写索引的超时
	traceUploadTimeout = 30 * time.Second   // 片段结束时收尾（快照、上传、写索引）的总超时
	maxStderrTailBytes = 4 << 10            // 片段行里标准错误末尾的上限
	maxPurposeLen      = 16                 // 与 agent_model_calls.purpose 的列宽一致
	traceSpanSummary   = 255                // 与索引表 summary 的列宽一致
	traceTempExt       = ".jsonl"           // 本机临时文件的扩展名，启动时据此找回上次没来得及上传的
	traceDefaultDir    = "agent-traces"     // 临时目录名
	traceIDRandBytes   = 4                  // span 标识里的随机部分，8 个十六进制字符
	chatPurposeDefault = "chat"             // 没有标明用途的模型调用
	recoveredLaunch    = "recovered"        // 启动时找回的孤儿片段
)

// TraceRepo 是记录器需要的数据访问，由 *repository.AgentRepository 实现。
type TraceRepo interface {
	// CreateTraceSpan 写一条工具或片段的索引行。
	CreateTraceSpan(ctx context.Context, s *model.AgentTraceSpan) error
	// TraceCounters 返回某运行已用到的最大 span 序号和最大片段序号。
	TraceCounters(ctx context.Context, runID uint64) (maxSeq, maxSegment int, err error)
	// UpdateRunTrace 更新运行的轨迹字段（字节数、完整性、工具声明 hash、技能版本）。
	UpdateRunTrace(ctx context.Context, runID uint64, in repository.RunTraceUpdate) error
	// GetCanvas 按 id + user_id 读画布，用来留开跑前和结束时的画布快照。
	GetCanvas(ctx context.Context, userID, id uint64) (*model.CanvasProject, error)
}

// TraceStores 是轨迹原文所在的对象存储：新对象写默认存储，读取和删除按索引行记下的 storage_id，换了默认存储也找得到旧的。
type TraceStores interface {
	// Default 返回当前默认存储。
	Default(ctx context.Context) (*storage.Handle, error)
	// Get 按存储配置 id 取存储。
	Get(ctx context.Context, id uint64) (*storage.Handle, error)
}

// TraceOptions 是记录器的配置，来自 config.Agent.Trace。
type TraceOptions struct {
	Enabled      bool             // 是否记录；false 时所有方法都是空操作
	MaxSpanBytes int              // 单个 span 原文的上限
	MaxRunBytes  int64            // 单个运行原文的总上限，超出后只记索引
	Dir          string           // 本机临时文件目录，空表示系统临时目录下的 agent-traces
	Now          func() time.Time // 时钟，测试注入
}

// TraceRecorder 是轨迹记录器。每个运行片段用 BeginSegment 开一个 SegmentTrace，片段内的记录都走它。
type TraceRecorder struct {
	repo   TraceRepo
	stores TraceStores
	opt    TraceOptions
	bootAt time.Time // 记录器创建的时刻：比它早的临时文件是上次进程留下的孤儿，比它晚的是正在写的
}

// NewTraceRecorder 创建记录器。
func NewTraceRecorder(repo TraceRepo, stores TraceStores, opt TraceOptions) *TraceRecorder {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Dir == "" {
		opt.Dir = filepath.Join(os.TempDir(), traceDefaultDir)
	}
	if opt.MaxSpanBytes <= 0 {
		opt.MaxSpanBytes = 256 << 10
	}
	if opt.MaxRunBytes <= 0 {
		opt.MaxRunBytes = 8 << 20
	}
	return &TraceRecorder{repo: repo, stores: stores, opt: opt, bootAt: opt.Now()}
}

// Enabled 返回记录器是否开着。nil 记录器视为关闭。
func (r *TraceRecorder) Enabled() bool { return r != nil && r.opt.Enabled }

// newSpanID 生成 span 标识：前缀（c 模型调用 / t 工具 / s 片段）加 8 个随机十六进制字符。
// 用随机数而不是自增，是因为一个运行的多个片段可能由不同实例记录，不需要它们之间协调。
func newSpanID(prefix string) string {
	var b [traceIDRandBytes]byte
	_, _ = rand.Read(b[:]) // crypto/rand 在受支持的平台上不会失败
	return prefix + hex.EncodeToString(b[:])
}

// ToolsHash 是工具声明 JSON 的 sha256 前 16 个十六进制字符，run 上的 tools_hash 和 system 行里的都是它。
func ToolsHash(toolsJSON []byte) string {
	sum := sha256.Sum256(toolsJSON)
	return hex.EncodeToString(sum[:8])
}

// SegmentTrace 是一个运行片段的记录句柄。
type SegmentTrace struct {
	rec        *TraceRecorder
	runID      uint64
	userID     uint64
	canvasID   uint64
	runCreated time.Time
	segSeq     int
	launch     string
	spanID     string
	seq        int
	startedAt  time.Time
	toolsHash  string
	seqCounter atomic.Int64

	mu         sync.Mutex
	f          *os.File
	path       string
	runUsed    int64 // 开始这个片段时运行已记录的字节数
	added      int64 // 本片段新记录的字节数
	overflow   bool  // 单个运行的上限用完，后面的原文只记索引
	broken     bool  // 本机文件写失败，后面不再写
	incomplete bool  // 索引或原文没能记全：写索引失败、上传失败、进程崩溃
	ended      bool
	open       map[string]*ToolTrace // 还没结束的 Agent 自己的工具（如 codemode），嵌套调用靠它找到外层 span
	pins       func() map[string]uint64
}

// BeginSegment 为一个运行片段开始记录：开本机临时文件，写 system 行；启动方式是 start 时顺便留下开跑前的画布快照。
// input 是即将交给 Node 的启动参数，从里面取 system_prompt 和 tools。
// 没开轨迹、运行不记轨迹、或任何一步出错都返回 nil（后续调用全是空操作），并打 warn 日志。
func (r *TraceRecorder) BeginSegment(ctx context.Context, run *model.AgentRun, launch string, input map[string]any) *SegmentTrace {
	if !r.Enabled() || run == nil || !run.TraceEnabled {
		return nil
	}
	maxSeq, maxSeg, err := r.repo.TraceCounters(ctx, run.ID)
	if err != nil {
		logger.Warn("轨迹：读取序号失败，本片段不记录", zap.Error(err), zap.Uint64("run_id", run.ID))
		return nil
	}
	now := r.opt.Now()
	s := &SegmentTrace{rec: r, runID: run.ID, userID: run.UserID, canvasID: run.CanvasID, runCreated: run.CreatedAt, segSeq: maxSeg + 1,
		launch: launch, spanID: newSpanID("s"), startedAt: now, runUsed: run.TraceBytes, open: map[string]*ToolTrace{}}
	s.seqCounter.Store(int64(maxSeq))
	s.seq = s.nextSeq()
	if err := os.MkdirAll(r.opt.Dir, 0o700); err != nil {
		logger.Warn("轨迹：创建临时目录失败，本片段不记录", zap.Error(err), zap.String("dir", r.opt.Dir))
		return nil
	}
	s.path = filepath.Join(r.opt.Dir, fmt.Sprintf("%d-%d-%s-%d%s", run.ID, s.segSeq, s.runCreated.UTC().Format("20060102"), now.UnixNano(), traceTempExt))
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		logger.Warn("轨迹：创建临时文件失败，本片段不记录", zap.Error(err), zap.String("path", s.path))
		return nil
	}
	s.f = f
	s.writeSystem(run, input)
	if launch == "start" {
		s.snapshotCanvas(ctx, "before")
	}
	return s
}

// nextSeq 分配运行内的下一个序号。
func (s *SegmentTrace) nextSeq() int { return int(s.seqCounter.Add(1)) }

// SegmentSeq 返回这个片段是运行的第几个片段；nil 返回 0。
func (s *SegmentTrace) SegmentSeq() int {
	if s == nil {
		return 0
	}
	return s.segSeq
}

// writeSystem 写第一行 system：提示词版本、工具声明 hash、系统提示词全文和工具声明全文。
func (s *SegmentTrace) writeSystem(run *model.AgentRun, input map[string]any) {
	tools, err := json.Marshal(input["tools"])
	if err != nil || len(tools) == 0 || string(tools) == "null" {
		tools = []byte("[]")
	}
	s.toolsHash = ToolsHash(tools)
	version := run.PromptVersion
	if version == 0 {
		version = prompts.Version
	}
	prompt, _ := input["system_prompt"].(string)
	// 系统提示词和工具声明是固定内容，不受单 span 上限约束（截断了就失去「当时是哪一版」的意义），但仍计入整轮字节数
	s.appendLine(map[string]any{"seq": 0, "kind": "system", "prompt_version": version, "tools_hash": s.toolsHash,
		"system_prompt": prompt, "tools": json.RawMessage(tools)}, appendEssential)
}

// snapshotCanvas 读画布当前的 payload_json 留一份快照（phase 为 before 或 after）。
// 快照是复盘最需要的东西，所以不受整轮上限拦截，但每份不超过整轮上限，也计入总字节数。
func (s *SegmentTrace) snapshotCanvas(ctx context.Context, phase string) {
	c, err := s.rec.repo.GetCanvas(ctx, s.userID, s.canvasID)
	if err != nil {
		logger.Warn("轨迹：读取画布快照失败", zap.Error(err), zap.Uint64("run_id", s.runID), zap.String("phase", phase))
		return
	}
	if int64(len(c.PayloadJSON)) > s.rec.opt.MaxRunBytes {
		logger.Warn("轨迹：画布快照超过整轮上限，不记录", zap.Uint64("run_id", s.runID), zap.String("phase", phase), zap.Int("bytes", len(c.PayloadJSON)))
		return
	}
	var payload any = json.RawMessage(c.PayloadJSON)
	if !json.Valid(c.PayloadJSON) {
		payload = string(c.PayloadJSON)
	}
	s.appendLine(map[string]any{"kind": "canvas", "phase": phase, "segment_seq": s.segSeq, "revision": c.Revision, "payload": payload}, appendCanvas)
}

// appendMode 决定一行原文受哪些上限约束。
type appendMode int

const (
	appendSpan      appendMode = iota // 普通 span：受单 span 和整轮上限约束
	appendEssential                   // system 行：不截断，但计入整轮字节数
	appendCanvas                      // 画布快照：不截断、不受整轮上限拦截，计入字节数
)

// appendLine 把一行原文追加到本机临时文件，返回写入的字节数、有没有被截断、有没有真正写入。
func (s *SegmentTrace) appendLine(line map[string]any, mode appendMode) (n int, truncated, written bool) {
	var b []byte
	if mode == appendSpan {
		b, truncated = fitLine(line, s.rec.opt.MaxSpanBytes)
	} else {
		b = marshalLine(line)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended || s.broken || s.f == nil {
		return 0, truncated, false
	}
	if mode == appendSpan && s.runUsed+s.added+int64(len(b))+1 > s.rec.opt.MaxRunBytes {
		s.overflow = true // 整轮的原文预算用完，这一行只留索引
		return 0, truncated, false
	}
	b = append(b, '\n')
	if _, err := s.f.Write(b); err != nil {
		s.broken = true
		logger.Warn("轨迹：写本机临时文件失败，本片段后面不再记录原文", zap.Error(err), zap.Uint64("run_id", s.runID))
		return 0, truncated, false
	}
	s.added += int64(len(b))
	return len(b), truncated, true
}

// Abandon 放弃这个片段的记录并删掉临时文件：片段没能真正启动时用，不上传、不写索引。
func (s *SegmentTrace) Abandon() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.ended = true
	s.closeFileLocked()
	_ = os.Remove(s.path) // 临时文件清不掉只是留一点垃圾，下次启动会当孤儿处理
}

func (s *SegmentTrace) closeFileLocked() {
	if s.f != nil {
		_ = s.f.Close() // 只写文件，关闭失败不影响已经写进去的内容
		s.f = nil
	}
}

// SetSkillPins 登记取技能版本的函数；片段结束时把本片段固定的技能版本合并进运行行。
func (s *SegmentTrace) SetSkillPins(f func() map[string]uint64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.pins = f
	s.mu.Unlock()
}

// ---- 模型调用 ----

// CallTraceMeta 是模型调用开始时写进计费记录的轨迹字段。
type CallTraceMeta struct {
	SpanID     string
	Seq        int
	Purpose    string
	SegmentSeq int
}

// CallTrace 是模型调用结束时写回计费记录的轨迹字段。
type CallTrace struct {
	FirstTokenMS  int
	DurationMS    int
	FinishReason  string
	UpstreamModel string
	ChannelID     string
}

// ChatTrace 是一次模型调用的记录句柄。
type ChatTrace struct {
	seg      *SegmentTrace
	spanID   string
	seq      int
	purpose  string
	start    time.Time
	body     []byte
	modelKey string
	firstNS  atomic.Int64 // 第一个增量到达的时刻（UnixNano），0 表示还没收到
}

// BeginChat 开始记录一次模型调用。body 是 Node 发来的请求体原文（不含请求头）。purpose 为空按 chat 算。
func (s *SegmentTrace) BeginChat(purpose, modelKey string, body []byte) *ChatTrace {
	if s == nil {
		return nil
	}
	if purpose == "" {
		purpose = chatPurposeDefault
	}
	if len(purpose) > maxPurposeLen {
		purpose = purpose[:maxPurposeLen]
	}
	return &ChatTrace{seg: s, spanID: newSpanID("c"), seq: s.nextSeq(), purpose: purpose, start: s.rec.opt.Now(), body: body, modelKey: modelKey}
}

// Meta 返回写进计费记录的开始字段；nil 返回零值（计费记录里就是默认值）。
func (c *ChatTrace) Meta() CallTraceMeta {
	if c == nil {
		return CallTraceMeta{}
	}
	return CallTraceMeta{SpanID: c.spanID, Seq: c.seq, Purpose: c.purpose, SegmentSeq: c.seg.segSeq}
}

// MarkFirstToken 记下第一个增量到达的时刻，之后的调用无效。
func (c *ChatTrace) MarkFirstToken() {
	if c == nil {
		return
	}
	c.firstNS.CompareAndSwap(0, c.seg.rec.opt.Now().UnixNano())
}

// Outcome 返回结算时要写回计费记录的字段。upstreamModel 和 channel 来自模型快照。
func (c *ChatTrace) Outcome(res *llmgateway.Result, upstreamModel, channel string) *CallTrace {
	if c == nil {
		return nil
	}
	now := c.seg.rec.opt.Now()
	out := &CallTrace{DurationMS: max(int(now.Sub(c.start).Milliseconds()), 1), UpstreamModel: upstreamModel, ChannelID: channel}
	if ns := c.firstNS.Load(); ns != 0 {
		out.FirstTokenMS = max(int(time.Unix(0, ns).Sub(c.start).Milliseconds()), 0)
	}
	if res != nil {
		out.FinishReason = res.FinishReason
	}
	return out
}

// Record 把这次调用的原文写进轨迹：请求体（脱敏后）、响应、用量、扣费。settled 为 nil 表示结算失败。
func (c *ChatTrace) Record(res *llmgateway.Result, serr error, settled *SettleResult) {
	if c == nil {
		return
	}
	line := map[string]any{"span_id": c.spanID, "kind": "chat", "seq": c.seq, "name": c.modelKey, "purpose": c.purpose,
		"request": SanitizeChatRequest(c.body, c.seg.toolsHash), "truncated": false}
	if res != nil {
		calls := make([]map[string]any, len(res.ToolCalls))
		for i, tc := range res.ToolCalls {
			calls[i] = map[string]any{"id": tc.ID, "name": tc.Name, "arguments": tc.Arguments}
		}
		line["response"] = map[string]any{"text": res.Text, "thinking": res.Thinking, "tool_calls": calls, "finish_reason": res.FinishReason}
		line["usage"] = map[string]any{"input": res.Usage.InputTokens, "output": res.Usage.OutputTokens, "cached": res.Usage.CachedTokens}
	}
	if settled != nil {
		line["credits"] = settled.Charged
	}
	if serr != nil {
		line["error"] = callError(serr)
	}
	c.seg.appendLine(line, appendSpan)
}

// ---- 工具调用 ----

// ToolTrace 是一次工具调用的记录句柄。
type ToolTrace struct {
	seg        *SegmentTrace
	spanID     string
	parentSpan string
	seq        int
	toolCallID string
	parentCall string
	name       string
	args       json.RawMessage
	start      time.Time
}

// BeginTool 开始记录一次工具调用。parentToolCallID 非空表示这是 codemode 脚本里发起的嵌套调用，
// 它的 parent_span_id 指向外层调用（外层是 Agent 自己的工具，经 BeginAgentTool 登记）。
func (s *SegmentTrace) BeginTool(toolCallID, parentToolCallID, name string, args json.RawMessage) *ToolTrace {
	if s == nil {
		return nil
	}
	t := &ToolTrace{seg: s, spanID: newSpanID("t"), seq: s.nextSeq(), toolCallID: toolCallID, parentCall: parentToolCallID, name: name, args: args, start: s.rec.opt.Now()}
	if parentToolCallID != "" {
		s.mu.Lock()
		if p := s.open[parentToolCallID]; p != nil {
			t.parentSpan = p.spanID
		}
		s.mu.Unlock()
	}
	return t
}

// BeginAgentTool 登记 Agent 自己的工具（如 codemode）开始：它不经桥的 /tool，只有开始和结束两次报备，
// 但它里面的嵌套调用要能找到它的 span。
func (s *SegmentTrace) BeginAgentTool(toolCallID, name string) {
	if s == nil {
		return
	}
	t := s.BeginTool(toolCallID, "", name, nil)
	s.mu.Lock()
	s.open[toolCallID] = t
	s.mu.Unlock()
}

// EndAgentTool 登记 Agent 自己的工具结束。没有对应的开始（比如开始时记录器还没开）也不是错误。
func (s *SegmentTrace) EndAgentTool(ctx context.Context, toolCallID string, isError bool, summary string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	t := s.open[toolCallID]
	delete(s.open, toolCallID)
	s.mu.Unlock()
	status := model.TraceStatusOK
	if isError {
		status = model.TraceStatusError
	}
	t.End(ctx, ToolEnd{Status: status, IsError: isError, Content: summary, Summary: summary})
}

// ToolEnd 是一次工具调用的结果。
type ToolEnd struct {
	Status     string // model.TraceStatus*
	ErrorCode  string // 错误分类，成功为空
	IsError    bool   // 返回给模型的结果是不是错误
	Content    string // 返回给模型的文本原文
	Summary    string // 一行摘要
	MutationID uint64 // 关联的画布改动，没有为 0
}

// End 写工具调用的原文和索引行。
func (t *ToolTrace) End(ctx context.Context, e ToolEnd) {
	if t == nil {
		return
	}
	s := t.seg
	now := s.rec.opt.Now()
	var args any = t.args
	if len(t.args) == 0 {
		args = map[string]any{}
	} else if !json.Valid(t.args) {
		args = string(t.args)
	}
	line := map[string]any{"span_id": t.spanID, "kind": "execute_tool", "seq": t.seq, "name": t.name, "tool_call_id": t.toolCallID,
		"args": args, "result": e.Content, "is_error": e.IsError, "truncated": false}
	if t.parentCall != "" {
		line["parent_tool_call_id"] = t.parentCall
	}
	if e.MutationID != 0 {
		line["mutation_id"] = e.MutationID
	}
	n, _, _ := s.appendLine(line, appendSpan)
	row := &model.AgentTraceSpan{RunID: s.runID, Seq: t.seq, SpanID: t.spanID, ParentSpanID: t.parentSpan, Kind: model.TraceSpanExecuteTool, Name: t.name,
		ToolCallID: t.toolCallID, Status: e.Status, ErrorCode: e.ErrorCode, Summary: truncateRunes(e.Summary, traceSpanSummary), DurationMS: int(now.Sub(t.start).Milliseconds()),
		Bytes: n, SegmentSeq: s.segSeq, StartedAt: t.start, EndedAt: &now}
	if e.MutationID != 0 {
		row.MutationID = &e.MutationID
	}
	s.createSpan(ctx, row)
}

// createSpan 写索引行，失败只打日志。用不会被取消的上下文：工具调用的请求断了，索引也要落下。
func (s *SegmentTrace) createSpan(ctx context.Context, row *model.AgentTraceSpan) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), traceDBTimeout)
	defer cancel()
	if err := s.rec.repo.CreateTraceSpan(wctx, row); err != nil {
		logger.Warn("轨迹：写索引失败", zap.Error(err), zap.Uint64("run_id", s.runID), zap.String("span_id", row.SpanID))
		s.markIncomplete()
	}
}

// markIncomplete 标记这个片段的轨迹不完整（索引或原文没能记全）。
func (s *SegmentTrace) markIncomplete() {
	s.mu.Lock()
	s.incomplete = true
	s.mu.Unlock()
}

// isIncomplete 返回本片段的轨迹是否已被标为不完整；本机文件写失败也算。
func (s *SegmentTrace) isIncomplete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.incomplete || s.broken
}

// ---- 片段结束 ----

// SegmentEnd 是片段结束时的信息。
type SegmentEnd struct {
	ExitCode   int    // 进程退出码
	StderrTail string // 标准错误末尾
	Crashed    bool   // 进程没报告结束就没了（崩溃、被杀）：轨迹标为不完整
}

// End 结束片段：留结束时的画布快照，写片段行，压缩上传原文对象，写索引，更新运行行的轨迹字段。
// 只能调用一次，之后的调用无效；任何一步失败只打日志，不影响调用方（进程退出后的收尾）。
func (s *SegmentTrace) End(ctx context.Context, e SegmentEnd) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), traceUploadTimeout)
	defer cancel()

	s.snapshotCanvas(ctx, "after")
	now := s.rec.opt.Now()
	status := model.TraceStatusOK
	if e.Crashed {
		status = model.TraceStatusAborted
	} else if e.ExitCode != 0 {
		status = model.TraceStatusError
	}
	tail := strings.ToValidUTF8(e.StderrTail, "")
	if len(tail) > maxStderrTailBytes {
		tail = tail[len(tail)-maxStderrTailBytes:]
	}
	s.appendLine(map[string]any{"span_id": s.spanID, "kind": "segment", "seq": s.seq, "name": s.launch, "launch": s.launch, "segment_seq": s.segSeq,
		"exit_code": e.ExitCode, "stderr_tail": tail, "status": status}, appendEssential)

	s.mu.Lock()
	s.ended = true
	s.closeFileLocked()
	overflow, added, pins := s.overflow, s.added, s.pins
	s.mu.Unlock()
	if e.Crashed {
		s.markIncomplete()
	}

	row := &model.AgentTraceSpan{RunID: s.runID, Seq: s.seq, SpanID: s.spanID, Kind: model.TraceSpanSegment, Name: s.launch, Status: status,
		Summary: segmentSummary(e, overflow), DurationMS: int(now.Sub(s.startedAt).Milliseconds()), Bytes: int(added), SegmentSeq: s.segSeq,
		StartedAt: s.startedAt, EndedAt: &now}
	if e.Crashed {
		row.ErrorCode = "crashed"
	} else if e.ExitCode != 0 {
		row.ErrorCode = "exit_" + strconv.Itoa(e.ExitCode)
	}
	storageID, key, err := s.upload(ctx)
	if err != nil {
		logger.Warn("轨迹：上传原文失败，这个片段的原文丢失", zap.Error(err), zap.Uint64("run_id", s.runID), zap.Int("segment_seq", s.segSeq))
		s.markIncomplete()
	} else {
		row.StorageID, row.ObjectKey = storageID, key
	}
	_ = os.Remove(s.path) // 上传成功就不再需要；上传失败的也删掉，免得磁盘越积越多（索引和不完整标记已经留下线索）
	if err := s.rec.repo.CreateTraceSpan(ctx, row); err != nil {
		logger.Warn("轨迹：写片段索引失败", zap.Error(err), zap.Uint64("run_id", s.runID))
		s.markIncomplete()
	}
	upd := repository.RunTraceUpdate{AddBytes: added, Incomplete: s.isIncomplete(), ToolsHash: s.toolsHash}
	if pins != nil {
		upd.SkillPins = pins()
	}
	if err := s.rec.repo.UpdateRunTrace(ctx, s.runID, upd); err != nil {
		logger.Warn("轨迹：更新运行的轨迹字段失败", zap.Error(err), zap.Uint64("run_id", s.runID))
	}
}

// segmentSummary 是片段行的一行摘要。
func segmentSummary(e SegmentEnd, overflow bool) string {
	var parts []string
	switch {
	case e.Crashed:
		parts = append(parts, fmt.Sprintf("进程异常退出（退出码 %d）", e.ExitCode))
	case e.ExitCode != 0:
		parts = append(parts, fmt.Sprintf("退出码 %d", e.ExitCode))
	default:
		parts = append(parts, "正常结束")
	}
	if overflow {
		parts = append(parts, "超出整轮上限，后面只记索引")
	}
	return strings.Join(parts, "，")
}

// upload 把临时文件压缩后上传到默认存储，返回存储配置 id 和对象 key。空文件不上传（返回空 key，不算错误）。
func (s *SegmentTrace) upload(ctx context.Context) (uint64, string, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return 0, "", fmt.Errorf("读取临时文件失败：%w", err)
	}
	if len(raw) == 0 {
		return 0, "", nil
	}
	return uploadTraceObject(ctx, s.rec, s.runCreated, s.runID, s.segSeq, raw)
}

// uploadTraceObject 压缩并上传一个片段的原文，key 按运行创建日期分目录（同一个运行的所有片段在同一个目录）。
func uploadTraceObject(ctx context.Context, r *TraceRecorder, runCreated time.Time, runID uint64, segSeq int, raw []byte) (uint64, string, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return 0, "", fmt.Errorf("压缩失败：%w", err)
	}
	if err := zw.Close(); err != nil {
		return 0, "", fmt.Errorf("压缩失败：%w", err)
	}
	h, err := r.stores.Default(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("取默认存储失败：%w", err)
	}
	d := runCreated.UTC()
	key := fmt.Sprintf("%s/%04d/%02d/%02d/%d/%d.jsonl.gz", traceKeyPrefix, d.Year(), int(d.Month()), d.Day(), runID, segSeq)
	if err := h.Storage.Put(ctx, key, bytes.NewReader(buf.Bytes()), int64(buf.Len()), traceContentType); err != nil {
		return 0, "", fmt.Errorf("上传失败：%w", err)
	}
	return h.ID, key, nil
}

// ---- 启动时找回孤儿 ----

// RecoverOrphans 启动时把上次没来得及上传的本机临时文件补上传，并把对应的运行标为轨迹不完整，返回处理的文件数。
// 这些文件只会在后端进程被杀（或崩溃）时留下。只处理比本记录器创建时刻更早的文件，所以可以在后台跑，不会碰正在写的片段。
func (r *TraceRecorder) RecoverOrphans(ctx context.Context) int {
	if r == nil {
		return 0
	}
	entries, err := os.ReadDir(r.opt.Dir)
	if err != nil {
		return 0 // 目录不存在说明没有孤儿
	}
	done := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), traceTempExt) {
			continue
		}
		path := filepath.Join(r.opt.Dir, e.Name())
		if r.recoverFile(ctx, path, e.Name()) {
			done++
		}
	}
	return done
}

// recoverFile 补传一个孤儿文件。文件名是 {run_id}-{segment_seq}-{yyyymmdd}-{起始纳秒}.jsonl，不符合就当垃圾删掉。
func (r *TraceRecorder) recoverFile(ctx context.Context, path, name string) bool {
	parts := strings.Split(strings.TrimSuffix(name, traceTempExt), "-")
	if len(parts) != 4 {
		_ = os.Remove(path)
		return false
	}
	runID, err1 := strconv.ParseUint(parts[0], 10, 64)
	segSeq, err2 := strconv.Atoi(parts[1])
	created, err3 := time.Parse("20060102", parts[2])
	nanos, err4 := strconv.ParseInt(parts[3], 10, 64)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		_ = os.Remove(path)
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), traceUploadTimeout)
	defer cancel()
	if nanos >= r.bootAt.UnixNano() {
		return false // 本次启动之后才创建的文件，是正在写的片段，不是孤儿
	}
	started := time.Unix(0, nanos)
	row := &model.AgentTraceSpan{RunID: runID, SpanID: newSpanID("s"), Kind: model.TraceSpanSegment, Name: recoveredLaunch, Status: model.TraceStatusAborted,
		ErrorCode: "crashed", Summary: "服务退出时这个片段还没结束，原文是启动后找回的", Bytes: len(raw), SegmentSeq: segSeq, StartedAt: started}
	if len(raw) > 0 {
		sid, key, err := uploadTraceObject(ctx, r, created, runID, segSeq, raw)
		if err != nil {
			logger.Warn("轨迹：补传孤儿文件失败，下次启动再试", zap.Error(err), zap.String("path", path))
			return false
		}
		row.StorageID, row.ObjectKey = sid, key
	}
	if maxSeq, _, err := r.repo.TraceCounters(ctx, runID); err == nil {
		row.Seq = maxSeq + 1
	}
	if err := r.repo.CreateTraceSpan(ctx, row); err != nil {
		logger.Warn("轨迹：补传后写索引失败", zap.Error(err), zap.String("path", path))
		return false
	}
	if err := r.repo.UpdateRunTrace(ctx, runID, repository.RunTraceUpdate{AddBytes: int64(len(raw)), Incomplete: true}); err != nil {
		logger.Warn("轨迹：补传后更新运行失败", zap.Error(err), zap.Uint64("run_id", runID))
	}
	_ = os.Remove(path)
	return true
}

// ---- 读取（导出用）----

// ReadTraceObject 读出并解压一个片段的原文对象，逐行回调（空行跳过）。对象不存在返回 storage.ErrNotFound。
// 单行最长不超过 maxLine，超出说明对象被改过，返回错误而不是撑爆内存。
func ReadTraceObject(ctx context.Context, h *storage.Handle, key string, maxLine int, fn func(line []byte) error) error {
	rc, err := h.Storage.Open(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close() //nolint:errcheck // 只读，关闭失败不影响已经读到的内容
	zr, err := gzip.NewReader(rc)
	if err != nil {
		return fmt.Errorf("解压失败：%w", err)
	}
	defer zr.Close() //nolint:errcheck // 同上
	br := bufio.NewReaderSize(zr, 64<<10)
	for {
		line, err := readLine(br, maxLine)
		if len(bytes.TrimSpace(line)) > 0 {
			if ferr := fn(line); ferr != nil {
				return ferr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// readLine 读一行（不含换行符），超过 maxLine 返回错误。
func readLine(br *bufio.Reader, maxLine int) ([]byte, error) {
	var out []byte
	for {
		chunk, isPrefix, err := br.ReadLine()
		out = append(out, chunk...)
		if len(out) > maxLine {
			return nil, fmt.Errorf("轨迹对象里有超过 %d 字节的行", maxLine)
		}
		if err != nil {
			return out, err
		}
		if !isPrefix {
			return out, nil
		}
	}
}
