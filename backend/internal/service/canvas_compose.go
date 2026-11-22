package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// canvasComposeTimeout 同时限制下载、全部转码和对象写入，避免单个成片任务无限占用 worker。
const canvasComposeTimeout = 15 * time.Minute

// composeHostKind 判断某种节点能不能承载成片：分镜表（一期入口）和成片合成节点（老画布用）。
// 成片挂载点只是节点上的 finalAssetId，跟节点种类没有强绑定，所以两种都放行、其余仍拒绝，
// 免得随便传个节点 id 就把成片写到无关节点上。
func composeHostKind(kind string) bool {
	return kind == "storyboard" || kind == "compose"
}

// CanvasComposeRepo 是成片合成要的存储：画布归属与 CAS 写回、异步任务、素材登记。
type CanvasComposeRepo interface {
	// GetCanvas 同时满足 WriteCanvas 的 CanvasReader：按用户隔离读画布。
	GetCanvas(ctx context.Context, userID, canvasID uint64) (*model.CanvasProject, error)
	CanvasOwned(ctx context.Context, userID, canvasID uint64) error
	WithTx(ctx context.Context, fn func(repository.CanvasComposeTx) error) error
	ComposeJobByID(ctx context.Context, userID, jobID uint64) (*model.CanvasComposeJob, error)
	ActiveComposeJob(ctx context.Context, userID, canvasID uint64, nodeID string) (*model.CanvasComposeJob, error)
	LastComposeJob(ctx context.Context, userID, canvasID uint64, nodeID string) (*model.CanvasComposeJob, error)
}

// CanvasComposeDeps 是成片合成服务的依赖。
type CanvasComposeDeps struct {
	Repo        CanvasComposeRepo
	Assets      DramaComposeAssets // 复用短剧的素材打开 / 上传 / 回收，只上传不登记
	Frames      DramaVideoComposer // 复用短剧的 ffmpeg 合成器，零改动
	Broadcaster ws.Broadcaster     // 为 nil 时不推送
}

// CanvasComposeService 把一组已生成的视频按给定顺序拼成一个 mp4，并把成片挂回承载它的节点
// （分镜表，或画布上的成片合成节点）。
//
// 顺序不在画布上持久化：提交时前端把有序的 assetIds 冻结进任务行，之后改顺序就是再提交一次。
// 这样画布图结构、存档格式、组语义都不用动。
type CanvasComposeService struct {
	repo   CanvasComposeRepo
	assets DramaComposeAssets
	frames DramaVideoComposer
	bc     ws.Broadcaster
}

// NewCanvasComposeService 创建画布成片合成服务。
func NewCanvasComposeService(d CanvasComposeDeps) *CanvasComposeService {
	bc := d.Broadcaster
	if bc == nil {
		bc = ws.NopBroadcaster{}
	}
	return &CanvasComposeService{repo: d.Repo, assets: d.Assets, frames: d.Frames, bc: bc}
}

// Submit 提交一次合成：校验画布归属和节点、冻结有序素材序列，然后在同一事务里插任务行并入队。
// 合成不扣积分，所以不建积分账户、不查余额、不写快照。
func (s *CanvasComposeService) Submit(ctx context.Context, userID, canvasID uint64, req *model.CanvasComposeReq) (*model.CanvasComposeResp, error) {
	ids, err := parseComposeAssetIDs(req.AssetIDs)
	if err != nil {
		return nil, err
	}
	// 成片要挂回这个节点：节点必须还在画布上且是能承载成片的种类，否则不建任务
	cv, err := s.repo.GetCanvas(ctx, userID, canvasID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrCanvasNotFound
		}
		return nil, err
	}
	g, err := canvasgraph.Parse(cv.PayloadJSON)
	if err != nil {
		return nil, errcode.ErrCanvasPayload
	}
	if node := g.Node(req.NodeID); node == nil || !composeHostKind(node.Kind()) {
		return nil, errcode.ErrCanvasComposeNoClip.WithMsg("找不到要挂成片的节点")
	}

	input, err := json.Marshal(model.CanvasComposeInput{AssetIDs: ids, Seqs: req.Seqs})
	if err != nil {
		return nil, err
	}
	job := &model.CanvasComposeJob{
		UserID: userID, CanvasID: canvasID, NodeID: req.NodeID, Mode: req.Mode,
		Status: model.ComposeJobQueued, InputJSON: input,
		Fingerprint: composeFingerprint(ids), SegmentCount: len(ids),
	}
	// 同画布同节点已有非终态任务时，部分唯一索引会拦住（返回“正在合成”）
	if err := s.repo.WithTx(ctx, func(tx repository.CanvasComposeTx) error { return tx.CreateComposeJob(ctx, job) }); err != nil {
		if errors.Is(err, repository.ErrComposeJobBusy) {
			return nil, errcode.ErrCanvasComposeBusy
		}
		return nil, err
	}
	return composeJobView(job), nil
}

// HandleCanvasCompose 是队列 worker 的入口：接单 → 下载合成上传 → 单事务写回 → 推送。
// 只有基础设施错误才返回 error 让队列重试；业务失败落成任务失败并返回 nil。
func (s *CanvasComposeService) HandleCanvasCompose(ctx context.Context, jobID uint64) error {
	var start *model.CanvasComposeJob
	if err := s.repo.WithTx(ctx, func(tx repository.CanvasComposeTx) error {
		job, err := tx.ComposeJobForUpdate(ctx, jobID)
		if err != nil {
			return err
		}
		// 只挡终态：running 必须可重入。否则进程在合成途中崩溃后，重投会因为“已不是 queued”直接返回，
		// 任务永久悬挂并占死部分唯一索引。
		if !composeJobActive(job.Status) || job.ResultAssetID != nil {
			return nil
		}
		start = job
		start.Status = model.ComposeJobRunning
		start.Attempts = job.Attempts + 1
		return tx.UpdateComposeJob(ctx, jobID, map[string]any{
			"status": model.ComposeJobRunning, "attempts": start.Attempts,
		})
	}); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil // 任务被删了
		}
		return err
	}
	if start == nil {
		return nil
	}
	s.publish(ctx, start)

	ctx, run := logger.BeginRun(ctx, logger.RunRef{Kind: "canvas_compose", ID: idcodec.Encode(jobID), UserID: start.UserID})
	run.Start(zap.String("node_id", start.NodeID), zap.Uint64("canvas_id", start.CanvasID), zap.Int("segments", start.SegmentCount))

	asset, durationMS, err := s.prepare(ctx, start)
	if err != nil {
		// 上下文被取消（服务退出）时交回队列，不算业务失败
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return s.failJob(ctx, run, start, composeError(err))
	}
	if _, err := s.commit(ctx, start, asset, durationMS); err != nil {
		// 节点被删或合成被取消都是业务终态；其余是基础设施错误，交回队列（对象由 defer 回收）
		if errors.Is(err, repository.ErrRevisionConflict) {
			return err
		}
		if isComposeBizError(err) {
			return s.failJob(ctx, run, start, err)
		}
		return err
	}
	run.End(logger.StatusSucceeded, zap.Int64("duration_ms", durationMS), zap.Uint64("asset_id", asset.ID))
	return nil
}

// prepare 在事务外做重活：逐段下载 → ffmpeg 合成 → 上传成片对象（不登记素材行）。
func (s *CanvasComposeService) prepare(ctx context.Context, job *model.CanvasComposeJob) (*model.Asset, int64, error) {
	var input model.CanvasComposeInput
	if err := json.Unmarshal(job.InputJSON, &input); err != nil {
		return nil, 0, errcode.ErrCanvasComposeFailed
	}
	if len(input.AssetIDs) == 0 {
		return nil, 0, errcode.ErrCanvasComposeNoClip
	}
	ctx, cancel := context.WithTimeout(ctx, canvasComposeTimeout)
	defer cancel()

	dir, err := os.MkdirTemp("", "vc-canvas-compose-*")
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	paths := make([]string, 0, len(input.AssetIDs))
	var durationMS int64
	for i, id := range input.AssetIDs {
		path := filepath.Join(dir, fmt.Sprintf("input-%06d.mp4", i))
		ms, err := s.download(ctx, job.UserID, id, path)
		if err != nil {
			return nil, 0, err
		}
		durationMS += ms
		paths = append(paths, path)
	}
	dst := filepath.Join(dir, "final.mp4")
	if err := s.frames.Compose(ctx, paths, dst); err != nil {
		return nil, 0, fmt.Errorf("合成失败：%w", err)
	}
	asset, err := s.assets.PrepareDramaVideo(ctx, job.UserID, dst)
	if err != nil {
		return nil, 0, fmt.Errorf("成片存储失败：%w", err)
	}
	return asset, durationMS, nil
}

// download 从用户素材存储下载一段到临时目录，沿用素材转存的大小与取消约束。
func (s *CanvasComposeService) download(ctx context.Context, userID, assetID uint64, path string) (int64, error) {
	file, err := s.assets.Open(ctx, userID, assetID)
	if err != nil {
		if errors.Is(err, provider.ErrAssetNotFound) {
			return 0, errcode.ErrCanvasComposeNoClip.WithMsg("有片段素材已不存在，请重新生成后再合成")
		}
		return 0, err
	}
	defer func() { _ = file.Body.Close() }()
	if file.Asset == nil || file.Asset.UserID != userID || file.Asset.Kind != "video" {
		return 0, errcode.ErrCanvasComposeNoClip.WithMsg("只能合成当前用户的视频素材")
	}
	f, err := os.Create(path) //nolint:gosec // path 完全由服务端临时目录和序号组成。
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(f, io.LimitReader(&assetSrcReader{ctx: ctx, r: file.Body}, defaultMaxResult+1))
	closeErr := f.Close()
	if copyErr != nil {
		return 0, copyErr
	}
	if n == 0 || n > defaultMaxResult {
		return 0, errcode.ErrCanvasComposeNoClip.WithMsg("片段视频为空或超过大小限制")
	}
	if closeErr != nil {
		return 0, closeErr
	}
	return file.Asset.DurationMs, nil
}

// commit 把成片资产登记、任务终态和画布写回放进同一个事务（评审 A10）：
// WriteCanvas 每次重试都会重读画布，apply 必须自带事务与幂等，否则 CAS 冲突会留下“成片已落库、节点没引用”的孤儿。
func (s *CanvasComposeService) commit(ctx context.Context, job *model.CanvasComposeJob, asset *model.Asset, durationMS int64) (uint64, error) {
	c := &composeCommit{ctx: ctx, svc: s, job: job, asset: asset, durationMS: durationMS}
	defer func() {
		if !c.committed {
			s.assets.DiscardCopies(context.WithoutCancel(ctx), []*model.Asset{asset})
		}
	}()
	if _, err := WriteCanvas(ctx, s.repo, job.UserID, job.CanvasID, c.apply); err != nil {
		return 0, err
	}
	c.publish()
	return asset.ID, nil
}

// composeCommit 是一次提交的状态。WriteCanvas 可能因 CAS 冲突重跑闭包：
// committed 要跨重试保留（重投时不再重复登记素材），素材主键则每次重试都要重新分配。
type composeCommit struct {
	ctx        context.Context
	svc        *CanvasComposeService
	job        *model.CanvasComposeJob
	asset      *model.Asset
	durationMS int64
	committed  bool
	changes    []canvasgraph.Change
	revBefore  uint64
	revAfter   uint64
}

// apply 是 WriteCanvas 的闭包：已经提交过的重投直接放过，其余在事务里写素材、任务终态与画布。
func (c *composeCommit) apply(g *canvasgraph.Graph, rev uint64) (struct{}, error) {
	if c.job.ResultAssetID != nil {
		c.committed = true // 重投：上次已提交，不再重复登记素材
		return struct{}{}, nil
	}
	err := c.svc.repo.WithTx(c.ctx, func(tx repository.CanvasComposeTx) error {
		return c.applyTx(tx, g, rev)
	})
	return struct{}{}, err
}

func (c *composeCommit) applyTx(tx repository.CanvasComposeTx, g *canvasgraph.Graph, rev uint64) error {
	current, err := tx.ComposeJobForUpdate(c.ctx, c.job.ID)
	if err != nil {
		return err
	}
	if current.Status == model.ComposeJobCanceled {
		return errcode.ErrCanvasComposeFailed.WithMsg("合成已取消")
	}
	// 先登记素材拿到主键，再写 finalAssetId：顺序反了会把 0 写进画布，
	// 表现为成片其实已生成、节点却一直显示没有成片（asset.ID 在 CreateAsset 之前恒为 0）。
	// 主键归零是因为 WriteCanvas 遇 CAS 冲突会重跑这段，上一次事务已回滚、ID 要重新分配。
	c.asset.ID = 0
	if err := tx.CreateAsset(c.ctx, c.asset); err != nil {
		return err
	}
	g2, diff, err := composeCanvasPatch(g, c.job, c.asset.ID, c.durationMS)
	if err != nil {
		return err
	}
	if err := tx.UpdateComposeJob(c.ctx, c.job.ID, composeJobSucceeded(c.asset.ID, c.durationMS, c.job.SegmentCount)); err != nil {
		return err
	}
	if c.revAfter, err = commitComposeCanvas(c.ctx, tx, c.job.UserID, c.job.CanvasID, rev, g2, diff); err != nil {
		return err
	}
	c.changes, c.revBefore, c.committed = diff, rev, true
	return nil
}

// publish 把任务切到成功并把画布补丁推给前端：画布改动用与 Agent 相同格式的补丁，
// 任务状态另走自己的事件。
func (c *composeCommit) publish() {
	c.job.Status = model.ComposeJobSucceeded
	c.job.ResultAssetID = &c.asset.ID
	c.job.DurationMS = c.durationMS
	PublishCanvasPatch(c.ctx, c.svc.bc, c.job.UserID, CanvasPatch{
		CanvasID: c.job.CanvasID, Kind: "compose",
		RevisionBefore: c.revBefore, RevisionAfter: c.revAfter, Changes: c.changes,
	})
	c.svc.publish(c.ctx, c.job)
}

// composeCanvasPatch 深拷贝画布并把成片写回节点，返回新图与差异。
// Node.Data() 返回底层 map 的引用，必须先深拷贝，否则 Diff(g, g2) 恒为空、画布不会被写。
func composeCanvasPatch(
	g *canvasgraph.Graph, job *model.CanvasComposeJob, assetID uint64, durationMS int64,
) (*canvasgraph.Graph, []canvasgraph.Change, error) {
	g2 := g.Clone()
	node := g2.Node(job.NodeID)
	if node == nil {
		return nil, nil, errcode.ErrCanvasComposeNoClip.WithMsg("承载成片的节点已被删除，成片没有挂回去")
	}
	data := node.Data()
	data["finalAssetId"] = strconv.FormatUint(assetID, 10)
	data["finalFingerprint"] = job.Fingerprint
	data["finalDurationMs"] = float64(durationMS)
	data["finalSegmentCount"] = float64(job.SegmentCount)
	return g2, canvasgraph.Diff(g, g2), nil
}

// composeJobSucceeded 是一次成功落库要写的字段。
func composeJobSucceeded(assetID uint64, durationMS int64, segments int) map[string]any {
	return map[string]any{
		"status":          model.ComposeJobSucceeded,
		"result_asset_id": assetID,
		"duration_ms":     durationMS,
		"segment_count":   segments,
		"error":           "",
	}
}

// failJob 业务失败：落错误、不扣费、推送；总是返回 nil（同样的输入重试结果一样）。
func (s *CanvasComposeService) failJob(ctx context.Context, run *logger.Run, job *model.CanvasComposeJob, bizErr error) error {
	msg := bizErr.Error()
	if err := s.repo.WithTx(ctx, func(tx repository.CanvasComposeTx) error {
		return tx.UpdateComposeJob(ctx, job.ID, map[string]any{"status": model.ComposeJobFailed, "error": msg})
	}); err != nil {
		return err
	}
	job.Status = model.ComposeJobFailed
	job.Error = msg
	run.End(logger.StatusFailed, zap.String("error", msg))
	s.publish(ctx, job)
	return nil
}

// Active 读某个节点上进行中的任务，并给出成片是否过期。
// currentAssetIDs 是前端当前准备合成的素材序列（原样传入，服务端自己算指纹）；stale 由服务端比对，前端零计算。
func (s *CanvasComposeService) Active(ctx context.Context, userID, canvasID uint64, nodeID string, currentAssetIDs []string) (*model.CanvasComposeResp, error) {
	job, err := s.repo.ActiveComposeJob(ctx, userID, canvasID, nodeID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if job == nil {
		// 没有进行中的任务时，退回到最近一次成功的任务，仍然告诉前端成片是否过期
		last, lerr := s.repo.LastComposeJob(ctx, userID, canvasID, nodeID)
		if lerr != nil {
			if errors.Is(lerr, repository.ErrNotFound) {
				return nil, nil
			}
			return nil, lerr
		}
		job = last
	}
	v := composeJobView(job)
	stale := false
	if ids, perr := parseComposeAssetIDs(currentAssetIDs); perr == nil {
		stale = job.Fingerprint != composeFingerprint(ids)
	}
	v.Stale = &stale
	s.attachFinal(ctx, v, job)
	return v, nil
}

// Job 按 id 读任务摘要（断线对账用）；别人的任务按不存在处理。
func (s *CanvasComposeService) Job(ctx context.Context, userID, jobID uint64) (*model.CanvasComposeResp, error) {
	job, err := s.repo.ComposeJobByID(ctx, userID, jobID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrCanvasComposeJobNotFound
		}
		return nil, err
	}
	v := composeJobView(job)
	s.attachFinal(ctx, v, job)
	return v, nil
}

// attachFinal 给成功的任务补上成片地址；拿不到地址不影响任务状态本身的返回。
func (s *CanvasComposeService) attachFinal(ctx context.Context, v *model.CanvasComposeResp, job *model.CanvasComposeJob) {
	if job.ResultAssetID == nil || v.ResultAssetURL != "" {
		return
	}
	view, err := s.assets.View(ctx, job.UserID, *job.ResultAssetID)
	if err != nil || view == nil {
		return
	}
	v.ResultAssetURL = view.URL
	if v.DurationMS == 0 {
		v.DurationMS = view.DurationMs
	}
}

// Cancel 取消一条还没开始执行的任务；已经在跑的交给 worker 在提交前发现并放弃。
func (s *CanvasComposeService) Cancel(ctx context.Context, userID, canvasID, jobID uint64) error {
	return s.repo.WithTx(ctx, func(tx repository.CanvasComposeTx) error {
		job, err := tx.ComposeJobForUpdate(ctx, jobID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return errcode.ErrCanvasComposeJobNotFound
			}
			return err
		}
		if job.UserID != userID || job.CanvasID != canvasID {
			return errcode.ErrCanvasComposeJobNotFound
		}
		if !composeJobActive(job.Status) {
			return nil // 已终态，取消是空操作
		}
		return tx.UpdateComposeJob(ctx, jobID, map[string]any{"status": model.ComposeJobCanceled})
	})
}

// publish 向用户频道推一条合成状态变化；数据就是接口返回的那份任务快照。
func (s *CanvasComposeService) publish(ctx context.Context, job *model.CanvasComposeJob) {
	v := composeJobView(job)
	s.attachFinal(ctx, v, job)
	channel := ws.UserChannel(job.UserID)
	s.bc.Publish(ctx, channel, ws.Message{Type: ws.TypeCanvasCompose, Channel: channel, Data: v})
}

// commitComposeCanvas 有差异时 CAS 写回画布并返回新 revision；无差异保持原 revision。
// 与短剧的 commitCanvas 同语义，只是这里用合成任务自己的事务接口。
func commitComposeCanvas(ctx context.Context, tx repository.CanvasComposeTx, userID, canvasID, revision uint64, g *canvasgraph.Graph, changes []canvasgraph.Change) (uint64, error) {
	if len(changes) == 0 {
		return revision, nil
	}
	payload, err := g.Marshal()
	if err != nil {
		return revision, err
	}
	if err := tx.UpdateCanvas(ctx, userID, canvasID, revision, payload); err != nil {
		return revision, err
	}
	return revision + 1, nil
}

// parseComposeAssetIDs 解析并校验有序素材列表：接受画布节点里存的十进制素材 id，也接受接口编码 id。
// 保持原顺序（同一素材可复用多镜），只拒绝空值和非法项。
func parseComposeAssetIDs(raw []string) ([]uint64, error) {
	out := make([]uint64, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, ok := decodeAssetRef(s)
		if !ok {
			return nil, errcode.ErrInvalidParams.WithMsg("素材 id 格式错误")
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, errcode.ErrCanvasComposeNoClip
	}
	return out, nil
}

// decodeAssetRef 同时接受接口编码 id 和画布节点里的十进制 id。
func decodeAssetRef(s string) (uint64, bool) {
	if id, ok := idcodec.Decode(s); ok {
		return id, true
	}
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

// composeFingerprint 是输入素材序列的指纹：顺序变了指纹就变，用来判定成片是否过期。
func composeFingerprint(ids []uint64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatUint(id, 10)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return hex.EncodeToString(sum[:])
}

// composeJobActive 判断任务是否还在非终态。
func composeJobActive(status string) bool {
	return status == model.ComposeJobQueued || status == model.ComposeJobRunning
}

// isComposeBizError 判断是不是应该落成任务失败（而不是交给队列重试）的业务错误。
func isComposeBizError(err error) bool {
	var e *errcode.Error
	if errors.As(err, &e) {
		return true
	}
	return errors.Is(err, repository.ErrNotFound)
}

// composeError 把执行期错误换成给用户看的那句。
func composeError(err error) error {
	var e *errcode.Error
	if errors.As(err, &e) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errcode.ErrCanvasComposeFailed.WithMsg("合成超时，片段可能太长，请减少镜头后重试")
	}
	return errcode.ErrCanvasComposeFailed
}

// composeJobView 把任务行转成接口 / 推送用的结构。
func composeJobView(job *model.CanvasComposeJob) *model.CanvasComposeResp {
	v := &model.CanvasComposeResp{
		ID: idcodec.Encode(job.ID), CanvasID: idcodec.Encode(job.CanvasID), NodeID: job.NodeID,
		Mode: job.Mode, Status: job.Status, Fingerprint: job.Fingerprint,
		DurationMS: job.DurationMS, SegmentCount: job.SegmentCount, Error: job.Error,
	}
	if job.ResultAssetID != nil {
		v.ResultAssetID = strconv.FormatUint(*job.ResultAssetID, 10)
	}
	return v
}
