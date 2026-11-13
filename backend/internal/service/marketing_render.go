// 本文件是营销脚本出片：提示词预览、组装任务输入、单份与全部出片。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"video-canvas/internal/marketing"
	"video-canvas/internal/marketing/format"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// assetIDString 把素材 id 转成任务输入里的十进制字符串。
func assetIDString(id uint64) string { return strconv.FormatUint(id, 10) }

// renderInput 组装出片任务的输入：提示词由格式化器拼，op=omni，图片 = 商品图 + 模特图，视频 = 参考视频，generate_audio=true。
func (s *MarketingService) renderInput(c *scriptCtx, snap *provider.Snapshot, params map[string]any, prompt string) (map[string]any, error) {
	var ids []uint64
	if err := json.Unmarshal(c.project.ProductAssetIDs, &ids); err != nil {
		return nil, err
	}
	images := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		images = append(images, assetIDString(id))
	}
	if c.round.ModelAssetID != nil {
		images = append(images, assetIDString(*c.round.ModelAssetID))
	}
	input := map[string]any{"prompt": prompt, "op": modelcfg.OpOmni, "images": images, "generate_audio": true}
	if c.round.RefVideoAssetID != nil {
		input["videos"] = []any{assetIDString(*c.round.RefVideoAssetID)}
	}
	caps := snap.Model.Capabilities
	for k, v := range params {
		if _, ok := caps.Params.Get(k); ok && k != "generate_audio" {
			input[k] = v
		}
	}
	return input, nil
}

// mergeParams 合并出片参数：本轮设置为底，请求里的覆盖。
func mergeParams(base, over map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// Render 给一份脚本出片：格式化器拼提示词 → 用生成任务服务创建 omni 任务（节点 mkt:{脚本id}）。
// 旁白有问题（软问题非空）68006；视频模型不可用 68004；提示词超长 68008；积分不足 40001。
// 同一个 idempotency_key 重复提交返回第一次的结果。
func (s *MarketingService) Render(ctx context.Context, userID, id uint64, req *model.RenderMarketingScriptReq) (*model.MarketingScriptView, error) {
	c, err := s.loadScript(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if c.sc.Status == model.MarketingScriptRewriting {
		return nil, errcode.ErrMarketingScriptBusy
	}
	if flags := flagList(c.sc); len(flags) > 0 {
		return nil, errcode.ErrMarketingScriptBusy.WithMsg("旁白有问题（" + flagSummary(flags) + "），请先修改后再出片")
	}
	st := c.st
	if req.VideoModel != "" && req.VideoModel != st.VideoModel {
		st.VideoModel = req.VideoModel
		if req.Params == nil {
			st.Params = map[string]any{}
		}
	}
	snap, err := s.videoSnapshot(ctx, st.VideoModel)
	if err != nil {
		return nil, err
	}
	input, err := s.buildRenderInput(ctx, userID, c, st, snap, req.Params)
	if err != nil {
		return nil, err
	}
	if err := s.submitRender(ctx, userID, id, req.IdempotencyKey, st.VideoModel, input); err != nil {
		return nil, err
	}
	sc, err := s.reconcileScript(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return s.scriptViewByRow(ctx, userID, sc)
}

// buildRenderInput 校验模型支持全能参考，拼出片提示词（超长 68008），组装任务输入。
// 需要转写的模型（H3）用转写稿：没有或过期时先转写（预检余额时把出片费一起算上）。
func (s *MarketingService) buildRenderInput(ctx context.Context, userID uint64, c *scriptCtx, st model.MarketingSettings,
	snap *provider.Snapshot, params map[string]any) (map[string]any, error) {
	caps := snap.Model.Capabilities
	if !caps.Refs.Image.On || !hasOp(caps, modelcfg.OpOmni) {
		return nil, errcode.ErrMarketingVideoModel.WithMsg("所选视频模型不支持全能参考，不能用商品图出片")
	}
	prompt, err := s.renderPrompt(ctx, userID, c, st, snap)
	if err != nil {
		return nil, err
	}
	return s.renderInput(c, snap, mergeParams(st.Params, params), prompt)
}

// renderPrompt 返回出片用的提示词：需要转写的模型取转写稿（手改过的可能超长，超了报 68008），其余用格式化器直接拼。
func (s *MarketingService) renderPrompt(ctx context.Context, userID uint64, c *scriptCtx, st model.MarketingSettings, snap *provider.Snapshot) (string, error) {
	caps := snap.Model.Capabilities
	if marketing.NeedsAdapt(caps.PromptFormat) {
		images := productCount(c.project)
		if c.round.ModelAssetID != nil {
			images++
		}
		reserve := s.renderCredits(ctx, st, images, c.round.RefVideoAssetID != nil)
		prompt, err := s.ensureAdapted(ctx, userID, c, st, snap, reserve, false)
		if err != nil {
			return "", err
		}
		if limit := caps.Prompt.MaxLength; limit > 0 && utf8.RuneCountInString(prompt) > limit {
			return "", errcode.ErrMarketingPromptLong
		}
		return prompt, nil
	}
	in, err := s.formatInput(c, st, caps)
	if err != nil {
		return "", err
	}
	prompt, err := format.For(caps.PromptFormat).Render(in)
	if errors.Is(err, format.ErrTooLong) {
		return "", errcode.ErrMarketingPromptLong
	}
	return prompt, err
}

// submitRender 用生成任务服务创建出片任务（节点 mkt:{脚本id}），把节点级错误还原成业务错误。
func (s *MarketingService) submitRender(ctx context.Context, userID, id uint64, key, videoModel string, input map[string]any) error {
	resp, err := s.tasks.Create(ctx, userID, key, &model.CreateGenerationTaskReq{Kind: model.KindVideo, ModelID: videoModel, NodeID: marketingNode(id), Input: input})
	if err != nil {
		return err
	}
	if resp == nil || len(resp.Items) != 1 {
		return errors.New("出片任务创建未返回唯一结果")
	}
	item := resp.Items[0]
	if item.Error != nil {
		return errcode.New(item.Error.Code, item.Error.Message, item.Error.Status)
	}
	if item.Task == nil {
		return errors.New("出片任务创建结果缺少任务")
	}
	// 同一用户的幂等键可能已用于别的资源，不能把别的任务当成本脚本的出片结果返回。
	if item.Task.NodeID != marketingNode(id) || item.Task.Kind != model.KindVideo {
		return errcode.ErrInvalidParams.WithMsg("幂等键已用于其他生成请求")
	}
	return nil
}

// hasOp 判断模型是否支持某生成方式。
func hasOp(c modelcfg.Capabilities, op string) bool {
	for _, o := range c.Ops {
		if o == op {
			return true
		}
	}
	return false
}

// flagList 解码脚本的软问题。
func flagList(sc *model.MarketingScript) []model.MarketingFlagView {
	var flags []model.MarketingFlagView
	if len(sc.Flags) > 0 {
		_ = json.Unmarshal(sc.Flags, &flags)
	}
	return flags
}

// flagSummary 把软问题概括成给用户看的一句话。
func flagSummary(flags []model.MarketingFlagView) string {
	names := map[string]string{marketing.FlagForbidden: "禁用说法", marketing.FlagClaim: "背书无依据", marketing.FlagBrand: "第三方品牌"}
	seen := map[string]bool{}
	var parts []string
	for _, f := range flags {
		n := names[f.Type]
		if n != "" && !seen[n] {
			seen[n] = true
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, "、")
}

// RenderRound 给轮次里能出片的脚本全部出片：旁白有问题、正在重写或出片、已出过片的跳过并说明原因。
// 需要转写的模型先并发转写（见 preAdapt），转写失败的跳过。
// 积分不足时已经出的保留，剩下的跳过；一份都没出成且是积分不足则返回 40001。
func (s *MarketingService) RenderRound(ctx context.Context, userID, roundID uint64, req *model.RenderMarketingRoundReq) (*model.MarketingRenderAll, error) {
	r, err := s.repo.Round(ctx, userID, roundID, false)
	if mktNotFound(err) {
		return nil, errcode.ErrMarketingNotFound
	}
	if err != nil {
		return nil, err
	}
	scripts, byNode, err := s.roundTasks(ctx, userID, r.ID)
	if err != nil {
		return nil, err
	}
	statuses, eligible := batchEligible(scripts, byNode)
	failed := s.preAdapt(ctx, userID, r, eligible)
	out := &model.MarketingRenderAll{Skipped: []model.MarketingSkipped{}}
	created, noCredits := 0, false
	for i := range scripts {
		sc := &scripts[i]
		reason := batchSkipReason(sc, statuses[i], noCredits)
		if reason == "" {
			var short bool
			if reason, short, err = s.batchRender(ctx, userID, sc.ID, req.IdempotencyKey, failed[sc.ID]); err != nil {
				return nil, err
			}
			noCredits = noCredits || short
			if reason == "" {
				created++
			}
		}
		if reason != "" {
			out.Skipped = append(out.Skipped, model.MarketingSkipped{ScriptID: hexID(sc.ID), Reason: reason})
		}
	}
	if noCredits && created == 0 {
		return nil, errcode.ErrInsufficientCredits
	}
	if out.Round, err = s.roundViewByID(ctx, userID, roundID); err != nil {
		return nil, err
	}
	return out, nil
}

// batchEligible 按出片任务算出每份脚本的状态，并挑出本次能出片的脚本。
func batchEligible(scripts []model.MarketingScript, byNode map[string][]model.GenerationTask) ([]string, []uint64) {
	statuses := make([]string, len(scripts))
	var eligible []uint64
	for i := range scripts {
		statuses[i], _ = deriveScriptStatus(scripts[i].Status, byNode[marketingNode(scripts[i].ID)])
		if batchSkipReason(&scripts[i], statuses[i], false) == "" {
			eligible = append(eligible, scripts[i].ID)
		}
	}
	return statuses, eligible
}

// batchRender 出一份：转写已经失败的直接按失败原因跳过，否则出片。
func (s *MarketingService) batchRender(ctx context.Context, userID, scriptID uint64, batchKey string, adaptErr error) (string, bool, error) {
	if adaptErr != nil {
		return skipOf(adaptErr)
	}
	return s.renderOne(ctx, userID, scriptID, batchKey)
}

// preAdapt 全部出片前并发转写（最多同时 4 份）：转写一份要十几到几十秒，挨个转会超过请求超时。
// 本轮模型不需要转写时什么也不做；返回转写失败的脚本及原因。
func (s *MarketingService) preAdapt(ctx context.Context, userID uint64, r *model.MarketingRound, ids []uint64) map[uint64]error {
	st, err := settingsOf(r)
	if err != nil || len(ids) == 0 {
		return nil
	}
	video, err := s.videoSnapshot(ctx, st.VideoModel)
	if err != nil || !marketing.NeedsAdapt(video.Model.Capabilities.PromptFormat) {
		return nil
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out = map[uint64]error{}
		sem = make(chan struct{}, 4)
	)
	for _, id := range ids {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			c, err := s.loadScript(ctx, userID, id)
			if err == nil {
				_, err = s.ensureAdapted(ctx, userID, c, st, video, 0, false)
			}
			if err != nil {
				mu.Lock()
				out[id] = err
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return out
}

// roundTasks 返回轮次的脚本和它们的出片任务（按节点 id 分组，时间正序）。
func (s *MarketingService) roundTasks(ctx context.Context, userID, roundID uint64) ([]model.MarketingScript, map[string][]model.GenerationTask, error) {
	scripts, err := s.repo.RoundScripts(ctx, roundID)
	if err != nil {
		return nil, nil, err
	}
	nodes := make([]string, 0, len(scripts))
	for i := range scripts {
		nodes = append(nodes, marketingNode(scripts[i].ID))
	}
	tasks, err := s.repo.ScriptTasks(ctx, userID, nodes)
	if err != nil {
		return nil, nil, err
	}
	byNode := map[string][]model.GenerationTask{}
	for _, t := range tasks {
		byNode[t.NodeID] = append(byNode[t.NodeID], t)
	}
	return scripts, byNode, nil
}

// batchSkipReason 返回全部出片时这份脚本被跳过的原因；能出片返回空串。
func batchSkipReason(sc *model.MarketingScript, status string, noCredits bool) string {
	switch {
	case noCredits:
		return "积分不足"
	case status == model.MarketingScriptRewriting:
		return "正在重写"
	case status == model.MarketingScriptRendering:
		return "正在出片"
	case status == model.MarketingScriptDone:
		return "已出片"
	case len(flagList(sc)) > 0:
		return "旁白有问题，请先修改"
	}
	return ""
}

// renderOne 给批量出片里的一份脚本出片：成功返回空原因；业务错误作为跳过原因返回（积分不足时 noCredits 为真）；其他错误原样返回。
func (s *MarketingService) renderOne(ctx context.Context, userID, scriptID uint64, batchKey string) (reason string, noCredits bool, err error) {
	key := batchKey + ":" + idcodec.Encode(scriptID)
	_, err = s.Render(ctx, userID, scriptID, &model.RenderMarketingScriptReq{IdempotencyKey: key})
	if err == nil {
		return "", false, nil
	}
	return skipOf(err)
}

// skipOf 把批量出片里一份的错误转成跳过原因：业务错误作为原因（积分不足时 noCredits 为真），其他错误原样返回。
func skipOf(err error) (reason string, noCredits bool, _ error) {
	var biz *errcode.Error
	if errors.As(err, &biz) {
		return biz.Msg, biz.Code == errcode.ErrInsufficientCredits.Code, nil
	}
	return "", false, err
}
