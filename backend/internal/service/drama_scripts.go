package service

import (
	"context"
	"strings"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// runScripts 每集串行请求并立即保存，后续失败不丢弃已成功集。
func (s *DramaService) runScripts(ctx context.Context, p *model.DramaProject, j *model.DramaJob, snap *provider.Snapshot, all, selected []model.DramaEpisode) (any, int, error) {
	cost := 0
	outputs := []map[string]any{}
	for i, ep := range selected {
		vars := s.dramaVariables(p)
		vars["episode"] = episodeView(ep).DramaEpisodeSummary
		vars["episodes"] = dramaRequestInputs(all)
		previous := ""
		for _, candidate := range all {
			if candidate.No == ep.No-1 {
				previous = candidate.ScriptMD
			}
		}
		vars["previous_ending"] = dramaEnding(previous, 800)
		v, c, e := s.callDrama(ctx, j, snap, "scripts", vars, nil)
		if e != nil {
			if markErr := s.markScriptFailed(ctx, j, ep.No); markErr != nil {
				return nil, 0, markErr
			}
			return nil, 0, e
		}
		script := v.(drama.ScriptOutput)
		if e = s.saveDramaScript(ctx, j, ep.No, script.ScriptMD, "ready"); e != nil {
			return nil, 0, e
		}
		cost += c
		for k := range all {
			if all[k].No == ep.No {
				all[k].ScriptMD = script.ScriptMD
				all[k].ScriptStatus = "ready"
			}
		}
		outputs = append(outputs, map[string]any{"no": ep.No, "script_md": script.ScriptMD})
		if e = s.dramaProgress(ctx, j, (i+1)*90/len(selected)); e != nil {
			return nil, 0, e
		}
	}
	return outputs, cost, nil
}

// saveDramaScript 在项目与任务锁内写入当前集，取消或锁定后拒绝覆盖。
func (s *DramaService) saveDramaScript(ctx context.Context, j *model.DramaJob, no int, text, status string) error {
	return s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		if _, e := tx.Project(ctx, j.UserID, j.DramaID, true); e != nil {
			return e
		}
		active, e := tx.Job(ctx, j.UserID, j.DramaID, j.ID, true)
		if e != nil {
			return e
		}
		if !dramaActive(active.Status) {
			return errcode.ErrTaskNotCancelable
		}
		eps, e := tx.Episodes(ctx, j.UserID, j.DramaID)
		if e != nil {
			return e
		}
		for _, ep := range eps {
			if ep.No != no {
				continue
			}
			if ep.LockedAt != nil {
				return errcode.ErrDramaEpisodeLocked
			}
			revision := ep.Revision
			ep.ScriptStatus = status
			if status == "ready" {
				ep.ScriptMD = text
			}
			return tx.SaveEpisode(ctx, &ep, revision)
		}
		return errcode.ErrDramaNotFound
	})
}

// markScriptFailed 仅标记失败集，不覆盖该集已有正文。
func (s *DramaService) markScriptFailed(ctx context.Context, j *model.DramaJob, no int) error {
	if e := s.ensureDramaActive(ctx, j); e != nil {
		return nil
	}
	return s.saveDramaScript(ctx, j, no, "", "failed")
}

// dramaVariables 模板只接收原文摘要，完整原文仅在研读分块时单独传入。
func (s *DramaService) dramaVariables(p *model.DramaProject) map[string]any {
	style := p.StyleCustom
	for _, v := range drama.Styles().Styles {
		if v.Key == p.StyleKey {
			style = v.Prompt
		}
	}
	return map[string]any{"source": p.SourceSummary, "source_kind": p.SourceKind, "summary": p.SourceSummary, "characters_md": p.CharactersMD, "outline_md": p.OutlineMD, "genre": p.Genre, "style": style, "ratio": p.Ratio, "target_episodes": p.TargetEpisodes, "episode_seconds": p.EpisodeSeconds}
}

// dramaScriptInputs 用公开snake_case结构提供拆解剧本，隐藏数据库字段。
func dramaScriptInputs(eps []model.DramaEpisode) []model.DramaEpisodeView {
	out := make([]model.DramaEpisodeView, 0, len(eps))
	for _, ep := range eps {
		out = append(out, episodeView(ep))
	}
	return out
}

// dramaEnding 截取上一集结尾，按字符处理避免UTF8截断。
func dramaEnding(text string, n int) string {
	runes := []rune(text)
	if len(runes) > n {
		runes = runes[len(runes)-n:]
	}
	return strings.TrimSpace(string(runes))
}
