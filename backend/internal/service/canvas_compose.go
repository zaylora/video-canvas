package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

const composeMaxBytes = 512 * 1024 * 1024

type canvasComposeAssets interface {
	Open(context.Context, uint64, uint64) (*provider.AssetFile, error)
	StageCanvasMedia(context.Context, uint64, UploadInput, func(*model.Asset) error) (*model.Asset, error)
	FileURL(string) string
}

// CanvasComposeService 提交可恢复的画布成片任务并执行工作进程的转码。
type CanvasComposeService struct {
	repo     *repository.CanvasCollaborationRepository
	shares   *repository.CanvasShareRepository
	assets   canvasComposeAssets
	composer DramaVideoComposer
	enqueue  repository.CanvasComposeEnqueuer
	bus      *ws.CanvasCollaborationBus
}

// CanvasComposeView 给提交者返回成片任务状态和授权素材编号。
type CanvasComposeView struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	SegmentCount  int    `json:"segment_count"`
	ResultAssetID string `json:"result_asset_id,omitempty"`
	ResultNode    string `json:"result_node,omitempty"`
	Error         string `json:"error,omitempty"`
}

// NewCanvasComposeService 组装转码、素材、事务和授权边界。
func NewCanvasComposeService(repo *repository.CanvasCollaborationRepository, shares *repository.CanvasShareRepository, assets canvasComposeAssets, composer DramaVideoComposer) *CanvasComposeService {
	return &CanvasComposeService{repo: repo, shares: shares, assets: assets, composer: composer}
}

// WithEnqueuer 在数据库事务内接入 River 入队服务。
func (s *CanvasComposeService) WithEnqueuer(enqueue repository.CanvasComposeEnqueuer) *CanvasComposeService {
	s.enqueue = enqueue
	return s
}

// WithRoomBus 将已成片事务通知当前房间成员。
func (s *CanvasComposeService) WithRoomBus(bus *ws.CanvasCollaborationBus) *CanvasComposeService {
	s.bus = bus
	return s
}

// Available 返回当前站点是否开放成片任务提交。
func (s *CanvasComposeService) Available() bool { return s != nil && s.enqueue != nil }

// WithCompose enables queue-backed rendering through the collaboration service.
func (s *CanvasCollaborationService) WithCompose(compose *CanvasComposeService) *CanvasCollaborationService {
	s.compose = compose
	return s
}

// SubmitCompose queues an ordered selection of canvas video nodes.
func (s *CanvasCollaborationService) SubmitCompose(ctx context.Context, actor, canvas uint64, req model.CanvasComposeReq) (*CanvasComposeView, error) {
	return s.compose.Submit(ctx, actor, canvas, req)
}

// ComposeJob looks up or cancels a composition created by the current actor.
func (s *CanvasCollaborationService) ComposeJob(ctx context.Context, actor, canvas, epoch, id uint64, cancel bool) (*CanvasComposeView, error) {
	return s.compose.Job(ctx, actor, canvas, epoch, id, cancel)
}

// Submit 固定有授权的素材顺序和版本，再转交队列执行。
func (s *CanvasComposeService) Submit(ctx context.Context, actor, canvas uint64, req model.CanvasComposeReq) (*CanvasComposeView, error) {
	if !s.Available() {
		return nil, errcode.ErrCanvasNotFound
	}
	job, err := s.repo.SubmitCompose(ctx, actor, canvas, req, s.enqueue)
	if errors.Is(err, repository.ErrCanvasComposeBusy) {
		return nil, errcode.ErrTooManyTasks.WithMsg(err.Error())
	}
	if err != nil {
		return nil, collaborationError(err)
	}
	return canvasComposeView(job), nil
}

// Job 查询或取消当前成员提交的任务，取消动作阻止任何尚未确认的产物写回。
func (s *CanvasComposeService) Job(ctx context.Context, actor, canvas, epoch, id uint64, cancel bool) (*CanvasComposeView, error) {
	if s == nil {
		return nil, errcode.ErrCanvasNotFound
	}
	job, err := s.repo.ComposeJob(ctx, actor, canvas, epoch, id, cancel)
	if err != nil {
		return nil, collaborationError(err)
	}
	return canvasComposeView(job), nil
}

func canvasComposeView(job *model.CanvasComposeJob) *CanvasComposeView {
	var clips []model.CanvasComposeClip
	_ = json.Unmarshal(job.Inputs, &clips)
	view := &CanvasComposeView{ID: idcodec.Encode(job.ID), Status: job.Status, SegmentCount: len(clips), Error: job.Error, ResultNode: job.ResultNode}
	if job.AssetID != 0 {
		view.ResultAssetID = strconv.FormatUint(job.AssetID, 10)
	}
	return view
}

// HandleCanvasComposeAttempt 领取成片任务，完成后原子提交结果或记录终态。
func (s *CanvasComposeService) HandleCanvasComposeAttempt(ctx context.Context, id uint64, attempt int, final bool) error {
	job, err := s.repo.ClaimCompose(ctx, id, attempt)
	if err != nil || job == nil {
		return err
	}
	work, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	err = s.runCompose(work, job)
	if err == nil || ctx.Err() != nil {
		return err
	}
	if errors.Is(err, repository.ErrNotFound) || errors.Is(err, repository.ErrCanvasCollaborationSession) || errors.Is(err, repository.ErrCanvasMediaDraftStale) {
		return s.repo.FailCompose(ctx, job, "来源素材、权限或协作会话已变更")
	}
	if !final {
		return err
	}
	return s.repo.FailCompose(ctx, job, "成片处理失败")
}

func (s *CanvasComposeService) runCompose(ctx context.Context, job *model.CanvasComposeJob) error {
	var clips []model.CanvasComposeClip
	if err := json.Unmarshal(job.Inputs, &clips); err != nil || len(clips) == 0 {
		return errors.New("成片输入不可用")
	}
	owner, err := s.repo.CheckComposeSources(ctx, job, clips)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "vc-canvas-compose-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	paths, err := s.downloadComposeClips(ctx, owner, clips, root)
	if err != nil {
		return err
	}
	const outputName = "final.mp4"
	destination := filepath.Join(dir, outputName)
	main := make([]videoframe.ComposeClip, 0, len(paths))
	for _, path := range paths {
		main = append(main, videoframe.ComposeClip{Path: path})
	}
	if err := s.composer.ComposeTimeline(ctx, videoframe.ComposePlan{Main: main}, "", destination); err != nil {
		return err
	}
	stage, err := s.stageComposition(ctx, owner, root, outputName)
	if err != nil {
		return err
	}
	result, err := s.repo.FinishCompose(ctx, job, clips, stage, s.assets.FileURL)
	if err == nil && s.bus != nil {
		s.bus.Notice(ctx, ws.CanvasCollaborationNotice{CanvasID: idcodec.Encode(job.CanvasID), Epoch: result.Epoch, Revision: result.Revision})
	}
	return err
}

func (s *CanvasComposeService) downloadComposeClips(ctx context.Context, owner uint64, clips []model.CanvasComposeClip, root *os.Root) ([]string, error) {
	remaining := int64(composeMaxBytes)
	paths := make([]string, 0, len(clips))
	for index, clip := range clips {
		name := fmt.Sprintf("clip-%d.mp4", index)
		size, err := s.downloadCompositionSource(ctx, owner, clip.Asset, root, name, remaining)
		if err != nil {
			return nil, err
		}
		remaining -= size
		paths = append(paths, filepath.Join(root.Name(), name))
	}
	return paths, nil
}

func (s *CanvasComposeService) downloadCompositionSource(ctx context.Context, owner, id uint64, root *os.Root, name string, remaining int64) (int64, error) {
	source, err := s.assets.Open(ctx, owner, id)
	if err != nil {
		return 0, err
	}
	defer func() { _ = source.Body.Close() }()
	if source.Asset == nil || source.Asset.Kind != "video" || source.Asset.UserID != owner || source.Asset.ByteSize > remaining {
		return 0, errors.New("成片输入不是授权视频或超过容量限制")
	}
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	size, copied := io.Copy(file, io.LimitReader(&assetSrcReader{ctx: ctx, r: source.Body}, remaining+1))
	if err := errors.Join(copied, file.Close()); err != nil {
		return 0, err
	}
	if size == 0 || size > remaining {
		return 0, errors.New("成片输入为空或超过容量限制")
	}
	return size, nil
}

func (s *CanvasComposeService) stageComposition(ctx context.Context, owner uint64, root *os.Root, name string) (model.CanvasGenerationMedia, error) {
	file, err := root.Open(name)
	if err != nil {
		return model.CanvasGenerationMedia{}, err
	}
	defer func() { _ = file.Close() }()
	attempt := rand.Text()
	asset, err := s.assets.StageCanvasMedia(ctx, owner, UploadInput{FileName: "成片.mp4", Size: -1, Body: io.LimitReader(file, composeMaxBytes+1)}, func(asset *model.Asset) error {
		if asset.Kind != "video" || asset.ByteSize > composeMaxBytes {
			return errors.New("成片文件无效或超过容量限制")
		}
		return s.shares.Stage(ctx, &model.CanvasDeliveryStage{Attempt: attempt, StorageID: asset.StorageID, StorageKey: asset.StorageKey})
	})
	return model.CanvasGenerationMedia{Asset: asset, Attempt: attempt}, err
}
