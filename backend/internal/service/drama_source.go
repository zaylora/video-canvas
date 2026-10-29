package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
)

// runAnalyze 按章节分块研读，每块最多占模型上下文60%，最后合并摘要。
func (s *DramaService) runAnalyze(ctx context.Context, p *model.DramaProject, j *model.DramaJob, snap *provider.Snapshot) (any, int, error) {
	chunks := dramaSourceChunks(p.SourceText, snap)
	summaries := []drama.AnalyzeOutput{}
	cost := 0
	for i, chunk := range chunks {
		vars := s.dramaVariables(p)
		vars["source"] = chunk
		v, c, e := s.callDrama(ctx, j, snap, "analyze", vars, nil)
		if e != nil {
			return nil, 0, e
		}
		summaries = append(summaries, v.(drama.AnalyzeOutput))
		cost += c
		if e = s.dramaProgress(ctx, j, (i+1)*80/len(chunks)); e != nil {
			return nil, 0, e
		}
	}
	for len(summaries) > 1 {
		groups := dramaSummaryGroups(summaries, dramaContextLimit(snap))
		if len(groups) == len(summaries) {
			return nil, 0, errcode.ErrDramaOutputInvalid.WithMsg("研读摘要超出模型上下文")
		}
		merged, c, e := s.mergeDramaSummaries(ctx, p, j, snap, groups)
		if e != nil {
			return nil, 0, e
		}
		summaries = merged
		cost += c
	}
	return summaries[0], cost, nil
}

// mergeDramaSummaries 对一轮摘要分组逐个汇总，避免把长小说重新塞回上下文。
func (s *DramaService) mergeDramaSummaries(ctx context.Context, p *model.DramaProject, j *model.DramaJob, snap *provider.Snapshot, groups [][]drama.AnalyzeOutput) ([]drama.AnalyzeOutput, int, error) {
	merged := make([]drama.AnalyzeOutput, 0, len(groups))
	cost := 0
	for _, group := range groups {
		vars := s.dramaVariables(p)
		vars["source"] = ""
		vars["summaries"] = group
		v, c, e := s.callDrama(ctx, j, snap, "analyze", vars, nil)
		if e != nil {
			return nil, 0, e
		}
		merged = append(merged, v.(drama.AnalyzeOutput))
		cost += c
	}
	return merged, cost, nil
}

// dramaSummaryGroups 按上下文预算合并相邻摘要，保持原文章节顺序。
func dramaSummaryGroups(summaries []drama.AnalyzeOutput, limit int) [][]drama.AnalyzeOutput {
	groups := [][]drama.AnalyzeOutput{}
	var group []drama.AnalyzeOutput
	used := 0
	for _, v := range summaries {
		raw, _ := json.Marshal(v)
		size := utf8.RuneCount(raw)
		if used+size > limit && len(group) > 0 {
			groups = append(groups, group)
			group = nil
			used = 0
		}
		group = append(group, v)
		used += size
	}
	if len(group) > 0 {
		groups = append(groups, group)
	}
	return groups
}

// dramaContextLimit 保守按1字1Token限制原文块为上下文窗口的60%。
func dramaContextLimit(snap *provider.Snapshot) int {
	if c := snap.Model.Capabilities.Context; c != nil && c.Window > 0 {
		return max(1, c.Window*60/100)
	}
	return 12000
}

var dramaChapterPattern = regexp.MustCompile(`(?m)^\s*第[零〇一二三四五六七八九十百千万0-9]+[章节卷回].*$`)
var dramaEpisodePattern = regexp.MustCompile(`(?m)^\s*第([零〇一二三四五六七八九十百0-9]+)集[^\r\n]*`)

// dramaSourceChunks 优先章边界，单章超限按字符切分且不丢正文。
func dramaSourceChunks(source string, snap *provider.Snapshot) []string {
	limit := dramaContextLimit(snap)
	matches := dramaChapterPattern.FindAllStringIndex(source, -1)
	starts := []int{0}
	for _, m := range matches {
		if m[0] > 0 {
			starts = append(starts, m[0])
		}
	}
	starts = append(starts, len(source))
	out := []string{}
	pending := ""
	for i := range len(starts) - 1 {
		chapter := []rune(source[starts[i]:starts[i+1]])
		if utf8.RuneCountInString(pending)+len(chapter) > limit && pending != "" {
			out = append(out, pending)
			pending = ""
		}
		for len(chapter) > limit {
			out = append(out, string(chapter[:limit]))
			chapter = chapter[limit:]
		}
		pending += string(chapter)
	}
	if pending != "" {
		out = append(out, pending)
	}
	if len(out) == 0 {
		return []string{source}
	}
	return out
}

// dramaMarkedEpisodes 识别中文和数字分集标记，原文正文逐集原样保留。
func dramaMarkedEpisodes(source string) []drama.OutlineEpisode {
	matches := dramaEpisodePattern.FindAllStringIndex(source, -1)
	if len(matches) == 0 || len(matches) > 100 {
		return nil
	}
	out := make([]drama.OutlineEpisode, 0, len(matches))
	for i, m := range matches {
		end := len(source)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		title := strings.TrimSpace(source[m[0]:m[1]])
		script := strings.TrimSpace(source[m[0]:end])
		out = append(out, drama.OutlineEpisode{No: i + 1, Title: title, Synopsis: title, ScriptMD: script})
	}
	return out
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
