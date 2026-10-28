package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
)

// DramaCanvasRepo 是短剧对公共画布读写的依赖，事务中原子写入集锁、角色卡和画布。
type DramaCanvasRepo interface {
	CanvasReader
	// WithTx 在同一事务执行 fn，任何错误都回滚关联写入。
	WithTx(ctx context.Context, fn func(repository.DramaCanvasTx) error) error
}

// DramaCanvasAsset 是本次新增节点与模型原始资产，供后续 asset_prompts 精确处理新增范围。
type DramaCanvasAsset struct {
	NodeID string      `json:"node_id"`
	Asset  drama.Asset `json:"asset"`
}

// DramaCanvasService 提供不依赖 Agent run 的短剧画布写入口，复用公共图差异、CAS 重放和 patch 协议。
type DramaCanvasService struct {
	repo DramaCanvasRepo
	bc   ws.Broadcaster
}

// NewDramaCanvasService 创建短剧画布服务，bc 为 nil 时仅持久化而不广播。
func NewDramaCanvasService(repo DramaCanvasRepo, bc ws.Broadcaster) *DramaCanvasService {
	return &DramaCanvasService{repo: repo, bc: bc}
}

// breakdownAttempt 是一次 CAS 尝试的产物，仅事务提交后才对外可见。
type breakdownAttempt struct {
	created  []DramaCanvasAsset
	changes  []canvasgraph.Change
	before   uint64
	revision uint64
}

// dramaScope 是一次拆解事务的归属与范围。
type dramaScope struct {
	userID, dramaID, canvasID, jobID uint64
	episodes                         []int
	selected                         map[int]bool
}

// ApplyBreakdown 合并资产并原子锁定选择集；只有新增角色建无头像卡，只返回本次新增节点。
func (s *DramaCanvasService) ApplyBreakdown(ctx context.Context, userID, dramaID, canvasID, jobID uint64, episodes []int, assets []drama.Asset) ([]DramaCanvasAsset, error) {
	// 1. 先校验真实模型结果与选择范围，避免非法输出造成部分建卡或锁集。
	if err := (drama.BreakdownOutput{Assets: assets}).Validate(); err != nil {
		return nil, errcode.ErrDramaOutputInvalid
	}
	selected, err := dramaEpisodeSet(episodes)
	if err != nil {
		return nil, err
	}
	sc := dramaScope{userID: userID, dramaID: dramaID, canvasID: canvasID, jobID: jobID, episodes: episodes, selected: selected}
	// 2. CAS 冲突重读重放整个事务；失败尝试创建的角色卡与集锁一起回滚，不能留下孤儿卡。
	out, err := WriteCanvas(ctx, s.repo, userID, canvasID, func(g *canvasgraph.Graph, revision uint64) (breakdownAttempt, error) {
		attempt := breakdownAttempt{created: []DramaCanvasAsset{}, before: revision, revision: revision}
		err := s.repo.WithTx(ctx, func(tx repository.DramaCanvasTx) error {
			return breakdownTx(ctx, tx, g, revision, &attempt, sc, assets)
		})
		return attempt, err
	})
	if errors.Is(err, repository.ErrRevisionConflict) {
		return nil, errcode.ErrCanvasConflict
	}
	if err != nil {
		return nil, err
	}
	// 3. 只在事务完整提交后广播，失败和纯集锁事务均不产生画布事件。
	PublishCanvasPatch(ctx, s.bc, userID, CanvasPatch{CanvasID: canvasID, Kind: "breakdown", RevisionBefore: out.before, RevisionAfter: out.revision, Changes: out.changes})
	return out.created, nil
}

// dramaEpisodeSet 校验集号非空、为正且不重复。
func dramaEpisodeSet(episodes []int) (map[int]bool, error) {
	if len(episodes) == 0 {
		return nil, errcode.ErrDramaStepNotReady
	}
	selected := make(map[int]bool, len(episodes))
	for _, no := range episodes {
		if no < 1 || selected[no] {
			return nil, errcode.ErrDramaStepNotReady
		}
		selected[no] = true
	}
	return selected, nil
}

// lockedJobCheckpoint 读取并校验仍在运行的任务及其崩溃恢复标记。
func lockedJobCheckpoint(ctx context.Context, tx repository.DramaCanvasTx, userID, canvasID, jobID uint64, step string) (*model.DramaJob, canvasCheckpoint, error) {
	job, err := tx.JobForUpdate(ctx, userID, canvasID, jobID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, canvasCheckpoint{}, errcode.ErrDramaStepNotReady
	}
	if err != nil {
		return nil, canvasCheckpoint{}, err
	}
	if job.Status != "running" || job.Step != step {
		return nil, canvasCheckpoint{}, errcode.ErrDramaStepNotReady
	}
	checkpoint, err := readCanvasCheckpoint(job.Output)
	return job, checkpoint, err
}

// breakdownTx 在单个事务里完成任务校验、集锁校验、合并建卡、画布 CAS、锁集与提交标记。
func breakdownTx(ctx context.Context, tx repository.DramaCanvasTx, g *canvasgraph.Graph, revision uint64, attempt *breakdownAttempt, sc dramaScope, assets []drama.Asset) error {
	job, checkpoint, err := lockedJobCheckpoint(ctx, tx, sc.userID, sc.canvasID, sc.jobID, "breakdown")
	if err != nil {
		return err
	}
	if job.DramaID != sc.dramaID {
		return errcode.ErrDramaNotFound
	}
	if checkpoint.CanvasCommitted {
		attempt.created = checkpoint.NewAssets
		return nil
	}
	if err := checkEpisodesLockable(ctx, tx, sc); err != nil {
		return err
	}
	if err := tx.CheckCanvasRevision(ctx, sc.userID, sc.canvasID, revision); err != nil {
		return err
	}
	graphAssets := make([]canvasgraph.DramaAsset, len(assets))
	for i, a := range assets {
		graphAssets[i] = canvasgraph.DramaAsset{Type: a.Type, Name: a.Name, Look: a.Look, Aliases: a.Aliases, Episodes: a.Episodes}
	}
	merged, err := canvasgraph.MergeDrama(g, graphAssets, canvasgraph.Options{})
	if err != nil {
		return errcode.ErrDramaOutputInvalid
	}
	if attempt.created, err = createDramaCharacters(ctx, tx, sc, merged, assets); err != nil {
		return err
	}
	// 绑定角色 id 后再计算差异，patch 必须和提交的 payload 完全一致。
	attempt.changes = canvasgraph.Diff(g, merged.Graph)
	if attempt.revision, err = commitCanvas(ctx, tx, sc.userID, sc.canvasID, revision, merged.Graph, attempt.changes); err != nil {
		return err
	}
	if err := tx.LockEpisodes(ctx, sc.userID, sc.dramaID, sc.episodes); err != nil {
		return err
	}
	marker, err := json.Marshal(canvasCheckpoint{CanvasCommitted: true, NewAssets: attempt.created})
	if err != nil {
		return err
	}
	return tx.MarkCanvasCommitted(ctx, sc.userID, sc.jobID, marker)
}

// commitCanvas 有差异时 CAS 写回画布并返回新 revision；无差异保持原 revision。
func commitCanvas(ctx context.Context, tx repository.DramaCanvasTx, userID, canvasID, revision uint64, g *canvasgraph.Graph, changes []canvasgraph.Change) (uint64, error) {
	if len(changes) == 0 {
		return revision, nil
	}
	payload, err := g.Marshal()
	if err != nil {
		return revision, err
	}
	if err := tx.UpdateCanvas(ctx, userID, canvasID, revision, payload); err != nil {
		return revision, err
	}
	return revision + 1, nil
}

// checkEpisodesLockable 要求所选集全部存在、已有剧本且未锁定。
func checkEpisodesLockable(ctx context.Context, tx repository.DramaCanvasTx, sc dramaScope) error {
	rows, err := tx.EpisodesForUpdate(ctx, sc.userID, sc.dramaID, sc.canvasID, sc.episodes)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrDramaNotFound
	}
	if err != nil {
		return err
	}
	if len(rows) != len(sc.selected) {
		return errcode.ErrDramaStepNotReady
	}
	for _, ep := range rows {
		if !sc.selected[ep.No] || ep.LockedAt != nil || ep.ScriptStatus != "ready" {
			return errcode.ErrDramaStepNotReady
		}
	}
	return nil
}

// createDramaCharacters 为本次新增的角色节点建无主图角色卡并回填 characterId。
func createDramaCharacters(ctx context.Context, tx repository.DramaCanvasTx, sc dramaScope, merged *canvasgraph.DramaResult, assets []drama.Asset) ([]DramaCanvasAsset, error) {
	created := []DramaCanvasAsset{}
	for _, c := range merged.Created {
		asset := assets[c.AssetIndex]
		if asset.Type == "character" {
			dramaID := sc.dramaID
			card := &model.CharacterCard{UserID: sc.userID, DramaID: &dramaID, Name: strings.TrimSpace(asset.Name),
				LookName: strings.TrimSpace(asset.Look), Description: asset.Description, VoicePrompt: asset.VoicePrompt,
				AvatarAssetID: nil, ExtraAssetIDs: []uint64{},
			}
			if err := tx.CreateCharacter(ctx, card); err != nil {
				return nil, err
			}
			merged.Graph.Node(c.NodeID).Data()["characterId"] = idcodec.Encode(card.ID)
		}
		created = append(created, DramaCanvasAsset{NodeID: c.NodeID, Asset: asset})
	}
	return created, nil
}

// ApplyPrompts 为本次新增资产写提示词，保留用户其他参数；CAS 冲突时在最新画布上重新应用。
func (s *DramaCanvasService) ApplyPrompts(ctx context.Context, userID, canvasID, jobID uint64, prompts []drama.AssetPrompt) error {
	// 1. 只接受资源层校验通过的节点提示词，不把非法模型输出透传成画布编辑。
	if err := (drama.AssetPromptsOutput{Prompts: prompts}).Validate(); err != nil {
		return errcode.ErrDramaOutputInvalid
	}
	byID := make(map[string]string, len(prompts))
	for _, prompt := range prompts {
		byID[prompt.NodeID] = prompt.Prompt
	}
	// 2. 用同一公共入口计算差异并事务 CAS，前端同时编辑的字段不会被旧快照覆盖。
	out, err := WriteCanvas(ctx, s.repo, userID, canvasID, func(g *canvasgraph.Graph, revision uint64) (breakdownAttempt, error) {
		out := breakdownAttempt{before: revision, revision: revision}
		err := s.repo.WithTx(ctx, func(tx repository.DramaCanvasTx) error {
			return promptsTx(ctx, tx, g, revision, &out, userID, canvasID, jobID, byID)
		})
		return out, err
	})
	if errors.Is(err, repository.ErrRevisionConflict) {
		return errcode.ErrCanvasConflict
	}
	if err != nil {
		return err
	}
	// 3. 提示词写入成功后发布同格式 patch，不依赖任何 Agent 运行或改动日志。
	PublishCanvasPatch(ctx, s.bc, userID, CanvasPatch{CanvasID: canvasID, Kind: "asset_prompts", RevisionBefore: out.before, RevisionAfter: out.revision, Changes: out.changes})
	return nil
}

// promptsTx 在单个事务里校验任务、写入提示词并标记画布已提交。
func promptsTx(ctx context.Context, tx repository.DramaCanvasTx, g *canvasgraph.Graph, revision uint64, out *breakdownAttempt, userID, canvasID, jobID uint64, byID map[string]string) error {
	_, checkpoint, err := lockedJobCheckpoint(ctx, tx, userID, canvasID, jobID, "asset_prompts")
	if err != nil {
		return err
	}
	if checkpoint.CanvasCommitted {
		return nil
	}
	if err := tx.CheckCanvasRevision(ctx, userID, canvasID, revision); err != nil {
		return err
	}
	ed, err := canvasgraph.ApplyDramaPrompts(g, byID)
	if err != nil {
		return errcode.ErrDramaOutputInvalid
	}
	out.changes = ed.Changes
	if out.revision, err = commitCanvas(ctx, tx, userID, canvasID, revision, ed.Graph, ed.Changes); err != nil {
		return err
	}
	return tx.MarkCanvasCommitted(ctx, userID, jobID, []byte(`{"canvas_committed":true}`))
}

// canvasCheckpoint 与流水线共用持久提交标记；result/cost 由核心写入，仓储只合并这里的字段。
type canvasCheckpoint struct {
	CanvasCommitted bool               `json:"canvas_committed"`
	NewAssets       []DramaCanvasAsset `json:"new_assets"`
}

// readCanvasCheckpoint 读取崩溃恢复标记，已提交的画布不能因重放再次建卡或锁集。
func readCanvasCheckpoint(raw []byte) (canvasCheckpoint, error) {
	var checkpoint canvasCheckpoint
	if len(raw) == 0 {
		return checkpoint, nil
	}
	err := json.Unmarshal(raw, &checkpoint)
	return checkpoint, err
}
