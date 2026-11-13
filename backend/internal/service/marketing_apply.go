package service

import (
	"context"
	"encoding/json"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// scriptFlags 把软问题转成落库和视图用的形状（shot 是从 0 起的镜头下标，不省略 0）。
func scriptFlags(flags []marketing.Flag) []model.MarketingFlagView {
	out := make([]model.MarketingFlagView, 0, len(flags))
	for _, f := range flags {
		out = append(out, model.MarketingFlagView{Shot: f.Shot, Type: f.Type, Text: f.Text})
	}
	return out
}

// scriptRow 把一份脚本转成落库行：原稿、分镜、软问题都序列化成 JSON。
func scriptRow(p *model.MarketingProject, roundID, runID uint64, kind string, seq int, sc marketing.Script, flags []marketing.Flag, followMap []marketing.FollowMapItem) (model.MarketingScript, error) {
	shots, err := json.Marshal(sc.Shots)
	if err != nil {
		return model.MarketingScript{}, err
	}
	original, err := json.Marshal(sc)
	if err != nil {
		return model.MarketingScript{}, err
	}
	fl, err := json.Marshal(scriptFlags(flags))
	if err != nil {
		return model.MarketingScript{}, err
	}
	row := model.MarketingScript{UserID: p.UserID, ProjectID: p.ID, RoundID: roundID, StepRunID: runID, Kind: kind, Angle: sc.Angle, Seq: seq,
		Title: sc.Title, Hook: sc.Hook, Shots: shots, Music: sc.Music, Voice: sc.Voice, Flags: fl, Original: original,
		Status: model.MarketingScriptDraft, TaskIDs: []byte("[]"), Revision: 1}
	if followMap != nil {
		if row.FollowMap, err = json.Marshal(followMap); err != nil {
			return row, err
		}
	}
	return row, nil
}

// applyFollow 返回落库动作：写跟拍脚本。轮次里已有跟拍脚本（重写）就原地更新，保留出片历史和 id，否则新建；
// 同时记下跟拍据以写成的拆解，用来判断拆解是否被改过。
func (s *MarketingService) applyFollow(fo marketing.FollowOutput, p *model.MarketingProject, st model.MarketingSettings, out *stepOutcome) func(context.Context, repository.MarketingTx, *model.MarketingRound) error {
	return func(ctx context.Context, tx repository.MarketingTx, cur *model.MarketingRound) error {
		sc := marketing.FollowScript(fo)
		var rev *marketing.Reverse
		if len(out.basis) > 0 {
			r, err := reverseOf(out.basis)
			if err != nil {
				return err
			}
			rev = &r
		}
		flags := marketing.ScanScript(sc.Shots, st.Platform, factsOf(p), rev)
		row, err := scriptRow(p, cur.ID, out.runID, marketing.KindFollow, 0, sc, flags, fo.FollowMap)
		if err != nil {
			return err
		}
		existing, err := tx.RoundScripts(ctx, cur.ID)
		if err != nil {
			return err
		}
		for i := range existing {
			if existing[i].Kind != marketing.KindFollow {
				continue
			}
			fields := map[string]any{"step_run_id": out.runID, "title": row.Title, "hook": row.Hook, "shots": row.Shots, "music": row.Music, "voice": row.Voice,
				"flags": row.Flags, "follow_map": row.FollowMap, "original": row.Original, "status": model.MarketingScriptDraft, "error": "",
				"revision": repository.Add("revision", 1)}
			if err := tx.UpdateScript(ctx, existing[i].ID, fields); err != nil {
				return err
			}
			return tx.UpdateRound(ctx, cur.ID, map[string]any{"follow_basis": []byte(out.basis)})
		}
		if err := tx.InsertScripts(ctx, []model.MarketingScript{row}); err != nil {
			return err
		}
		return tx.UpdateRound(ctx, cur.ID, map[string]any{"follow_basis": []byte(out.basis)})
	}
}

// applyCreate 返回落库动作：写创意脚本，每份各自扫一遍软问题（创意脚本没有拆解，只扫极限词）。
func (s *MarketingService) applyCreate(co marketing.CreateOutput, p *model.MarketingProject, st model.MarketingSettings, out *stepOutcome) func(context.Context, repository.MarketingTx, *model.MarketingRound) error {
	return func(ctx context.Context, tx repository.MarketingTx, cur *model.MarketingRound) error {
		facts := factsOf(p)
		rows := make([]model.MarketingScript, 0, len(co.Scripts))
		for i, sc := range co.Scripts {
			row, err := scriptRow(p, cur.ID, out.runID, marketing.KindCreate, i+1, sc, marketing.ScanScript(sc.Shots, st.Platform, facts, nil), nil)
			if err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return tx.InsertScripts(ctx, rows)
	}
}
