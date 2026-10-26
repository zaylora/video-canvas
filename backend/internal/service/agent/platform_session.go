package agent

import (
	"context"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// 方案二里会话、运行、事件、审批归 Agent 服务，它自己实现前端用的 REST 接口；但这几件事只有 Go 能做：
// 画布是不是这个用户的、有哪些已发布的 Agent 模型、消息里引用的节点 / 模型 / 技能是否有效（都在 Postgres 和模型注册表里）、
// 撤销本轮所依据的改动记录（agent_mutations）。下面是 Agent 服务通过平台 API 来问的入口，都不碰运行记录。

// CheckCanvas 确认画布属于这个用户。别人的画布和不存在的画布一样处理，不泄露存在与否（返回画布不存在）。
func (b *AgentBridge) CheckCanvas(ctx context.Context, userID, canvasID uint64) error {
	if userID == 0 || canvasID == 0 {
		return errcode.ErrInvalidParams.WithMsg("缺少 user_id 或 canvas_id")
	}
	return b.d.Agent.checkCanvas(ctx, userID, canvasID)
}

// AgentModels 返回当前可用的 Agent 模型；没有任何已发布的模型时是空数组。
func (b *AgentBridge) AgentModels(ctx context.Context) ([]model.AgentModelView, error) {
	return b.d.Agent.Models(ctx)
}

// CheckMessage 校验用户消息里的引用（节点、模型、技能必须有效，一条消息最多引用若干个对象），
// 并把选中的节点里已经不存在的去掉（界面状态过期不该拦住发送）。规则就是发起运行时用的那一份。
func (b *AgentBridge) CheckMessage(ctx context.Context, userID, canvasID uint64, message string, selection []string) ([]string, error) {
	if userID == 0 || canvasID == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("缺少 user_id 或 canvas_id")
	}
	return b.d.Agent.checkRefs(ctx, userID, canvasID, message, selection)
}

// UndoPlatformRun 撤销某运行对画布的全部改动。运行是不是这个用户的、是不是已经结束，由持有运行记录的 Agent 服务校验。
func (b *AgentBridge) UndoPlatformRun(ctx context.Context, rc RunContext) (*UndoResult, error) {
	if err := rc.validate(); err != nil {
		return nil, err
	}
	return b.d.Canvas.UndoRun(ctx, rc.run())
}
