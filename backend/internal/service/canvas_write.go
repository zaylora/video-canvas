package service

import (
	"context"
	"errors"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
)

// CanvasReader 按用户隔离读取公共画布，不要求存在 Agent 运行。
type CanvasReader interface {
	// GetCanvas 查询 id + user_id，不存在或不属于该用户返回 repository.ErrNotFound。
	GetCanvas(ctx context.Context, userID, canvasID uint64) (*model.CanvasProject, error)
}

// WriteCanvas 在最新图上执行原子写入，版本冲突最多重读重放三次；apply 必须把关联写入和 CAS 放在同一事务。
// 调用方负责已读、幂等、撤销等业务校验；重试耗尽返回 repository.ErrRevisionConflict。
func WriteCanvas[T any](ctx context.Context, repo CanvasReader, userID, canvasID uint64, apply func(*canvasgraph.Graph, uint64) (T, error)) (T, error) {
	var zero T
	// 1. 每次重试都重新读取用户自己的画布，不能复用旧快照覆盖用户同时保存的内容。
	for range 3 {
		cv, err := repo.GetCanvas(ctx, userID, canvasID)
		if errors.Is(err, repository.ErrNotFound) {
			return zero, errcode.ErrCanvasNotFound
		}
		if err != nil {
			return zero, err
		}
		g, err := canvasgraph.Parse(cv.PayloadJSON)
		if err != nil {
			return zero, errcode.ErrCanvasPayload
		}
		// 2. apply 负责事务边界；CAS 失败必须回滚关联行，下一次才可安全重新建卡或检查幂等。
		out, err := apply(g, cv.Revision)
		if errors.Is(err, repository.ErrRevisionConflict) {
			continue
		}
		if errors.Is(err, repository.ErrNotFound) {
			return zero, errcode.ErrCanvasNotFound
		}
		return out, err
	}
	return zero, repository.ErrRevisionConflict
}

// CanvasPatch 是公共画布修改事件，Agent 可附带运行和改动日志标识；普通业务写入不伪造运行。
type CanvasPatch struct {
	MutationID     uint64
	RunID          uint64
	CanvasID       uint64
	Kind           string
	RevisionBefore uint64
	RevisionAfter  uint64
	Changes        []canvasgraph.Change
}

// PublishCanvasPatch 只在事务提交后发布与 Agent 相同格式的 canvas.patch，用户频道保证资源隔离。
func PublishCanvasPatch(ctx context.Context, bc ws.Broadcaster, userID uint64, patch CanvasPatch) {
	// 1. 没有差异或没有广播器时不推送；纯锁集事务不会制造不存在的画布 revision。
	if bc == nil || len(patch.Changes) == 0 {
		return
	}
	// 2. 公共业务事件保留相同字段，但不带虚构的 Agent id；前端以 canvas_id 和 revision 对账。
	data := map[string]any{
		"canvas_id": idcodec.ID(patch.CanvasID), "kind": patch.Kind,
		"revision_before": patch.RevisionBefore, "revision_after": patch.RevisionAfter,
		"changes": patch.Changes,
	}
	if patch.MutationID != 0 {
		data["mutation_id"] = idcodec.ID(patch.MutationID)
	}
	if patch.RunID != 0 {
		data["run_id"] = idcodec.ID(patch.RunID)
	}
	channel := ws.UserChannel(userID)
	bc.Publish(ctx, channel, ws.Message{Type: ws.TypeCanvasPatch, Channel: channel, Data: data})
}
