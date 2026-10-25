package agent

import (
	"context"
	"encoding/json"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// 平台 API 的「上下文模式」。
//
// Agent 作为独立服务（方案二）后，会话、运行、审批、事件都归它，Go 里没有这个运行的 agent_runs 记录，也没有桥令牌。
// 调用方（Agent 服务）随每个请求带上运行上下文 RunContext，Go 据此执行平台能力：画布读写、生成、技能、联网、模型代理和计费。
// 上下文模式下 Go 不发事件、不记步数、不改运行状态（这些归 Agent 服务）；画布写入的乐观锁、撤销依据、联网次数上限、
// 技能固定版本这些平台规则照旧。调用方通过服务令牌认证，所以它声明的用户 id 是可信的，但画布仍按 id + user_id 查，
// 不属于这个用户的画布一律「不存在」。

// RunContext 是调用方随请求声明的运行信息。ID 在 JSON 里用十进制字符串：Agent 服务用雪花算法生成的 ID 超过 2^53，
// 用 JSON 数字会在 JavaScript 里丢精度。
type RunContext struct {
	RunID     uint64   `json:"run_id,string"`
	UserID    uint64   `json:"user_id,string"`
	SessionID uint64   `json:"session_id,string"`
	CanvasID  uint64   `json:"canvas_id,string"`
	Mode      string   `json:"mode"`                // 任务模式，空按全能创作
	ModelKey  string   `json:"model_key"`           // 本次运行的 Agent 模型（模型代理和看图工具要用）
	Selection []string `json:"selection,omitempty"` // 用户选中的节点，读画布时优先展示

	BudgetCredits int `json:"budget_credits,omitempty"` // 本轮积分预算，0 表示不限制，只看账户可用积分
	SpentExtra    int `json:"spent_extra,omitempty"`    // 除模型调用外本轮已花的积分（已批准的生成）；模型调用的部分由 Go 按 agent_model_calls 累加
}

// validate 检查上下文：身份字段必须齐，任务模式必须认识（空模式按全能创作）。
func (rc RunContext) validate() error {
	if rc.RunID == 0 || rc.UserID == 0 || rc.SessionID == 0 || rc.CanvasID == 0 {
		return errcode.ErrInvalidParams.WithMsg("运行上下文缺少 run_id、user_id、session_id 或 canvas_id")
	}
	if rc.Mode != "" && !model.ValidAgentMode(rc.Mode) {
		return errcode.ErrInvalidParams.WithMsg("运行上下文里的任务模式不认识")
	}
	return nil
}

// run 构造给工具处理函数用的运行：只有身份字段，没有数据库记录（Status 恒为 running，Steps 等不使用）。
func (rc RunContext) run() *model.AgentRun {
	return &model.AgentRun{
		ID: rc.RunID, SessionID: rc.SessionID, CanvasID: rc.CanvasID, UserID: rc.UserID,
		Status: model.RunRunning, Mode: rc.Mode, BudgetCredits: rc.BudgetCredits, SpentCredits: rc.SpentExtra,
	}
}

// PlatformRunIdleTTL 是上下文模式下运行的内存状态闲置多久后被清掉：Agent 服务崩了没来得及调 EndPlatformRun 时，内存不会一直涨。
const PlatformRunIdleTTL = 12 * time.Hour

// platformRun 是一个运行在 Go 内存里的状态：令牌模式下它们挂在令牌会话上，随令牌失效；上下文模式没有令牌，按运行 id 存。
type platformRun struct {
	sess     *bridgeSession
	lastUsed time.Time
}

// platformSession 取（没有就建）这个运行的会话状态，并用本次请求声明的上下文刷新它；同时顺手清掉闲置过期的运行。
func (b *AgentBridge) platformSession(rc RunContext) *bridgeSession {
	now := b.d.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, p := range b.platformRuns {
		if now.Sub(p.lastUsed) > PlatformRunIdleTTL {
			delete(b.platformRuns, id)
		}
	}
	p, ok := b.platformRuns[rc.RunID]
	if !ok {
		p = &platformRun{sess: &bridgeSession{runID: rc.RunID, userID: rc.UserID, sessionID: rc.SessionID, canvasID: rc.CanvasID}}
		b.platformRuns[rc.RunID] = p
	}
	p.lastUsed = now
	// 模式、模型、选中节点以本次请求为准；计数和技能固定版本留在原来的对象上
	p.sess.mode, p.sess.modelKey, p.sess.selection = firstNonEmpty(rc.Mode, model.AgentModeAll), rc.ModelKey, rc.Selection
	return p.sess
}

// EndPlatformRun 释放一个运行在内存里的状态（联网次数、技能固定版本）。运行结束时由调用方通知；没通知也会在闲置过期后清掉。
func (b *AgentBridge) EndPlatformRun(runID uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.platformRuns, runID)
}

// platformExcluded 是不能经通用入口执行的工具：计划和提问只在 Agent 服务里；生成和删除会创建审批，审批归 Agent 服务，
// Go 只提供「预估」和「执行」两个专门的接口（见 platform_approval.go）。
var platformExcluded = map[string]bool{"plan_update": true, "ask_user": true, "canvas_delete": true, "generate_media": true}

// ExecutePlatformTool 在上下文模式下执行一次工具调用。和令牌模式的 ExecuteTool 的区别：
// 没有运行记录要查、不发 tool.start / tool.end（Agent 服务自己发）、不记步数和步数上限（Agent 服务自己数）、不留存工具调用记录。
// 任务模式的限制仍然在这里强制，不依赖调用方自觉。
// 工具层的失败（参数不对、校验不过、模式不允许）作为 IsError 的结果返回；只有上下文不合法、调用了不能经这个入口执行的工具、
// 或基础设施出错才返回 error。幂等键由调用方放进 ctx（WithIdempotencyKey），带了键的画布写入重放时不会再写一遍。
func (b *AgentBridge) ExecutePlatformTool(ctx context.Context, rc RunContext, toolCallID, name string, args json.RawMessage) (*ToolResult, error) {
	if err := rc.validate(); err != nil {
		return nil, err
	}
	spec := findTool(name)
	if spec == nil {
		return rejectedPlatformTool(toolCallID, name, "未知的工具："+name), nil
	}
	if platformExcluded[name] {
		return nil, errcode.ErrInvalidParams.WithMsg("工具 " + name + " 不能经这个接口执行")
	}
	sess := b.platformSession(rc)
	if !spec.allowsMode(sess.mode) {
		return rejectedPlatformTool(toolCallID, name, "当前是「"+modeLabel(sess.mode)+"」模式，不能使用 "+name+"。"), nil
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	out, herr := spec.run(b, ctx, &toolCall{run: rc.run(), sess: sess, id: toolCallID, args: args})
	res := b.finishTool(out, herr)
	res.ToolEnd = toolEndEvent(toolCallID, "", name, out, res)
	return res, nil
}

// rejectedPlatformTool 是被拒绝的调用（未知工具、模式不允许）的结果：和令牌模式一样，拒绝也有一条 tool.end，摘要就是原因。
func rejectedPlatformTool(toolCallID, name, msg string) *ToolResult {
	res := &ToolResult{Content: msg, IsError: true}
	res.ToolEnd = withToolLabel(map[string]any{"id": toolCallID, "name": name, "is_error": true, "summary": msg}, name)
	return res
}
