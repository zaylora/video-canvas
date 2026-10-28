package service

import (
	"context"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// Create 在用户分集末尾或指定位置插入片段，保持seq连续。
func (s *DramaSegmentService) Create(ctx context.Context, userID, dramaID uint64, no int, req *model.CreateDramaSegmentReq) (*model.DramaSegmentView, error) {
	// 1. 复用项目归属检查；默认五秒便于用户新增空白片段。
	_, ep, err := s.episode(ctx, userID, dramaID, no)
	if err != nil {
		return nil, err
	}
	seconds := 5
	if req.Seconds != nil {
		seconds = *req.Seconds
	}
	if seconds < 1 || (req.AfterSeq != nil && *req.AfterSeq < 0) {
		return nil, errcode.ErrInvalidParams
	}
	row := &model.DramaSegment{UserID: userID, EpisodeID: ep.ID, Seconds: seconds, Prompt: req.Prompt, Shots: []byte(`[]`), Revision: 1}
	// 2. 锁项目后移动后续片段；插入和所有seq变化同事务提交。
	err = s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		if _, err := tx.Project(ctx, userID, dramaID, true); err != nil {
			return err
		}
		return insertDramaSegment(ctx, tx, row, req.AfterSeq)
	})
	if err != nil {
		return nil, dramaError(err)
	}
	return s.changedView(ctx, userID, row.ID)
}

// Update 按revision更新片段，只允许采用同画布本片段已成功的视频任务。
func (s *DramaSegmentService) Update(ctx context.Context, userID, sid uint64, req *model.UpdateDramaSegmentReq) (*model.DramaSegmentView, error) {
	// 1. service也校验核心约束，非HTTP调用不能绕过revision和时长要求。
	if req.Revision < 1 || (req.Seconds != nil && *req.Seconds < 1) {
		return nil, errcode.ErrInvalidParams
	}
	// 2. 按项目→片段加锁；先比较版本，防止旧请求覆盖自动采用。
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		p, _, row, err := lockSegment(ctx, tx, userID, sid)
		if err != nil {
			return err
		}
		if row.Revision != req.Revision {
			return errcode.ErrCanvasConflict
		}
		if req.ChosenTaskID != nil {
			tasks, err := tx.SegmentTasks(ctx, userID, p.CanvasID, segmentNode(sid, false))
			if err != nil {
				return err
			}
			if !validSegmentTake(tasks, uint64(*req.ChosenTaskID)) {
				return errcode.ErrDramaTakeInvalid
			}
			id := uint64(*req.ChosenTaskID)
			row.ChosenTaskID = &id
		}
		if req.Prompt != nil {
			row.Prompt = *req.Prompt
		}
		if req.Seconds != nil {
			row.Seconds = *req.Seconds
		}
		return tx.SaveSegment(ctx, row, req.Revision)
	})
	if err != nil {
		return nil, segmentError(err)
	}
	return s.changedView(ctx, userID, sid)
}

// Delete 只删除片段并收拢后续序号，保留所有生成任务和素材。
func (s *DramaSegmentService) Delete(ctx context.Context, userID, sid uint64) error {
	// 1. 先锁项目再锁片段，避免与分镜替换或并发排序交错。
	var project *model.DramaProject
	var episode *model.DramaEpisode
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		p, ep, row, err := lockSegment(ctx, tx, userID, sid)
		if err != nil {
			return err
		}
		project, episode = p, ep
		if err := tx.DeleteSegment(ctx, userID, sid); err != nil {
			return err
		}
		rows, err := tx.Segments(ctx, userID, row.EpisodeID)
		if err != nil {
			return err
		}
		for i := range rows {
			if rows[i].Seq != i {
				rows[i].Seq = i
				if err := tx.SaveSegment(ctx, &rows[i], rows[i].Revision); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return segmentError(err)
	}
	// 2. 删除事件不虚构SegmentView；剩余片段的seq变化通过完整列表和事件同步。
	views, err := s.List(ctx, userID, project.ID, episode.No)
	if err != nil {
		return err
	}
	for i := range views {
		s.publishSegment(ctx, project, episode, &views[i])
	}
	return nil
}

// Order 只接受该集片段的完整不重复集合，在同一事务重排并更新变更片段版本。
func (s *DramaSegmentService) Order(ctx context.Context, userID, dramaID uint64, no int, req *model.OrderDramaSegmentsReq) ([]model.DramaSegmentView, error) {
	// 1. 通过用户项目定位集，排序集合在项目锁内重新读取。
	_, ep, err := s.episode(ctx, userID, dramaID, no)
	if err != nil {
		return nil, segmentError(err)
	}
	err = s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		if _, err := tx.Project(ctx, userID, dramaID, true); err != nil {
			return err
		}
		rows, err := tx.Segments(ctx, userID, ep.ID)
		if err != nil {
			return err
		}
		return saveDramaSegmentOrder(ctx, tx, rows, req)
	})
	if err != nil {
		return nil, segmentError(err)
	}
	// 2. 提交后读取完整视图，任务或首帧的变化也反映在返回值中。
	views, err := s.List(ctx, userID, dramaID, no)
	if err != nil {
		return nil, err
	}
	p, err := s.repo.Project(ctx, userID, dramaID, false)
	if err != nil {
		return nil, dramaError(err)
	}
	for i := range views {
		s.publishSegment(ctx, p, ep, &views[i])
	}
	return views, nil
}

// validSegmentTake 只允许本次归属查询得到的成功视频任务，失败、其他片段或空ID均不接受。
func validSegmentTake(tasks []model.GenerationTask, id uint64) bool {
	for i := range tasks {
		if tasks[i].ID == id && tasks[i].Status == model.TaskSucceeded && tasks[i].Kind == model.KindVideo {
			return true
		}
	}
	return false
}

// insertDramaSegment 在已持有项目锁的事务中校验插入位置并顺移后续片段。
func insertDramaSegment(ctx context.Context, tx repository.DramaTx, row *model.DramaSegment, afterSeq *int) error {
	rows, err := tx.Segments(ctx, row.UserID, row.EpisodeID)
	if err != nil {
		return err
	}
	row.Seq = len(rows)
	if afterSeq != nil {
		if *afterSeq >= len(rows) {
			return errcode.ErrInvalidParams.WithMsg("插入位置不存在")
		}
		row.Seq = *afterSeq + 1
	}
	for i := range rows {
		if rows[i].Seq < row.Seq {
			continue
		}
		rows[i].Seq++
		if err := tx.SaveSegment(ctx, &rows[i], rows[i].Revision); err != nil {
			return err
		}
	}
	return tx.SaveSegment(ctx, row, 0)
}

// saveDramaSegmentOrder 拒绝缺失、重复和跨集ID，只更新序号实际变化的片段。
func saveDramaSegmentOrder(ctx context.Context, tx repository.DramaTx, rows []model.DramaSegment, req *model.OrderDramaSegmentsReq) error {
	if len(rows) != len(req.IDs) {
		return errcode.ErrDramaSegmentOrderInvalid
	}
	byID := make(map[uint64]*model.DramaSegment, len(rows))
	for i := range rows {
		byID[rows[i].ID] = &rows[i]
	}
	for i, id := range req.IDs {
		row, ok := byID[uint64(id)]
		if !ok {
			return errcode.ErrDramaSegmentOrderInvalid
		}
		delete(byID, uint64(id))
		if row.Seq == i {
			continue
		}
		row.Seq = i
		if err := tx.SaveSegment(ctx, row, row.Revision); err != nil {
			return err
		}
	}
	return nil
}
