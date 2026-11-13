package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"video-canvas/internal/marketing"
	"video-canvas/internal/marketing/format"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// scriptCtx 是处理一份脚本需要的上下文：脚本、所属轮次、项目和轮次设置。
type scriptCtx struct {
	sc      *model.MarketingScript
	round   *model.MarketingRound
	project *model.MarketingProject
	st      model.MarketingSettings
}

// loadScript 读取脚本及其上下文（按用户隔离），不存在返回 68001。
func (s *MarketingService) loadScript(ctx context.Context, userID, id uint64) (*scriptCtx, error) {
	sc, err := s.repo.Script(ctx, userID, id, false)
	if mktNotFound(err) {
		return nil, errcode.ErrMarketingNotFound
	}
	if err != nil {
		return nil, err
	}
	r, err := s.repo.RoundByID(ctx, sc.RoundID, false)
	if err != nil {
		return nil, err
	}
	p, err := s.repo.Project(ctx, userID, sc.ProjectID, false)
	if mktNotFound(err) {
		return nil, errcode.ErrMarketingNotFound
	}
	if err != nil {
		return nil, err
	}
	st, err := settingsOf(r)
	if err != nil {
		return nil, err
	}
	return &scriptCtx{sc: sc, round: r, project: p, st: st}, nil
}

// scanReverse 取脚本扫描品牌和背书用的拆解：只有跟拍脚本扫，用它据以写成的拆解（没有就用当前拆解）。
func scanReverse(sc *model.MarketingScript, r *model.MarketingRound) *marketing.Reverse {
	if sc.Kind != marketing.KindFollow {
		return nil
	}
	raw := r.FollowBasis
	if len(raw) == 0 {
		raw = r.Reverse
	}
	if len(raw) == 0 {
		return nil
	}
	rev, err := reverseOf(raw)
	if err != nil {
		return nil
	}
	return &rev
}

// UpdateScript 保存用户改的旁白（长度必须等于镜头数），服务端重扫软问题。
// 脚本在出片或重写 68006；revision 不一致 68009。
func (s *MarketingService) UpdateScript(ctx context.Context, userID, id uint64, req *model.UpdateMarketingScriptReq) (*model.MarketingScriptView, error) {
	err := s.editNarrations(ctx, userID, id, &req.Revision, func([]marketing.ScriptShot, *model.MarketingScript) ([]string, error) {
		return req.Narrations, nil
	})
	if err != nil {
		return nil, err
	}
	return s.afterScriptEdit(ctx, userID, id)
}

// RestoreScript 把旁白恢复成模型原稿，并重扫软问题。
func (s *MarketingService) RestoreScript(ctx context.Context, userID, id uint64) (*model.MarketingScriptView, error) {
	err := s.editNarrations(ctx, userID, id, nil, func(shots []marketing.ScriptShot, sc *model.MarketingScript) ([]string, error) {
		var orig marketing.Script
		if err := json.Unmarshal(sc.Original, &orig); err != nil {
			return nil, err
		}
		if len(orig.Shots) != len(shots) {
			return nil, errcode.ErrInvalidParams.WithMsg("原稿镜头数不一致")
		}
		out := make([]string, len(orig.Shots))
		for i, sh := range orig.Shots {
			out[i] = sh.Narration
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return s.afterScriptEdit(ctx, userID, id)
}

// afterScriptEdit 返回编辑后的脚本视图并推送 marketing.script。
func (s *MarketingService) afterScriptEdit(ctx context.Context, userID, id uint64) (*model.MarketingScriptView, error) {
	sc, err := s.repo.Script(ctx, userID, id, false)
	if err != nil {
		return nil, err
	}
	v, err := s.scriptViewByRow(ctx, userID, sc)
	if err != nil {
		return nil, err
	}
	s.publishScript(ctx, userID, sc, v)
	return v, nil
}

// editNarrations 在一个事务里改脚本的旁白：锁脚本、检查没有在出片或重写、按 revision 乐观锁写回，并重扫软问题。
// pick 按当前镜头和脚本行给出新的旁白列表；revision 为 nil 时用当前版本（恢复原稿）。
func (s *MarketingService) editNarrations(ctx context.Context, userID, id uint64, revision *int64,
	pick func([]marketing.ScriptShot, *model.MarketingScript) ([]string, error)) error {
	return s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		sc, err := tx.Script(ctx, userID, id, true)
		if mktNotFound(err) {
			return errcode.ErrMarketingNotFound
		}
		if err != nil {
			return err
		}
		rev := sc.Revision
		if revision != nil {
			rev = *revision
		}
		if rev != sc.Revision {
			return errcode.ErrMarketingConflict
		}
		if err := s.scriptIdle(ctx, tx, sc); err != nil {
			return err
		}
		var shots []marketing.ScriptShot
		if err := json.Unmarshal(sc.Shots, &shots); err != nil {
			return err
		}
		narrations, err := pick(shots, sc)
		if err != nil {
			return err
		}
		if len(narrations) != len(shots) {
			return errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("旁白数量要和镜头数一致（%d 个）", len(shots)))
		}
		return s.saveNarrations(ctx, tx, sc, shots, narrations, rev)
	})
}

// saveNarrations 把新旁白写进镜头，按本轮设置重扫软问题，按 revision 乐观锁写回。
func (s *MarketingService) saveNarrations(ctx context.Context, tx repository.MarketingTx, sc *model.MarketingScript, shots []marketing.ScriptShot, narrations []string, rev int64) error {
	scenes := make([]string, len(shots))
	for i := range shots {
		shots[i].Narration = strings.TrimSpace(narrations[i])
		scenes[i] = shots[i].Scene
	}
	round, err := tx.RoundByID(ctx, sc.RoundID, false)
	if err != nil {
		return err
	}
	st, err := settingsOf(round)
	if err != nil {
		return err
	}
	p, err := tx.ProjectByID(ctx, sc.ProjectID)
	if err != nil {
		return err
	}
	flags := marketing.ScanNarrations(narrationsOf(shots), scenes, st.Platform, factsOf(p), scanReverse(sc, round))
	shotsJSON, err := json.Marshal(shots)
	if err != nil {
		return err
	}
	flagsJSON, err := json.Marshal(scriptFlags(flags))
	if err != nil {
		return err
	}
	err = tx.SaveScript(ctx, sc.UserID, sc.ID, rev, map[string]any{"shots": shotsJSON, "flags": flagsJSON})
	if errors.Is(err, repository.ErrRevisionConflict) {
		return errcode.ErrMarketingConflict
	}
	return err
}

// narrationsOf 取每个镜头的旁白。
func narrationsOf(shots []marketing.ScriptShot) []string {
	out := make([]string, len(shots))
	for i, sh := range shots {
		out[i] = sh.Narration
	}
	return out
}

// scriptOf 把脚本行转成业务脚本结构（旁白是用户改过的当前值）。
func scriptOf(sc *model.MarketingScript) (marketing.Script, error) {
	out := marketing.Script{Angle: sc.Angle, Title: sc.Title, Hook: sc.Hook, Music: sc.Music, Voice: sc.Voice}
	if err := json.Unmarshal(sc.Shots, &out.Shots); err != nil {
		return out, err
	}
	return out, nil
}

// formatInput 组装格式化器的输入。
func (s *MarketingService) formatInput(c *scriptCtx, st model.MarketingSettings, caps modelcfg.Capabilities) (format.Input, error) {
	script, err := scriptOf(c.sc)
	if err != nil {
		return format.Input{}, err
	}
	brief := briefOf(st)
	in := format.Input{Facts: factsOf(c.project), Script: script, Platform: marketing.PlatformOf(st.Platform), Language: st.Language,
		AspectRatio: brief.AspectRatio, Duration: brief.Duration, ProductImages: productCount(c.project), HasModel: c.round.ModelAssetID != nil,
		HasRefVideo: c.round.RefVideoAssetID != nil, MaxLen: caps.Prompt.MaxLength}
	raw := c.round.FollowBasis
	if len(raw) == 0 || c.sc.Kind != marketing.KindFollow {
		raw = c.round.Reverse
	}
	if len(raw) > 0 {
		if rev, err := reverseOf(raw); err == nil {
			in.Reverse = &rev
		}
	}
	return in, nil
}

// videoSnapshot 取视频模型快照：不存在或不是视频模型返回 68004。
func (s *MarketingService) videoSnapshot(ctx context.Context, key string) (*provider.Snapshot, error) {
	snap, err := s.models.Snapshot(ctx, key)
	if errors.Is(err, provider.ErrModelUnavailable) {
		return nil, errcode.ErrMarketingVideoModel
	}
	if err != nil {
		return nil, err
	}
	if snap.Model.Kind != modelcfg.KindVideo {
		return nil, errcode.ErrMarketingVideoModel
	}
	return snap, nil
}

// Prompt 返回脚本按某个视频模型格式化出的提示词预览（默认用本轮的视频模型）。
// 视频模型不可用 68004；提示词超出模型上限 68008。
func (s *MarketingService) Prompt(ctx context.Context, userID, id uint64, videoModel string) (*model.MarketingPrompt, error) {
	c, err := s.loadScript(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	snap, err := s.promptVideo(ctx, c, videoModel)
	if err != nil {
		return nil, err
	}
	if marketing.NeedsAdapt(snap.Model.Capabilities.PromptFormat) {
		return s.adaptPromptView(ctx, c, snap)
	}
	f := format.For(snap.Model.Capabilities.PromptFormat)
	in, err := s.formatInput(c, c.st, snap.Model.Capabilities)
	if err != nil {
		return nil, err
	}
	prompt, err := f.Render(in)
	if errors.Is(err, format.ErrTooLong) {
		return nil, errcode.ErrMarketingPromptLong
	}
	if err != nil {
		return nil, err
	}
	kind := "generic"
	if f.Key() == "seedance" {
		kind = "seedance"
	}
	return &model.MarketingPrompt{Format: kind, Prompt: prompt}, nil
}
