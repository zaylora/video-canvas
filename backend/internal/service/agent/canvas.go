package agent

import (
	"context"
	"encoding/json"
	"errors"

	"go.uber.org/zap"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// maxWriteAttempts 是画布写入遇到版本冲突时最多尝试的次数：每次都会重读最新画布、重新校验并应用同一批操作。
const maxWriteAttempts = 3

// AgentCanvasRepo 是 Agent 改画布所需的数据访问。
type AgentCanvasRepo interface {
	// GetCanvas 按 id + user_id 读画布，不存在或不属于该用户返回 repository.ErrNotFound。
	GetCanvas(ctx context.Context, userID, id uint64) (*model.CanvasProject, error)
	// GetRun 按 id + user_id 读运行，不存在或不属于该用户返回 repository.ErrNotFound。
	GetRun(ctx context.Context, userID, id uint64) (*model.AgentRun, error)
	// ListMutations 列出某运行的全部改动，按序号升序。
	ListMutations(ctx context.Context, runID uint64) ([]model.AgentMutation, error)
	// CommitCanvasMutation 在一个事务里按乐观锁更新画布、记录改动、标记被撤销的改动；
	// 版本不一致返回 repository.ErrRevisionConflict，画布不存在返回 repository.ErrNotFound。
	CommitCanvasMutation(ctx context.Context, in repository.CommitInput) error
}

// AgentCanvasService 是 Agent 读写画布的业务：把工具调用变成对 payload_json 的安全修改，
// 每次写入都带乐观锁、记改动日志、向前端推送 canvas.patch。
//
// 工具层的错误（*canvasgraph.ValidationError、canvasgraph.ErrInvalid）原样返回，由调用方转成给模型看的工具错误，
// 不是 HTTP 错误；其余业务错误都是 errcode.*。
type AgentCanvasService struct {
	repo     AgentCanvasRepo
	bc       ws.Broadcaster
	modelOps ModelOpsSource // 校验和展示生成方式用；nil 表示不校验、不展示
}

// ModelOpsSource 返回已上架、可用的图片和视频模型各自支持的生成方式（模型 key → ops）。
type ModelOpsSource func(ctx context.Context) (map[string][]string, error)

// SetModelOps 设置模型的生成方式来源。装配桥时由 NewAgentBridge 用模型注册表设置。
func (s *AgentCanvasService) SetModelOps(src ModelOpsSource) { s.modelOps = src }

// registryModelOps 用模型注册表做生成方式来源；注册表缓存了编译结果，每次调用不查库。
func registryModelOps(reg provider.Registry) ModelOpsSource {
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
func (s *AgentCanvasService) modelOpsLookup(ctx context.Context) canvasgraph.ModelOpsFunc {
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

// NewAgentCanvasService 创建服务；bc 为 nil 时不推送。
func NewAgentCanvasService(repo AgentCanvasRepo, bc ws.Broadcaster) *AgentCanvasService {
	if bc == nil {
		bc = ws.NopBroadcaster{}
	}
	return &AgentCanvasService{repo: repo, bc: bc}
}

// WriteResult 是一次画布写入的结果。没有任何变化时 MutationID 为 0，也不会产生新的 revision。
type WriteResult struct {
	IDMap      map[string]string `json:"id_map,omitempty"`   // tempId → 真实 id
	MutationID uint64            `json:"mutation_id"`        // 改动日志 id，0 表示没有写入
	Revision   uint64            `json:"revision"`           // 写入后的画布 revision
	Changes    int               `json:"changes"`            // 改动项数
	Created    []string          `json:"-"`                  // 新建的节点 id，前端的改动摘要用
	Updated    []string          `json:"-"`                  // 改了字段的节点 id（不含新建和删除）
	Warnings   []string          `json:"warnings,omitempty"` // 写入后发现的问题提醒（如生成方式和已连素材对不上），不拦截
}

// nodeIDsByOp 取出差异里某种操作涉及的节点 id，连线不算。
func nodeIDsByOp(changes []canvasgraph.Change, op string) []string {
	ids := []string{}
	for _, c := range changes {
		if c.Kind == "node" && c.Op == op {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// UndoResult 是撤销本轮的结果。
type UndoResult struct {
	Reverted int                `json:"reverted"` // 恢复或删除了多少项
	Skipped  []canvasgraph.Skip `json:"skipped"`  // 没处理的项和原因
	Revision uint64             `json:"revision"` // 撤销后的画布 revision，没有写入时为 0
}

// edit 是对一张图的一次修改：新图、差异，以及要带回给调用方的附加信息。
type edit struct {
	graph   *canvasgraph.Graph
	changes []canvasgraph.Change
	idMap   map[string]string
	extra   any
}

// ApplyOps 应用一批编辑操作（建改节点和组、连线、移动）。整批原子：任何一项不合法都不会写入。
func (s *AgentCanvasService) ApplyOps(ctx context.Context, run *model.AgentRun, toolCallID string, raw []byte) (*WriteResult, error) {
	// 1. 先解析操作：格式不对、超过 30 项、带了产物字段，都在读画布之前挡掉
	ops, err := canvasgraph.ParseOps(raw)
	if err != nil {
		return nil, err
	}
	// 2. 在最新画布上应用并写入；版本冲突时重读重试。模型的生成方式在重试之前取一次：mode 要按它校验，写入后还要据它检查提醒
	lookup := s.modelOpsLookup(ctx)
	res, extra, err := s.mutate(ctx, run, toolCallID, model.MutationApplyOps, 0, func(g *canvasgraph.Graph) (*edit, error) {
		r, err := canvasgraph.Apply(g, ops, canvasgraph.Options{ModelOps: lookup})
		if err != nil {
			return nil, err
		}
		warnings := canvasgraph.ModeWarnings(r.Graph, touchedNodeIDs(r.Changes), lookup)
		return &edit{graph: r.Graph, changes: r.Changes, idMap: r.IDMap, extra: warnings}, nil
	})
	if w, ok := extra.([]string); ok && res != nil {
		res.Warnings = w
	}
	return res, err
}

// touchedNodeIDs 取出这批改动涉及的节点：新建、修改的节点，以及新连线的两端。
func touchedNodeIDs(changes []canvasgraph.Change) []string {
	var ids []string
	for _, c := range changes {
		switch {
		case c.Kind == "node" && c.Op != canvasgraph.ChangeDelete:
			ids = append(ids, c.ID)
		case c.Kind == "edge" && c.Op == canvasgraph.ChangeCreate:
			for _, k := range []string{"source", "target"} {
				if id, _ := c.After[k].(string); id != "" {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

// Arrange 整理布局：排列一个组的成员或一组节点。
func (s *AgentCanvasService) Arrange(ctx context.Context, run *model.AgentRun, toolCallID string, target canvasgraph.ArrangeTarget, layout canvasgraph.Layout) (*WriteResult, error) {
	res, _, err := s.mutate(ctx, run, toolCallID, model.MutationArrange, 0, func(g *canvasgraph.Graph) (*edit, error) {
		r, err := canvasgraph.Arrange(g, target, layout, nil)
		if err != nil {
			return nil, err
		}
		return &edit{graph: r.Graph, changes: r.Changes}, nil
	})
	return res, err
}

// DeleteApproved 执行用户已批准的删除。删除是破坏性操作，调用方必须先走审批，这里不再校验审批状态。
func (s *AgentCanvasService) DeleteApproved(ctx context.Context, run *model.AgentRun, toolCallID string, nodeIDs, edgeIDs []string) (*WriteResult, error) {
	res, _, err := s.mutate(ctx, run, toolCallID, model.MutationDelete, 0, func(g *canvasgraph.Graph) (*edit, error) {
		r, err := canvasgraph.Delete(g, nodeIDs, edgeIDs)
		if err != nil {
			return nil, err
		}
		return &edit{graph: r.Graph, changes: r.Changes}, nil
	})
	return res, err
}

// Undo 撤销某个运行对画布的全部改动。
func (s *AgentCanvasService) Undo(ctx context.Context, userID, runID uint64) (*UndoResult, error) {
	// 1. 只能撤销自己的运行；别人的和不存在的统一返回「运行不存在」
	run, err := s.repo.GetRun(ctx, userID, runID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrAgentRunMissing
	}
	if err != nil {
		return nil, err
	}
	// 2. 运行还在跑或在等用户时不能撤销，否则 Agent 马上又会写进来
	if model.IsActiveRun(run.Status) {
		return nil, errcode.ErrAgentState
	}
	// 3. 取出还没被撤销的改动。生成绑定（bind）不参与：撤销不会取消已批准的生成任务，
	//    也就不能把节点上的 taskId 抹掉
	changes, err := s.undoableChanges(ctx, runID)
	if err != nil {
		return nil, err
	}
	if changes == nil {
		return &UndoResult{Skipped: []canvasgraph.Skip{}}, nil
	}
	// 4. 在最新画布上逆向撤销：字段只有仍等于 Agent 写入的值才恢复，用户后来改的不动
	var rr *canvasgraph.RevertResult
	res, _, err := s.mutate(ctx, run, "", model.MutationUndo, runID, func(g *canvasgraph.Graph) (*edit, error) {
		rr = canvasgraph.Revert(g, changes)
		return &edit{graph: rr.Graph, changes: canvasgraph.Diff(g, rr.Graph)}, nil
	})
	if err != nil {
		return nil, err
	}
	out := &UndoResult{Reverted: rr.Reverted, Skipped: rr.Skipped, Revision: res.Revision}
	if out.Skipped == nil {
		out.Skipped = []canvasgraph.Skip{}
	}
	return out, nil
}

// undoableChanges 汇总某运行里还没被撤销的改动（按序号升序拼接）。
// 返回 nil 表示这个运行根本没有改过画布；所有改动都已被撤销返回 ErrAgentAlreadyUndone。
func (s *AgentCanvasService) undoableChanges(ctx context.Context, runID uint64) ([]canvasgraph.Change, error) {
	muts, err := s.repo.ListMutations(ctx, runID)
	if err != nil {
		return nil, err
	}
	var all []canvasgraph.Change
	hasOriginal, hasPending := false, false
	for _, m := range muts {
		if m.Kind == model.MutationUndo {
			continue
		}
		hasOriginal = true
		if m.UndoneAt != nil {
			continue
		}
		hasPending = true
		if m.Kind == model.MutationBind {
			continue
		}
		var cs []canvasgraph.Change
		if err := json.Unmarshal(m.ChangesJSON, &cs); err != nil {
			return nil, err
		}
		all = append(all, cs...)
	}
	switch {
	case !hasOriginal:
		return nil, nil
	case !hasPending:
		return nil, errcode.ErrAgentAlreadyUndone
	}
	if all == nil {
		all = []canvasgraph.Change{}
	}
	return all, nil
}

// Catalog 返回画布目录，作为每轮对话的上下文。
func (s *AgentCanvasService) Catalog(ctx context.Context, run *model.AgentRun, opt canvasgraph.CatalogOptions) (*canvasgraph.Catalog, error) {
	g, _, err := s.load(ctx, run)
	if err != nil {
		return nil, err
	}
	c := canvasgraph.BuildCatalog(g, opt)
	return &c, nil
}

// Detail 读取一批节点的详情（不含媒体地址）。
func (s *AgentCanvasService) Detail(ctx context.Context, run *model.AgentRun, ids []string) (*canvasgraph.DetailResult, error) {
	g, _, err := s.load(ctx, run)
	if err != nil {
		return nil, err
	}
	d, err := canvasgraph.Detail(g, ids)
	if err != nil {
		return nil, err
	}
	canvasgraph.AnnotateModes(d, s.modelOpsLookup(ctx))
	return d, nil
}

// load 读取并解析运行所属用户的画布，返回图和当前 revision。
func (s *AgentCanvasService) load(ctx context.Context, run *model.AgentRun) (*canvasgraph.Graph, uint64, error) {
	cv, err := s.repo.GetCanvas(ctx, run.UserID, run.CanvasID)
	if errors.Is(err, repository.ErrNotFound) {
		// 按 id + user_id 查，别人的画布和不存在的画布统一返回「画布不存在」
		return nil, 0, errcode.ErrCanvasNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	g, err := canvasgraph.Parse(cv.PayloadJSON)
	if err != nil {
		return nil, 0, errcode.ErrCanvasPayload
	}
	return g, cv.Revision, nil
}

// mutate 是所有写画布操作的公共流程：读最新画布 → fn 修改 → 带乐观锁写入并记日志 → 推送 patch。
// 写入时版本冲突（用户刚好保存了）就重读最新画布、重新执行 fn，最多 maxWriteAttempts 次，
// 这样 Agent 的改动总是叠在用户最新的内容上，不会覆盖用户的保存。fn 没有产生任何变化时不写入。
func (s *AgentCanvasService) mutate(ctx context.Context, run *model.AgentRun, toolCallID, kind string, undoRunID uint64, fn func(*canvasgraph.Graph) (*edit, error)) (*WriteResult, any, error) {
	for attempt := 0; attempt < maxWriteAttempts; attempt++ {
		g, revision, err := s.load(ctx, run)
		if err != nil {
			return nil, nil, err
		}
		ed, err := fn(g)
		if err != nil {
			return nil, nil, err
		}
		if len(ed.changes) == 0 {
			return &WriteResult{IDMap: ed.idMap, Revision: revision}, ed.extra, nil
		}
		payload, err := ed.graph.Marshal()
		if err != nil {
			return nil, nil, err
		}
		changesJSON, err := json.Marshal(ed.changes)
		if err != nil {
			return nil, nil, err
		}
		mut := &model.AgentMutation{RunID: run.ID, ToolCallID: toolCallID, Kind: kind, ChangesJSON: changesJSON}
		err = s.repo.CommitCanvasMutation(ctx, repository.CommitInput{
			UserID: run.UserID, CanvasID: run.CanvasID, BaseRevision: revision, Payload: payload, Mutation: mut, UndoRunID: undoRunID,
		})
		switch {
		case errors.Is(err, repository.ErrRevisionConflict):
			continue
		case errors.Is(err, repository.ErrNotFound):
			return nil, nil, errcode.ErrCanvasNotFound
		case err != nil:
			return nil, nil, err
		}
		s.publishPatch(ctx, run, mut, ed.changes)
		return &WriteResult{
			IDMap: ed.idMap, MutationID: mut.ID, Revision: mut.RevisionAfter, Changes: len(ed.changes),
			Created: nodeIDsByOp(ed.changes, "create"), Updated: nodeIDsByOp(ed.changes, "update"),
		}, ed.extra, nil
	}
	return nil, nil, errcode.ErrAgentWriteConflict
}

// publishPatch 在写入提交之后向用户推送改动，前端据此做三方合并。推送可以丢，前端会按 revision 对账。
// 走用户频道（连接时自动订阅，不需要订阅授权），消息里带编码后的 canvas_id，前端按它过滤。
func (s *AgentCanvasService) publishPatch(ctx context.Context, run *model.AgentRun, m *model.AgentMutation, changes []canvasgraph.Change) {
	channel := ws.UserChannel(run.UserID)
	s.bc.Publish(ctx, channel, ws.Message{
		Type:    ws.TypeCanvasPatch,
		Channel: channel,
		Data: map[string]any{
			"mutation_id":     idcodec.ID(m.ID),
			"run_id":          idcodec.ID(run.ID),
			"canvas_id":       idcodec.ID(run.CanvasID),
			"kind":            m.Kind,
			"revision_before": m.RevisionBefore,
			"revision_after":  m.RevisionAfter,
			"changes":         changes,
		},
	})
}

// Generations 读出若干节点发起生成所需的输入，顺序与 ids 一致。任何一个节点不能生成都整体返回 canvasgraph.ErrInvalid。
func (s *AgentCanvasService) Generations(ctx context.Context, run *model.AgentRun, ids []string) ([]*canvasgraph.GenSource, error) {
	g, _, err := s.load(ctx, run)
	if err != nil {
		return nil, err
	}
	out := make([]*canvasgraph.GenSource, len(ids))
	for i, id := range ids {
		if out[i], err = canvasgraph.Generation(g, id); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// BindTasks 把批准后创建的生成任务绑定到节点（节点 id → 任务 id 的十进制字符串）。
// 绑定只写 taskId 和状态，记为 bind 改动：撤销本轮不会碰它，因为已提交的任务撤销不了。
func (s *AgentCanvasService) BindTasks(ctx context.Context, run *model.AgentRun, toolCallID string, tasks map[string]string) (*WriteResult, error) {
	res, _, err := s.mutate(ctx, run, toolCallID, model.MutationBind, 0, func(g *canvasgraph.Graph) (*edit, error) {
		r := canvasgraph.BindTasks(g, tasks)
		return &edit{graph: r.Graph, changes: r.Changes}, nil
	})
	return res, err
}

// VideoRef 取一个视频节点当前的视频素材，给 Agent「看视频」和截帧用。
func (s *AgentCanvasService) VideoRef(ctx context.Context, run *model.AgentRun, id string) (canvasgraph.VideoSource, error) {
	g, _, err := s.load(ctx, run)
	if err != nil {
		return canvasgraph.VideoSource{}, err
	}
	return canvasgraph.VideoRef(g, id)
}

// CheckFrames 在不写入的前提下校验截帧请求：视频节点要有产物、每帧的连线目标合法。
// 截帧要先抽帧、存素材，最后才改画布；先在这里挡掉不合法的请求，免得白存一批素材。
// connects[i] 是第 i 帧要连到的节点。
func (s *AgentCanvasService) CheckFrames(ctx context.Context, run *model.AgentRun, videoID string, connects [][]string) (canvasgraph.VideoSource, error) {
	g, _, err := s.load(ctx, run)
	if err != nil {
		return canvasgraph.VideoSource{}, err
	}
	src, err := canvasgraph.VideoRef(g, videoID)
	if err != nil {
		return canvasgraph.VideoSource{}, err
	}
	specs := make([]canvasgraph.FrameSpec, len(connects))
	for i, to := range connects {
		specs[i] = canvasgraph.FrameSpec{Label: "帧", ConnectTo: to}
	}
	if _, err := canvasgraph.AddFrameNodes(g, videoID, specs, canvasgraph.Options{}); err != nil {
		return canvasgraph.VideoSource{}, err
	}
	return src, nil
}

// AddFrames 把已存好素材的帧图建成图片节点：放在视频节点右侧，连来源线，写了 ConnectTo 的再连到目标作参考。
// 整批原子：任何一项不合法都不会写入。
func (s *AgentCanvasService) AddFrames(ctx context.Context, run *model.AgentRun, toolCallID, videoID string, frames []canvasgraph.FrameSpec) (*WriteResult, error) {
	res, _, err := s.mutate(ctx, run, toolCallID, model.MutationFrames, 0, func(g *canvasgraph.Graph) (*edit, error) {
		r, err := canvasgraph.AddFrameNodes(g, videoID, frames, canvasgraph.Options{})
		if err != nil {
			return nil, err
		}
		return &edit{graph: r.Graph, changes: r.Changes, idMap: r.IDMap}, nil
	})
	return res, err
}

// ImageRefs 取若干图片节点当前的素材，顺序与 ids 一致，给 Agent「看图」用。
func (s *AgentCanvasService) ImageRefs(ctx context.Context, run *model.AgentRun, ids []string) ([]canvasgraph.ImageRef, error) {
	g, _, err := s.load(ctx, run)
	if err != nil {
		return nil, err
	}
	return canvasgraph.ImageRefs(g, ids)
}
