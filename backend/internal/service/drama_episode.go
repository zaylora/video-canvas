package service

import (
	"context"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// GetEpisode 读取单集，集不存在和项目不属于用户均返回67001。
func (s *DramaService) GetEpisode(ctx context.Context, userID, id uint64, no int) (*model.DramaEpisodeView, error) {
	// 1. 先查项目归属，随后按集号匹配。
	if _, err := s.repo.Project(ctx, userID, id, false); err != nil {
		return nil, dramaError(err)
	}
	eps, err := s.repo.Episodes(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	for _, e := range eps {
		if e.No == no {
			return s.dramaEpisodeView(ctx, e)
		}
	}
	return nil, errcode.ErrDramaNotFound
}

// CreateEpisode 持有项目行锁追加集，保证并发下集号唯一。
func (s *DramaService) CreateEpisode(ctx context.Context, userID, id uint64, req *model.CreateDramaEpisodeReq) (*model.DramaEpisodeView, error) {
	// 1. 同一项目追加串行化，不把行ID暴露给调用方。
	var e model.DramaEpisode
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		if _, err := tx.Project(ctx, userID, id, true); err != nil {
			return err
		}
		eps, err := tx.Episodes(ctx, userID, id)
		if err != nil {
			return err
		}
		no := 1
		for _, v := range eps {
			no = max(no, v.No+1)
		}
		if no > 100 {
			return errcode.ErrInvalidParams.WithMsg("最多100集")
		}
		e = model.DramaEpisode{UserID: userID, DramaID: id, No: no, Title: req.Title, Synopsis: req.Synopsis, ScriptStatus: "empty", Revision: 1}
		return tx.SaveEpisode(ctx, &e, 0)
	})
	if err != nil {
		return nil, dramaError(err)
	}
	return s.dramaEpisodeView(ctx, e)
}

// UpdateEpisode 乐观锁更新单集；拆解锁定后仅允许改标题。
func (s *DramaService) UpdateEpisode(ctx context.Context, userID, id uint64, no int, req *model.UpdateDramaEpisodeReq) (*model.DramaEpisodeView, error) {
	// 1. 项目行锁与画布拆解锁集采用同一顺序，避免绕过锁定。
	var result model.DramaEpisode
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		if _, e := tx.Project(ctx, userID, id, true); e != nil {
			return e
		}
		eps, e := tx.Episodes(ctx, userID, id)
		if e != nil {
			return e
		}
		for _, ep := range eps {
			if ep.No != no {
				continue
			}
			if e := applyDramaEpisodePatch(&ep, req); e != nil {
				return e
			}
			if e := tx.SaveEpisode(ctx, &ep, req.Revision); e != nil {
				return e
			}
			result = ep
			return nil
		}
		return errcode.ErrDramaNotFound
	})
	if err != nil {
		return nil, dramaError(err)
	}
	return s.dramaEpisodeView(ctx, result)
}

// applyDramaEpisodePatch 校验revision与拆解锁，再应用实际出现的字段。
func applyDramaEpisodePatch(ep *model.DramaEpisode, req *model.UpdateDramaEpisodeReq) error {
	if ep.Revision != req.Revision {
		return errcode.ErrCanvasConflict
	}
	if ep.LockedAt != nil && (req.ScriptMD != nil || req.Synopsis != nil) {
		return errcode.ErrDramaEpisodeLocked
	}
	if req.Title != nil {
		ep.Title = *req.Title
	}
	if req.Synopsis != nil {
		ep.Synopsis = *req.Synopsis
	}
	if req.ScriptMD != nil {
		ep.ScriptMD = *req.ScriptMD
		ep.ScriptStatus = "empty"
		if strings.TrimSpace(ep.ScriptMD) != "" {
			ep.ScriptStatus = "ready"
		}
	}
	return nil
}
