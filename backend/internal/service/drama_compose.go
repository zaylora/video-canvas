package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// dramaComposeTimeout 同时限制下载、全部转码和对象写入，避免整集任务无限占用 worker。
const dramaComposeTimeout = 10 * time.Minute

// DramaVideoComposer 统一片段编码并按顺序拼接，由 videoframe.Extractor 实现。
type DramaVideoComposer interface {
	// Compose 将本地 sources 拼接到 destination，调用方负责最终成片文件清理。
	Compose(context.Context, []string, string) error
}

// DramaComposeAssets 复用素材存储，未提交对象和数据库登记分离以保证取消与重试安全。
type DramaComposeAssets interface {
	// Open 只打开当前用户素材，返回值的 Body 由调用方关闭。
	Open(context.Context, uint64, uint64) (*provider.AssetFile, error)
	// PrepareDramaVideo 上传本地产物但不插入素材行，事务提交后才能公开引用。
	PrepareDramaVideo(context.Context, uint64, string) (*model.Asset, error)
	// DiscardCopies 清理尚未提交的对象，复用画布导入的失败回收路径。
	DiscardCopies(context.Context, []*model.Asset)
	// View 读取当前用户素材的稳定视图。
	View(context.Context, uint64, uint64) (*model.AssetView, error)
	// ViewOf 为已经校验归属的素材生成稳定视图，不做网络请求。
	ViewOf(context.Context, *model.Asset) (*model.AssetView, error)
}

// dramaComposeInput 固定提交时的采用、跳过列表与片段版本，重试不能改用后来选中的 take。
type dramaComposeInput struct {
	EpisodeID      uint64   `json:"episode_id"`
	SegmentVersion int64    `json:"segment_version"`
	AssetIDs       []uint64 `json:"asset_ids"`
	SkippedSeqs    []int    `json:"skipped_seqs"`
}

// estimateCompose 与提交使用同一项目锁和片段校验，完全不解析模型或访问积分账户。
func (s *DramaService) estimateCompose(ctx context.Context, userID, dramaID uint64, req *model.DramaJobReq) (int, error) {
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		p, err := tx.Project(ctx, userID, dramaID, true)
		if err != nil {
			return err
		}
		eps, err := s.prepareDramaJob(ctx, tx, p, req)
		if err != nil {
			return err
		}
		_, err = composeInput(ctx, tx, p, eps[0])
		return err
	})
	return 0, dramaError(err)
}

// enqueueCompose 直接入队零费用任务，不建积分账户、流水或模型快照。
func enqueueCompose(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, req *model.DramaJobReq, eps []model.DramaEpisode, approvalID *uint64) (*model.DramaJob, error) {
	input, err := composeInput(ctx, tx, p, eps[0])
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	job := &model.DramaJob{UserID: p.UserID, DramaID: p.ID, Step: "compose", Episodes: append([]int{}, req.Episodes...), Status: "queued", ConfigSnapshot: []byte(`{}`), InputJSON: raw, Output: []byte(`{}`), ApprovalID: approvalID}
	if err := tx.InsertJob(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

// composeInput 只跳过没有采用的片段；非法、非成功或跨节点的采用不能静默混入成片。
func composeInput(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, ep model.DramaEpisode) (*dramaComposeInput, error) {
	rows, err := tx.Segments(ctx, p.UserID, ep.ID)
	if err != nil {
		return nil, err
	}
	input := &dramaComposeInput{EpisodeID: ep.ID, SegmentVersion: ep.SegmentVersion, AssetIDs: []uint64{}, SkippedSeqs: []int{}}
	for _, row := range rows {
		if row.ChosenTaskID == nil {
			input.SkippedSeqs = append(input.SkippedSeqs, row.Seq)
			continue
		}
		tasks, err := tx.SegmentTasks(ctx, p.UserID, p.CanvasID, segmentNode(row.ID))
		if err != nil {
			return nil, err
		}
		id, err := composeChosenAsset(tasks, *row.ChosenTaskID)
		if err != nil {
			return nil, err
		}
		input.AssetIDs = append(input.AssetIDs, id)
	}
	if len(input.AssetIDs) == 0 {
		return nil, errcode.ErrDramaNothingToCompose
	}
	return input, nil
}

// composeChosenAsset 复用片段任务输出解析，成功任务也必须有已转存的视频素材。
func composeChosenAsset(tasks []model.GenerationTask, chosen uint64) (uint64, error) {
	for i := range tasks {
		if tasks[i].ID != chosen || tasks[i].Kind != model.KindVideo || tasks[i].Status != model.TaskSucceeded {
			continue
		}
		id, err := segmentOutputAsset(&tasks[i], model.KindVideo)
		if err != nil {
			return 0, err
		}
		if id > 0 {
			return id, nil
		}
	}
	return 0, errcode.ErrInternal.WithMsg("采用视频不可用，请重新采用片段")
}

// handleCompose 将重型文件处理放在事务外，成片素材、集引用和任务成功原子提交。
func (s *DramaService) handleCompose(ctx context.Context, run *dramaRun) error {
	// 1. 限制整次处理时间，内部超时使用原 ctx 写失败，worker 退出取消则留给队列恢复。
	workCtx, cancel := context.WithTimeout(ctx, dramaComposeTimeout)
	defer cancel()
	asset, input, err := s.renderCompose(workCtx, run.job)
	if err != nil {
		return s.failDrama(ctx, run.job, err)
	}
	// 2. 未被事务采纳的对象一律回收，包含取消、重复投递和数据库提交失败。
	committed := false
	defer func() {
		if !committed {
			s.assets.DiscardCopies(context.WithoutCancel(ctx), []*model.Asset{asset})
		}
	}()
	committed, err = s.commitCompose(ctx, run.job, asset, input)
	return err
}

// renderCompose 下载和转码阶段只返回固定原因文案，避免暴露存储地址或 ffmpeg 本地路径。
func (s *DramaService) renderCompose(ctx context.Context, job *model.DramaJob) (*model.Asset, *dramaComposeInput, error) {
	if s.compose == nil || s.assets == nil {
		return nil, nil, errcode.ErrInternal.WithMsg("转码失败：合成服务未配置")
	}
	var input dramaComposeInput
	if err := json.Unmarshal(job.InputJSON, &input); err != nil || len(input.AssetIDs) == 0 {
		return nil, nil, errcode.ErrInternal.WithMsg("转码失败：合成输入不可用")
	}
	dir, err := os.MkdirTemp("", "vc-drama-compose-*")
	if err != nil {
		return nil, nil, errcode.ErrInternal.WithMsg("转码失败：无法创建临时目录")
	}
	defer func() { _ = os.RemoveAll(dir) }() // 临时文件没有外部引用，所有退出路径均清理。
	paths := make([]string, 0, len(input.AssetIDs))
	for i, id := range input.AssetIDs {
		path := filepath.Join(dir, fmt.Sprintf("input-%d.mp4", i))
		if err := s.downloadCompose(ctx, job.UserID, id, path); err != nil {
			return nil, nil, errcode.ErrInternal.WithMsg("下载失败：无法读取采用视频")
		}
		paths = append(paths, path)
		if err := s.dramaProgress(ctx, job, (i+1)*40/len(input.AssetIDs)); err != nil {
			return nil, nil, err
		}
	}
	dst := filepath.Join(dir, "final.mp4")
	if err := s.compose.Compose(ctx, paths, dst); err != nil {
		return nil, nil, errcode.ErrInternal.WithMsg("转码失败：无法合成视频")
	}
	if err := s.dramaProgress(ctx, job, 80); err != nil {
		return nil, nil, err
	}
	asset, err := s.assets.PrepareDramaVideo(ctx, job.UserID, dst)
	if err != nil {
		return nil, nil, errcode.ErrInternal.WithMsg("转码失败：成片存储失败")
	}
	return asset, &input, nil
}

// downloadCompose 从用户素材存储下载到临时目录，沿用素材转存的大小和取消约束。
func (s *DramaService) downloadCompose(ctx context.Context, userID, assetID uint64, path string) error {
	file, err := s.assets.Open(ctx, userID, assetID)
	if err != nil {
		return err
	}
	defer func() { _ = file.Body.Close() }()
	if file.Asset == nil || file.Asset.UserID != userID || file.Asset.Kind != "video" {
		return errors.New("采用素材不是当前用户的视频")
	}
	f, err := os.Create(path) //nolint:gosec // path 完全由服务端临时目录和序号组成。
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, io.LimitReader(&assetSrcReader{ctx: ctx, r: file.Body}, defaultMaxResult+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if n == 0 || n > defaultMaxResult {
		return errors.New("采用视频为空或超过大小限制")
	}
	return closeErr
}

// commitCompose 重新读取当前集版本，合成期间修改过片段时成片立即显示过期。
func (s *DramaService) commitCompose(ctx context.Context, job *model.DramaJob, asset *model.Asset, input *dramaComposeInput) (bool, error) {
	committed := false
	var completed *model.DramaJob
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		var err error
		completed, committed, err = s.commitComposeTx(ctx, tx, job, asset, input)
		return err
	})
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil // 项目或集已删除时不发布成片；调用方仍回收临时对象。
	}
	if err != nil {
		return false, err
	}
	s.publish(ctx, completed)
	return committed, nil
}

// commitComposeTx 在项目锁内登记素材、写集成片并结束任务；任务已被取消时什么都不写。
func (s *DramaService) commitComposeTx(ctx context.Context, tx repository.DramaTx, job *model.DramaJob, asset *model.Asset, input *dramaComposeInput) (*model.DramaJob, bool, error) {
	if _, err := tx.Project(ctx, job.UserID, job.DramaID, true); err != nil {
		return nil, false, err
	}
	current, err := tx.Job(ctx, job.UserID, job.DramaID, job.ID, true)
	if err != nil || !dramaActive(current.Status) {
		return current, false, err
	}
	ep, err := tx.SegmentEpisode(ctx, job.UserID, input.EpisodeID)
	if err != nil {
		return nil, false, err
	}
	if asset.UserID != job.UserID || ep.DramaID != job.DramaID {
		return nil, false, repository.ErrNotFound
	}
	if err := tx.CreateAsset(ctx, asset); err != nil {
		return nil, false, err
	}
	ep.FinalAssetID, ep.FinalSegmentVersion = &asset.ID, input.SegmentVersion
	if err := tx.SaveEpisode(ctx, ep, ep.Revision); err != nil {
		return nil, false, err
	}
	view, err := s.assets.ViewOf(ctx, asset)
	if err != nil {
		return nil, false, err
	}
	result := model.DramaComposeResult{Asset: *view, SkippedSeqs: input.SkippedSeqs}
	if err := s.finishTx(ctx, tx, current, "succeeded", 0, "", 0, result); err != nil {
		return nil, false, err
	}
	return current, true, nil
}
