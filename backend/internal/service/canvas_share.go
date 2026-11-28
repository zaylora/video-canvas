package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"video-canvas/internal/canvasdoc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// CanvasShareRepo 提供授权管理和原子交付所需的持久化操作。
type CanvasShareRepo interface {
	Stage(context.Context, *model.CanvasDeliveryStage) error
	Recipient(context.Context, string) (uint64, error)
	Grant(context.Context, uint64, uint64, uint64) error
	Remove(context.Context, uint64, uint64, uint64) error
	Members(context.Context, uint64, uint64) ([]model.CanvasShareMember, error)
	Shared(context.Context, uint64, string, int, int) ([]model.CanvasShareItem, int64, error)
	SharedCanvas(context.Context, uint64, uint64) (*model.CanvasProject, error)
	SharedCanvasRevision(context.Context, uint64, uint64) (uint64, error)
	Source(context.Context, uint64, uint64) (*model.CanvasProject, error)
	DeliverySource(context.Context, model.CanvasDelivery) (*model.CanvasProject, error)
	Claim(context.Context, model.CanvasDelivery) (bool, uint64, error)
	Release(context.Context, model.CanvasDelivery) error
	Result(context.Context, model.CanvasDelivery) (uint64, error)
	Receipt(context.Context, uint64, uint64, string) (string, error)
	Ready(context.Context, uint64, uint64) error
	WithDelivery(context.Context, model.CanvasDelivery, uint64, uint64, func(repository.CanvasShareTx) (uint64, error)) error
}

// CanvasShareAssets 提供按所有者鉴权的读取以及可回收的独立副本。
type CanvasShareAssets interface {
	Get(context.Context, uint64, uint64) (*model.Asset, error)
	Open(context.Context, uint64, uint64) (*provider.AssetFile, error)
	OpenCanvasPoster(context.Context, uint64, uint64) (*provider.AssetFile, error)
	StageCanvasAsset(context.Context, *model.Asset, uint64, func(uint64, string) error) (*model.Asset, error)
	DiscardCopies(context.Context, []*model.Asset)
	FileURL(string) string
}

// CanvasShareService 处理普通画布的只读授权、副本与所有权转移。
type CanvasShareService struct {
	repo    CanvasShareRepo
	assets  CanvasShareAssets
	reserve CanvasDeliveryGuard
	bus     *ws.CanvasCollaborationBus
}

// CanvasDeliveryGuard 在交付期间持有阻止 Agent 运行的租约。
type CanvasDeliveryGuard func(context.Context, uint64, uint64) (func(), error)

// NewCanvasShareService 组装共享仓储、素材存储和 Agent 运行保护。
func NewCanvasShareService(repo CanvasShareRepo, assets CanvasShareAssets, reserve CanvasDeliveryGuard) *CanvasShareService {
	return &CanvasShareService{repo: repo, assets: assets, reserve: reserve}
}

// WithRoomBus attaches room invalidations to authorization and ownership changes.
func (s *CanvasShareService) WithRoomBus(bus *ws.CanvasCollaborationBus) *CanvasShareService {
	s.bus = bus
	return s
}

func canvasShareError(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return errcode.ErrCanvasNotFound
	case errors.Is(err, repository.ErrRevisionConflict):
		return errcode.ErrCanvasConflict
	case errors.Is(err, repository.ErrInUse):
		return errcode.ErrCanvasDeliveryBusy
	case errors.Is(err, provider.ErrAssetNotFound), errors.Is(err, ErrAssetUnreadable), errors.Is(err, ErrAssetNotFoundByHash):
		return errcode.ErrAssetNotFound.WithMsg("画布引用的素材缺失或不可读取")
	case errors.Is(err, canvasdoc.ErrInvalid):
		return errcode.ErrCanvasPayload.WithMsg("画布中有无法独立交付的内容，请检查素材引用")
	default:
		return err
	}
}

// Share 按指定方式把普通画布共享给启用的账号。
func (s *CanvasShareService) Share(ctx context.Context, user, id uint64, req model.CanvasShareReq) error {
	recipient, err := s.repo.Recipient(ctx, strings.ToLower(strings.TrimSpace(req.Email)))
	if errors.Is(err, repository.ErrNotFound) || recipient == user {
		return errcode.ErrInvalidParams.WithMsg("请输入其他可接收用户的邮箱")
	}
	if err != nil {
		return err
	}
	if req.Mode == "view" {
		return canvasShareError(s.repo.Grant(ctx, user, id, recipient))
	}
	if (req.Mode != "copy" && req.Mode != "transfer") || req.RequestKey == "" || req.Revision == 0 {
		return errcode.ErrInvalidParams
	}
	err = s.deliver(ctx, model.CanvasDelivery{UserID: user, CanvasID: id, Recipient: recipient, Revision: req.Revision, RequestKey: req.RequestKey, Mode: req.Mode})
	if err == nil && req.Mode == "transfer" {
		s.invalidateCanvasRoom(ctx, id)
	}
	return canvasShareError(err)
}

// Receipt checks the original request without starting a new delivery.
func (s *CanvasShareService) Receipt(ctx context.Context, user, canvas uint64, key string) (string, error) {
	status, err := s.repo.Receipt(ctx, user, canvas, key)
	return status, canvasShareError(err)
}

// Members 列出当前所有者授权的查看成员。
func (s *CanvasShareService) Members(ctx context.Context, user, id uint64) ([]model.CanvasShareMember, error) {
	items, err := s.repo.Members(ctx, user, id)
	return items, canvasShareError(err)
}

// Remove 撤销单个成员的授权或退出共享。
func (s *CanvasShareService) Remove(ctx context.Context, user, id, member uint64) error {
	if err := s.repo.Remove(ctx, user, id, member); err != nil {
		return canvasShareError(err)
	}
	s.invalidateCanvasRoom(ctx, id)
	return nil
}

func (s *CanvasShareService) invalidateCanvasRoom(ctx context.Context, id uint64) {
	if s.bus != nil {
		s.bus.Notice(ctx, ws.CanvasCollaborationNotice{CanvasID: idcodec.Encode(id)})
	}
}

// Shared 按标题分页列出共享给用户的普通画布。
func (s *CanvasShareService) Shared(ctx context.Context, user uint64, keyword string, page, size int) ([]model.CanvasShareItem, int64, error) {
	return s.repo.Shared(ctx, user, keyword, (page-1)*size, size)
}

func projectedCanvas(row *model.CanvasProject) ([]byte, *shareGraph, error) {
	return projectedCanvasWith(row, canvasdoc.ProjectShared)
}

func projectedCanvasWith(row *model.CanvasProject, project func([]byte, func(string) string) ([]byte, error)) ([]byte, *shareGraph, error) {
	raw, err := project(row.PayloadJSON, func(string) string { return "" })
	if err != nil {
		return nil, nil, err
	}
	graph := &shareGraph{assets: map[uint64]*sharedAsset{}}
	if _, err = graph.payload(raw); err != nil {
		return nil, nil, err
	}
	if row.CoverAssetID != 0 && graph.assets[row.CoverAssetID] == nil {
		graph.ref(row.CoverAssetID).coverOnly = true
	}
	return raw, graph, nil
}

// Read 返回仅含已保存创作内容的授权视图。
func (s *CanvasShareService) Read(ctx context.Context, user, id uint64) (*model.CanvasProjectView, error) {
	row, err := s.repo.SharedCanvas(ctx, user, id)
	if err != nil {
		return nil, canvasShareError(err)
	}
	return canvasSharedView(row)
}

func canvasSharedView(row *model.CanvasProject) (*model.CanvasProjectView, error) {
	return canvasSharedViewWith(row, canvasdoc.ProjectShared)
}

func canvasSharedViewWith(row *model.CanvasProject, project func([]byte, func(string) string) ([]byte, error)) (*model.CanvasProjectView, error) {
	raw, graph, err := projectedCanvasWith(row, project)
	if err != nil {
		return nil, canvasShareError(err)
	}
	for asset, ref := range graph.assets {
		ref.asset = &model.Asset{ID: asset}
		ref.url = "/api/v1/canvas-media/" + idcodec.Encode(row.ID) + "/" + idcodec.Encode(asset)
	}
	view := row.View()
	view.PayloadJSON, err = graph.payload(raw)
	return view, err
}

// ReadIfChanged 检查已保存版本，仅在变化时生成共享投影。
func (s *CanvasShareService) ReadIfChanged(ctx context.Context, user, id, version uint64) (*model.CanvasProjectView, error) {
	if version != 0 {
		current, err := s.repo.SharedCanvasRevision(ctx, user, id)
		if err != nil {
			return nil, canvasShareError(err)
		}
		if version == current {
			return nil, nil
		}
	}
	return s.Read(ctx, user, id)
}

// CheckAccess 只校验访问关系，不读取画布正文。
func (s *CanvasShareService) CheckAccess(ctx context.Context, user, id uint64) error {
	_, err := s.repo.SharedCanvasRevision(ctx, user, id)
	return canvasShareError(err)
}

// Media 复核当前授权和素材引用范围后打开文件。
func (s *CanvasShareService) Media(ctx context.Context, user, id, asset uint64) (*provider.AssetFile, error) {
	return s.openMedia(ctx, user, id, asset, s.assets.Open)
}

// Poster 复核共享范围后读取视频封面，沿用原视频的查看授权。
func (s *CanvasShareService) Poster(ctx context.Context, user, id, asset uint64) (*provider.AssetFile, error) {
	return s.openMedia(ctx, user, id, asset, s.assets.OpenCanvasPoster)
}

func (s *CanvasShareService) openMedia(ctx context.Context, user, id, asset uint64, open func(context.Context, uint64, uint64) (*provider.AssetFile, error)) (*provider.AssetFile, error) {
	row, err := s.repo.SharedCanvas(ctx, user, id)
	if err != nil {
		return nil, canvasShareError(err)
	}
	return openProjectedMedia(ctx, row, asset, canvasdoc.ProjectShared, open)
}

// DownloadMedia 读取调用者当前有权查看的画布素材，并返回文件流。
func (s *CanvasShareService) DownloadMedia(ctx context.Context, user, id, asset uint64) (*provider.AssetFile, error) {
	row, err := s.repo.Source(ctx, user, id)
	if errors.Is(err, repository.ErrNotFound) {
		return s.Media(ctx, user, id, asset)
	}
	if err != nil {
		return nil, canvasShareError(err)
	}
	return openProjectedMedia(ctx, row, asset, canvasdoc.ProjectCollaboration, s.assets.Open)
}

func openProjectedMedia(ctx context.Context, row *model.CanvasProject, asset uint64, project func([]byte, func(string) string) ([]byte, error), open func(context.Context, uint64, uint64) (*provider.AssetFile, error)) (*provider.AssetFile, error) {
	_, graph, err := projectedCanvasWith(row, project)
	if err != nil {
		return nil, canvasShareError(err)
	}
	if graph.assets[asset] == nil {
		return nil, errcode.ErrAssetNotFound
	}
	file, err := open(ctx, row.UserID, asset)
	return file, canvasShareError(err)
}

func (s *CanvasShareService) deliver(ctx context.Context, request model.CanvasDelivery) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute+30*time.Second)
	defer cancel()
	request.Attempt = rand.Text()
	claimed, _, err := s.repo.Claim(ctx, request)
	if err != nil || !claimed {
		return err
	}
	err = s.prepareAndCommit(ctx, request)
	if err != nil {
		releaseErr := s.repo.Release(context.WithoutCancel(ctx), request)
		if releaseErr != nil {
			return errors.Join(err, releaseErr)
		}
	}
	return err
}

func (s *CanvasShareService) prepareAndCommit(ctx context.Context, request model.CanvasDelivery) error {
	source, graph, raw, err := s.deliverySnapshot(ctx, request)
	if err != nil {
		return err
	}
	if s.reserve != nil {
		release, err := s.reserve(ctx, request.UserID, request.CanvasID)
		if err != nil {
			return err
		}
		defer release()
	}
	if s.bus != nil && source.CollaborationActive && request.Mode == "transfer" {
		s.bus.Notice(ctx, ws.CanvasCollaborationNotice{
			CanvasID: idcodec.Encode(request.CanvasID), Epoch: source.CollaborationEpoch, TransferPending: true,
		})
	}
	copies, err := s.stageAssets(ctx, request, graph)
	if err != nil {
		s.assets.DiscardCopies(context.WithoutCancel(ctx), copies)
		return err
	}
	err = s.commitDelivery(ctx, request, source, graph, raw)
	if err == nil {
		return nil
	}
	result, lookupErr := s.repo.Result(context.WithoutCancel(ctx), request)
	if lookupErr != nil {
		return errors.Join(err, lookupErr)
	}
	if result != 0 {
		return nil
	}
	s.assets.DiscardCopies(context.WithoutCancel(ctx), copies)
	return err
}
func (s *CanvasShareService) deliverySnapshot(ctx context.Context, request model.CanvasDelivery) (*model.CanvasProject, *shareGraph, []byte, error) {
	source, err := s.repo.DeliverySource(ctx, request)
	if err != nil {
		return nil, nil, nil, err
	}
	if err = s.repo.Ready(ctx, request.UserID, request.CanvasID); err != nil {
		return nil, nil, nil, err
	}
	raw, graph, err := projectedCanvas(source)
	if err != nil {
		return nil, nil, nil, err
	}
	if err = s.checkLimits(ctx, source, graph); err != nil {
		return nil, nil, nil, err
	}

	return source, graph, raw, nil
}
func (s *CanvasShareService) commitDelivery(ctx context.Context, request model.CanvasDelivery, source *model.CanvasProject, graph *shareGraph, raw []byte) error {
	return s.repo.WithDelivery(ctx, request, source.CoverAssetID, source.CollaborationEpoch, func(tx repository.CanvasShareTx) (uint64, error) {
		for _, ref := range graph.assets {
			if ref.asset != nil {
				if err := tx.CreateAsset(ctx, ref.asset); err != nil {
					return 0, err
				}
			}
		}
		payload, err := graph.payload(raw)
		if err != nil {
			return 0, err
		}
		target := &model.CanvasProject{UserID: request.Recipient, Title: source.Title, Revision: 1, PayloadJSON: payload}
		if ref := graph.assets[source.CoverAssetID]; ref != nil && ref.asset != nil {
			target.CoverAssetID = ref.asset.ID
		}
		if err = tx.CreateCanvas(ctx, target); err != nil {
			return 0, err
		}
		return target.ID, nil
	})
}

func (s *CanvasShareService) checkLimits(ctx context.Context, source *model.CanvasProject, graph *shareGraph) error {
	limit := canvasdoc.DefaultLimits()
	var data struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	if int64(len(source.PayloadJSON)) > limit.MaxBytes || len(graph.assets) > limit.MaxAssets {
		return errcode.ErrCanvasExportTooLarge
	}
	if err := json.Unmarshal(source.PayloadJSON, &data); err != nil {
		return err
	}
	if len(data.Nodes) > limit.MaxNodes {
		return errcode.ErrCanvasExportTooLarge
	}
	var total int64
	for id, ref := range graph.assets {
		asset, err := s.assets.Get(ctx, source.UserID, id)
		if ref.coverOnly && errors.Is(err, provider.ErrAssetNotFound) {
			delete(graph.assets, id)
			continue
		}
		if err != nil {
			return err
		}
		if asset.ByteSize < 0 || asset.ByteSize > limit.MaxAssetBytes-total {
			return errcode.ErrCanvasExportTooLarge
		}
		total += asset.ByteSize
	}
	return nil
}

func (s *CanvasShareService) stageAssets(ctx context.Context, req model.CanvasDelivery, graph *shareGraph) ([]*model.Asset, error) {
	group, work := errgroup.WithContext(ctx)
	group.SetLimit(deliveryCopyConcurrency)
	for id, ref := range graph.assets {
		group.Go(func() error {
			src, err := s.assets.Get(work, req.UserID, id)
			if ref.coverOnly && errors.Is(err, provider.ErrAssetNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			a, err := s.assets.StageCanvasAsset(work, src, req.Recipient, func(storage uint64, key string) error {
				return s.repo.Stage(work, &model.CanvasDeliveryStage{StorageID: storage, StorageKey: key, Attempt: req.Attempt})
			})
			if ref.coverOnly && errors.Is(err, ErrAssetUnreadable) {
				return nil
			}
			if err != nil {
				return err
			}
			ref.asset = a
			ref.url = s.assets.FileURL(a.StorageKey)
			return nil
		})
	}
	err := group.Wait()
	copies := make([]*model.Asset, 0, len(graph.assets))
	for _, ref := range graph.assets {
		if ref.asset != nil {
			copies = append(copies, ref.asset)
		}
	}
	return copies, err
}
