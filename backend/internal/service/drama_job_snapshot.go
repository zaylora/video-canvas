package service

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"unicode/utf8"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// dramaJobConfig 保持原模型快照顶层兼容，同时冻结本任务正文和实时名单。
type dramaJobConfig struct {
	provider.Snapshot
	Prompt    model.DramaPromptSetting `json:"prompt"`
	KnownCast []drama.CastEntry        `json:"known_cast"`
}

// jobConfig 解码任务快照，缺少新字段的旧任务使用内置正文。
func jobConfig(j *model.DramaJob) dramaJobConfig {
	var cfg dramaJobConfig
	_ = json.Unmarshal(j.ConfigSnapshot, &cfg)
	return cfg
}

// snapshotDramaJob 在项目锁内取画布名单，固定正文与批次完整原文。
func (s *DramaService) snapshotDramaJob(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, j *model.DramaJob, snap *provider.Snapshot) error {
	setting, ok := promptSettings(p)[dramaStepGroup(j.Step)]
	if !ok {
		setting = model.DramaPromptSetting{Source: "builtin", Label: "内置", Bodies: map[string]string{j.Step: drama.PromptBody(j.Step)}}
	}
	cfg := dramaJobConfig{Snapshot: *snap, Prompt: setting, KnownCast: []drama.CastEntry{}}
	if j.Step != "analyze" && p.UnitsTotal > 0 {
		cast, err := snapshotKnownCast(ctx, tx, p)
		if err != nil {
			return err
		}
		cfg.KnownCast = cast
	}
	if j.Step == "analyze" || j.Step == "plan" {
		if err := s.snapshotPipelineInput(ctx, tx, p, j, &cfg); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	j.ConfigSnapshot = raw
	return nil
}

// Skills 返回短剧可选技能，不配置技能库时返回空目录。
func (s *DramaService) Skills(ctx context.Context, step string) ([]model.DramaSkillView, error) {
	// 1. 只展示已启用且管理员标记适用的技能。
	if DramaPromptPages(step) == nil {
		return nil, errcode.ErrInvalidParams
	}
	if s.skills == nil {
		return []model.DramaSkillView{}, nil
	}
	return s.skills.DramaSkills(ctx, step)
}

// snapshotKnownCast 从实时画布取身份，最近三个规划批次决定场景与道具优先级。
func snapshotKnownCast(ctx context.Context, tx repository.DramaTx, p *model.DramaProject) ([]drama.CastEntry, error) {
	canvas, err := tx.Canvas(ctx, p.UserID, p.CanvasID)
	if err != nil {
		return nil, err
	}
	jobs, err := tx.RecentDramaBatches(ctx, p.UserID, p.ID)
	if err != nil {
		return nil, err
	}
	eps, err := tx.Episodes(ctx, p.UserID, p.ID)
	if err != nil {
		return nil, err
	}
	return drama.KnownCast(canvas.PayloadJSON, recentCastEpisode(jobs, eps))
}

// recentCastEpisode 以最近三批的首个原文单元映射最近集号，剧本型按最近三次拆解范围计算。
func recentCastEpisode(jobs []model.DramaJob, eps []model.DramaEpisode) int {
	slices.SortFunc(jobs, func(a, b model.DramaJob) int { return cmp.Compare(b.ID, a.ID) })
	unitFrom, episodeFrom, count := 0, 0, 0
	for _, j := range jobs {
		if j.Status != "succeeded" {
			continue
		}
		if j.Step == "plan" {
			unitFrom = j.BatchFrom
			count++
		} else if j.Step == "breakdown" && unitFrom == 0 {
			episodeFrom = earliestDramaEpisode(episodeFrom, j.Episodes)
			count++
		}
		if count == 3 {
			break
		}
	}
	if unitFrom > 0 {
		for _, ep := range eps {
			if ep.UnitTo >= unitFrom {
				return ep.No
			}
		}
	}
	return max(1, episodeFrom)
}

// snapshotPipelineInput 研读读取全书抽样，规划只读取游标后最多十二个单元。
func (s *DramaService) snapshotPipelineInput(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, j *model.DramaJob, cfg *dramaJobConfig) error {
	from, to := 1, 0
	if j.Step == "plan" {
		from = p.PlanCursor + 1
		to = min(p.RangeEnd, p.PlanCursor+12)
	}
	rows, err := tx.Units(ctx, p.UserID, p.ID, from, to)
	if err != nil {
		return err
	}
	vars := s.dramaVariables(p)
	if j.Step == "analyze" {
		vars["source"] = drama.Sample(sourceUnits(rows), dramaContextLimit(&cfg.Snapshot))
	} else {
		if err := s.planBatchVariables(ctx, tx, p, j, cfg, sourceUnits(rows), vars); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(vars)
	if err != nil {
		return err
	}
	j.InputJSON = raw
	return nil
}

// planBatchVariables 实际正文、名单与固定协议都计入上下文预算，批次原文绝不截断。
func (s *DramaService) planBatchVariables(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, j *model.DramaJob, cfg *dramaJobConfig, units []drama.Unit, vars map[string]any) error {
	eps, err := tx.Episodes(ctx, p.UserID, p.ID)
	if err != nil {
		return err
	}
	vars["next_episode_no"], vars["previous_cliffhanger"] = 1, ""
	for _, ep := range eps {
		vars["next_episode_no"] = ep.No + 1
		vars["previous_cliffhanger"] = ep.Cliffhanger
	}
	vars["known_cast"], vars["source"] = cfg.KnownCast, ""
	prompt, err := drama.PromptWithBody("plan", vars, cfg.Prompt.Bodies["plan"])
	if err != nil {
		return err
	}
	budget := dramaContextLimit(&cfg.Snapshot) - utf8.RuneCountInString(prompt)
	batch, err := drama.NextBatch(units, p.PlanCursor, p.RangeEnd, budget)
	if err != nil {
		return errcode.ErrDramaContext
	}
	if len(batch) == 0 {
		return errcode.ErrDramaPhase
	}
	j.BatchFrom, j.BatchTo = batch[0].No, batch[len(batch)-1].No
	vars["source"] = drama.UnitSource(batch)
	return nil
}

// earliestDramaEpisode 合并最近拆解范围的最早集号。
func earliestDramaEpisode(first int, scope []int) int {
	for _, no := range scope {
		if first == 0 || no < first {
			first = no
		}
	}
	return first
}
