package agent

import (
	"encoding/json"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
)

// RunExportView 是一次运行的结构化导出：本地 CLI 存成 .json，工具参数和结果完整保留（Markdown 里每段只留 2KB）。
type RunExportView struct {
	Digest      *AgentRunDigestView `json:"digest"`       // 运行摘要；运行刚创建还没有摘要时为 null
	Run         *AgentRunView       `json:"run"`          // 运行本身
	Events      []*AgentEventView   `json:"events"`       // 本轮的落库事件，按序号升序
	ToolCalls   []*ToolCallView     `json:"tool_calls"`   // 工具调用，含参数和结果
	ModelCalls  []*ModelCallView    `json:"model_calls"`  // 模型调用
	Mutations   []*MutationView     `json:"mutations"`    // 画布改动
	Approvals   []*ApprovalExport   `json:"approvals"`    // 审批
	Feedback    *FeedbackView       `json:"feedback"`     // 用户评价；没评价为 null
	Context     []*AgentEventView   `json:"context"`      // 同会话更早两轮的用户消息和最终回复
	RuntimeTail string              `json:"runtime_tail"` // Node 进程 stderr 末尾；过保留期后为空
}

// ToolCallView 是导出里的一次工具调用。
type ToolCallView struct {
	ID              string          `json:"id"`               // 模型给的调用 id
	ParentID        string          `json:"parent_id"`        // codemode 脚本里发起的调用，指向脚本那次调用
	Name            string          `json:"name"`             // 工具名
	Args            json.RawMessage `json:"args"`             // 调用参数；过保留期后为 null
	Result          *string         `json:"result"`           // 返回给模型的文本；过保留期后为 null
	ResultTruncated bool            `json:"result_truncated"` // 结果在落库时被截断过
	IsError         bool            `json:"is_error"`         // 工具没做成
	Summary         string          `json:"summary"`          // 一行摘要
	StartedAt       time.Time       `json:"started_at"`       // 开始时间
	EndedAt         *time.Time      `json:"ended_at"`         // 结束时间
	DurationMs      int             `json:"duration_ms"`      // 耗时（毫秒）
	Purged          bool            `json:"purged"`           // 参数和结果已过保留期被清理
}

// ModelCallView 是导出里的一次模型调用。
type ModelCallView struct {
	Purpose        string    `json:"purpose"`         // main 对话调用，summary 摘要调用
	ModelKey       string    `json:"model_key"`       // Agent 模型
	ChannelKey     string    `json:"channel_key"`     // 实际走的渠道
	UpstreamModel  string    `json:"upstream_model"`  // 实际调用的上游模型
	InputTokens    int       `json:"input_tokens"`    // 输入 Token
	OutputTokens   int       `json:"output_tokens"`   // 输出 Token
	CachedTokens   int       `json:"cached_tokens"`   // 命中缓存的输入 Token
	UsageEstimated bool      `json:"usage_estimated"` // Token 数是估的
	Credits        int       `json:"credits"`         // 应收积分
	Charged        int       `json:"charged"`         // 实扣积分
	LatencyMs      int       `json:"latency_ms"`      // 总耗时
	TTFTMs         int       `json:"ttft_ms"`         // 首字耗时
	StopReason     string    `json:"stop_reason"`     // 上游的结束原因
	Error          string    `json:"error"`           // 失败或中断的原因
	CreatedAt      time.Time `json:"created_at"`      // 发起时间
}

// MutationView 是导出里的一次画布改动。
type MutationView struct {
	Seq            int             `json:"seq"`             // 运行内序号
	ToolCallID     string          `json:"tool_call_id"`    // 触发它的工具调用
	Kind           string          `json:"kind"`            // 改动种类
	Changes        json.RawMessage `json:"changes"`         // 改动明细
	RevisionBefore uint64          `json:"revision_before"` // 写入前的画布版本
	RevisionAfter  uint64          `json:"revision_after"`  // 写入后的画布版本
	UndoneAt       *time.Time      `json:"undone_at"`       // 被撤销的时间
}

// ApprovalExport 是导出里的一次审批。
type ApprovalExport struct {
	ID           idcodec.ID      `json:"id"`            // 审批 ID
	Kind         string          `json:"kind"`          // 审批种类
	ToolCallID   string          `json:"tool_call_id"`  // 触发它的工具调用
	Status       string          `json:"status"`        // 状态
	QuoteCredits int             `json:"quote_credits"` // 预估积分
	Payload      json.RawMessage `json:"payload"`       // 待决定的内容
	Decision     json.RawMessage `json:"decision"`      // 用户的决定
	Result       json.RawMessage `json:"result"`        // 执行结果
	DecidedAt    *time.Time      `json:"decided_at"`    // 决定时间
}

// exportView 把导出事实转成对外的结构化视图。
func exportView(f *model.AgentExportFacts) *RunExportView {
	v := &RunExportView{Run: runView(&f.Run), RuntimeTail: f.RuntimeTail,
		Events: eventViews(f.Events), Context: eventViews(f.Context),
		ToolCalls: []*ToolCallView{}, ModelCalls: []*ModelCallView{}, Mutations: []*MutationView{}, Approvals: []*ApprovalExport{}}
	if f.Digest.RunID != 0 {
		v.Digest = digestView(&f.Digest)
	}
	for i := range f.ToolCalls {
		c := &f.ToolCalls[i]
		v.ToolCalls = append(v.ToolCalls, &ToolCallView{ID: c.ToolCallID, ParentID: c.ParentID, Name: c.Name, Args: json.RawMessage(c.ArgsJSON), Result: c.ResultText,
			ResultTruncated: c.ResultTruncated, IsError: c.IsError, Summary: c.Summary, StartedAt: c.StartedAt, EndedAt: c.EndedAt, DurationMs: c.DurationMs, Purged: c.PurgedAt != nil})
	}
	for i := range f.ModelCalls {
		m := &f.ModelCalls[i]
		v.ModelCalls = append(v.ModelCalls, &ModelCallView{Purpose: m.Purpose, ModelKey: m.ModelKey, ChannelKey: m.ChannelKey, UpstreamModel: m.UpstreamModel,
			InputTokens: m.InputTokens, OutputTokens: m.OutputTokens, CachedTokens: m.CachedTokens, UsageEstimated: m.UsageEstimated, Credits: m.Credits, Charged: m.Charged,
			LatencyMs: m.LatencyMs, TTFTMs: m.TTFTMs, StopReason: m.StopReason, Error: m.Error, CreatedAt: m.CreatedAt})
	}
	for i := range f.Mutations {
		m := &f.Mutations[i]
		v.Mutations = append(v.Mutations, &MutationView{Seq: m.Seq, ToolCallID: m.ToolCallID, Kind: m.Kind, Changes: json.RawMessage(m.ChangesJSON),
			RevisionBefore: m.RevisionBefore, RevisionAfter: m.RevisionAfter, UndoneAt: m.UndoneAt})
	}
	for i := range f.Approvals {
		a := &f.Approvals[i]
		v.Approvals = append(v.Approvals, &ApprovalExport{ID: idcodec.ID(a.ID), Kind: a.Kind, ToolCallID: a.ToolCallID, Status: a.Status, QuoteCredits: a.QuoteCredits,
			Payload: json.RawMessage(a.PayloadJSON), Decision: json.RawMessage(a.DecisionJSON), Result: json.RawMessage(a.ResultJSON), DecidedAt: a.DecidedAt})
	}
	if f.Feedback != nil {
		var reasons []string
		_ = json.Unmarshal(f.Feedback.Reasons, &reasons)
		if reasons == nil {
			reasons = []string{}
		}
		v.Feedback = &FeedbackView{Rating: f.Feedback.Rating, Reasons: reasons, Comment: f.Feedback.Comment}
	}
	return v
}

func eventViews(evs []model.AgentEvent) []*AgentEventView {
	out := make([]*AgentEventView, len(evs))
	for i := range evs {
		out[i] = eventView(&evs[i])
	}
	return out
}
