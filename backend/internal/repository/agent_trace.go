package repository

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// 轨迹相关的数据访问：记录器写索引、后台读列表和导出、清理任务删过期数据。设计见 docs/design/Agent调优Harness/Agent调优Harness.md 第 8 节。

// CreateTraceSpan 写一条轨迹索引行（工具调用或运行片段）。同一运行里 span_id 重复（唯一索引）返回底层错误。
func (r *AgentRepository) CreateTraceSpan(ctx context.Context, s *model.AgentTraceSpan) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// TraceCounters 返回某运行已用到的最大 span 序号和最大片段序号，新片段从它们之后接着数。
// 序号来自两张表：工具和片段在 agent_trace_spans，模型调用在 agent_model_calls。运行还没有任何轨迹时返回 0, 0。
func (r *AgentRepository) TraceCounters(ctx context.Context, runID uint64) (maxSeq, maxSegment int, err error) {
	var row struct {
		MaxSeq     int
		MaxSegment int
	}
	err = r.db.WithContext(ctx).Raw(`
		SELECT GREATEST(
		         COALESCE((SELECT MAX(seq) FROM agent_trace_spans WHERE run_id = ?), 0),
		         COALESCE((SELECT MAX(seq) FROM agent_model_calls WHERE run_id = ?), 0)) AS max_seq,
		       GREATEST(
		         COALESCE((SELECT MAX(segment_seq) FROM agent_trace_spans WHERE run_id = ?), 0),
		         COALESCE((SELECT MAX(segment_seq) FROM agent_model_calls WHERE run_id = ?), 0)) AS max_segment`,
		runID, runID, runID, runID).Scan(&row).Error
	return row.MaxSeq, row.MaxSegment, err
}

// RunTraceUpdate 是一个运行片段结束时对运行行的轨迹字段更新。
type RunTraceUpdate struct {
	AddBytes   int64             // 本片段新记录的原文字节数，累加到 trace_bytes
	Incomplete bool              // 为 true 时把 trace_complete 置为 false（只会置 false，不会置回 true）
	ToolsHash  string            // 非空时写入 tools_hash
	SkillPins  map[string]uint64 // 非空时合并进 skill_pins（同名技能以本次为准）
}

// UpdateRunTrace 更新运行的轨迹字段：累加字节数、按需置不完整、写工具声明 hash、合并技能版本。运行不存在返回 ErrNotFound。
// 技能版本用 jsonb 的 || 在数据库里合并，不读出来再写回，避免并发的两个片段互相覆盖。
func (r *AgentRepository) UpdateRunTrace(ctx context.Context, runID uint64, in RunTraceUpdate) error {
	fields := map[string]any{}
	if in.AddBytes != 0 {
		fields["trace_bytes"] = gorm.Expr("trace_bytes + ?", in.AddBytes)
	}
	if in.Incomplete {
		fields["trace_complete"] = false
	}
	if in.ToolsHash != "" {
		fields["tools_hash"] = in.ToolsHash
	}
	if len(in.SkillPins) > 0 {
		raw, err := json.Marshal(in.SkillPins)
		if err != nil {
			return err
		}
		fields["skill_pins"] = gorm.Expr("skill_pins || ?::jsonb", string(raw))
	}
	if len(fields) == 0 {
		return nil
	}
	res := r.db.WithContext(ctx).Model(&model.AgentRun{}).Where("id = ?", runID).UpdateColumns(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// GetRunAny 按 id 读运行，不限用户（后台导出用，调用方必须已经确认是管理员）。不存在返回 ErrNotFound。
func (r *AgentRepository) GetRunAny(ctx context.Context, id uint64) (*model.AgentRun, error) {
	var run model.AgentRun
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&run).Error; err != nil {
		return nil, translate(err)
	}
	return &run, nil
}

// ListTraceSpans 列出某运行的工具和片段索引，按 seq 升序。
func (r *AgentRepository) ListTraceSpans(ctx context.Context, runID uint64) ([]model.AgentTraceSpan, error) {
	var out []model.AgentTraceSpan
	err := r.db.WithContext(ctx).Where("run_id = ?", runID).Order("seq ASC, id ASC").Find(&out).Error
	return out, err
}

// ListModelCallsByRun 列出某运行的模型调用记录，按 seq、id 升序（老数据没有 seq，按 id 即调用先后）。
func (r *AgentRepository) ListModelCallsByRun(ctx context.Context, runID uint64) ([]model.AgentModelCall, error) {
	var out []model.AgentModelCall
	err := r.db.WithContext(ctx).Where("run_id = ?", runID).Order("seq ASC, id ASC").Find(&out).Error
	return out, err
}

// ListRunEvents 列出某运行的全部事件，按会话内序号升序。一轮运行的事件有上限（步数 × 几条），不分页。
func (r *AgentRepository) ListRunEvents(ctx context.Context, runID uint64) ([]model.AgentEvent, error) {
	var out []model.AgentEvent
	err := r.db.WithContext(ctx).Where("run_id = ?", runID).Order("seq ASC").Find(&out).Error
	return out, err
}

// AdminRunFilter 是后台运行列表的筛选条件，零值字段表示不筛。
type AdminRunFilter struct {
	UserID   uint64     // 精确匹配用户 id
	UserLike string     // 邮箱包含匹配（大小写不敏感）；与 UserID 同时给时满足任一即可
	CanvasID uint64     // 精确匹配画布
	RunID    uint64     // 精确匹配运行
	Status   string     // 精确匹配运行状态
	Harness  *bool      // true 只看 harness 用例的运行，false 只看真实用户的，nil 不筛
	From     time.Time  // 创建时间下限（含）
	To       *time.Time // 创建时间上限（不含），nil 不限
	Offset   int
	Limit    int
}

// AdminRunRow 是后台列表的一行：运行本身加上联查出来的用户邮箱、画布标题和工具报错数。
type AdminRunRow struct {
	model.AgentRun `gorm:"embedded"`
	UserEmail      string // 运行所属用户的邮箱，用户不存在为空
	CanvasTitle    string // 画布标题，画布已删除为空
	ToolErrors     int    // execute_tool 里 status=error 的 span 数
}

// ListAdminRuns 按筛选条件分页列出运行，创建时间倒序；同时返回满足条件的总数。
// 用户、画布用左联接：用户或画布被删除的运行仍然要能在列表里看到。
func (r *AgentRepository) ListAdminRuns(ctx context.Context, f AdminRunFilter) ([]AdminRunRow, int64, error) {
	q := r.db.WithContext(ctx).Table("agent_runs").
		Joins("LEFT JOIN users u ON u.id = agent_runs.user_id").
		Joins("LEFT JOIN canvas_projects c ON c.id = agent_runs.canvas_id").
		Where("agent_runs.created_at >= ?", f.From)
	if f.To != nil {
		q = q.Where("agent_runs.created_at < ?", *f.To)
	}
	switch {
	case f.UserID != 0 && f.UserLike != "":
		q = q.Where("(agent_runs.user_id = ? OR u.email ILIKE ?)", f.UserID, "%"+escapeLike(f.UserLike)+"%")
	case f.UserID != 0:
		q = q.Where("agent_runs.user_id = ?", f.UserID)
	case f.UserLike != "":
		q = q.Where("u.email ILIKE ?", "%"+escapeLike(f.UserLike)+"%")
	}
	if f.CanvasID != 0 {
		q = q.Where("agent_runs.canvas_id = ?", f.CanvasID)
	}
	if f.RunID != 0 {
		q = q.Where("agent_runs.id = ?", f.RunID)
	}
	if f.Status != "" {
		q = q.Where("agent_runs.status = ?", f.Status)
	}
	if f.Harness != nil {
		if *f.Harness {
			q = q.Where("agent_runs.harness_case <> ''")
		} else {
			q = q.Where("agent_runs.harness_case = ''")
		}
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []AdminRunRow
	err := q.Select(`agent_runs.*, COALESCE(u.email, '') AS user_email, COALESCE(c.title, '') AS canvas_title,
		(SELECT count(*) FROM agent_trace_spans s WHERE s.run_id = agent_runs.id AND s.kind = 'execute_tool' AND s.status = 'error') AS tool_errors`).
		Order("agent_runs.created_at DESC, agent_runs.id DESC").Offset(f.Offset).Limit(f.Limit).Scan(&rows).Error
	return rows, total, err
}

// ListExpiredTraceSegments 列出开始时间早于 before、带原文对象的片段行，最多 limit 条。清理任务据此删对象。
func (r *AgentRepository) ListExpiredTraceSegments(ctx context.Context, before time.Time, limit int) ([]model.AgentTraceSpan, error) {
	var out []model.AgentTraceSpan
	err := r.db.WithContext(ctx).
		Where("kind = ? AND object_key <> '' AND started_at < ?", model.TraceSpanSegment, before).
		Order("started_at ASC, id ASC").Limit(limit).Find(&out).Error
	return out, err
}

// DeleteTraceSpansByID 按主键删除索引行，返回删除的行数。
func (r *AgentRepository) DeleteTraceSpansByID(ctx context.Context, ids []uint64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&model.AgentTraceSpan{})
	return res.RowsAffected, res.Error
}

// DeleteExpiredTraceSpans 删除开始时间早于 before 的索引行，一次最多 limit 行，返回实际删除的行数。
// 带原文对象的片段行也会被删：调用方必须先用 ListExpiredTraceSegments 把对象删掉，否则对象会失去索引成为孤儿。
func (r *AgentRepository) DeleteExpiredTraceSpans(ctx context.Context, before time.Time, limit int) (int64, error) {
	res := r.db.WithContext(ctx).Exec(
		`DELETE FROM agent_trace_spans WHERE id IN (SELECT id FROM agent_trace_spans WHERE started_at < ? AND NOT (kind = ? AND object_key <> '') ORDER BY id LIMIT ?)`,
		before, model.TraceSpanSegment, limit)
	return res.RowsAffected, res.Error
}

// DeleteEventsBefore 删除创建时间早于 before 的 Agent 事件，一次最多 limit 行，返回实际删除的行数。
// 事件只用来回放和对账，保留期过了不会再有人读；会话的 last_seq 不动，新事件的序号照常往后排。
func (r *AgentRepository) DeleteEventsBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	res := r.db.WithContext(ctx).Exec(
		`DELETE FROM agent_events WHERE id IN (SELECT id FROM agent_events WHERE created_at < ? ORDER BY id LIMIT ?)`, before, limit)
	return res.RowsAffected, res.Error
}
