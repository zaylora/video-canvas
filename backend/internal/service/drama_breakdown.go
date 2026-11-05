package service

import (
	"context"
	"unicode/utf8"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
)

// breakdownBatches 每批读取完整剧本；全部有效后统一合并画布，失败不写半套资产。
func (s *DramaService) breakdownBatches(ctx context.Context, run *dramaRun, vars map[string]any, selected []model.DramaEpisode) (drama.BreakdownOutput, int, error) {
	units := make([]drama.Unit, 0, len(selected))
	byNo := map[int]model.DramaEpisode{}
	for _, ep := range selected {
		units = append(units, drama.Unit{No: ep.No, Text: ep.ScriptMD, Chars: utf8.RuneCountInString(ep.ScriptMD)})
		byNo[ep.No] = ep
	}
	result := drama.BreakdownOutput{Assets: []drama.Asset{}}
	cost := 0
	for _, batch := range drama.ScriptBatches(units) {
		eps := make([]model.DramaEpisode, 0, len(batch))
		for _, u := range batch {
			eps = append(eps, byNo[u.No])
		}
		vars["episodes"] = dramaScriptInputs(eps)
		scope := dramaEpisodeNumbers(eps)
		out, c, err := s.callDrama(ctx, run.job, &run.snapshot, "breakdown", vars, func(v any) error { return validateBreakdownScope(v.(drama.BreakdownOutput), scope) })
		if err != nil {
			return drama.BreakdownOutput{}, 0, err
		}
		result.Assets = append(result.Assets, out.(drama.BreakdownOutput).Assets...)
		cost += c
	}
	return result, cost, nil
}
