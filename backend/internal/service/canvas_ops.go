package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"gorm.io/datatypes"

	"go.uber.org/zap"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// canvasOpsPatchKind 是节点级编辑推送 canvas.patch 时的来源标记。
const canvasOpsPatchKind = "cli_ops"

// CanvasModelOpsSource 返回已上架、可用的图片和视频模型各自支持的生成方式（模型 key → ops）。
type CanvasModelOpsSource func(ctx context.Context) (map[string][]string, error)

// RegistryModelOps 用模型注册表做生成方式来源；注册表缓存了编译结果，每次调用不查库。
// 画布 Agent 与节点级编辑共用这一份，避免两边对「模型支持哪些 mode」各有各的答案。
func RegistryModelOps(reg provider.Registry) CanvasModelOpsSource {
	return func(ctx context.Context) (map[string][]string, error) {
		list, err := reg.ListModels(ctx, "")
		if err != nil {
			return nil, err
		}
		out := make(map[string][]string, len(list))
		for _, m := range list {
			if (m.Kind == "video" || m.Kind == "image") && len(m.Capabilities.Ops) > 0 {
				out[m.Key] = m.Capabilities.Ops
			}
		}
		return out, nil
	}
}

// modelOpsLookup 取一次模型的生成方式并包成查询函数；没有来源或取失败时返回 nil（不校验，生成时再兜底）。
func (s *CanvasProjectService) modelOpsLookup(ctx context.Context) canvasgraph.ModelOpsFunc {
	if s.modelOps == nil {
		return nil
	}
	ops, err := s.modelOps(ctx)
	if err != nil {
		logger.Warn("读取模型生成方式失败，本次不校验 mode", zap.Error(err))
		return nil
	}
	return func(model string) ([]string, bool) {
		v, ok := ops[model]
		return v, ok
	}
}

// CanvasOpsReq 是一次节点级编辑：一批 canvasgraph 操作，外加可选的删除节点 id。
//
// Revision 必须由调用方显式给出，且必须是它「看到并确认过」的那个版本：
// 服务端只在这个版本仍然成立时写入，否则返回 409。这样人确认的差异与实际落库的差异
// 不会因为并发修改而对不上。
type CanvasOpsReq struct {
	Revision      uint64
	Ops           json.RawMessage
	DeleteNodeIDs []string
}

// CanvasOpsResult 是节点级编辑的结果：新版本、逐项改动、提醒与临时 id 映射。
type CanvasOpsResult struct {
	Revision uint64               `json:"revision"`
	Changes  []canvasgraph.Change `json:"changes"`
	Warnings []string             `json:"warnings,omitempty"`
	IDMap    map[string]string    `json:"id_map,omitempty"`
}

// ApplyOps 在服务端执行一批节点级操作。
//
// 语义只有一份：全部走 canvasgraph（与 Agent 的 canvas_apply_ops 同一套规则），
// CLI 不自己拼图、也不自己判断连线合法性。素材类操作（assetId）在 CLI 这条路上不支持，
// 因为素材归属无法在这里校验；需要参考素材时用连线。
//
//nolint:gocognit,funlen // 校验 → 应用 ops → 删节点 → 写回，四步各有前置条件，平铺比拆函数清楚
func (s *CanvasProjectService) ApplyOps(ctx context.Context, userID, id uint64, req *CanvasOpsReq) (*CanvasOpsResult, error) {
	if req.Revision == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("必须带 revision：节点级修改只在你看过的版本上生效")
	}
	hasOps := len(bytes.TrimSpace(req.Ops)) > 0
	if !hasOps && len(req.DeleteNodeIDs) == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("ops 与 deleteNodeIds 至少给一个")
	}

	cv, err := s.repo.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrCanvasNotFound
	}
	if err != nil {
		return nil, err
	}
	// 短剧画布的节点由短剧服务按剧本结构维护（回填也不写它），命令行按差异改会破坏结构，一律拒绝。
	if cv.DramaID != nil {
		return nil, errcode.ErrInvalidParams.WithMsg("短剧画布的节点由短剧项目维护，请在网页里编辑")
	}
	if cv.CollaborationActive {
		return nil, errcode.ErrCanvasCollaborationActive
	}
	g, err := canvasgraph.Parse(cv.PayloadJSON)
	if err != nil {
		return nil, errcode.ErrCanvasPayload
	}

	out := &CanvasOpsResult{IDMap: map[string]string{}}
	changed := false

	if hasOps {
		ops, err := canvasgraph.ParseOps(req.Ops)
		if err != nil {
			return nil, errcode.ErrInvalidParams.WithMsg(err.Error())
		}
		for _, op := range ops {
			if op.AssetID != "" {
				return nil, errcode.ErrInvalidParams.WithMsg("不支持按 assetId 建节点：素材归属不在这里校验，请改用连线引用已有节点")
			}
		}
		res, err := canvasgraph.Apply(g, ops, canvasgraph.Options{ModelOps: s.modelOpsLookup(ctx)})
		if err != nil {
			return nil, errcode.ErrInvalidParams.WithMsg(err.Error())
		}
		g = res.Graph
		out.Changes = append(out.Changes, res.Changes...)
		out.Warnings = append(out.Warnings, res.Warnings...)
		for k, v := range res.IDMap {
			out.IDMap[k] = v
		}
		changed = changed || len(res.Changes) > 0
	}

	if len(req.DeleteNodeIDs) > 0 {
		res, err := canvasgraph.DeleteNodes(g, req.DeleteNodeIDs)
		if err != nil {
			return nil, errcode.ErrInvalidParams.WithMsg(err.Error())
		}
		g = res.Graph
		out.Changes = append(out.Changes, res.Changes...)
		changed = changed || len(res.Changes) > 0
	}

	// 没有任何实际改动就不写库：避免为了空操作把 revision 顶上去，让前端白刷一次。
	if !changed {
		out.Revision = cv.Revision
		return out, nil
	}

	payload, err := g.Marshal()
	if err != nil {
		return nil, err
	}
	saved, err := s.repo.Update(ctx, userID, id, req.Revision, map[string]any{
		"payload_json": datatypes.JSON(payload),
	})
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil, errcode.ErrCanvasNotFound
	case errors.Is(err, repository.ErrRevisionConflict):
		return nil, errcode.ErrCanvasConflict
	case errors.Is(err, repository.ErrCanvasCollaborationActive):
		return nil, errcode.ErrCanvasCollaborationActive
	case err != nil:
		return nil, err
	}
	out.Revision = saved.Revision
	// 写库成功后再推：浏览器据此把 revision 追上，不会在下次自动保存时撞 409。推送可丢，客户端会对账。
	PublishCanvasPatch(ctx, s.bc, userID, CanvasPatch{
		CanvasID: id, Kind: canvasOpsPatchKind,
		RevisionBefore: req.Revision, RevisionAfter: saved.Revision, Changes: out.Changes,
	})
	return out, nil
}
