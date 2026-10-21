package agent

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
	agentsvc "video-canvas/internal/service/agent"
)

// ObserveAPI 是 handler 依赖的观测业务，由 *agentsvc.ObserveService 实现。
type ObserveAPI interface {
	// ListRuns 按条件列出运行摘要，游标分页。
	ListRuns(ctx context.Context, who agentsvc.ObserveActor, f *agentsvc.RunListFilter) (*agentsvc.RunListPage, error)
	// ExportRun 导出一次运行，给 AI 读。
	ExportRun(ctx context.Context, who agentsvc.ObserveActor, runID uint64, format string) (*agentsvc.RunExport, error)
	// Stats 按版本或模型分组聚合指标。
	Stats(ctx context.Context, who agentsvc.ObserveActor, groupBy string, since, until time.Time) ([]model.AgentStatsRow, error)
	// IssueToken 签发分析令牌，明文只返回这一次。
	IssueToken(ctx context.Context, adminID uint64, req *model.IssueAnalysisTokenReq) (*agentsvc.IssuedToken, error)
	// ListTokens 列出全部分析令牌。
	ListTokens(ctx context.Context) ([]*agentsvc.AnalysisTokenView, error)
	// RevokeToken 注销分析令牌。
	RevokeToken(ctx context.Context, adminID, id uint64) error
}

// AgentObserveHandler 是后台查看 Agent 运行记录的只读接口，以及分析令牌管理。
// 运行记录接口挂在 AdminOrAnalysisToken 之后，令牌管理接口只接受管理员 JWT。
type AgentObserveHandler struct {
	svc ObserveAPI
}

// NewAgentObserveHandler 创建 handler。
func NewAgentObserveHandler(svc ObserveAPI) *AgentObserveHandler {
	return &AgentObserveHandler{svc: svc}
}

// actor 取当前访问者：管理员本人，或管理员签发的分析令牌。
func actor(c *gin.Context) agentsvc.ObserveActor {
	return agentsvc.ObserveActor{UserID: uint64(middleware.GetUserID(c)), TokenID: middleware.GetAnalysisTokenID(c)}
}

// runListQuery 是运行摘要列表的查询参数。
type runListQuery struct {
	Since         string `form:"since" binding:"max=64" label:"since"`                            // 起，RFC3339 或 YYYY-MM-DD
	Until         string `form:"until" binding:"max=64" label:"until"`                            // 止（不含）
	Status        string `form:"status" binding:"max=400" label:"status"`                         // 逗号分隔的运行状态
	Feedback      string `form:"feedback" binding:"max=16" label:"feedback"`                      // down / up / none / any
	Undone        bool   `form:"undone" label:"undone"`                                           // 只要被撤销过的
	HarnessKey    string `form:"harness_key" binding:"max=12" label:"harness_key"`                // Harness 版本
	PromptVersion *int   `form:"prompt_version" binding:"omitempty,min=0" label:"prompt_version"` // 系统提示词版本
	ModelKey      string `form:"model_key" binding:"max=128" label:"model_key"`                   // Agent 模型
	UserID        uint64 `form:"user_id" label:"user_id"`                                         // 用户
	Cursor        string `form:"cursor" binding:"max=128" label:"cursor"`                         // 游标（本地 CLI 用）
	Page          int    `form:"page" binding:"omitempty,min=1" label:"page"`                     // 页码，从 1 起（后台页面用）；和 cursor 不能同时给
	Limit         int    `form:"limit" binding:"omitempty,min=1,max=500" label:"limit"`           // 每页条数
}

// parseQueryTime 解析 since / until：支持 RFC3339，也支持 YYYY-MM-DD（按 UTC 当天 0 点）。空串返回零值。
func parseQueryTime(name, v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t, nil
	}
	return time.Time{}, errcode.ErrInvalidParams.WithMsg(name + " 需要 RFC3339 时间或 YYYY-MM-DD 日期")
}

// ListRuns 列出运行摘要：GET /admin/agent/runs。
func (h *AgentObserveHandler) ListRuns(c *gin.Context) {
	var q runListQuery
	if !handler.BindQuery(c, &q) {
		return
	}
	since, err := parseQueryTime("since", q.Since)
	if err != nil {
		response.Fail(c, err)
		return
	}
	until, err := parseQueryTime("until", q.Until)
	if err != nil {
		response.Fail(c, err)
		return
	}
	var statuses []string
	for _, s := range strings.Split(q.Status, ",") {
		if s = strings.TrimSpace(s); s != "" {
			statuses = append(statuses, s)
		}
	}
	page, err := h.svc.ListRuns(c.Request.Context(), actor(c), &agentsvc.RunListFilter{
		Since: since, Until: until, Statuses: statuses, Feedback: q.Feedback, Undone: q.Undone, HarnessKey: q.HarnessKey,
		PromptVersion: q.PromptVersion, ModelKey: q.ModelKey, UserID: q.UserID, Cursor: q.Cursor, Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, page)
}

// Get 返回一次运行的结构化详情：GET /admin/agent/runs/:rid，内容与 export?format=json 相同。
func (h *AgentObserveHandler) Get(c *gin.Context) {
	h.export(c, agentsvc.ExportJSON)
}

// Export 导出一次运行：GET /admin/agent/runs/:rid/export?format=md|json|both，默认 md。
// md 返回 {format, content}；json 直接返回结构化对象；both 返回 {format, content, data}（本地 CLI 一次请求拿全）。
func (h *AgentObserveHandler) Export(c *gin.Context) {
	format := c.DefaultQuery("format", agentsvc.ExportMD)
	h.export(c, format)
}

func (h *AgentObserveHandler) export(c *gin.Context, format string) {
	id, ok := encodedParam(c, "rid")
	if !ok {
		return
	}
	out, err := h.svc.ExportRun(c.Request.Context(), actor(c), id, format)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if format == agentsvc.ExportJSON {
		response.OK(c, out.Data)
		return
	}
	response.OK(c, out)
}

// statsQuery 是版本对比的查询参数。
type statsQuery struct {
	GroupBy string `form:"group_by" binding:"required,max=32" label:"group_by"` // harness_key / prompt_version / model_key
	Since   string `form:"since" binding:"max=64" label:"since"`
	Until   string `form:"until" binding:"max=64" label:"until"`
}

// Stats 按版本分组聚合指标：GET /admin/agent/stats?group_by=harness_key，返回 {groups}。
func (h *AgentObserveHandler) Stats(c *gin.Context) {
	var q statsQuery
	if !handler.BindQuery(c, &q) {
		return
	}
	since, err := parseQueryTime("since", q.Since)
	if err != nil {
		response.Fail(c, err)
		return
	}
	until, err := parseQueryTime("until", q.Until)
	if err != nil {
		response.Fail(c, err)
		return
	}
	rows, err := h.svc.Stats(c.Request.Context(), actor(c), q.GroupBy, since, until)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if rows == nil {
		rows = []model.AgentStatsRow{}
	}
	response.OK(c, gin.H{"groups": rows})
}

// ListTokens 列出分析令牌：GET /admin/agent/tokens，返回 {items}。
func (h *AgentObserveHandler) ListTokens(c *gin.Context) {
	items, err := h.svc.ListTokens(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

// IssueToken 签发分析令牌：POST /admin/agent/tokens，明文只在这次响应里返回。
func (h *AgentObserveHandler) IssueToken(c *gin.Context) {
	var req model.IssueAnalysisTokenReq
	if !handler.BindJSON(c, &req) {
		return
	}
	out, err := h.svc.IssueToken(c.Request.Context(), uint64(middleware.GetUserID(c)), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// RevokeToken 注销分析令牌：DELETE /admin/agent/tokens/:id。
func (h *AgentObserveHandler) RevokeToken(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("id 格式错误"))
		return
	}
	if err := h.svc.RevokeToken(c.Request.Context(), uint64(middleware.GetUserID(c)), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
