package service

import (
	"context"
	"slices"
	"strings"

	"video-canvas/internal/model"
)

// ScriptGaps 列出所选集里未被本次生成覆盖、且尚无剧本的上一集，供估价弹窗提示。
func (s *DramaService) ScriptGaps(ctx context.Context, userID, id uint64, req *model.DramaJobReq) ([]int, error) {
	// 1. 非剧本步骤无需衔接提示，空切片保证 JSON 输出 []。
	out := []int{}
	if req.Step != "scripts" {
		return out, nil
	}
	p, err := s.repo.Project(ctx, userID, id, false)
	if err != nil {
		return nil, dramaError(err)
	}
	eps, err := s.repo.Episodes(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	selected, err := selectDramaEpisodes(p, eps, req)
	if err != nil {
		return nil, err
	}
	scope := dramaEpisodeNumbers(selected)
	for _, ep := range selected {
		previous := ep.No - 1
		if previous < 1 || slices.Contains(scope, previous) {
			continue
		}
		written := false
		for _, candidate := range eps {
			if candidate.No == previous && strings.TrimSpace(candidate.ScriptMD) != "" {
				written = true
				break
			}
		}
		if !written {
			out = append(out, previous)
		}
	}
	return out, nil
}
