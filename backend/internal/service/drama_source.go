package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
)

// runAnalyze 使用入队时冻结的全书抽样，只请求一次，不再合并分块摘要。
func (s *DramaService) runAnalyze(ctx context.Context, p *model.DramaProject, j *model.DramaJob, snap *provider.Snapshot) (any, int, error) {
	vars := s.dramaVariables(p)
	if err := json.Unmarshal(j.InputJSON, &vars); err != nil {
		return nil, 0, err
	}
	return s.callDrama(ctx, j, snap, "analyze", vars, nil)
}

// dramaContextLimit 保守按1字1Token限制原文块为上下文窗口的60%。
func dramaContextLimit(snap *provider.Snapshot) int {
	if c := snap.Model.Capabilities.Context; c != nil && c.Window > 0 {
		return max(1, c.Window*60/100)
	}
	return 12000
}

// validateBreakdownScope 资产只能引用本次集，空范围交画布按本次集补齐。
func validateBreakdownScope(out drama.BreakdownOutput, scope []int) error {
	for _, asset := range out.Assets {
		for _, no := range asset.Episodes {
			if !slices.Contains(scope, no) {
				return fmt.Errorf("资产集号%d不属于本次范围", no)
			}
		}
	}
	return nil
}

// validatePromptCoverage 要求恰好覆盖本次新建节点，拒绝触及既有资产。
func validatePromptCoverage(out drama.AssetPromptsOutput, assets []DramaCanvasAsset) error {
	if len(out.Prompts) != len(assets) {
		return errors.New("提示词数量与新资产不一致")
	}
	for _, p := range out.Prompts {
		found := false
		for _, a := range assets {
			if a.NodeID == p.NodeID {
				found = true
				break
			}
		}
		if !found {
			return errors.New("提示词包含非本次资产节点")
		}
	}
	return nil
}
