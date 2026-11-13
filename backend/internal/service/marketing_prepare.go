// 本文件是环节运行前的输入准备（读素材、组装环节输入）。
package service

import (
	"context"
	"encoding/json"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/stepkit"
)

// prepareError 是准备输入阶段的业务失败，msg 直接给用户看。
type prepareError struct{ msg string }

func (e *prepareError) Error() string { return e.msg }

// stepPrep 是准备好的环节输入。
type stepPrep struct {
	input    stepkit.Input
	warnings []string
	basis    []byte
}

// prepareInput 按环节组装输入：事实卡读商品图；拆解读参考视频（能看视频就内联，否则截帧）；跟拍和创意用已有结果。
func (s *MarketingService) prepareInput(ctx context.Context, round *model.MarketingRound, p *model.MarketingProject, st model.MarketingSettings,
	stepKey string, snap *provider.Snapshot) (*stepPrep, error) {
	brief := briefOf(st)
	switch stepKey {
	case marketing.StepFacts:
		var ids []uint64
		if err := json.Unmarshal(p.ProductAssetIDs, &ids); err != nil {
			return nil, err
		}
		images, err := s.imageDataURLs(ctx, p.UserID, ids)
		if err != nil {
			return nil, err
		}
		return &stepPrep{input: marketing.FactsInput(brief, images)}, nil
	case marketing.StepReverse:
		return s.reverseInput(ctx, round, brief, snap)
	case marketing.StepFollow:
		rev, err := reverseOf(round.Reverse)
		if err != nil {
			return nil, err
		}
		in := marketing.FollowInput(brief, factsOf(p), rev, marketing.AssetNote{ProductImages: productCount(p), HasModel: round.ModelAssetID != nil})
		return &stepPrep{input: in, basis: round.Reverse}, nil
	default:
		createCount, err := planOf(st, round.RefVideoAssetID != nil)
		if err != nil {
			return nil, err
		}
		var revIn *marketing.Reverse
		if len(round.Reverse) > 0 {
			rev, err := reverseOf(round.Reverse)
			if err != nil {
				return nil, err
			}
			revIn = &rev
		}
		return &stepPrep{input: marketing.CreateInput(brief, factsOf(p), st.Angles, createCount, revIn)}, nil
	}
}

// productCount 返回项目商品图张数。
func productCount(p *model.MarketingProject) int {
	var ids []uint64
	_ = json.Unmarshal(p.ProductAssetIDs, &ids)
	return len(ids)
}
