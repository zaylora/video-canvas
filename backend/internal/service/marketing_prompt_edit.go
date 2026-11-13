// 本文件是出片原稿的手改：原稿默认按脚本现拼，用户手改后按提示词格式存在脚本上，
// 出片（H3 是转写）改用手改稿。脚本再改只标过期，手改稿照用，直到用户还原。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"video-canvas/internal/marketing"
	"video-canvas/internal/marketing/format"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// sourcePrompt 是出片依据的原稿：没手改时是按脚本拼的；手改过时是手改稿，Stale 表示脚本在手改后又变了。
type sourcePrompt struct {
	Text   string
	Edited bool
	Stale  bool
}

// promptKey 是视频模型的原稿格式键（seedance / h3 / generic），手改稿按它存。
func promptKey(caps modelcfg.Capabilities) string { return format.For(caps.PromptFormat).Key() }

// assembleSource 按脚本拼原稿：H3 拼不限长的 Seedance 写法（转写的依据），其余按模型格式拼；超长返回 format.ErrTooLong。
func (s *MarketingService) assembleSource(c *scriptCtx, st model.MarketingSettings, caps modelcfg.Capabilities) (format.Input, string, error) {
	f, fcaps := format.For(caps.PromptFormat), caps
	if marketing.NeedsAdapt(caps.PromptFormat) {
		f, fcaps = format.For(format.KeySeedance), modelcfg.Capabilities{}
	}
	in, err := s.formatInput(c, st, fcaps)
	if err != nil {
		return format.Input{}, "", err
	}
	text, err := f.Render(in)
	return in, text, err
}

// resolveSource 返回出片依据的原稿：有手改稿用手改稿（拼出的原稿变了或拼超长只标过期）；没有时拼超长报 68008。
func (s *MarketingService) resolveSource(c *scriptCtx, st model.MarketingSettings, caps modelcfg.Capabilities) (format.Input, sourcePrompt, error) {
	in, assembled, err := s.assembleSource(c, st, caps)
	tooLong := errors.Is(err, format.ErrTooLong)
	if err != nil && !tooLong {
		return in, sourcePrompt{}, err
	}
	edits, perr := promptEditsOf(c.sc)
	if perr != nil {
		return in, sourcePrompt{}, perr
	}
	edit, ok := edits[promptKey(caps)]
	if !ok {
		if tooLong {
			return in, sourcePrompt{}, errcode.ErrMarketingPromptLong
		}
		return in, sourcePrompt{Text: assembled}, nil
	}
	return in, sourcePrompt{Text: edit.Prompt, Edited: true, Stale: tooLong || edit.BaseHash != marketing.SourceHash(assembled)}, nil
}

// promptEditsOf 解出脚本上的全部手改稿；空列当作没有，返回非 nil 的空表。
func promptEditsOf(sc *model.MarketingScript) (map[string]model.MarketingPromptEdit, error) {
	all := map[string]model.MarketingPromptEdit{}
	if len(sc.PromptEdits) == 0 {
		return all, nil
	}
	if err := json.Unmarshal(sc.PromptEdits, &all); err != nil {
		return nil, err
	}
	if all == nil {
		all = map[string]model.MarketingPromptEdit{}
	}
	return all, nil
}

// savePromptEdit 在锁住脚本的事务里存下（edit 为 nil 时删掉）某格式的手改稿。
func (s *MarketingService) savePromptEdit(ctx context.Context, userID, id uint64, key string, edit *model.MarketingPromptEdit) error {
	return s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		sc, err := tx.Script(ctx, userID, id, true)
		if mktNotFound(err) {
			return errcode.ErrMarketingNotFound
		}
		if err != nil {
			return err
		}
		all, err := promptEditsOf(sc)
		if err != nil {
			return err
		}
		if edit == nil {
			delete(all, key)
		} else {
			all[key] = *edit
		}
		raw, err := json.Marshal(all)
		if err != nil {
			return err
		}
		return tx.UpdateScript(ctx, id, map[string]any{"prompt_edits": raw})
	})
}

// editableVideo 取本轮的视频模型能力，并确认请求的格式就是它的原稿格式（模型切换后前端拿的是旧预览）。
func (s *MarketingService) editableVideo(ctx context.Context, c *scriptCtx, key string) (modelcfg.Capabilities, error) {
	video, err := s.promptVideo(ctx, c, "")
	if err != nil {
		return modelcfg.Capabilities{}, err
	}
	caps := video.Model.Capabilities
	if promptKey(caps) != key {
		return modelcfg.Capabilities{}, errcode.ErrInvalidParams.WithMsg("视频模型已切换，请刷新后再改提示词")
	}
	return caps, nil
}

// EditPrompt 手改一份脚本的原稿（本轮视频模型的格式），记下此刻拼出的原稿指纹用来判断之后是否过期。
// 脚本不存在 68001；格式和本轮视频模型不符或全是空白 10001。
func (s *MarketingService) EditPrompt(ctx context.Context, userID, id uint64, req *model.EditMarketingPromptReq) (*model.MarketingPrompt, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, errcode.ErrInvalidParams.WithMsg("提示词不能为空")
	}
	c, err := s.loadScript(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	caps, err := s.editableVideo(ctx, c, req.Format)
	if err != nil {
		return nil, err
	}
	_, assembled, err := s.assembleSource(c, c.st, caps)
	if err != nil && !errors.Is(err, format.ErrTooLong) {
		return nil, err
	}
	edit := &model.MarketingPromptEdit{Prompt: prompt, UpdatedAt: s.now()}
	if err == nil {
		edit.BaseHash = marketing.SourceHash(assembled)
	}
	if err := s.savePromptEdit(ctx, userID, id, req.Format, edit); err != nil {
		return nil, err
	}
	return s.Prompt(ctx, userID, id, "")
}

// RestorePrompt 删掉某格式的手改稿，回到按脚本拼出的原稿。脚本不存在 68001。
func (s *MarketingService) RestorePrompt(ctx context.Context, userID, id uint64, req *model.RestoreMarketingPromptReq) (*model.MarketingPrompt, error) {
	if err := s.savePromptEdit(ctx, userID, id, req.Format, nil); err != nil {
		return nil, err
	}
	return s.Prompt(ctx, userID, id, "")
}
