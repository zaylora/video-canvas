package agent

import (
	"encoding/json"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
)

// 对外的视图放在 service 包而不是 model：id 用编码串（idcodec），而 model 不允许依赖内部包（depguard A1）。

// AgentRunView 是返回给前端的运行。
type AgentRunView struct {
	ID            idcodec.ID `json:"id"`             // 运行 ID
	SessionID     idcodec.ID `json:"session_id"`     // 所属会话
	CanvasID      idcodec.ID `json:"canvas_id"`      // 所属画布
	Status        string     `json:"status"`         // 运行状态
	Mode          string     `json:"mode"`           // 任务模式
	BudgetCredits int        `json:"budget_credits"` // 本轮积分预算
	SpentCredits  int        `json:"spent_credits"`  // 已花积分
	ContextTokens int        `json:"context_tokens"` // 当前上下文大小（Token）：最近一次对话调用的输入
	Steps         int        `json:"steps"`          // 已执行步数
	MaxSteps      int        `json:"max_steps"`      // 步数上限
	ErrorCode     string     `json:"error_code"`     // 失败时的错误码
	ErrorMessage  string     `json:"error_message"`  // 失败时给用户看的文案
	CreatedAt     time.Time  `json:"created_at"`     // 创建时间
	EndedAt       *time.Time `json:"ended_at"`       // 结束时间，未结束为 null
}

// runView 转成返回给前端的结构。
func runView(r *model.AgentRun) *AgentRunView {
	return &AgentRunView{ID: idcodec.ID(r.ID), SessionID: idcodec.ID(r.SessionID), CanvasID: idcodec.ID(r.CanvasID),
		Status: r.Status, Mode: r.Mode, BudgetCredits: r.BudgetCredits, SpentCredits: r.SpentCredits, ContextTokens: r.ContextTokens, Steps: r.Steps,
		MaxSteps: r.MaxSteps, ErrorCode: r.ErrorCode, ErrorMessage: r.ErrorMessage, CreatedAt: r.CreatedAt, EndedAt: r.EndedAt}
}

// AgentEventView 是返回给前端的事件（HTTP 回放与 WebSocket 推送共用）。
type AgentEventView struct {
	SessionID idcodec.ID      `json:"session_id"` // 所属会话
	CanvasID  idcodec.ID      `json:"canvas_id"`  // 所属画布：推送走用户频道，前端靠它过滤出当前画布的事件
	RunID     *idcodec.ID     `json:"run_id"`     // 所属运行，会话级事件为 null
	Seq       int64           `json:"seq"`        // 会话内递增序号
	Type      string          `json:"type"`       // 事件类型
	Data      json.RawMessage `json:"data"`       // 事件内容
	CreatedAt time.Time       `json:"created_at"` // 创建时间
}

// eventView 转成返回给前端的结构。
func eventView(e *model.AgentEvent) *AgentEventView {
	v := &AgentEventView{SessionID: idcodec.ID(e.SessionID), Seq: e.Seq, Type: e.Type, Data: json.RawMessage(e.PayloadJSON), CreatedAt: e.CreatedAt}
	if e.RunID != 0 {
		id := idcodec.ID(e.RunID)
		v.RunID = &id
	}
	return v
}
