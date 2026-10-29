// Package dramasegmentfake 提供片段service与handler共用的内存仓储，保持事务回滚和归属语义。
package dramasegmentfake

import (
	"context"
	"slices"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
)

// Repo 是片段测试的完整依赖，未使用的短剧原语通过嵌入暴露误调用。
type Repo struct {
	repository.DramaTx
	Rows           []model.DramaSegment
	Tasks          []model.GenerationTask
	Messages       []ws.Message
	ProjectDeleted bool
	Created        *model.CreateGenerationTaskReq
	IdempotencyKey string
	CreateError    *model.TaskItemError
	Err            error
	SegmentVersion int64
	nextID         uint64
}

// New 返回用户1、项目7、画布9、集10的独立测试仓储。
func New() *Repo { return &Repo{nextID: 1} }

// WithTx 在错误时恢复片段快照，防止非法重排造成部分写入。
func (r *Repo) WithTx(_ context.Context, fn func(repository.DramaTx) error) error {
	if r.Err != nil {
		return r.Err
	}
	before := slices.Clone(r.Rows)
	version := r.SegmentVersion
	err := fn(r)
	if err != nil {
		r.Rows = before
		r.SegmentVersion = version
	}
	return err
}

// Project 只返回当前用户的存活项目。
func (r *Repo) Project(_ context.Context, userID, id uint64, _ bool) (*model.DramaProject, error) {
	if r.Err != nil {
		return nil, r.Err
	}
	if r.ProjectDeleted || userID != 1 || id != 7 {
		return nil, repository.ErrNotFound
	}
	return &model.DramaProject{BaseModel: model.BaseModel{ID: 7}, UserID: 1, CanvasID: 9, Revision: 1}, nil
}

// Episodes 返回存活项目的一集。
func (r *Repo) Episodes(ctx context.Context, userID, id uint64) ([]model.DramaEpisode, error) {
	if _, err := r.Project(ctx, userID, id, false); err != nil {
		return nil, err
	}
	return []model.DramaEpisode{{BaseModel: model.BaseModel{ID: 10}, UserID: 1, DramaID: 7, No: 1, Revision: 1, SegmentVersion: r.SegmentVersion}}, nil
}

// SegmentEpisode 校验分集ID与父项目归属。
func (r *Repo) SegmentEpisode(ctx context.Context, userID, episodeID uint64) (*model.DramaEpisode, error) {
	eps, err := r.Episodes(ctx, userID, 7)
	if err != nil {
		return nil, err
	}
	if episodeID != 10 {
		return nil, repository.ErrNotFound
	}
	return &eps[0], nil
}

// Segment 返回当前用户的存活片段副本。
func (r *Repo) Segment(ctx context.Context, userID, sid uint64, _ bool) (*model.DramaSegment, error) {
	if _, err := r.Project(ctx, userID, 7, false); err != nil {
		return nil, err
	}
	for _, row := range r.Rows {
		if row.ID == sid && row.UserID == userID && !row.DeletedAt.Valid {
			return &row, nil
		}
	}
	return nil, repository.ErrNotFound
}

// Segments 返回本集片段，按seq、ID排序。
func (r *Repo) Segments(ctx context.Context, userID, episodeID uint64) ([]model.DramaSegment, error) {
	if _, err := r.SegmentEpisode(ctx, userID, episodeID); err != nil {
		return nil, err
	}
	rows := []model.DramaSegment{}
	for _, row := range r.Rows {
		if row.UserID == userID && row.EpisodeID == episodeID && !row.DeletedAt.Valid {
			rows = append(rows, row)
		}
	}
	slices.SortFunc(rows, func(a, b model.DramaSegment) int {
		if a.Seq < b.Seq || (a.Seq == b.Seq && a.ID < b.ID) {
			return -1
		}
		if a.Seq == b.Seq && a.ID == b.ID {
			return 0
		}
		return 1
	})
	return rows, nil
}

// SaveSegment 插入或CAS更新，原样保留任务与素材。
func (r *Repo) SaveSegment(ctx context.Context, row *model.DramaSegment, revision int64) error {
	if _, err := r.SegmentEpisode(ctx, row.UserID, row.EpisodeID); err != nil {
		return err
	}
	if row.ID == 0 {
		row.ID, row.CreatedAt, row.UpdatedAt = r.nextID, time.Now(), time.Now()
		r.nextID++
		r.Rows = append(r.Rows, *row)
		r.SegmentVersion++
		return nil
	}
	for i := range r.Rows {
		if r.Rows[i].ID == row.ID && r.Rows[i].UserID == row.UserID && !r.Rows[i].DeletedAt.Valid {
			if r.Rows[i].Revision != revision {
				return repository.ErrStateConflict
			}
			old := r.Rows[i]
			if old.Seq != row.Seq || !sameChosen(old.ChosenTaskID, row.ChosenTaskID) {
				r.SegmentVersion++
			}
			row.Revision, row.UpdatedAt = revision+1, time.Now()
			r.Rows[i] = *row
			return nil
		}
	}
	return repository.ErrStateConflict
}

// DeleteSegment 软删片段，不动Tasks。
func (r *Repo) DeleteSegment(ctx context.Context, userID, sid uint64) error {
	if _, err := r.Segment(ctx, userID, sid, false); err != nil {
		return err
	}
	for i := range r.Rows {
		if r.Rows[i].ID == sid {
			r.Rows[i].DeletedAt.Time, r.Rows[i].DeletedAt.Valid = time.Now(), true
			r.SegmentVersion++
			return nil
		}
	}
	return repository.ErrNotFound
}

// SegmentTasks 按用户、画布、节点过滤正式任务，并稳定按创建时间和ID排序。
func (r *Repo) SegmentTasks(_ context.Context, userID, canvasID uint64, nodeID string) ([]model.GenerationTask, error) {
	rows := []model.GenerationTask{}
	for _, task := range r.Tasks {
		if task.UserID == userID && task.CanvasProjectID != nil && *task.CanvasProjectID == canvasID && task.NodeID == nodeID && !task.IsTest {
			rows = append(rows, task)
		}
	}
	slices.SortFunc(rows, func(a, b model.GenerationTask) int {
		if order := a.CreatedAt.Compare(b.CreatedAt); order != 0 {
			return order
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return rows, nil
}

// Create 记录生成参数，并模拟现有生成服务的逐项结果。
func (r *Repo) Create(_ context.Context, _ uint64, key string, req *model.CreateGenerationTaskReq) (*model.CreateGenerationTaskResp, error) {
	r.Created, r.IdempotencyKey = req, key
	if r.CreateError != nil {
		return &model.CreateGenerationTaskResp{Items: []model.CreateTaskItem{{NodeID: req.NodeID, Error: r.CreateError}}}, nil
	}
	canvas := req.CanvasID
	task := &model.GenerationTaskView{ID: 101, CanvasProjectID: &canvas, NodeID: req.NodeID, Kind: req.Kind, Status: model.TaskPending}
	return &model.CreateGenerationTaskResp{Items: []model.CreateTaskItem{{NodeID: req.NodeID, Task: task}}}, nil
}

// View 返回测试素材视图。
func (r *Repo) View(_ context.Context, _, id uint64) (*model.AssetView, error) {
	return &model.AssetView{ID: id, URL: "/files/test", Kind: "image"}, nil
}

// Publish 记录完整用户事件。
func (r *Repo) Publish(_ context.Context, _ string, message ws.Message) {
	r.Messages = append(r.Messages, message)
}

// HasSegmentTakes 只检查本集现存片段的视频任务。
func (r *Repo) HasSegmentTakes(ctx context.Context, userID, episodeID uint64) (bool, error) {
	rows, err := r.Segments(ctx, userID, episodeID)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		tasks, err := r.SegmentTasks(ctx, userID, 9, "seg:"+idcodec.Encode(row.ID))
		if err != nil {
			return false, err
		}
		for _, task := range tasks {
			if task.Kind == model.KindVideo {
				return true, nil
			}
		}
	}
	return false, nil
}

// sameChosen 比较可空采用ID，让共享fake遵循真实仓储的过期判定。
func sameChosen(a, b *uint64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
