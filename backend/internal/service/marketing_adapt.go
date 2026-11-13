// 本文件是出片前的提示词转写：H3 这类模型有自己的写法（<Picture N> 标签、六段式），
// 原稿（Seedance 写法，代码拼出）交给文本模型转写，输出不合格时带校验反馈重写一次。
// 转写稿按格式存在脚本上，原稿指纹变了就过期，出片时自动重新转写；转写稿可以手改。
// 扣费按文本模型实际用量，和转写稿写回在同一个事务里：转写失败不扣。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/marketing"
	"video-canvas/internal/marketing/format"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// adaptSource 返回脚本的转写输入：原稿是不删减的 Seedance 写法，上限取目标视频模型的提示词上限。
func (s *MarketingService) adaptSource(c *scriptCtx, st model.MarketingSettings, video modelcfg.Capabilities) (marketing.AdaptInput, error) {
	in, err := s.formatInput(c, st, modelcfg.Capabilities{})
	if err != nil {
		return marketing.AdaptInput{}, err
	}
	source, err := format.For(format.KeySeedance).Render(in)
	if err != nil {
		return marketing.AdaptInput{}, err
	}
	shots := make([]marketing.AdaptShot, len(in.Script.Shots))
	seconds := float64(in.Duration)
	for i, sh := range in.Script.Shots {
		shots[i] = marketing.AdaptShot{No: i + 1, Narration: strings.TrimSpace(sh.Narration)}
		if i > 0 {
			shots[i].Cut = marketing.CutTime(sh.Start)
		}
		if in.Duration <= 0 {
			seconds = sh.End
		}
	}
	return marketing.AdaptInput{Source: source, Seconds: seconds, ProductImages: in.ProductImages, HasModel: in.HasModel,
		HasRefVideo: in.HasRefVideo, Shots: shots, MaxLen: video.Prompt.MaxLength}, nil
}

// adaptationsOf 解出脚本上的全部转写稿；空列当作没有，返回非 nil 的空表。
func adaptationsOf(sc *model.MarketingScript) (map[string]model.MarketingAdaptation, error) {
	all := map[string]model.MarketingAdaptation{}
	if len(sc.Adaptations) == 0 {
		return all, nil
	}
	if err := json.Unmarshal(sc.Adaptations, &all); err != nil {
		return nil, err
	}
	if all == nil {
		all = map[string]model.MarketingAdaptation{}
	}
	return all, nil
}

// freshAdaptation 返回脚本在某个格式下、对当前原稿仍然有效的转写稿。
func freshAdaptation(sc *model.MarketingScript, promptFormat, hash string) (string, bool) {
	all, err := adaptationsOf(sc)
	if err != nil {
		return "", false
	}
	a, ok := all[promptFormat]
	if !ok || a.SourceHash != hash {
		return "", false
	}
	return a.Prompt, true
}

// ensureAdapted 返回出片用的转写稿：已有且未过期直接用；否则（或 force）调文本模型转写、存下并扣费。
// reserve 是转写之后还要花的积分（出片费），预检余额时一起算，免得转写完了出不了片。
func (s *MarketingService) ensureAdapted(ctx context.Context, userID uint64, c *scriptCtx, st model.MarketingSettings,
	video *provider.Snapshot, reserve int, force bool) (string, error) {
	f := video.Model.Capabilities.PromptFormat
	in, err := s.adaptSource(c, st, video.Model.Capabilities)
	if err != nil {
		return "", err
	}
	hash := marketing.SourceHash(in.Source)
	if prompt, ok := freshAdaptation(c.sc, f, hash); ok && !force {
		return prompt, nil
	}
	key := strconv.FormatUint(c.sc.ID, 10) + ":" + f
	if _, busy := s.adapting.LoadOrStore(key, struct{}{}); busy {
		return "", errcode.ErrMarketingScriptBusy.WithMsg("正在转写这份脚本的提示词，请稍候再试")
	}
	defer s.adapting.Delete(key)
	out, credits, err := s.callAdapt(ctx, userID, st.TextModel, in, reserve)
	if err != nil {
		return "", err
	}
	note := "营销出片转写 · H3 · " + clipRunes(c.sc.Title, 40)
	if err := s.saveAdaptation(ctx, userID, c.sc.ID, f, model.MarketingAdaptation{Prompt: out, SourceHash: hash, UpdatedAt: s.now()}, credits, note); err != nil {
		return "", err
	}
	return out, nil
}

// callAdapt 调文本模型转写，最多两次：第一次不合格时把校验原因交给模型修正；只收通过校验那一次的钱。
func (s *MarketingService) callAdapt(ctx context.Context, userID uint64, textModel string, in marketing.AdaptInput, reserve int) (string, int, error) {
	snap, err := s.pickTextModel(ctx, textModel, needAny)
	if err != nil {
		return "", 0, err
	}
	if err := s.checkAdaptBalance(ctx, userID, snap, in, reserve); err != nil {
		return "", 0, err
	}
	callCtx, cancel := context.WithTimeout(ctx, adaptTimeout)
	defer cancel()
	for range 2 {
		prompt, err := marketing.AdaptPrompt(in)
		if err != nil {
			return "", 0, err
		}
		res, err := s.caller.Complete(callCtx, snap, []llmgateway.Message{{Role: "user", Text: prompt}})
		if err != nil {
			logger.Warn("营销出片转写调用失败", zap.String("model", snap.Model.Key), zap.Error(err))
			return "", 0, errcode.ErrMarketingAdaptFailed.WithMsg("转写提示词失败：文本模型没有返回结果，请稍后重试")
		}
		out, err := marketing.DecodeAdapt(res.Text)
		if err == nil {
			if err = marketing.ValidateAdapt(in, out); err == nil {
				return out, adaptCost(snap, utf8.RuneCountInString(prompt), res), nil
			}
		}
		in.ValidationError = err.Error()
	}
	return "", 0, errcode.ErrMarketingAdaptFailed
}

// adaptEstimate 按转写提示词字数预估一次转写的积分。
func adaptEstimate(snap *provider.Snapshot, in marketing.AdaptInput) int {
	prompt, err := marketing.AdaptPrompt(in)
	if err != nil {
		return 0
	}
	return max(modelcfg.Quote(snap.Model.Pricing, snap.Model.Capabilities, modelcfg.Spec{PromptChars: utf8.RuneCountInString(prompt), Params: map[string]any{}}), 0)
}

// checkAdaptBalance 调模型前检查可用积分（余额减冻结）够不够「转写预估 + reserve」；不够返回积分不足，不调模型。
// 没有账户时按新账户初始积分惰性建立。
func (s *MarketingService) checkAdaptBalance(ctx context.Context, userID uint64, snap *provider.Snapshot, in marketing.AdaptInput, reserve int) error {
	need := adaptEstimate(snap, in) + reserve
	if need <= 0 {
		return nil
	}
	initial, err := s.credits.InitialCredits(ctx)
	if err != nil {
		return err
	}
	return s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		if err := tx.EnsureCredit(ctx, userID, initial); err != nil {
			return err
		}
		credit, err := tx.LockCredit(ctx, userID)
		if err != nil {
			return err
		}
		if credit.Balance-credit.Frozen < need {
			return errcode.ErrInsufficientCredits
		}
		return nil
	})
}

// saveAdaptation 在锁住脚本的事务里存下一份转写稿并扣转写费。
func (s *MarketingService) saveAdaptation(ctx context.Context, userID, scriptID uint64, promptFormat string, a model.MarketingAdaptation, credits int, note string) error {
	return s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		sc, err := tx.Script(ctx, userID, scriptID, true)
		if err != nil {
			return err
		}
		all, err := adaptationsOf(sc)
		if err != nil {
			return err
		}
		all[promptFormat] = a
		raw, err := json.Marshal(all)
		if err != nil {
			return err
		}
		if err := tx.UpdateScript(ctx, scriptID, map[string]any{"adaptations": raw}); err != nil {
			return err
		}
		return chargeMarketingAdapt(ctx, tx, userID, scriptID, credits, note)
	})
}

// chargeMarketingAdapt 扣转写费：实扣封顶在可用积分，不扣成负数、不动进行中任务冻结的积分；
// 和短剧转写一样成对写一条冻结流水和一条结算流水，后台对账等式保持成立。应收为 0 或账户不存在时不写。
func chargeMarketingAdapt(ctx context.Context, tx repository.MarketingTx, userID, scriptID uint64, credits int, note string) error {
	if credits <= 0 {
		return nil
	}
	credit, err := tx.LockCredit(ctx, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	charged := min(credits, max(credit.Balance-credit.Frozen, 0))
	if charged == 0 {
		return nil
	}
	for _, typ := range []string{model.LedgerFreeze, model.LedgerSettle} {
		if _, err := tx.InsertLedger(ctx, &model.CreditLedger{UserID: userID, MarketingScriptID: &scriptID, Type: typ, Amount: charged, Note: note}); err != nil {
			return err
		}
	}
	return tx.AddCredit(ctx, userID, -charged, 0)
}

// adaptPromptView 组装需要转写的模型的提示词预览：原稿、转写稿（含是否过期）、转写一次的预估积分。
func (s *MarketingService) adaptPromptView(ctx context.Context, c *scriptCtx, video *provider.Snapshot) (*model.MarketingPrompt, error) {
	f := video.Model.Capabilities.PromptFormat
	in, err := s.adaptSource(c, c.st, video.Model.Capabilities)
	if err != nil {
		return nil, err
	}
	v := &model.MarketingPrompt{Format: f, Prompt: in.Source}
	all, err := adaptationsOf(c.sc)
	if err != nil {
		return nil, err
	}
	if a, ok := all[f]; ok {
		v.Adaptation = &model.MarketingAdaptationView{Prompt: a.Prompt, Stale: a.SourceHash != marketing.SourceHash(in.Source), Edited: a.Edited, UpdatedAt: a.UpdatedAt}
	}
	if text, err := s.pickTextModel(ctx, c.st.TextModel, needAny); err == nil {
		v.AdaptCredits = adaptEstimate(text, in)
	}
	return v, nil
}

// Adapt 手动转写一份脚本的出片提示词（不传视频模型用本轮的），已有转写稿也重新转写。
// 模型不需要转写 10001；脚本重写中 68006；积分不足 40001；转写失败 68018。
func (s *MarketingService) Adapt(ctx context.Context, userID, id uint64, req *model.AdaptMarketingScriptReq) (*model.MarketingPrompt, error) {
	c, err := s.loadScript(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	video, err := s.promptVideo(ctx, c, req.VideoModel)
	if err != nil {
		return nil, err
	}
	if !marketing.NeedsAdapt(video.Model.Capabilities.PromptFormat) {
		return nil, errcode.ErrInvalidParams.WithMsg("这个视频模型直接用原稿出片，不需要转写")
	}
	if c.sc.Status == model.MarketingScriptRewriting {
		return nil, errcode.ErrMarketingScriptBusy
	}
	if _, err := s.ensureAdapted(ctx, userID, c, c.st, video, 0, true); err != nil {
		return nil, err
	}
	return s.Prompt(ctx, userID, id, req.VideoModel)
}

// EditAdaptation 手改一份已有的转写稿：标记 edited，指纹不变（过期与否仍按原稿判断，过期后出片会重新转写）。
func (s *MarketingService) EditAdaptation(ctx context.Context, userID, id uint64, req *model.EditMarketingAdaptationReq) (*model.MarketingPrompt, error) {
	err := s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		sc, err := tx.Script(ctx, userID, id, true)
		if mktNotFound(err) {
			return errcode.ErrMarketingNotFound
		}
		if err != nil {
			return err
		}
		all, err := adaptationsOf(sc)
		if err != nil {
			return err
		}
		a, ok := all[req.Format]
		if !ok {
			return errcode.ErrInvalidParams.WithMsg("还没有转写稿，不能修改")
		}
		a.Prompt, a.Edited, a.UpdatedAt = strings.TrimSpace(req.Prompt), true, time.Now()
		all[req.Format] = a
		raw, err := json.Marshal(all)
		if err != nil {
			return err
		}
		return tx.UpdateScript(ctx, id, map[string]any{"adaptations": raw})
	})
	if err != nil {
		return nil, err
	}
	return s.Prompt(ctx, userID, id, "")
}

// promptVideo 取预览 / 转写用的视频模型快照：传了就用传的，否则用本轮的。
func (s *MarketingService) promptVideo(ctx context.Context, c *scriptCtx, videoModel string) (*provider.Snapshot, error) {
	key := videoModel
	if key == "" {
		key = c.st.VideoModel
	}
	return s.videoSnapshot(ctx, key)
}

// adaptQuote 是视图里预估出片前转写费用用的：本轮视频模型需要转写时才有，带上文本模型和视频模型能力。
type adaptQuote struct {
	text  *provider.Snapshot
	video modelcfg.Capabilities
}

// adaptQuoteFor 返回本轮的转写报价依据；不需要转写、模型不可用或没有文本模型时为 nil（报价不含转写）。
func (s *MarketingService) adaptQuoteFor(ctx context.Context, st model.MarketingSettings) *adaptQuote {
	video, err := s.models.Snapshot(ctx, st.VideoModel)
	if err != nil || video == nil || !marketing.NeedsAdapt(video.Model.Capabilities.PromptFormat) {
		return nil
	}
	text, err := s.pickTextModel(ctx, st.TextModel, needAny)
	if err != nil {
		return nil
	}
	return &adaptQuote{text: text, video: video.Model.Capabilities}
}

// adaptExtra 是一份脚本出片前还要付的转写费：已有未过期的转写稿为 0。
func (s *MarketingService) adaptExtra(q *adaptQuote, c *scriptCtx) int {
	if q == nil {
		return 0
	}
	in, err := s.adaptSource(c, c.st, q.video)
	if err != nil {
		return 0
	}
	if _, ok := freshAdaptation(c.sc, q.video.PromptFormat, marketing.SourceHash(in.Source)); ok {
		return 0
	}
	return adaptEstimate(q.text, in)
}
