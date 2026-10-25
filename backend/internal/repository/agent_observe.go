package repository

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// ErrInvalidCursor 分页游标不是本接口发出的，或已损坏。
var ErrInvalidCursor = errors.New("invalid cursor")

// ErrInvalidGroupBy 版本对比的分组字段不在白名单里。
var ErrInvalidGroupBy = errors.New("invalid group_by")

// 摘要列表每页条数。
const (
	defaultDigestPage = 100
	maxDigestPage     = 500
	contextTurns      = 2 // 导出时补充的更早轮数
)

// AgentObserveRepository 是 Agent 运行观测的数据访问：工具调用、反馈、运行摘要、导出、统计、清理和分析令牌。
// 它读的是别的表（运行、事件等）写下的数据，但只读；后台查看不带 user_id 条件，调用方必须先做管理员鉴权。
type AgentObserveRepository struct {
	db *gorm.DB
}

// NewAgentObserveRepository 创建仓储。
func NewAgentObserveRepository(db *gorm.DB) *AgentObserveRepository {
	return &AgentObserveRepository{db: db}
}

// StartToolCall 记一条刚开始的工具调用。同一运行里 tool_call_id 重复时保留第一条，不报错：
// Node 重试或进程续跑可能让同一个调用 id 再来一次，观测数据不该因此失败。
func (r *AgentObserveRepository) StartToolCall(ctx context.Context, c *model.AgentToolCall) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(c).Error
}

// FinishToolCall 补记工具调用的结果。耗时传 0 时按 started_at 推算；找不到开始记录（开始时写库失败过）不是错误。
func (r *AgentObserveRepository) FinishToolCall(ctx context.Context, runID uint64, toolCallID string, res model.AgentToolCallResult) error {
	fields := map[string]any{
		"result_text": res.ResultText, "result_truncated": res.Truncated, "is_error": res.IsError, "summary": res.Summary, "ended_at": res.EndedAt,
	}
	if res.DurationMs > 0 {
		fields["duration_ms"] = res.DurationMs
	} else {
		fields["duration_ms"] = gorm.Expr("GREATEST(0, (EXTRACT(EPOCH FROM (?::timestamptz - started_at)) * 1000)::bigint)", res.EndedAt)
	}
	return r.db.WithContext(ctx).Model(&model.AgentToolCall{}).Where("run_id = ? AND tool_call_id = ?", runID, toolCallID).Updates(fields).Error
}

// GetFeedback 读某人对某运行的评价，没有返回 ErrNotFound。
func (r *AgentObserveRepository) GetFeedback(ctx context.Context, runID, userID uint64) (*model.AgentFeedback, error) {
	var fb model.AgentFeedback
	if err := r.db.WithContext(ctx).Where("run_id = ? AND user_id = ?", runID, userID).First(&fb).Error; err != nil {
		return nil, translate(err)
	}
	return &fb, nil
}

// UpsertFeedback 写评价：同一人对同一运行只有一条，再写就是改选（评分、原因、说明全部覆盖）。
func (r *AgentObserveRepository) UpsertFeedback(ctx context.Context, fb *model.AgentFeedback) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "run_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"rating", "reasons", "comment", "updated_at"}),
	}).Create(fb).Error
}

// DeleteFeedback 取消评价；本来就没有也不是错误（取消是幂等的）。
func (r *AgentObserveRepository) DeleteFeedback(ctx context.Context, runID, userID uint64) error {
	return r.db.WithContext(ctx).Where("run_id = ? AND user_id = ?", runID, userID).Delete(&model.AgentFeedback{}).Error
}

// RunFacts 读生成运行摘要需要的事实，不按用户过滤（后台和事件钩子用）。运行不存在返回 ErrNotFound。
// 工具调用不读参数和结果、审批和改动不读内容：摘要只数数，大字段留给导出。
func (r *AgentObserveRepository) RunFacts(ctx context.Context, runID uint64) (*model.AgentRunFacts, error) {
	db := r.db.WithContext(ctx)
	var f model.AgentRunFacts
	if err := db.First(&f.Run, runID).Error; err != nil {
		return nil, translate(err)
	}
	// 会话被用户软删除后运行和事件仍在：标出来，这件事本身也是信号
	var sessions int64
	if err := db.Model(&model.AgentSession{}).Where("id = ?", f.Run.SessionID).Count(&sessions).Error; err != nil {
		return nil, err
	}
	f.SessionDeleted = sessions == 0
	if err := db.Omit("args_json", "result_text").Where("run_id = ?", runID).Order("started_at, id").Find(&f.ToolCalls).Error; err != nil {
		return nil, err
	}
	if err := db.Where("run_id = ?", runID).Order("id").Find(&f.ModelCalls).Error; err != nil {
		return nil, err
	}
	if err := db.Omit("payload_json", "decision_json", "result_json").Where("run_id = ?", runID).Order("id").Find(&f.Approvals).Error; err != nil {
		return nil, err
	}
	if err := db.Omit("changes_json").Where("run_id = ?", runID).Order("seq").Find(&f.Mutations).Error; err != nil {
		return nil, err
	}
	if err := db.Where("run_id = ? AND type = ?", runID, "message.user").Order("seq").Find(&f.UserMessages).Error; err != nil {
		return nil, err
	}
	var fb model.AgentFeedback
	switch err := db.Where("run_id = ? AND user_id = ?", runID, f.Run.UserID).First(&fb).Error; {
	case err == nil:
		f.Feedback = &fb
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, err
	}
	// Node 进程 stderr 末尾：已有摘要时沿用，运行时退出时会重新写入
	var prev model.AgentRunDigest
	if err := db.Select("runtime_tail").First(&prev, runID).Error; err == nil && prev.RuntimeTail != nil {
		f.RuntimeTail = *prev.RuntimeTail
	}
	return &f, nil
}

// LocalRunFacts 读运行本身在 Agent 服务里时 Go 自己持有的那部分摘要事实：工具调用（不含参数和结果）、模型调用、画布改动、runtime_tail。
// 没有记录不是错误，各部分为空。
func (r *AgentObserveRepository) LocalRunFacts(ctx context.Context, runID uint64) (*model.AgentRunFacts, error) {
	db := r.db.WithContext(ctx)
	var f model.AgentRunFacts
	if err := db.Omit("args_json", "result_text").Where("run_id = ?", runID).Order("started_at, id").Find(&f.ToolCalls).Error; err != nil {
		return nil, err
	}
	if err := db.Where("run_id = ?", runID).Order("id").Find(&f.ModelCalls).Error; err != nil {
		return nil, err
	}
	if err := db.Omit("changes_json").Where("run_id = ?", runID).Order("seq").Find(&f.Mutations).Error; err != nil {
		return nil, err
	}
	var prev model.AgentRunDigest
	if err := db.Select("runtime_tail").First(&prev, runID).Error; err == nil && prev.RuntimeTail != nil {
		f.RuntimeTail = *prev.RuntimeTail
	}
	return &f, nil
}

// LocalExportFacts 读导出时 Go 自己持有的那部分：摘要、含参数和结果的工具调用、模型调用、模型请求摘要（不含请求体）、画布改动。
func (r *AgentObserveRepository) LocalExportFacts(ctx context.Context, runID uint64) (*model.AgentExportFacts, error) {
	db := r.db.WithContext(ctx)
	var f model.AgentExportFacts
	if err := db.First(&f.Digest, runID).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := db.Where("run_id = ?", runID).Order("started_at, id").Find(&f.ToolCalls).Error; err != nil {
		return nil, err
	}
	if err := db.Where("run_id = ?", runID).Order("id").Find(&f.ModelCalls).Error; err != nil {
		return nil, err
	}
	if err := db.Omit("body").Where("run_id = ?", runID).Order("call_id").Find(&f.Requests).Error; err != nil {
		return nil, err
	}
	if err := db.Where("run_id = ?", runID).Order("seq").Find(&f.Mutations).Error; err != nil {
		return nil, err
	}
	if f.Digest.RuntimeTail != nil {
		f.RuntimeTail = *f.Digest.RuntimeTail
	}
	return &f, nil
}

// UpsertDigest 写运行摘要：一次运行一行，再写就是整行覆盖（续跑后再次进入终态、反馈或撤销之后都会重写）。
func (r *AgentObserveRepository) UpsertDigest(ctx context.Context, d *model.AgentRunDigest) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "run_id"}},
		// created_at 是运行创建时间，只在第一次写入时确定，之后不改
		DoUpdates: clause.AssignmentColumns(digestUpdateColumns()),
	}).Create(d).Error
}

// digestUpdateColumns 是摘要覆盖时更新的列：除主键和 created_at 以外的全部。
func digestUpdateColumns() []string {
	return []string{"session_id", "user_id", "canvas_id", "mode", "model_key", "prompt_version", "tools_hash", "build_sha", "harness_key", "skills",
		"status", "error_code", "request_head", "steps", "tool_calls", "tool_errors", "model_calls", "input_tokens", "output_tokens", "spent_credits",
		"duration_ms", "approvals_total", "approvals_rejected", "steer_count", "mutation_count", "undone", "feedback", "runtime_tail", "ended_at", "updated_at"}
}

// GetDigest 读一条运行摘要，没有返回 ErrNotFound。
func (r *AgentObserveRepository) GetDigest(ctx context.Context, runID uint64) (*model.AgentRunDigest, error) {
	var d model.AgentRunDigest
	if err := r.db.WithContext(ctx).First(&d, runID).Error; err != nil {
		return nil, translate(err)
	}
	return &d, nil
}

// digestFilter 把筛选条件（不含分页）拼到查询上，列表和计数共用，保证总数和列表命中的是同一批。
func (r *AgentObserveRepository) digestFilter(ctx context.Context, q model.DigestQuery) *gorm.DB {
	tx := r.db.WithContext(ctx).Model(&model.AgentRunDigest{})
	if !q.Since.IsZero() {
		tx = tx.Where("created_at >= ?", q.Since)
	}
	if !q.Until.IsZero() {
		tx = tx.Where("created_at < ?", q.Until)
	}
	if len(q.Statuses) > 0 {
		tx = tx.Where("status IN ?", q.Statuses)
	}
	switch q.Feedback {
	case "down":
		tx = tx.Where("feedback = ?", model.FeedbackDown)
	case "up":
		tx = tx.Where("feedback = ?", model.FeedbackUp)
	case "none":
		tx = tx.Where("feedback = 0")
	}
	if q.Undone {
		tx = tx.Where("undone = true")
	}
	if q.HarnessKey != "" {
		tx = tx.Where("harness_key = ?", q.HarnessKey)
	}
	if q.PromptVersion != nil {
		tx = tx.Where("prompt_version = ?", *q.PromptVersion)
	}
	if q.ModelKey != "" {
		tx = tx.Where("model_key = ?", q.ModelKey)
	}
	if q.UserID != 0 {
		tx = tx.Where("user_id = ?", q.UserID)
	}
	return tx
}

// CountDigests 统计命中筛选条件的摘要总数（页码分页的总页数用，忽略分页参数）。
func (r *AgentObserveRepository) CountDigests(ctx context.Context, q model.DigestQuery) (int64, error) {
	var n int64
	err := r.digestFilter(ctx, q).Count(&n).Error
	return n, err
}

// ListDigests 按创建时间倒序列出运行摘要。
// Page 大于 0 时按页码取（偏移分页，next 恒为空）；否则游标分页（创建时间 + run_id，同一时刻的多条也不会重漏），
// next 为空表示没有下一页，游标不是本接口发出的返回 ErrInvalidCursor。
func (r *AgentObserveRepository) ListDigests(ctx context.Context, q model.DigestQuery) ([]model.AgentRunDigest, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultDigestPage
	}
	limit = min(limit, maxDigestPage)
	tx := r.digestFilter(ctx, q)
	if q.Page > 0 {
		var items []model.AgentRunDigest
		err := tx.Order("created_at DESC, run_id DESC").Offset((q.Page - 1) * limit).Limit(limit).Find(&items).Error
		return items, "", err
	}
	if q.Cursor != "" {
		at, id, err := decodeDigestCursor(q.Cursor)
		if err != nil {
			return nil, "", err
		}
		tx = tx.Where("(created_at, run_id) < (?, ?)", at, id)
	}
	var items []model.AgentRunDigest
	// 多取一条判断有没有下一页
	if err := tx.Order("created_at DESC, run_id DESC").Limit(limit + 1).Find(&items).Error; err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeDigestCursor(last.CreatedAt, last.RunID)
	}
	return items, next, nil
}

func encodeDigestCursor(at time.Time, id uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at.UnixNano(), 10) + ":" + strconv.FormatUint(id, 10)))
}

func decodeDigestCursor(c string) (time.Time, uint64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, 0, ErrInvalidCursor
	}
	a, b, ok := strings.Cut(string(raw), ":")
	if !ok {
		return time.Time{}, 0, ErrInvalidCursor
	}
	ns, err1 := strconv.ParseInt(a, 10, 64)
	id, err2 := strconv.ParseUint(b, 10, 64)
	if err1 != nil || err2 != nil {
		return time.Time{}, 0, ErrInvalidCursor
	}
	return time.Unix(0, ns).UTC(), id, nil
}

// statsGroupColumns 是版本对比允许的分组字段：分组列名会拼进 SQL，只能来自这个白名单。
var statsGroupColumns = map[string]string{
	"harness_key":    "harness_key",
	"prompt_version": "prompt_version::text",
	"model_key":      "model_key",
}

// Stats 按版本（或模型）分组聚合运行摘要的指标，用来判断改了 Harness 之后有没有变好。分组字段不在白名单返回 ErrInvalidGroupBy。
// 按每组最近一次运行的时间倒序（版本对比只关心最近几个版本）。比率的分母为 0 时记 0。分组值为空（没记版本的老运行）单独成一组，key 为空串。
func (r *AgentObserveRepository) Stats(ctx context.Context, q model.StatsQuery) ([]model.AgentStatsRow, error) {
	col, ok := statsGroupColumns[q.GroupBy]
	if !ok {
		return nil, ErrInvalidGroupBy
	}
	failed := make([]string, len(model.FailedRunStatuses))
	copy(failed, model.FailedRunStatuses)
	sel := fmt.Sprintf(`%s AS key,
		COUNT(*) AS n,
		COALESCE(COUNT(*) FILTER (WHERE feedback <> 0)::float / NULLIF(COUNT(*), 0), 0) AS feedback_rate,
		COALESCE(COUNT(*) FILTER (WHERE feedback = -1)::float / NULLIF(COUNT(*) FILTER (WHERE feedback <> 0), 0), 0) AS down_rate,
		COALESCE(COUNT(*) FILTER (WHERE undone)::float / NULLIF(COUNT(*) FILTER (WHERE mutation_count > 0), 0), 0) AS undo_rate,
		COALESCE(COUNT(*) FILTER (WHERE status IN ?)::float / NULLIF(COUNT(*), 0), 0) AS fail_end_rate,
		COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms), 0) AS median_duration_ms,
		COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY steps), 0) AS median_steps,
		COALESCE(AVG(spent_credits), 0) AS avg_credits,
		MAX(prompt_version) AS prompt_version,
		COALESCE(MAX(build_sha), '') AS build_sha,
		MAX(created_at) AS last_at`, col)
	tx := r.db.WithContext(ctx).Model(&model.AgentRunDigest{}).Select(sel, failed)
	if !q.Since.IsZero() {
		tx = tx.Where("created_at >= ?", q.Since)
	}
	if !q.Until.IsZero() {
		tx = tx.Where("created_at < ?", q.Until)
	}
	var rows []model.AgentStatsRow
	if err := tx.Group(col).Order("last_at DESC, n DESC, key").Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ExportFacts 读导出一次运行（给 AI 读）需要的全部事实，含工具参数和结果。运行不存在返回 ErrNotFound。
// 摘要还没生成（运行刚创建、尚未进入过非活跃状态）时 Digest 为零值。
func (r *AgentObserveRepository) ExportFacts(ctx context.Context, runID uint64) (*model.AgentExportFacts, error) {
	db := r.db.WithContext(ctx)
	var f model.AgentExportFacts
	if err := db.First(&f.Run, runID).Error; err != nil {
		return nil, translate(err)
	}
	if err := db.First(&f.Digest, runID).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := db.Where("run_id = ?", runID).Order("seq").Find(&f.Events).Error; err != nil {
		return nil, err
	}
	if err := db.Where("run_id = ?", runID).Order("started_at, id").Find(&f.ToolCalls).Error; err != nil {
		return nil, err
	}
	if err := db.Where("run_id = ?", runID).Order("id").Find(&f.ModelCalls).Error; err != nil {
		return nil, err
	}
	// 请求体是大字段，导出默认只取摘要；要全文走 ModelCallRequests
	if err := db.Omit("body").Where("run_id = ?", runID).Order("call_id").Find(&f.Requests).Error; err != nil {
		return nil, err
	}
	if err := db.Where("run_id = ?", runID).Order("seq").Find(&f.Mutations).Error; err != nil {
		return nil, err
	}
	if err := db.Where("run_id = ?", runID).Order("id").Find(&f.Approvals).Error; err != nil {
		return nil, err
	}
	var fb model.AgentFeedback
	switch err := db.Where("run_id = ? AND user_id = ?", runID, f.Run.UserID).First(&fb).Error; {
	case err == nil:
		f.Feedback = &fb
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, err
	}
	if f.Digest.RuntimeTail != nil {
		f.RuntimeTail = *f.Digest.RuntimeTail
	}
	// 上下文：同会话里更早的 contextTurns 轮，只取用户消息和最终回复，让 AI 知道这一轮是接着什么说的
	var prior []uint64
	if err := db.Model(&model.AgentRun{}).Where("session_id = ? AND id < ?", f.Run.SessionID, runID).Order("id DESC").Limit(contextTurns).Pluck("id", &prior).Error; err != nil {
		return nil, err
	}
	if len(prior) > 0 {
		if err := db.Where("run_id IN ? AND type IN ?", prior, []string{"message.user", "message.done"}).Order("seq").Find(&f.Context).Error; err != nil {
			return nil, err
		}
	}
	return &f, nil
}

// PurgeObserveBefore 清理过期的观测大字段：早于 cutoff 开始的工具调用清空参数和结果（记 purged_at 为 now），
// 早于 cutoff 结束的运行摘要清空 runtime_tail。其余字段长期保留。返回两类各清理了多少行；重复执行是幂等的。
func (r *AgentObserveRepository) PurgeObserveBefore(ctx context.Context, cutoff, now time.Time) (toolCalls, digests int64, err error) {
	res := r.db.WithContext(ctx).Model(&model.AgentToolCall{}).
		Where("started_at < ? AND purged_at IS NULL", cutoff).
		Updates(map[string]any{"args_json": nil, "result_text": nil, "purged_at": now})
	if res.Error != nil {
		return 0, 0, res.Error
	}
	toolCalls = res.RowsAffected
	res = r.db.WithContext(ctx).Model(&model.AgentRunDigest{}).
		Where("ended_at < ? AND runtime_tail IS NOT NULL", cutoff).
		UpdateColumn("runtime_tail", nil)
	if res.Error != nil {
		return toolCalls, 0, res.Error
	}
	return toolCalls, res.RowsAffected, nil
}

// CreateToken 保存新签发的分析令牌；哈希重复（几乎不可能）返回 ErrDuplicate。
func (r *AgentObserveRepository) CreateToken(ctx context.Context, t *model.AdminAPIToken) error {
	if err := r.db.WithContext(ctx).Create(t).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// ListTokens 列出分析令牌（含已过期的，不含已注销的），新的在前。不含哈希。
// 已注销的令牌在库里仍保留（审计和鉴权据此判断「这个令牌被注销过」），只是不再出现在后台列表里。
func (r *AgentObserveRepository) ListTokens(ctx context.Context) ([]model.AdminAPIToken, error) {
	var out []model.AdminAPIToken
	err := r.db.WithContext(ctx).Where("revoked_at IS NULL").Order("id DESC").Find(&out).Error
	return out, err
}

// TokenByHash 按明文的 sha256 查令牌（含已注销的），没有返回 ErrNotFound。有效期、注销和权限由调用方判断。
func (r *AgentObserveRepository) TokenByHash(ctx context.Context, hash string) (*model.AdminAPIToken, error) {
	var t model.AdminAPIToken
	if err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&t).Error; err != nil {
		return nil, translate(err)
	}
	return &t, nil
}

// TouchToken 记下令牌最近一次使用的时间。
func (r *AgentObserveRepository) TouchToken(ctx context.Context, id uint64, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.AdminAPIToken{}).Where("id = ?", id).UpdateColumn("last_used_at", at).Error
}

// RevokeToken 注销令牌。重复注销是幂等的（保留第一次的注销时间）；令牌不存在返回 ErrNotFound。
func (r *AgentObserveRepository) RevokeToken(ctx context.Context, id uint64, at time.Time) error {
	var t model.AdminAPIToken
	if err := r.db.WithContext(ctx).Select("id").First(&t, id).Error; err != nil {
		return translate(err)
	}
	return r.db.WithContext(ctx).Model(&model.AdminAPIToken{}).Where("id = ? AND revoked_at IS NULL", id).UpdateColumn("revoked_at", at).Error
}

// SaveModelCallBody 保存一次模型调用的请求记录。同一次调用重复保存时保留第一条，不报错。
func (r *AgentObserveRepository) SaveModelCallBody(ctx context.Context, b *model.AgentModelCallBody) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(b).Error
}

// ModelCallRequests 读一次运行里各次模型调用的请求记录，按调用 id 升序。withBody 为 false 时不读请求体（只要摘要，省内存和带宽）。
func (r *AgentObserveRepository) ModelCallRequests(ctx context.Context, runID uint64, withBody bool) ([]model.AgentModelCallBody, error) {
	q := r.db.WithContext(ctx).Where("run_id = ?", runID).Order("call_id")
	if !withBody {
		q = q.Omit("body")
	}
	var out []model.AgentModelCallBody
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// PurgeModelCallBodiesBefore 删除早于 cutoff 创建的模型请求记录（整行删除，请求体是大头），返回删了多少行；重复执行是幂等的。
func (r *AgentObserveRepository) PurgeModelCallBodiesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where("created_at < ?", cutoff).Delete(&model.AgentModelCallBody{})
	return res.RowsAffected, res.Error
}
