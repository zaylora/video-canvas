package service

import (
	"context"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
)

// DramaSegmentRepo 片段归属查询和事务边界。
type DramaSegmentRepo interface {
	// WithTx 原子执行片段变更，锁顺序为项目后片段。
	WithTx(context.Context, func(repository.DramaTx) error) error
	// Project 校验项目归属和存活状态。
	Project(context.Context, uint64, uint64, bool) (*model.DramaProject, error)
	// Episodes 返回项目分集。
	Episodes(context.Context, uint64, uint64) ([]model.DramaEpisode, error)
	// SegmentEpisode 按集ID反查存活项目下的分集。
	SegmentEpisode(context.Context, uint64, uint64) (*model.DramaEpisode, error)
	// Segment 查询用户片段，缺失返回ErrNotFound。
	Segment(context.Context, uint64, uint64, bool) (*model.DramaSegment, error)
	// Segments 返回按seq排序的用户片段。
	Segments(context.Context, uint64, uint64) ([]model.DramaSegment, error)
}

// DramaSegmentTaskCreator 直接复用现有生成任务提交逻辑。
type DramaSegmentTaskCreator interface {
	// Create 校验生成参数、冻结积分并返回逐项创建结果。
	Create(context.Context, uint64, string, *model.CreateGenerationTaskReq) (*model.CreateGenerationTaskResp, error)
}

// DramaSegmentAssetReader 读取用户素材视图，复用AssetService。
type DramaSegmentAssetReader interface {
	// View 返回用户拥有的素材及稳定URL。
	View(context.Context, uint64, uint64) (*model.AssetView, error)
}

// DramaSegmentDeps 提供片段的持久化、生成、素材与用户事件依赖。
type DramaSegmentDeps struct {
	Repo        DramaSegmentRepo
	Tasks       DramaSegmentTaskCreator
	Assets      DramaSegmentAssetReader
	Broadcaster ws.Broadcaster
}

// DramaSegmentService 管理片段编辑、真实任务结果与自动采用。
type DramaSegmentService struct {
	repo   DramaSegmentRepo
	tasks  DramaSegmentTaskCreator
	assets DramaSegmentAssetReader
	bc     ws.Broadcaster
}

// NewDramaSegmentService 创建独立片段服务，任务状态观察者由initialize组装。
func NewDramaSegmentService(d DramaSegmentDeps) *DramaSegmentService {
	bc := d.Broadcaster
	if bc == nil {
		bc = ws.NopBroadcaster{}
	}
	return &DramaSegmentService{repo: d.Repo, tasks: d.Tasks, assets: d.Assets, bc: bc}
}

// List 校验用户项目后按顺序返回片段，并恢复漏处理的成功任务写回。
func (s *DramaSegmentService) List(ctx context.Context, userID, dramaID uint64, no int) ([]model.DramaSegmentView, error) {
	// 1. 项目和集必须都存活且属于当前用户。
	_, ep, err := s.episode(ctx, userID, dramaID, no)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.Segments(ctx, userID, ep.ID)
	if err != nil {
		return nil, err
	}
	// 2. 与观察者共用事务恢复逻辑，通知失败后读接口仍能自动采用与更新首帧。
	out := make([]model.DramaSegmentView, 0, len(rows))
	for i := range rows {
		v, p, episode, changed, err := s.readView(ctx, userID, rows[i].ID)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
		if changed {
			s.publishSegment(ctx, p, episode, v)
		}
	}
	return out, nil
}

// Summary 返回某集片段数、总秒数和已采用数，计数之前恢复任务写回。
func (s *DramaSegmentService) Summary(ctx context.Context, userID, episodeID uint64) (count, seconds, done int, err error) {
	// 1. 使用集主键反查存活项目，沿用列表的归属和任务恢复规则。
	ep, err := s.repo.SegmentEpisode(ctx, userID, episodeID)
	if err != nil {
		return 0, 0, 0, dramaError(err)
	}
	views, err := s.List(ctx, userID, ep.DramaID, ep.No)
	if err != nil {
		return 0, 0, 0, err
	}
	// 2. 已采用按chosen_task_id计数，不因后续失败take减少。
	for _, v := range views {
		seconds += v.Seconds
		if v.ChosenTaskID != "" {
			done++
		}
	}
	return len(views), seconds, done, nil
}

// episode 根据项目ID和集号定位，其他用户和父项目删除统一为短剧不存在。
func (s *DramaSegmentService) episode(ctx context.Context, userID, dramaID uint64, no int) (*model.DramaProject, *model.DramaEpisode, error) {
	p, err := s.repo.Project(ctx, userID, dramaID, false)
	if err != nil {
		return nil, nil, dramaError(err)
	}
	rows, err := s.repo.Episodes(ctx, userID, dramaID)
	if err != nil {
		return nil, nil, err
	}
	for i := range rows {
		if rows[i].No == no {
			return p, &rows[i], nil
		}
	}
	return nil, nil, errcode.ErrDramaNotFound
}

// lockSegment 先非锁定反查父项目，再按项目→片段顺序加锁，和分镜替换保持一致。
func lockSegment(ctx context.Context, tx repository.DramaTx, userID, sid uint64) (*model.DramaProject, *model.DramaEpisode, *model.DramaSegment, error) {
	row, err := tx.Segment(ctx, userID, sid, false)
	if err != nil {
		return nil, nil, nil, err
	}
	ep, err := tx.SegmentEpisode(ctx, userID, row.EpisodeID)
	if err != nil {
		return nil, nil, nil, err
	}
	p, err := tx.Project(ctx, userID, ep.DramaID, true)
	if err != nil {
		return nil, nil, nil, err
	}
	row, err = tx.Segment(ctx, userID, sid, true)
	return p, ep, row, err
}

// segmentError 将片段归属和版本哨兵转换为契约错误，不泄漏父项目是否存在。
func segmentError(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrDramaSegmentNotFound
	}
	if errors.Is(err, repository.ErrStateConflict) {
		return errcode.ErrCanvasConflict
	}
	var business *errcode.Error
	if errors.As(err, &business) && business.Code == errcode.ErrDramaNotFound.Code {
		return errcode.ErrDramaSegmentNotFound
	}
	return err
}

// publishSegment 在事务提交后推送完整视图，前端与HTTP共用相同字段。
func (s *DramaSegmentService) publishSegment(ctx context.Context, p *model.DramaProject, ep *model.DramaEpisode, v *model.DramaSegmentView) {
	s.bc.Publish(ctx, ws.UserChannel(p.UserID), ws.Message{Type: ws.TypeDramaSegment, Data: struct {
		DramaID   idcodec.ID              `json:"drama_id"`
		EpisodeNo int                     `json:"episode_no"`
		Segment   *model.DramaSegmentView `json:"segment"`
	}{idcodec.ID(p.ID), ep.No, v}})
}

// changedView 读取最终状态并通知用户，避免在持有数据库锁时访问素材或WebSocket。
func (s *DramaSegmentService) changedView(ctx context.Context, userID, sid uint64) (*model.DramaSegmentView, error) {
	v, p, ep, _, err := s.readView(ctx, userID, sid)
	if err == nil {
		s.publishSegment(ctx, p, ep, v)
	}
	return v, err
}
