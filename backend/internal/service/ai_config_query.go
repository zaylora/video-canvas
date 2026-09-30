package service

import (
	"context"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// ListConfigs 列出所有模型配置的概览（不含正文），带草稿 / 发布状态汇总。返回的切片永远不是 nil。
func (s *AIConfigService) ListConfigs(ctx context.Context) ([]ConfigListItem, error) {
	// 1. 取所有 draft / published 的 revision 元信息，按 key 归并；不读正文，避免列表接口读出大字段
	heads, err := s.repo.ListRevisionHeads(ctx, model.ConfigTargetModel, false)
	if err != nil {
		return nil, err
	}
	drafts, published := aiGroupHeads(heads)

	// 2. 与指针行合并：指针行的 enabled / sort 是运行时真值
	rows, err := s.repo.ListModelPointers(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]ConfigListItem, 0, len(rows))
	for _, r := range rows {
		item := ConfigListItem{Key: r.Key, Kind: r.Kind, Enabled: r.Enabled, Sort: r.Sort,
			PublishedRevisionID: r.PublishedRevisionID, UpdatedAt: r.UpdatedAt}
		if no, ok := drafts[r.Key]; ok {
			item.DraftRevisionNo, item.HasUnpublishedDraft = &no, true
		}
		if no, ok := published[r.Key]; ok {
			item.PublishedRevisionNo = &no
		}
		items = append(items, item)
	}
	return items, nil
}

// aiGroupHeads 把 revision 头信息按 key 归并成“最新草稿号”和“发布版本号”两张表。
func aiGroupHeads(heads []model.AIConfigRevision) (drafts, published map[string]int) {
	drafts, published = map[string]int{}, map[string]int{}
	for _, h := range heads {
		switch h.Status {
		case model.RevisionDraft:
			if old, ok := drafts[h.TargetKey]; !ok || h.RevisionNo > old {
				drafts[h.TargetKey] = h.RevisionNo
			}
		case model.RevisionPublished:
			published[h.TargetKey] = h.RevisionNo
		}
	}
	return drafts, published
}

// GetConfig 返回模型详情：最新草稿与当前已发布版本的完整正文。模型不存在返回 ErrConfigNotFound。
func (s *AIConfigService) GetConfig(ctx context.Context, key string) (*ConfigDetail, error) {
	// 1. 指针行：不存在就是 404
	m, err := s.repo.GetModelPointer(ctx, key)
	if err != nil {
		return nil, aiNotFound(err)
	}
	detail := &ConfigDetail{Target: model.ConfigTargetModel, Key: key, Kind: m.Kind, Enabled: m.Enabled, Sort: m.Sort, UpdatedAt: m.UpdatedAt}

	// 2. 草稿与发布版本：没有就是 null，不算错误
	draft, err := s.repo.GetDraft(ctx, model.ConfigTargetModel, key)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		detail.Draft = draft
	}
	pub, err := s.repo.GetPublishedRevision(ctx, model.ConfigTargetModel, key)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		detail.Published = pub
	}
	return detail, nil
}

// ListRevisions 返回模型的历史版本（不含正文，新到旧）。模型不存在返回 ErrConfigNotFound。
func (s *AIConfigService) ListRevisions(ctx context.Context, key string) ([]model.AIConfigRevision, error) {
	// 1. 模型必须存在：否则前端拿到空列表会误以为“存在但没有历史”
	exists, err := s.pointerExists(ctx, key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errcode.ErrConfigNotFound
	}
	// 2. 查询历史
	return s.repo.ListRevisions(ctx, model.ConfigTargetModel, key, aiRevisionListLimit)
}

// GetRevision 返回某个历史版本的完整正文（回滚前预览用）；revision 不属于该模型视为不存在。
func (s *AIConfigService) GetRevision(ctx context.Context, key string, revisionID uint64) (*model.AIConfigRevision, error) {
	// 1. 查询 revision；不存在翻译成 404
	rev, err := s.repo.GetRevision(ctx, revisionID)
	if err != nil {
		return nil, aiNotFound(err)
	}
	// 2. 核对归属：其他模型的 revision 一律按不存在处理，不暴露它们的存在
	if rev.Target != model.ConfigTargetModel || rev.TargetKey != key {
		return nil, errcode.ErrConfigNotFound
	}
	return rev, nil
}
