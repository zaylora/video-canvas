package agent

import (
	"context"
	"fmt"
	"strings"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// ReadDetail 返回节点详情并持久化已读记录，仅供 canvas_get_state 的显式详情读取使用。
// 审批预估等内部 Detail 调用不表示模型看过详情，不能获得写入资格。
func (s *AgentCanvasService) ReadDetail(ctx context.Context, run *model.AgentRun, ids []string) (*canvasgraph.DetailResult, error) {
	// 1. 先完成读取和校验；任何节点不存在或超限时，不记录这次读取
	detail, err := s.Detail(ctx, run, ids)
	if err != nil {
		return nil, err
	}
	// 2. 仅记录实际返回详情的节点；记录失败不返回成功，避免后续写入误以为已有资格
	readIDs := make([]string, 0, len(detail.Nodes))
	for _, n := range detail.Nodes {
		readIDs = append(readIDs, n.ID)
	}
	if err := s.repo.RecordCanvasReads(ctx, canvasReadRows(run, readIDs)); err != nil {
		return nil, fmt.Errorf("记录画布节点已读失败：%w", err)
	}
	return detail, nil
}

// canvasReadRows 把节点 id 绑定到完整运行作用域，防止跨用户、画布、会话或运行复用已读资格。
func canvasReadRows(run *model.AgentRun, ids []string) []model.AgentCanvasRead {
	rows := make([]model.AgentCanvasRead, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, model.AgentCanvasRead{UserID: run.UserID, CanvasID: run.CanvasID, SessionID: run.SessionID, RunID: run.ID, NodeID: id})
	}
	return rows
}

// requireReadNodes 在最新画布上检查整批操作的内容修改目标；同批已创建的临时引用不需要另读。
// 检查在 mutate 的重放判断之后、事务提交之前执行，版本冲突重试时也会再次检查。
func (s *AgentCanvasService) requireReadNodes(ctx context.Context, run *model.AgentRun, g *canvasgraph.Graph, ops []canvasgraph.Op) error {
	targets := contentTargets(g, ops)
	if len(targets) == 0 {
		return nil
	}
	ids, err := s.repo.ListCanvasReads(ctx, run)
	if err != nil {
		return fmt.Errorf("读取画布节点已读记录失败：%w", err)
	}
	read := make(map[string]bool, len(ids))
	for _, id := range ids {
		read[id] = true
	}
	var unread []string
	for _, id := range targets {
		if !read[id] {
			unread = append(unread, id)
		}
	}
	if len(unread) > 0 {
		return errcode.ErrAgentNodeUnread.WithMsg("操作没有生效：以下节点尚未读取详情：" + strings.Join(unread, "、") + "。请先调用 canvas_get_state，传 nodeIds 读取这些节点的详情，再重试整批操作")
	}
	return nil
}

// contentTargets 按执行顺序解析目标：update_node 改内容/参数，set_group 改组名/颜色；纯成员调整属于布局。
func contentTargets(g *canvasgraph.Graph, ops []canvasgraph.Op) []string {
	created, seen := map[string]bool{}, map[string]bool{}
	var ids []string
	for _, op := range ops {
		switch op.Op {
		case canvasgraph.OpCreateNode, canvasgraph.OpCreateGroup:
			if op.TempID != "" {
				created[op.TempID] = true
			}
		case canvasgraph.OpUpdateNode, canvasgraph.OpSetGroup:
			if op.Op == canvasgraph.OpSetGroup && op.Label == nil && op.Color == nil && op.LabelColor == nil {
				continue
			}
			if !created[op.ID] && !seen[op.ID] && g.Node(op.ID) != nil {
				ids, seen[op.ID] = append(ids, op.ID), true
			}
		}
	}
	return ids
}
