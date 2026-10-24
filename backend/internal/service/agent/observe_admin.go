package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// ObserveActor 是读取运行记录的人：管理员本人（JWT），或管理员签发的分析令牌（TokenID 非零）。
type ObserveActor struct {
	UserID  uint64 // 管理员
	TokenID uint64 // 分析令牌；用管理员 JWT 访问时为 0
}

// RunListFilter 是运行摘要列表的筛选条件，零值表示不限。
type RunListFilter struct {
	Since, Until  time.Time // 运行创建时间的范围，Since 含、Until 不含
	Statuses      []string  // 运行状态
	Feedback      string    // down / up / none / any
	Undone        bool      // 只要被撤销过的
	HarnessKey    string    // Harness 版本
	PromptVersion *int      // 系统提示词版本
	ModelKey      string    // Agent 模型
	UserID        uint64    // 所属用户
	Cursor        string    // 上一页返回的游标（游标分页，本地 CLI 用）
	Page          int       // 页码，从 1 起（页码分页，后台页面用）；和 Cursor 不能同时给
	Limit         int       // 每页条数
}

// AgentRunDigestView 是返回给后台和本地 CLI 的运行摘要：运行、会话、画布 id 用编码串，与用户端一致。
type AgentRunDigestView struct {
	RunID             idcodec.ID      `json:"run_id"`             // 运行 ID
	SessionID         idcodec.ID      `json:"session_id"`         // 会话 ID
	UserID            uint64          `json:"user_id"`            // 用户 ID（后台用，不编码）
	CanvasID          idcodec.ID      `json:"canvas_id"`          // 画布 ID
	Mode              string          `json:"mode"`               // 任务模式
	ModelKey          string          `json:"model_key"`          // Agent 模型
	PromptVersion     int             `json:"prompt_version"`     // 系统提示词版本
	ToolsHash         string          `json:"tools_hash"`         // 工具声明哈希
	BuildSHA          string          `json:"build_sha"`          // 构建的 git commit
	HarnessKey        string          `json:"harness_key"`        // Harness 版本短哈希
	Skills            json.RawMessage `json:"skills"`             // 读过的技能及版本
	Status            string          `json:"status"`             // 运行状态
	ErrorCode         string          `json:"error_code"`         // 失败时的错误码
	RequestHead       string          `json:"request_head"`       // 用户请求的前 200 字
	Steps             int             `json:"steps"`              // 步数
	ToolCalls         int             `json:"tool_calls"`         // 工具调用次数
	ToolErrors        int             `json:"tool_errors"`        // 工具报错次数
	ModelCalls        int             `json:"model_calls"`        // 模型调用次数
	InputTokens       int             `json:"input_tokens"`       // 输入 Token
	OutputTokens      int             `json:"output_tokens"`      // 输出 Token
	SpentCredits      int             `json:"spent_credits"`      // 已花积分
	DurationMs        int64           `json:"duration_ms"`        // 耗时（毫秒）
	ApprovalsTotal    int             `json:"approvals_total"`    // 审批总数
	ApprovalsRejected int             `json:"approvals_rejected"` // 被拒绝的审批数
	SteerCount        int             `json:"steer_count"`        // 用户插话次数
	MutationCount     int             `json:"mutation_count"`     // 改画布的次数
	Undone            bool            `json:"undone"`             // 改动被撤销过
	Feedback          int             `json:"feedback"`           // 1 有帮助，-1 没帮上忙，0 没评价
	EndedAt           *time.Time      `json:"ended_at"`           // 结束时间
	CreatedAt         time.Time       `json:"created_at"`         // 运行创建时间
	UpdatedAt         time.Time       `json:"updated_at"`         // 摘要更新时间，本地 CLI 据此做增量拉取
}

// digestView 转成对外的摘要视图。不含 runtime_tail 这类大字段，它只在导出里。
func digestView(d *model.AgentRunDigest) *AgentRunDigestView {
	skills := json.RawMessage(d.Skills)
	if len(skills) == 0 {
		skills = json.RawMessage("{}")
	}
	return &AgentRunDigestView{RunID: idcodec.ID(d.RunID), SessionID: idcodec.ID(d.SessionID), UserID: d.UserID, CanvasID: idcodec.ID(d.CanvasID),
		Mode: d.Mode, ModelKey: d.ModelKey, PromptVersion: d.PromptVersion, ToolsHash: d.ToolsHash, BuildSHA: d.BuildSHA, HarnessKey: d.HarnessKey, Skills: skills,
		Status: d.Status, ErrorCode: d.ErrorCode, RequestHead: d.RequestHead, Steps: d.Steps, ToolCalls: d.ToolCalls, ToolErrors: d.ToolErrors, ModelCalls: d.ModelCalls,
		InputTokens: d.InputTokens, OutputTokens: d.OutputTokens, SpentCredits: d.SpentCredits, DurationMs: d.DurationMs, ApprovalsTotal: d.ApprovalsTotal,
		ApprovalsRejected: d.ApprovalsRejected, SteerCount: d.SteerCount, MutationCount: d.MutationCount, Undone: d.Undone, Feedback: d.Feedback,
		EndedAt: d.EndedAt, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}
}

// RunListPage 是摘要列表的一页。
type RunListPage struct {
	Items      []*AgentRunDigestView `json:"items"`               // 本页的摘要，创建时间倒序
	NextCursor string                `json:"next_cursor"`         // 下一页的游标，空表示没有了；页码分页时恒为空
	Total      *int64                `json:"total,omitempty"`     // 命中总数，只有页码分页才有（游标分页不额外 count）
	Page       int                   `json:"page,omitempty"`      // 当前页码，只有页码分页才有
	PageSize   int                   `json:"page_size,omitempty"` // 每页条数，只有页码分页才有
}

// ListRuns 按条件列出运行摘要。管理员 JWT 访问不写审计；用分析令牌访问写一条（含条件和条数）。
func (s *ObserveService) ListRuns(ctx context.Context, who ObserveActor, f *RunListFilter) (*RunListPage, error) {
	// 1. 筛选值校验：不认识的反馈值直接拒绝，而不是悄悄当成不限（拼错了会拉回全部数据）
	if !slices.Contains([]string{"", "any", "down", "up", "none"}, f.Feedback) {
		return nil, errcode.ErrInvalidParams.WithMsg("feedback 只能是 down、up、none 或 any")
	}
	if len(f.Statuses) > maxStatusFilter {
		return nil, errcode.ErrInvalidParams.WithMsg("status 筛选太多")
	}
	if f.Page < 0 || (f.Page > 0 && f.Cursor != "") {
		return nil, errcode.ErrInvalidParams.WithMsg("page 和 cursor 不能同时使用")
	}
	// 2. 查询；游标损坏是调用方的错，不是服务端的
	q := model.DigestQuery{
		Since: f.Since, Until: f.Until, Statuses: f.Statuses, Feedback: f.Feedback, Undone: f.Undone, HarnessKey: f.HarnessKey,
		PromptVersion: f.PromptVersion, ModelKey: f.ModelKey, UserID: f.UserID, Cursor: f.Cursor, Page: f.Page, Limit: f.Limit,
	}
	rows, next, err := s.d.Repo.ListDigests(ctx, q)
	if errors.Is(err, repository.ErrInvalidCursor) {
		return nil, errcode.ErrInvalidParams.WithMsg("游标无效")
	}
	if err != nil {
		return nil, err
	}
	page := &RunListPage{Items: make([]*AgentRunDigestView, len(rows)), NextCursor: next}
	for i := range rows {
		page.Items[i] = digestView(&rows[i])
	}
	// 页码分页要给总数，前端才画得出总页数；总数用同一组筛选条件计，和列表命中的是同一批
	if f.Page > 0 {
		total, err := s.d.Repo.CountDigests(ctx, q)
		if err != nil {
			return nil, err
		}
		page.Total, page.Page, page.PageSize, page.NextCursor = &total, f.Page, effectivePageSize(f.Limit), "" // 页码模式没有游标
	}
	// 3. 令牌访问留痕：谁（哪个令牌）按什么条件拉了多少条
	if who.TokenID != 0 {
		s.audit(ctx, who.UserID, model.AdminAuditAgentRunsRead, who.TokenID, map[string]any{
			"token_id": who.TokenID, "op": "list", "count": len(rows),
			"filter": map[string]any{"feedback": f.Feedback, "undone": f.Undone, "statuses": f.Statuses, "harness_key": f.HarnessKey, "model_key": f.ModelKey, "user_id": f.UserID, "since": f.Since, "until": f.Until, "limit": f.Limit},
		})
	}
	return page, nil
}

// Stats 按版本（或模型）分组聚合指标。分组字段只能是 harness_key、prompt_version、model_key。
func (s *ObserveService) Stats(ctx context.Context, who ObserveActor, groupBy string, since, until time.Time) ([]model.AgentStatsRow, error) {
	// 先在这一层挡一次；仓储里还有一份白名单，因为分组列名会拼进 SQL，不能只信上层
	if !slices.Contains([]string{"harness_key", "prompt_version", "model_key"}, groupBy) {
		return nil, errcode.ErrInvalidParams.WithMsg("group_by 只能是 harness_key、prompt_version 或 model_key")
	}
	rows, err := s.d.Repo.Stats(ctx, model.StatsQuery{GroupBy: groupBy, Since: since, Until: until})
	if errors.Is(err, repository.ErrInvalidGroupBy) {
		return nil, errcode.ErrInvalidParams.WithMsg("group_by 只能是 harness_key、prompt_version 或 model_key")
	}
	if err != nil {
		return nil, err
	}
	if who.TokenID != 0 {
		s.audit(ctx, who.UserID, model.AdminAuditAgentRunsRead, who.TokenID, map[string]any{"token_id": who.TokenID, "op": "stats", "group_by": groupBy, "count": len(rows)})
	}
	return rows, nil
}

// 导出格式。
const (
	ExportMD   = "md"   // 只要 Markdown
	ExportJSON = "json" // 只要结构化数据
	ExportBoth = "both" // 两者都要（本地 CLI 一次请求拿全）
)

// RunExport 是一次运行的导出结果。
type RunExport struct {
	Format  string         `json:"format"`            // md / json / both
	Content string         `json:"content,omitempty"` // Markdown，format 为 md 或 both
	Data    *RunExportView `json:"data,omitempty"`    // 结构化数据，format 为 json 或 both
}

// ExportRun 导出一次运行，给 AI 读。运行不存在返回 60012。令牌访问每次导出写一条审计。
func (s *ObserveService) ExportRun(ctx context.Context, who ObserveActor, runID uint64, format string) (*RunExport, error) {
	if !slices.Contains([]string{ExportMD, ExportJSON, ExportBoth}, format) {
		return nil, errcode.ErrInvalidParams.WithMsg("format 只能是 md、json 或 both")
	}
	f, err := s.d.Repo.ExportFacts(ctx, runID)
	if err != nil {
		return nil, runErr(err)
	}
	out := &RunExport{Format: format}
	if format != ExportJSON {
		out.Content = RenderRunMarkdown(f)
	}
	if format != ExportMD {
		out.Data = exportView(f)
	}
	if who.TokenID != 0 {
		s.audit(ctx, who.UserID, model.AdminAuditAgentRunsRead, who.TokenID, map[string]any{"token_id": who.TokenID, "op": "export", "run_id": idcodec.Encode(runID), "format": format})
	}
	return out, nil
}

// RunRequestItem 是一次模型调用实际发给上游的请求，含完整请求体。
type RunRequestItem struct {
	ID idcodec.ID `json:"id"` // 调用 id，与导出里 model_calls[].id 对应
	ModelRequestView
}

// RunRequestsExport 是一次运行全部模型调用的请求体。
type RunRequestsExport struct {
	Items []*RunRequestItem `json:"items"` // 按调用先后；过了保留期的调用没有
}

// RunRequests 导出一次运行里各次模型调用实际发给上游的完整请求体（图片地址和数据已脱敏）。
// 体量大，所以不放进 ExportRun，要用时单独取。运行不存在返回 60012；令牌访问每次写一条审计。
func (s *ObserveService) RunRequests(ctx context.Context, who ObserveActor, runID uint64) (*RunRequestsExport, error) {
	if _, err := s.d.Repo.RunFacts(ctx, runID); err != nil {
		return nil, runErr(err)
	}
	rows, err := s.d.Repo.ModelCallRequests(ctx, runID, true)
	if err != nil {
		return nil, err
	}
	out := &RunRequestsExport{Items: make([]*RunRequestItem, 0, len(rows))}
	for i := range rows {
		item := &RunRequestItem{ID: idcodec.ID(rows[i].CallID), ModelRequestView: *requestSummary(&rows[i])}
		item.Kept = false
		if len(rows[i].Body) > 0 {
			body, err := gunzipBody(rows[i].Body)
			if err != nil {
				logger.Warn("解压模型请求体失败", zap.Error(err), zap.Uint64("call_id", rows[i].CallID))
			} else {
				item.Body, item.Kept = body, true
			}
		}
		out.Items = append(out.Items, item)
	}
	if who.TokenID != 0 {
		s.audit(ctx, who.UserID, model.AdminAuditAgentRunsRead, who.TokenID, map[string]any{"token_id": who.TokenID, "op": "requests", "run_id": idcodec.Encode(runID)})
	}
	return out, nil
}

// gunzipBody 把落库的压缩请求体还原成 JSON；最多读 MaxModelRequestBytes，防止异常数据撑爆内存。
func gunzipBody(b []byte) (json.RawMessage, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, MaxModelRequestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(out) > MaxModelRequestBytes {
		return nil, errors.New("请求体解压后超过上限")
	}
	return out, nil
}

// effectivePageSize 是页码分页实际用的每页条数：与仓储里的默认值和上限一致，回给前端算总页数。
func effectivePageSize(limit int) int {
	if limit <= 0 {
		limit = 100
	}
	return min(limit, 500)
}
