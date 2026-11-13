// 本文件是营销项目的列表聚合、修改和删除（视图组装在 marketing_view.go）。
package service

import (
	"context"
	"encoding/json"
	"strings"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/stepkit"
)

// ListProjects 按标题搜索并分页返回项目列表（更新时间倒序），带脚本状态统计、状态条和封面。
func (s *MarketingService) ListProjects(ctx context.Context, userID uint64, keyword string, page, size int) ([]model.MarketingProjectSummary, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = marketingPageSize
	}
	rows, total, err := s.repo.ListProjects(ctx, userID, keyword, page, size)
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.MarketingProjectSummary, 0, len(rows))
	if len(rows) == 0 {
		return out, total, nil
	}
	ids := make([]uint64, 0, len(rows))
	for _, p := range rows {
		ids = append(ids, p.ID)
	}
	agg, err := s.summaryAgg(ctx, userID, rows, ids)
	if err != nil {
		return nil, 0, err
	}
	for _, p := range rows {
		out = append(out, agg.summary(p))
	}
	return out, total, nil
}

// summaryAgg 是列表聚合出的数据：脚本计数、状态条、轮次概况、封面。
type summaryAgg struct {
	counts    map[uint64]model.MarketingScriptCounts
	bars      map[uint64][]string
	scripting map[uint64]int // 写脚本中的计划份数
	failed    map[uint64]bool
	cover     func(p model.MarketingProject) string
}

func (a *summaryAgg) summary(p model.MarketingProject) model.MarketingProjectSummary {
	c := a.counts[p.ID]
	bars := append([]string(nil), a.bars[p.ID]...)
	n, scripting := a.scripting[p.ID]
	for range n {
		bars = append([]string{model.MarketingScriptRendering}, bars...)
	}
	if len(bars) > 8 {
		bars = bars[:8]
	}
	if bars == nil {
		bars = []string{}
	}
	return model.MarketingProjectSummary{ID: hexID(p.ID), Title: p.Title, CoverURL: a.cover(p), Scripting: scripting, Rendering: c.Rendering,
		LastRoundFailed: a.failed[p.ID], ScriptCounts: c, Bars: bars, UpdatedAt: p.UpdatedAt}
}

// summaryAgg 查列表需要的聚合数据。
func (s *MarketingService) summaryAgg(ctx context.Context, userID uint64, rows []model.MarketingProject, ids []uint64) (*summaryAgg, error) {
	a := &summaryAgg{counts: map[uint64]model.MarketingScriptCounts{}, bars: map[uint64][]string{}, scripting: map[uint64]int{}, failed: map[uint64]bool{}}
	counts, err := s.repo.ScriptCounts(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, c := range counts {
		cur := a.counts[c.ProjectID]
		switch c.Status {
		case model.MarketingScriptDraft:
			cur.Draft += c.N
		case model.MarketingScriptRendering:
			cur.Rendering += c.N
		case model.MarketingScriptDone:
			cur.Done += c.N
		case model.MarketingScriptFailed:
			cur.Failed += c.N
		}
		a.counts[c.ProjectID] = cur
	}
	bars, err := s.repo.ScriptBars(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, b := range bars {
		a.bars[b.ProjectID] = append(a.bars[b.ProjectID], b.Status)
	}
	scripting, latest, err := s.repo.RoundBriefs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range scripting {
		a.scripting[r.ProjectID] += max(r.PlanCount, 1)
	}
	for _, r := range latest {
		a.failed[r.ProjectID] = r.Status == model.MarketingRoundFailed
	}
	// 封面：最新成片的首帧，没有就是第一张商品图。
	var assetIDs []uint64
	pick := map[uint64]uint64{}
	for _, p := range rows {
		id := uint64(0)
		if p.CoverAssetID != nil {
			id = *p.CoverAssetID
		} else {
			var pids []uint64
			_ = json.Unmarshal(p.ProductAssetIDs, &pids)
			if len(pids) > 0 {
				id = pids[0]
			}
		}
		if id != 0 {
			pick[p.ID] = id
			assetIDs = append(assetIDs, id)
		}
	}
	keys, err := s.repo.CoverKeys(ctx, userID, assetIDs)
	if err != nil {
		return nil, err
	}
	a.cover = func(p model.MarketingProject) string {
		if key, ok := keys[pick[p.ID]]; ok {
			return s.assets.FileURL(key)
		}
		return ""
	}
	return a, nil
}

// UpdateProject 改标题或事实卡，带 revision 乐观锁：冲突 68009，项目不存在 68001。
func (s *MarketingService) UpdateProject(ctx context.Context, userID, id uint64, req *model.UpdateMarketingProjectReq) (*model.MarketingProjectView, error) {
	fields := map[string]any{}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			return nil, errcode.ErrInvalidParams.WithMsg("标题不能为空")
		}
		fields["title"] = title
	}
	if req.Product != nil {
		facts := toFacts(req.Product)
		if err := marketing.ValidateFacts(facts); err != nil {
			return nil, errcode.ErrInvalidParams.WithMsg("事实卡不合法：" + strings.Join(stepkit.ErrorList(err), "；"))
		}
		raw, err := json.Marshal(facts)
		if err != nil {
			return nil, err
		}
		fields["product"] = raw
	}
	if len(fields) == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("没有要修改的内容")
	}
	err := s.repo.WithTx(ctx, func(tx repository.MarketingTx) error { return tx.SaveProject(ctx, userID, id, req.Revision, fields) })
	switch {
	case mktNotFound(err):
		return nil, errcode.ErrMarketingNotFound
	case err == repository.ErrRevisionConflict: //nolint:errorlint // 哨兵错误原样返回
		return nil, errcode.ErrMarketingConflict
	case err != nil:
		return nil, err
	}
	return s.GetProject(ctx, userID, id)
}

// toFacts 把请求里的事实卡转成业务类型。
func toFacts(f *model.MarketingFacts) marketing.ProductFacts {
	out := marketing.ProductFacts{Name: strings.TrimSpace(f.Name), Category: strings.TrimSpace(f.Category), Anchors: nonNil(f.Anchors),
		Prohibited: nonNil(f.Prohibited), Audience: strings.TrimSpace(f.Audience), CTA: strings.TrimSpace(f.CTA), SellingPoints: []marketing.SellingPoint{}}
	for _, sp := range f.SellingPoints {
		out.SellingPoints = append(out.SellingPoints, marketing.SellingPoint{Text: strings.TrimSpace(sp.Text), Proof: strings.TrimSpace(sp.Proof), Source: sp.Source})
	}
	return out
}

// DeleteProject 软删项目：有写脚本中的轮次或进行中的出片任务返回 68010；成片素材保留。
func (s *MarketingService) DeleteProject(ctx context.Context, userID, id uint64) error {
	return s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		if _, err := tx.Project(ctx, userID, id, true); mktNotFound(err) {
			return errcode.ErrMarketingNotFound
		} else if err != nil {
			return err
		}
		if n, err := tx.ScriptingRounds(ctx, id); err != nil {
			return err
		} else if n > 0 {
			return errcode.ErrMarketingBusyDelete
		}
		scriptIDs, err := s.repo.ProjectScriptIDs(ctx, userID, id)
		if err != nil {
			return err
		}
		nodes := make([]string, 0, len(scriptIDs))
		for _, sid := range scriptIDs {
			nodes = append(nodes, marketingNode(sid))
		}
		if n, err := s.repo.ActiveProjectTasks(ctx, userID, nodes); err != nil {
			return err
		} else if n > 0 {
			return errcode.ErrMarketingBusyDelete
		}
		return tx.DeleteProject(ctx, userID, id)
	})
}
