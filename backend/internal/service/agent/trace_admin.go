package agent

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
	"video-canvas/pkg/pagination"

	"go.uber.org/zap"
)

// 后台「日志管理 → Agent 运行」：运行列表与轨迹包导出。只有管理员能用，契约见 Agent调优Harness 第 8.4 节。

const (
	runListDefaultSpan = 7 * 24 * time.Hour // 列表没传开始时间时往前看多久
	traceMaxLineBytes  = 64 << 20           // 读轨迹对象时单行的上限：画布快照可以很大，但不会超过这个数
)

// 列表里「轨迹」一列的取值。
const (
	TraceAvailable = "available" // 可以下载
	TraceExpired   = "expired"   // 超过保留期，原文已清
	TraceDisabled  = "disabled"  // 当时轨迹记录是关闭的，从没记过
)

// TraceAdminRepo 是后台列表与导出需要的数据访问，由 *repository.AgentRepository 实现。
type TraceAdminRepo interface {
	// GetRunAny 按 id 读运行，不限用户；不存在返回 repository.ErrNotFound。
	GetRunAny(ctx context.Context, id uint64) (*model.AgentRun, error)
	// ListAdminRuns 按筛选条件分页列出运行（创建时间倒序），同时返回总数。
	ListAdminRuns(ctx context.Context, f repository.AdminRunFilter) ([]repository.AdminRunRow, int64, error)
	// ListTraceSpans 列出某运行的工具和片段索引。
	ListTraceSpans(ctx context.Context, runID uint64) ([]model.AgentTraceSpan, error)
	// ListModelCallsByRun 列出某运行的模型调用记录。
	ListModelCallsByRun(ctx context.Context, runID uint64) ([]model.AgentModelCall, error)
	// ListRunEvents 列出某运行的全部事件。
	ListRunEvents(ctx context.Context, runID uint64) ([]model.AgentEvent, error)
	// ListMutations 列出某运行的全部画布改动。
	ListMutations(ctx context.Context, runID uint64) ([]model.AgentMutation, error)
	// ListApprovals 列出某运行的审批，用来补上审批 span 的执行结果。
	ListApprovals(ctx context.Context, runID uint64) ([]model.AgentApproval, error)
}

// TraceAdminOptions 是后台轨迹服务的可选项。
type TraceAdminOptions struct {
	RetentionDays int              // 轨迹保留天数，超过的算「已过期」；<=0 取 14
	Now           func() time.Time // 时钟，测试注入
}

// TraceAdminService 提供后台的运行列表和轨迹包导出。
type TraceAdminService struct {
	repo   TraceAdminRepo
	stores TraceStores
	audit  SkillAuditWriter // 与技能管理共用的审计写入接口（admin_audit_logs）
	opt    TraceAdminOptions
}

// NewTraceAdminService 创建后台轨迹服务。
func NewTraceAdminService(repo TraceAdminRepo, stores TraceStores, audit SkillAuditWriter, opt TraceAdminOptions) *TraceAdminService {
	if opt.RetentionDays <= 0 {
		opt.RetentionDays = 14
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &TraceAdminService{repo: repo, stores: stores, audit: audit, opt: opt}
}

// AdminRunItem 是后台运行列表的一行，字段与契约 8.4 逐个对应。
type AdminRunItem struct {
	ID            idcodec.ID `json:"id"`
	CreatedAt     time.Time  `json:"created_at"`
	EndedAt       *time.Time `json:"ended_at"` // 没结束为 null
	UserID        idcodec.ID `json:"user_id"`
	UserEmail     string     `json:"user_email"`
	CanvasID      idcodec.ID `json:"canvas_id"`
	CanvasTitle   string     `json:"canvas_title"`
	Mode          string     `json:"mode"`
	Status        string     `json:"status"`
	Steps         int        `json:"steps"`
	ToolErrors    int        `json:"tool_errors"` // 轨迹关闭或已过期时为 0
	SpentCredits  int        `json:"spent_credits"`
	DurationMS    *int64     `json:"duration_ms"` // 没结束为 null
	PromptVersion int        `json:"prompt_version"`
	Source        string     `json:"source"`       // user | harness
	HarnessCase   string     `json:"harness_case"` // 真实用户为空串
	Trace         string     `json:"trace"`        // available | expired | disabled
	TraceComplete bool       `json:"trace_complete"`
}

// traceState 判断一轮运行的轨迹现状：没开过是 disabled，超过保留期是 expired，其余 available。
// 按运行的创建时间判断而不看清理任务跑没跑：保留期到了就是到了，不取决于哪天清理。
func (s *TraceAdminService) traceState(run *model.AgentRun) string {
	switch {
	case !run.TraceEnabled:
		return TraceDisabled
	case run.CreatedAt.Before(s.cutoff()):
		return TraceExpired
	}
	return TraceAvailable
}

// cutoff 是保留期的截止时间：早于它创建的运行，轨迹视为已过期。
func (s *TraceAdminService) cutoff() time.Time {
	return s.opt.Now().AddDate(0, 0, -s.opt.RetentionDays)
}

// ListRuns 分页列出运行。
func (s *TraceAdminService) ListRuns(ctx context.Context, req *model.ListAgentRunsReq) (*pagination.Result[AdminRunItem], error) {
	// 1. 解析筛选条件：id 是编码串，时间是 RFC3339；格式不对是参数错误
	f := repository.AdminRunFilter{Status: req.Status}
	var ok bool
	if req.CanvasID != "" {
		if f.CanvasID, ok = idcodec.Decode(req.CanvasID); !ok {
			return nil, errcode.ErrInvalidParams.WithMsg("画布 id 格式不正确")
		}
	}
	if req.RunID != "" {
		if f.RunID, ok = idcodec.Decode(req.RunID); !ok {
			return nil, errcode.ErrInvalidParams.WithMsg("运行 id 格式不正确")
		}
	}
	// 用户：编码后的 id 或纯数字 id 按 id 匹配，同时也当邮箱片段匹配（邮箱里可能碰巧有这串字符）
	if u := strings.TrimSpace(req.User); u != "" {
		f.UserLike = u
		if id, ok := idcodec.Decode(u); ok {
			f.UserID = id
		} else if n, err := strconv.ParseUint(u, 10, 64); err == nil && n > 0 {
			f.UserID = n
		}
	}
	switch req.Source {
	case "user":
		no := false
		f.Harness = &no
	case "harness":
		yes := true
		f.Harness = &yes
	}
	// 2. 时间范围：缺 to 不设上限；缺 from 取（to 或现在）往前 7 天
	var err error
	to := s.opt.Now()
	if req.To != "" {
		if to, err = time.Parse(time.RFC3339, req.To); err != nil {
			return nil, errcode.ErrInvalidParams.WithMsg("结束时间格式不正确，应为 RFC3339")
		}
		f.To = &to
	}
	f.From = to.Add(-runListDefaultSpan)
	if req.From != "" {
		if f.From, err = time.Parse(time.RFC3339, req.From); err != nil {
			return nil, errcode.ErrInvalidParams.WithMsg("开始时间格式不正确，应为 RFC3339")
		}
	}
	if f.To != nil && !f.From.Before(*f.To) {
		return nil, errcode.ErrInvalidParams.WithMsg("开始时间必须早于结束时间")
	}
	// 3. 分页：页码最小 1，每页缺省 20、最多 100
	q := pagination.AdminQuery(req.Page, req.PageSize)
	f.Offset, f.Limit = q.Offset(), q.Limit()
	rows, total, err := s.repo.ListAdminRuns(ctx, f)
	if err != nil {
		return nil, err
	}
	items := make([]AdminRunItem, len(rows)) // 永远不是 nil，序列化为 []
	for i := range rows {
		items[i] = s.runItem(&rows[i])
	}
	return &pagination.Result[AdminRunItem]{List: items, Total: total, Page: q.Page, PageSize: q.PageSize}, nil
}

// runItem 把一行查询结果转成列表项。
func (s *TraceAdminService) runItem(r *repository.AdminRunRow) AdminRunItem {
	it := AdminRunItem{ID: idcodec.ID(r.ID), CreatedAt: r.CreatedAt, EndedAt: r.EndedAt, UserID: idcodec.ID(r.UserID), UserEmail: r.UserEmail,
		CanvasID: idcodec.ID(r.CanvasID), CanvasTitle: r.CanvasTitle, Mode: r.Mode, Status: r.Status, Steps: r.Steps, ToolErrors: r.ToolErrors,
		SpentCredits: r.SpentCredits, PromptVersion: r.PromptVersion, Source: "user", HarnessCase: r.HarnessCase, Trace: s.traceState(&r.AgentRun), TraceComplete: r.TraceComplete}
	if r.HarnessCase != "" {
		it.Source = "harness"
	}
	if r.EndedAt != nil {
		d := r.EndedAt.Sub(r.CreatedAt).Milliseconds()
		it.DurationMS = &d
	}
	if it.Trace != TraceAvailable {
		it.ToolErrors = 0 // 轨迹关闭或已清理，报错数没有依据
	}
	return it
}

// TraceFile 是轨迹包里的一个文件。
type TraceFile struct {
	Name    string
	Content []byte
}

// TraceBundle 是一轮运行的轨迹包，文件顺序固定。
type TraceBundle struct {
	Files      []TraceFile
	ExportedAt time.Time
}

// Zip 打成 zip，文件名、顺序和修改时间都固定为导出时间，同样的内容得到同样的字节。
func (b *TraceBundle) Zip() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range b.Files {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: b.ExportedAt})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.Content); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// TextFiles 返回 {文件名: 文本内容}，给 ?format=json 用（命令行不必解 zip）。
func (b *TraceBundle) TextFiles() map[string]string {
	out := make(map[string]string, len(b.Files))
	for _, f := range b.Files {
		out[f.Name] = string(f.Content)
	}
	return out
}

// ExportTrace 生成一轮运行的轨迹包。actorID 是执行导出的管理员。
// 业务错误：运行不存在（60012）、轨迹记录当时是关闭的（60015）、轨迹已过期（60014）。
func (s *TraceAdminService) ExportTrace(ctx context.Context, actorID, runID uint64, format string) (*TraceBundle, error) {
	// 1. 运行必须存在；没开过轨迹的和已过期的用各自明确的错误，前端据此给出原因
	run, err := s.repo.GetRunAny(ctx, runID)
	if err != nil {
		return nil, runErr(err)
	}
	switch s.traceState(run) {
	case TraceDisabled:
		return nil, errcode.ErrAgentTraceDisabled
	case TraceExpired:
		return nil, errcode.ErrAgentTraceExpired
	}
	// 2. 先写审计再取数据：轨迹里是用户的原始对话，没有留下导出记录就不能给出去。
	//    所以审计写失败时整个导出失败，而不是照常导出（这与技能管理的「写审计失败只记日志」不同：那里变更已经发生了，这里还没有）
	if err := s.writeAudit(ctx, actorID, run, format); err != nil {
		return nil, err
	}
	// 3. 取数据并组装
	return s.buildBundle(ctx, actorID, run)
}

// writeAudit 写 agent.trace.export 审计：操作人、运行 id、格式；不含任何轨迹内容。
func (s *TraceAdminService) writeAudit(ctx context.Context, actorID uint64, run *model.AgentRun, format string) error {
	detail, err := json.Marshal(map[string]any{"format": format, "user_id": run.UserID, "canvas_id": run.CanvasID, "harness_case": run.HarnessCase})
	if err != nil {
		return err
	}
	return s.audit.Insert(ctx, &model.AdminAuditLog{ActorID: actorID, Action: model.AdminAuditAgentTraceExport,
		TargetType: model.AdminAuditTargetAgentRun, TargetID: run.ID, DetailJSON: model.JSONText(detail)})
}

// rawObject 是从各片段的原文对象里读出来的内容。
type rawObject struct {
	system       map[string]json.RawMessage            // 第一个片段的 system 行
	lines        map[string]map[string]json.RawMessage // span_id → 该 span 的原文行
	canvasBefore json.RawMessage
	canvasAfter  json.RawMessage
	unreadable   bool // 有片段的对象读不出来（被删、损坏、存储配置没了）
}

// readObjects 按片段行记下的存储和 key 读回全部原文。读不出来的片段只打日志、标记 unreadable，导出照常进行（索引还在）。
func (s *TraceAdminService) readObjects(ctx context.Context, spans []model.AgentTraceSpan) *rawObject {
	obj := &rawObject{lines: map[string]map[string]json.RawMessage{}}
	var segs []model.AgentTraceSpan
	for _, sp := range spans {
		if sp.Kind == model.TraceSpanSegment {
			segs = append(segs, sp)
		}
	}
	sortSegments(segs)
	for _, sg := range segs {
		if sg.ObjectKey == "" {
			continue // 这个片段没有留下原文（上传失败、没有内容）；是否不完整由 trace_complete 反映
		}
		h, err := s.stores.Get(ctx, sg.StorageID)
		if err == nil {
			err = ReadTraceObject(ctx, h, sg.ObjectKey, traceMaxLineBytes, func(line []byte) error {
				obj.add(line)
				return nil
			})
		}
		if err != nil {
			obj.unreadable = true
			logger.Warn("轨迹：读取原文对象失败，导出的轨迹缺这个片段的原文", zap.Error(err), zap.Uint64("run_id", sg.RunID), zap.String("key", sg.ObjectKey))
		}
	}
	return obj
}

// sortSegments 按片段序号升序（原地）。
func sortSegments(segs []model.AgentTraceSpan) {
	sort.SliceStable(segs, func(i, j int) bool { return segs[i].SegmentSeq < segs[j].SegmentSeq })
}

// add 解析并登记一行原文。解析不了的行跳过：一行坏了不该让整份轨迹作废。
func (o *rawObject) add(line []byte) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(line, &m); err != nil {
		return
	}
	var kind, phase, spanID string
	_ = json.Unmarshal(m["kind"], &kind)
	switch kind {
	case "system":
		if o.system == nil {
			o.system = m
		}
	case "canvas":
		_ = json.Unmarshal(m["phase"], &phase)
		payload := canvasPayload(m["payload"])
		if phase == "before" && o.canvasBefore == nil {
			o.canvasBefore = payload
		}
		if phase == "after" {
			o.canvasAfter = payload // 后面的片段覆盖前面的：取最后一份
		}
	default:
		if err := json.Unmarshal(m["span_id"], &spanID); err == nil && spanID != "" {
			o.lines[spanID] = m
		}
	}
}

// canvasPayload 取画布快照的内容：正常是 JSON 对象原样；存的是字符串（payload 不是合法 JSON 时）就解开成文本。
func canvasPayload(raw json.RawMessage) json.RawMessage {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return json.RawMessage(s)
	}
	return raw
}
