package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/stepkit"
)

// Options 返回平台与角度预设，以及参考视频时长上限。
func (s *MarketingService) Options() *model.MarketingOptions {
	out := &model.MarketingOptions{Platforms: []model.MarketingPlatformOption{}, Angles: []model.MarketingAngleOption{},
		RefVideoMaxSecond: MarketingRefMaxSeconds}
	for _, p := range marketing.Platforms() {
		out.Platforms = append(out.Platforms, model.MarketingPlatformOption{Key: p.Key, Name: p.Name, AspectRatio: p.AspectRatio, Language: p.Language, Style: p.Style})
	}
	for _, a := range marketing.Angles() {
		out.Angles = append(out.Angles, model.MarketingAngleOption{Key: a.Key, Name: a.Name, Desc: a.Desc})
	}
	return out
}

// textNeed 是环节对文本模型的能力要求。
type textNeed int

const (
	needAny    textNeed = iota // 任意 Agent 模型
	needVision                 // 能看图
	needVideo                  // 优先能看视频，退回能看图（再用截帧）
)

// pickTextModel 选环节用的文本模型：用户选的满足能力就用它，否则取第一个满足的 Agent 模型，都没有返回 68003。
// 拆解（needVideo）优先 Video=true 的模型；都不能看视频时退回 Vision 的模型，这时需要服务器能截帧。
func (s *MarketingService) pickTextModel(ctx context.Context, want string, need textNeed) (*provider.Snapshot, error) {
	snaps, err := s.agentSnapshots(ctx, want)
	if err != nil {
		return nil, err
	}
	passes := []bool{true}
	if need == needVideo {
		passes = []bool{true, false} // 先要能直接看视频的，再退回能看图（截帧）的
	}
	for _, strict := range passes {
		for _, sn := range snaps {
			if s.acceptsNeed(sn, need, strict) {
				return sn, nil
			}
		}
	}
	return nil, errcode.ErrMarketingNoTextModel
}

// agentSnapshots 返回可用的 Agent 模型快照：用户选的排第一，其余按清单顺序。
func (s *MarketingService) agentSnapshots(ctx context.Context, want string) ([]*provider.Snapshot, error) {
	list, err := s.models.ListModels(ctx, modelcfg.KindAgent)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(list))
	for _, m := range list {
		if m.Key == want {
			keys = append(keys, m.Key)
		}
	}
	for _, m := range list {
		if m.Key != want {
			keys = append(keys, m.Key)
		}
	}
	snaps := make([]*provider.Snapshot, 0, len(keys))
	for _, k := range keys {
		snap, err := s.models.Snapshot(ctx, k)
		if errors.Is(err, provider.ErrModelUnavailable) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if snap != nil && snap.Model.Kind == modelcfg.KindAgent {
			snaps = append(snaps, snap)
		}
	}
	return snaps, nil
}

// acceptsNeed 判断模型是否满足环节的能力要求；strict 只对拆解有意义：要求能直接看视频，否则只要能看图且服务器能截帧。
func (s *MarketingService) acceptsNeed(sn *provider.Snapshot, need textNeed, strict bool) bool {
	c := sn.Model.Capabilities
	switch {
	case need == needVision:
		return c.Vision
	case need == needVideo && strict:
		return c.Video
	case need == needVideo:
		return c.Vision && s.framesAvailable()
	}
	return true
}

// framesAvailable 报告服务器能不能截帧。
func (s *MarketingService) framesAvailable() bool { return s.frames != nil && s.frames.Available() }

// durationOf 取成片时长（秒）：来自参数 duration，没有或为自动（负数）取缺省，限制在 4 到 30。
func durationOf(params map[string]any) int {
	d := marketingDefaultDuration
	switch v := params["duration"].(type) {
	case float64:
		d = int(math.Round(v))
	case int:
		d = v
	case int64:
		d = int(v)
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			d = n
		}
	}
	if d < 0 {
		d = marketingDefaultDuration
	}
	return min(max(d, 4), 30)
}

// briefOf 取四个环节共用的设置摘要。
func briefOf(st model.MarketingSettings) marketing.Brief {
	p := marketing.PlatformOf(st.Platform)
	ratio := p.AspectRatio
	if v, ok := st.Params["aspect_ratio"].(string); ok && v != "" {
		ratio = v
	}
	return marketing.Brief{Platform: p.Key, Language: st.Language, Duration: durationOf(st.Params), AspectRatio: ratio, Description: st.Description}
}

// normalizeSettings 补全空值，让落库和视图里的数组、对象不为 null。
func normalizeSettings(st model.MarketingSettings) model.MarketingSettings {
	if st.Angles == nil {
		st.Angles = []string{}
	}
	if st.Params == nil {
		st.Params = map[string]any{}
	}
	return st
}

// planOf 校验角度与份数，返回创意脚本份数；不合法返回参数错误。
func planOf(st model.MarketingSettings, hasRef bool) (int, error) {
	n, err := marketing.ScriptPlan(hasRef, st.Angles, st.AutoCount)
	if err != nil {
		return 0, errcode.ErrInvalidParams.WithMsg(err.Error())
	}
	return n, nil
}

// stepCosts 是写脚本各环节的预估积分（已含重写一次的上限）。
type stepCosts struct {
	facts, reverse, follow, create int
}

func (c stepCosts) total() int { return c.facts + c.reverse + c.follow + c.create }

// estimateStep 按提示词字数预估一个环节的最高费用。
func (s *MarketingService) estimateStep(ctx context.Context, key string, snap *provider.Snapshot, in stepkit.Input) (int, error) {
	step, ok := s.steps.Get(key)
	if !ok {
		return 0, errcode.ErrStepNotFound
	}
	method, err := s.resolver.Method(ctx, step)
	if err != nil {
		return 0, err
	}
	return stepkit.Estimate(snap, step, method, in)
}

// placeholderFacts 是预估时占位的事实卡（只用来估提示词长度）。
func placeholderFacts() marketing.ProductFacts {
	return marketing.ProductFacts{Name: "商品", Category: "品类",
		Anchors:       []string{"外观锚点一", "外观锚点二", "外观锚点三"},
		SellingPoints: []marketing.SellingPoint{{Text: "卖点一", Proof: "画面证明一", Source: "description"}, {Text: "卖点二", Proof: "画面证明二", Source: "image"}},
		Prohibited:    []string{"不编造价格、销量、评价、认证与功效"}, Audience: "目标人群", CTA: "行动引导"}
}

// placeholderReverse 是预估时占位的拆解：按时长摊成若干镜头。
func placeholderReverse(duration int) marketing.Reverse {
	const n = 6
	r := marketing.Reverse{Brands: []string{}, Claims: []string{}}
	step := float64(duration) / n
	for i := range n {
		r.Shots = append(r.Shots, marketing.ReverseShot{ID: i + 1, Start: float64(i) * step, End: float64(i+1) * step, ShotSize: "中景", Camera: "固定",
			Subject: "主体", Action: "主体做出一个动作并展示商品", Scene: "场景", Lighting: "自然光", ProductInFrame: "商品居中", ProductRole: marketing.RoleDynamic, KeyColors: []string{"白色"}})
	}
	return r
}

// quoteSteps 预估本次设置要跑的各环节积分：无参考 facts、create；有参考再加 reverse、follow。
// hasFacts 为真（项目已有事实卡）时 facts 为 0 但仍列出。
func (s *MarketingService) quoteSteps(ctx context.Context, st model.MarketingSettings, hasRef, hasFacts bool, createCount int) (stepCosts, error) {
	var c stepCosts
	brief := briefOf(st)
	vision, err := s.pickTextModel(ctx, st.TextModel, needVision)
	if err != nil {
		return c, err
	}
	if !hasFacts {
		if c.facts, err = s.estimateStep(ctx, marketing.StepFacts, vision, marketing.FactsInput(brief, nil)); err != nil {
			return c, err
		}
	}
	anyModel, err := s.pickTextModel(ctx, st.TextModel, needAny)
	if err != nil {
		return c, err
	}
	facts, rev := placeholderFacts(), placeholderReverse(brief.Duration)
	if hasRef {
		vid, err := s.pickTextModel(ctx, st.TextModel, needVideo)
		if err != nil {
			return c, err
		}
		if c.reverse, err = s.estimateStep(ctx, marketing.StepReverse, vid, marketing.ReverseInput(brief, float64(brief.Duration), nil, nil, nil)); err != nil {
			return c, err
		}
		if c.follow, err = s.estimateStep(ctx, marketing.StepFollow, anyModel, marketing.FollowInput(brief, facts, rev, marketing.AssetNote{ProductImages: 1})); err != nil {
			return c, err
		}
	}
	var revIn *marketing.Reverse
	if hasRef {
		revIn = &rev
	}
	if createCount > 0 || !hasRef {
		c.create, err = s.estimateStep(ctx, marketing.StepCreate, anyModel, marketing.CreateInput(brief, facts, st.Angles, max(createCount, 1), revIn))
	}
	return c, err
}

// Quote 预估写脚本的积分：steps 列出本次会跑的全部环节（项目已有事实卡时 facts 为 0）。
func (s *MarketingService) Quote(ctx context.Context, userID uint64, req *model.MarketingQuoteReq) (*model.MarketingQuote, error) {
	st := normalizeSettings(req.Settings)
	createCount, err := planOf(st, req.HasRef)
	if err != nil {
		return nil, err
	}
	hasFacts := false
	if req.ProjectID != nil {
		p, err := s.repo.Project(ctx, userID, uint64(*req.ProjectID), false)
		if mktNotFound(err) {
			return nil, errcode.ErrMarketingNotFound
		}
		if err != nil {
			return nil, err
		}
		hasFacts = len(p.Product) > 0
	}
	c, err := s.quoteSteps(ctx, st, req.HasRef, hasFacts, createCount)
	if err != nil {
		return nil, err
	}
	out := &model.MarketingQuote{Credits: c.total(), Steps: []model.MarketingQuoteStep{{Step: marketing.NameFacts, Credits: c.facts}}}
	if req.HasRef {
		out.Steps = append(out.Steps, model.MarketingQuoteStep{Step: marketing.NameReverse, Credits: c.reverse}, model.MarketingQuoteStep{Step: marketing.NameFollow, Credits: c.follow})
	}
	out.Steps = append(out.Steps, model.MarketingQuoteStep{Step: marketing.NameCreate, Credits: c.create})
	return out, nil
}

// checkVideoModel 校验视频模型能出整条有声成片：存在、视频类、支持全能参考（omni）、图片参考上限够、有参考视频时收视频，
// 以及参数合法。不满足返回 68004，参数不合法返回参数错误。
func (s *MarketingService) checkVideoModel(ctx context.Context, key string, params map[string]any, images int, hasRef bool) (*provider.Snapshot, error) {
	snap, err := s.models.Snapshot(ctx, key)
	if errors.Is(err, provider.ErrModelUnavailable) {
		return nil, errcode.ErrMarketingVideoModel
	}
	if err != nil {
		return nil, err
	}
	caps := snap.Model.Capabilities
	switch {
	case snap.Model.Kind != modelcfg.KindVideo, !slices.Contains(caps.Ops, modelcfg.OpOmni):
		return nil, errcode.ErrMarketingVideoModel.WithMsg("所选视频模型不支持全能参考，不能用商品图出片")
	case !caps.Refs.Image.On || caps.Refs.Image.Max < images:
		return nil, errcode.ErrMarketingVideoModel.WithMsg(fmt.Sprintf("所选视频模型最多支持 %d 张参考图，本次需要 %d 张", caps.Refs.Image.Max, images))
	case hasRef && (!caps.Refs.Video.On || caps.Refs.Video.Max < 1):
		return nil, errcode.ErrMarketingVideoModel.WithMsg("所选视频模型不支持参考视频")
	}
	input := map[string]any{"prompt": "校验", "op": modelcfg.OpOmni}
	for k, v := range params {
		input[k] = v
	}
	if _, errs := modelcfg.ValidateInput(modelcfg.KindVideo, caps, input); len(errs) > 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("视频参数不合法：" + modelcfg.JoinFieldErrors(errs))
	}
	return snap, nil
}

// roundAssets 是一轮写脚本用到的素材：已校验归属与类型。
type roundAssets struct {
	product  []uint64
	model    *uint64
	ref      *uint64
	duration float64 // 参考视频时长（秒），未知为 0
}

// checkAssets 校验素材都属于该用户且种类对：商品图 1 到 4 张图片、模特图图片、参考视频视频；
// 无效返回 68002，参考视频超过时长或体积上限返回 68005。caps 为视频模型能力，用来查体积上限。
func (s *MarketingService) checkAssets(ctx context.Context, userID uint64, product []uint64, modelID, refID *uint64, caps modelcfg.Capabilities) (*roundAssets, error) {
	if len(product) < 1 || len(product) > 4 {
		return nil, errcode.ErrMarketingAssets
	}
	seen := map[uint64]bool{}
	out := &roundAssets{model: modelID, ref: refID}
	for _, id := range product {
		if seen[id] {
			return nil, errcode.ErrMarketingAssets.WithMsg("商品图不能重复")
		}
		seen[id] = true
		if err := s.checkAssetKind(ctx, userID, id, "image"); err != nil {
			return nil, err
		}
		out.product = append(out.product, id)
	}
	if modelID != nil {
		if err := s.checkAssetKind(ctx, userID, *modelID, "image"); err != nil {
			return nil, err
		}
	}
	if refID != nil {
		d, err := s.checkRefVideo(ctx, userID, *refID, caps)
		if err != nil {
			return nil, err
		}
		out.duration = d
	}
	return out, nil
}

// checkRefVideo 校验参考视频是本人的视频、不超过时长和体积上限，返回时长（秒）。
func (s *MarketingService) checkRefVideo(ctx context.Context, userID, id uint64, caps modelcfg.Capabilities) (float64, error) {
	a, err := s.assets.Get(ctx, userID, id)
	if errors.Is(err, provider.ErrAssetNotFound) || (err == nil && a.Kind != "video") {
		return 0, errcode.ErrMarketingAssets.WithMsg("参考视频无效")
	}
	if err != nil {
		return 0, err
	}
	duration := float64(a.DurationMs) / 1000
	if duration > MarketingRefMaxSeconds+0.5 {
		return 0, errcode.ErrMarketingRefTooLong.WithMsg(fmt.Sprintf("参考视频最长 %d 秒，当前约 %.0f 秒", MarketingRefMaxSeconds, duration))
	}
	if mb := caps.Refs.Video.MaxMB; mb > 0 && a.ByteSize > int64(mb)<<20 {
		return 0, errcode.ErrMarketingRefTooLong.WithMsg(fmt.Sprintf("参考视频不能超过 %d MB", mb))
	}
	return duration, nil
}

// checkAssetKind 校验一个素材存在、属于该用户且是指定种类。
func (s *MarketingService) checkAssetKind(ctx context.Context, userID, id uint64, kind string) error {
	a, err := s.assets.Get(ctx, userID, id)
	if errors.Is(err, provider.ErrAssetNotFound) || (err == nil && a.Kind != kind) {
		return errcode.ErrMarketingAssets
	}
	return err
}

// settingsOf 解码轮次的设置快照。
func settingsOf(r *model.MarketingRound) (model.MarketingSettings, error) {
	var st model.MarketingSettings
	if err := json.Unmarshal(r.Settings, &st); err != nil {
		return st, fmt.Errorf("解码轮次设置失败：%w", err)
	}
	return normalizeSettings(st), nil
}

// optionalID 把可选素材 id 转成指针（0 视为没有）。
func optionalID(id *uint64) *uint64 {
	if id == nil || *id == 0 {
		return nil
	}
	return id
}

// stepNameOf 把环节 key 转成接口里的环节名（marketing.reverse → reverse）。
func stepNameOf(key string) string { return strings.TrimPrefix(key, "marketing.") }
