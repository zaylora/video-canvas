package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// dramaStoryboardInput 持久化视频模型的时长与参考音频上限，任务执行不能依赖后续请求配置。
type dramaStoryboardInput struct {
	MaxSeconds int `json:"max_seconds"`
	MaxAudios  int `json:"max_audios"`
}

// dramaStoryboardAsset 仅暴露分镜所需的画布资产信息，不发送其他集资产或图片数据。
type dramaStoryboardAsset struct {
	NodeID        string `json:"node_id"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	Look          string `json:"look"`
	PromptSummary string `json:"prompt_summary"`
	// HasVoice 角色配了声音，片场提交视频时要占一个参考音频
	HasVoice bool `json:"has_voice"`
}

// checkStoryboardRequest 单集拆分必须显式选择范围；缺省时长遵循契约15秒。
func checkStoryboardRequest(req *model.DramaJobReq) error {
	if req.Step != "storyboard" {
		return nil
	}
	if len(req.Episodes) != 1 {
		return errcode.ErrInvalidParams.WithMsg("分镜须且只能选择一集")
	}
	if req.MaxSeconds != 0 && (req.MaxSeconds < 4 || req.MaxSeconds > 60) {
		return errcode.ErrInvalidParams.WithMsg("片段时长上限须为4至60秒")
	}
	return nil
}

// checkStoryboardTakes 与片段生成共用project行锁，阻止提交与最终替换覆盖并发产生的take。
func checkStoryboardTakes(ctx context.Context, tx repository.DramaTx, userID uint64, selected []model.DramaEpisode) error {
	for _, ep := range selected {
		has, err := tx.HasSegmentTakes(ctx, userID, ep.ID)
		if err != nil {
			return err
		}
		if has {
			return errcode.ErrDramaStoryboardHasTakes
		}
	}
	return nil
}

// estimateStoryboard 在项目锁下使用与提交一致的前置与take校验，不冻结积分。
func (s *DramaService) estimateStoryboard(ctx context.Context, userID, id uint64, req *model.DramaJobReq) (int, error) {
	var project *model.DramaProject
	var selected []model.DramaEpisode
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		var err error
		project, err = tx.Project(ctx, userID, id, true)
		if err != nil {
			return err
		}
		eps, err := tx.Episodes(ctx, userID, id)
		if err != nil {
			return err
		}
		selected, err = selectDramaEpisodes(project, eps, req)
		if err != nil {
			return err
		}
		return checkStoryboardTakes(ctx, tx, userID, selected)
	})
	if err != nil {
		return 0, dramaError(err)
	}
	// 与提交共用上限解析，视频模型读取失败或不可用时两边同样报错
	if _, _, err = s.storyboardLimits(ctx, project, req); err != nil {
		return 0, err
	}
	snap, err := s.resolveModel(ctx, project.TextModel)
	if err != nil {
		return 0, err
	}
	return dramaQuote(project, req.Step, selected, snap), nil
}

// enqueueStoryboard 保存不可变输入并标记generating，随冻结和队列插入一起回滚。
func enqueueStoryboard(ctx context.Context, tx repository.DramaTx, j *model.DramaJob, req *model.DramaJobReq, eps []model.DramaEpisode) error {
	// 提交前已由 storyboardLimits 解析；0 只在直接调用时出现，按契约缺省
	limit := req.MaxSeconds
	if limit == 0 {
		limit = storyboardDefaultSeconds
	}
	input, err := json.Marshal(dramaStoryboardInput{MaxSeconds: limit, MaxAudios: req.MaxAudios})
	if err != nil {
		return err
	}
	j.InputJSON = input
	for _, ep := range eps {
		ep.StoryboardStatus = "generating"
		if err = tx.SaveEpisode(ctx, &ep, ep.Revision); err != nil {
			return err
		}
	}
	return nil
}

// runStoryboard 读取本集可用资产后复用一期JSON重试，仅在全部输出合法时准备替换。
func (s *DramaService) runStoryboard(ctx context.Context, run *dramaRun, vars map[string]any, selected []model.DramaEpisode) (*dramaResult, error) {
	if len(selected) != 1 {
		return nil, errcode.ErrDramaStepNotReady
	}
	ep := selected[0]
	if err := checkDramaEpisodeStep(ep, "storyboard"); err != nil {
		return nil, err
	}
	var input dramaStoryboardInput
	if err := json.Unmarshal(run.job.InputJSON, &input); err != nil {
		return nil, err
	}
	if input.MaxSeconds < 4 || input.MaxSeconds > 60 {
		return nil, errcode.ErrDramaStepNotReady
	}
	assets, err := s.storyboardAssets(ctx, run.project, ep.No)
	if err != nil {
		return nil, err
	}
	vars["script_md"], vars["episode_no"], vars["assets"], vars["max_seconds"], vars["max_audios"] = ep.ScriptMD, ep.No, assets, input.MaxSeconds, input.MaxAudios
	// 创作段：写法都在这一段（内置正文是即梦 / Seedance 分栏，用户可以整段换掉），输出纯文本分镜稿，不解析结构。
	draft, draftCost, err := s.callDrama(ctx, run.job, &run.snapshot, "storyboard", "", vars, nil)
	if err != nil {
		return nil, err
	}
	// 格式段：系统固定，只拿创作稿与资产清单，负责切段、时长、资产引用和 JSON，不改写法。校验失败只重试这一段，不重跑创作段。
	formatVars := map[string]any{"draft_md": draft, "episode_no": ep.No, "assets": assets, "max_seconds": input.MaxSeconds, "max_audios": input.MaxAudios}
	voiced := map[string]bool{}
	for _, asset := range assets {
		voiced[asset.NodeID] = asset.HasVoice
	}
	output, formatCost, err := s.callDrama(ctx, run.job, &run.snapshot, "storyboard_format", "", formatVars, func(value any) error {
		out := value.(drama.StoryboardOutput)
		if err := out.ValidateMaxSeconds(input.MaxSeconds); err != nil {
			return err
		}
		return out.ValidateVoices(input.MaxAudios, voiced)
	})
	if err != nil {
		return nil, err
	}
	result := output.(drama.StoryboardOutput)
	ids := make(map[string]bool, len(assets))
	for _, asset := range assets {
		ids[asset.NodeID] = true
	}
	result.SanitizeReferences(ids)
	return &dramaResult{output: result, cost: draftCost + formatCost}, nil
}

// storyboardAssets 过滤本集或全集节点，并截取提示词摘要以避免把完整画布送进文本模型。
func (s *DramaService) storyboardAssets(ctx context.Context, p *model.DramaProject, no int) ([]dramaStoryboardAsset, error) {
	if s.canvasAssets == nil {
		return nil, errcode.ErrInternal
	}
	canvas, err := s.canvasAssets.GetByID(ctx, p.UserID, p.CanvasID)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Nodes []struct {
			ID   string `json:"id"`
			Data struct {
				Label string `json:"label"`
				Drama *struct {
					Type     string `json:"type"`
					Name     string `json:"name"`
					Look     string `json:"look"`
					Episodes []int  `json:"episodes"`
				} `json:"drama"`
				Params struct {
					Prompt string `json:"prompt"`
				} `json:"params"`
				Character *struct {
					Name  string `json:"name"`
					Voice *struct {
						Mode    string `json:"mode"`
						AssetID string `json:"assetId"`
					} `json:"voice"`
				} `json:"character"`
			} `json:"data"`
		} `json:"nodes"`
	}
	if err = json.Unmarshal(canvas.PayloadJSON, &payload); err != nil {
		return nil, err
	}
	assets := make([]dramaStoryboardAsset, 0, len(payload.Nodes))
	for _, node := range payload.Nodes {
		d := node.Data.Drama
		// Episodes 为 nil（JSON null）是未分类：所在的集被删了还没重新归类，不属于任何一集；[] 才是全集
		if node.ID == "" || d == nil || d.Episodes == nil || !slices.Contains([]string{"character", "scene", "prop"}, d.Type) || len(d.Episodes) > 0 && !slices.Contains(d.Episodes, no) {
			continue
		}
		// 旧画布的角色资产把名字放在 label/character.name，drama.name 为空；和前端取资产同一口径回退，
		// 否则模型拿到空名字，分镜里只能写纯文本，视频生成时无法绑定参考图。
		name := strings.TrimSpace(d.Name)
		if name == "" {
			name = strings.TrimSpace(node.Data.Label)
		}
		if name == "" && node.Data.Character != nil {
			name = strings.TrimSpace(node.Data.Character.Name)
		}
		if name == "" {
			continue
		}
		summary := []rune(strings.TrimSpace(node.Data.Params.Prompt))
		if len(summary) > 500 {
			summary = summary[:500]
		}
		// 和片场提交视频同一口径：角色卡绑了音频声音就会带上参考音频
		voice := node.Data.Character != nil && node.Data.Character.Voice != nil && node.Data.Character.Voice.Mode == "audio" && node.Data.Character.Voice.AssetID != ""
		assets = append(assets, dramaStoryboardAsset{NodeID: node.ID, Type: d.Type, Name: name, Look: strings.TrimSpace(d.Look), PromptSummary: string(summary), HasVoice: d.Type == "character" && voice})
	}
	return assets, nil
}

// saveStoryboardOutput 在project锁内再次校验take，再原子替换全部片段和集状态。
func saveStoryboardOutput(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, j *model.DramaJob, output drama.StoryboardOutput) error {
	eps, err := tx.Episodes(ctx, p.UserID, p.ID)
	if err != nil {
		return err
	}
	selected := selectedEpisodes(eps, j.Episodes)
	if len(selected) != 1 {
		return errcode.ErrDramaStepNotReady
	}
	ep := selected[0]
	if err = checkDramaEpisodeStep(ep, "storyboard"); err != nil {
		return err
	}
	if err = checkStoryboardTakes(ctx, tx, p.UserID, selected); err != nil {
		return err
	}
	segments := make([]model.DramaSegment, 0, len(output.Segments))
	for seq, segment := range output.Segments {
		segments = append(segments, model.DramaSegment{UserID: p.UserID, EpisodeID: ep.ID, Seq: seq, Seconds: segment.Seconds, Prompt: segment.Prompt, Revision: 1})
	}
	if err = tx.ReplaceSegments(ctx, p.UserID, ep.ID, segments); err != nil {
		return err
	}
	ep.StoryboardStatus = "ready"
	return tx.SaveEpisode(ctx, &ep, ep.Revision)
}

// finishStoryboardStatus 失败保留旧片段；取消恢复已有ready或empty，避免集卡片永久显示generating。
func finishStoryboardStatus(ctx context.Context, tx repository.DramaTx, j *model.DramaJob, status string) error {
	if j.Step != "storyboard" || status == "succeeded" {
		return nil
	}
	eps, err := tx.Episodes(ctx, j.UserID, j.DramaID)
	if err != nil {
		return err
	}
	for _, ep := range selectedEpisodes(eps, j.Episodes) {
		ep.StoryboardStatus = "failed"
		if status == "canceled" {
			segments, err := tx.Segments(ctx, j.UserID, ep.ID)
			if err != nil {
				return err
			}
			ep.StoryboardStatus = "empty"
			if len(segments) > 0 {
				ep.StoryboardStatus = "ready"
			}
		}
		if err = tx.SaveEpisode(ctx, &ep, ep.Revision); err != nil {
			return fmt.Errorf("更新分镜状态失败: %w", err)
		}
	}
	return nil
}

// syncBoardNodes 分镜成功落库后，在画布上把本集每个片段的故事板节点建好（空节点，进画布就能看到），
// 重新生成分镜时片段换了新的，旧片段的节点清掉。画布和片段是两次写入，这一步失败只记日志，
// 不影响分镜成功：之后点「生成」或进画布时会补建。
func (s *DramaService) syncBoardNodes(ctx context.Context, job *model.DramaJob) {
	if s.canvas == nil || len(job.Episodes) != 1 {
		return
	}
	no := job.Episodes[0]
	if err := s.syncEpisodeBoards(ctx, job.UserID, job.DramaID, no); err != nil {
		logger.Ctx(ctx).Warn("同步故事板节点失败，之后生成时会补建", zap.Uint64("drama_id", job.DramaID), zap.Int("episode", no), zap.Error(err))
	}
}

// syncEpisodeBoards 读本集的片段，交给画布服务对齐故事板节点。
func (s *DramaService) syncEpisodeBoards(ctx context.Context, userID, dramaID uint64, no int) error {
	p, err := s.repo.Project(ctx, userID, dramaID, false)
	if err != nil {
		return err
	}
	eps, err := s.repo.Episodes(ctx, userID, dramaID)
	if err != nil {
		return err
	}
	var ep *model.DramaEpisode
	for i := range eps {
		if eps[i].No == no {
			ep = &eps[i]
		}
	}
	if ep == nil {
		return nil
	}
	// 片段只能在事务接口里读；这里只读，事务马上结束，不和后面的画布写入嵌套
	var rows []model.DramaSegment
	if err = s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		var e error
		rows, e = tx.Segments(ctx, userID, ep.ID)
		return e
	}); err != nil {
		return err
	}
	segments := make([]DramaBoardSegment, 0, len(rows))
	for _, row := range rows {
		shots := drama.BoardShotCount(row.Prompt)
		segments = append(segments, DramaBoardSegment{
			ID: idcodec.Encode(row.ID), Seq: row.Seq, NewID: uuid.NewString(),
			Label:      fmt.Sprintf("故事板 · 第 %d 集 · %02d", no, row.Seq+1),
			SourceHash: drama.SourceHash(row.Prompt), Layout: drama.BoardLayout(shots), ShotCount: shots,
		})
	}
	return s.canvas.SyncBoards(ctx, userID, p.CanvasID, no, segments)
}
