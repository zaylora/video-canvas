package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"video-canvas/internal/imageproc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

const (
	processorNameMaxRunes = 64
	// processorCacheTTL 是“存储 → 已发布处理服务”的缓存时长。本实例的发布 / 停用 / 回滚 / 删除会立刻让它失效，
	// 多实例部署时其他实例最多等这么久。
	processorCacheTTL = 30 * time.Second
)

// ImageProcessorRepo 是图片处理服务的数据访问接口，由 repository.ImageProcessorRepository 实现。
type ImageProcessorRepo interface {
	// Create 新建处理服务；名称重复返回 repository.ErrDuplicate。
	Create(ctx context.Context, p *model.ImageProcessor) error
	// GetByID 按 id 查询，不存在返回 repository.ErrNotFound。
	GetByID(ctx context.Context, id uint64) (*model.ImageProcessor, error)
	// List 返回全部处理服务，按 id 升序。
	List(ctx context.Context) ([]model.ImageProcessor, error)
	// GetPublishedByStorage 返回绑定该存储的已发布处理服务，没有返回 repository.ErrNotFound。
	GetPublishedByStorage(ctx context.Context, storageID uint64) (*model.ImageProcessor, error)
	// Update 带乐观锁更新并把 version +1：不存在返回 ErrNotFound，版本不一致返回 ErrRevisionConflict，名称重复返回 ErrDuplicate。
	Update(ctx context.Context, id uint64, version int, fields map[string]any) error
	// SetCheck 记录最近一次校验与试跑的结果，不改 version，不存在返回 ErrNotFound。
	SetCheck(ctx context.Context, id uint64, result model.JSONText) error
	// Publish 事务内发布：核对 version（不一致返回 ErrRevisionConflict）、停用同存储的旧服务、写发布历史，返回新的发布版本号。
	Publish(ctx context.Context, in repository.PublishParams) (int, error)
	// Rollback 回滚到更早的发布版本并返回版本号；处理服务不存在或没有更早的版本都返回 ErrNotFound。
	Rollback(ctx context.Context, id uint64) (int, error)
	// Disable 停用已发布的处理服务，不存在或不是已发布状态返回 ErrNotFound。
	Disable(ctx context.Context, id uint64) error
	// Delete 删除处理服务与它的发布历史，不存在返回 ErrNotFound。
	Delete(ctx context.Context, id uint64) error
	// PreviousVersions 返回已发布处理服务各自可回滚到的上一个版本号；ids 为空表示全部。
	PreviousVersions(ctx context.Context, ids ...uint64) (map[uint64]int, error)
	// SampleAsset 返回该存储里最新的一个指定种类素材，用来试跑；没有返回 ErrNotFound。
	SampleAsset(ctx context.Context, storageID uint64, kind string) (*model.Asset, error)
}

// ProcessorStorages 是处理服务读取存储配置的依赖，由 repository.StorageConfigRepository 实现。
type ProcessorStorages interface {
	// GetByID 按 id 查询存储，不存在返回 repository.ErrNotFound。
	GetByID(ctx context.Context, id uint64) (*model.StorageConfig, error)
	// List 返回全部存储。
	List(ctx context.Context) ([]model.StorageConfig, error)
}

// ProcessorHandles 是处理服务取存储句柄（含解密后的凭证）的依赖，由 storage.Registry 实现。
type ProcessorHandles interface {
	// Get 按 id 返回存储，不存在返回 storage.ErrStorageNotFound。
	Get(ctx context.Context, id uint64) (*storage.Handle, error)
}

// ImageProcessorService 管理图片处理服务（草稿 → 校验 → 试跑 → 发布 / 回滚 / 停用），
// 并在运行时为素材解析变体地址（Resolve，供 AssetService 的 /files?v= 使用）。
type ImageProcessorService struct {
	repo     ImageProcessorRepo
	storages ProcessorStorages
	handles  ProcessorHandles
	fetcher  ProcessorFetcher
	now      func() time.Time

	mu    sync.Mutex
	cache map[uint64]resolvedEntry // 存储 id → 已发布的处理服务（nil 表示没有）
}

// NewImageProcessorService 创建图片处理服务管理。
func NewImageProcessorService(repo ImageProcessorRepo, storages ProcessorStorages, handles ProcessorHandles, fetcher ProcessorFetcher) *ImageProcessorService {
	return &ImageProcessorService{repo: repo, storages: storages, handles: handles, fetcher: fetcher, now: time.Now, cache: map[uint64]resolvedEntry{}}
}

// ProcessorView 是返回给后台的处理服务视图。
type ProcessorView struct {
	ID               uint64            `json:"id"`
	Name             string            `json:"name"`
	Vendor           string            `json:"vendor"`
	StorageID        uint64            `json:"storage_id"`
	StorageName      string            `json:"storage_name"`
	StorageProvider  string            `json:"storage_provider"`
	Status           string            `json:"status"`
	Config           imageproc.Config  `json:"config"`            // 工作配置（草稿）
	PublishedConfig  *imageproc.Config `json:"published_config"`  // 线上正在用的配置，从未发布为 nil
	HasDraft         bool              `json:"has_draft"`         // 已发布，且工作配置与线上配置不同
	PublishedVersion int               `json:"published_version"` // 线上版本号，从未发布为 0
	PreviousVersion  *int              `json:"previous_version"`  // 可回滚到的上一个版本号，没有为 nil
	Version          int               `json:"version"`           // 乐观锁版本
	Check            *ProcessorCheck   `json:"check"`             // 最近一次校验与试跑，没做过为 nil
	UpdatedAt        time.Time         `json:"updated_at"`
	CreatedAt        time.Time         `json:"created_at"`
}

// ProcessorCreateInput 是新建处理服务的参数。
type ProcessorCreateInput struct {
	Name      string
	Vendor    string
	StorageID uint64
	Config    imageproc.Config
}

// ProcessorUpdateInput 是保存草稿的参数（整份提交，厂商与存储创建后不可改）。
type ProcessorUpdateInput struct {
	Version int // 读到的版本，乐观锁
	Name    string
	Config  imageproc.Config
}

// Presets 返回全部厂商预设，给后台表单用。
func (s *ImageProcessorService) Presets() []imageproc.Preset { return imageproc.Presets() }

// List 返回全部处理服务（含绑定的存储名、可回滚版本）。
func (s *ImageProcessorService) List(ctx context.Context) ([]ProcessorView, error) {
	// 1. 读处理服务、存储与可回滚版本；存储按 id 建索引，避免每行再查一次
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	stores, err := s.storages.List(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint64]*model.StorageConfig, len(stores))
	for i := range stores {
		byID[stores[i].ID] = &stores[i]
	}
	prev, err := s.repo.PreviousVersions(ctx)
	if err != nil {
		return nil, err
	}

	// 2. 组装视图
	out := make([]ProcessorView, 0, len(rows))
	for i := range rows {
		v, err := s.view(&rows[i], byID[rows[i].StorageID], prev)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// Get 返回一个处理服务，不存在返回 52001。
func (s *ImageProcessorService) Get(ctx context.Context, id uint64) (*ProcessorView, error) {
	p, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.viewOf(ctx, p)
}

// Create 新建一个草稿处理服务。
func (s *ImageProcessorService) Create(ctx context.Context, actorID uint64, in ProcessorCreateInput) (*ProcessorView, error) {
	// 1. 基本校验：名称、厂商
	name, err := processorName(in.Name)
	if err != nil {
		return nil, err
	}
	if _, ok := imageproc.PresetOf(in.Vendor); !ok {
		return nil, errcode.ErrProcessorInvalid.WithMsg(fmt.Sprintf("不支持的处理服务厂商 %q", in.Vendor))
	}

	// 2. 存储必须存在，并且与厂商匹配（腾讯云只能绑 COS、阿里云只能绑 OSS、Cloudflare 只能绑有公开域名的 R2）
	st, err := s.storages.GetByID(ctx, in.StorageID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrStorageNotFound
		}
		return nil, err
	}
	if err := checkProcessorBinding(in.Vendor, st); err != nil {
		return nil, err
	}

	// 3. 配置补默认值并校验，错误原因直接给管理员看
	cfg, err := imageproc.Normalize(in.Vendor, in.Config)
	if err != nil {
		return nil, errcode.ErrProcessorInvalid.WithMsg(err.Error())
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("序列化处理服务配置失败: %w", err)
	}

	// 4. 写库：名称重复转成业务错误
	p := &model.ImageProcessor{
		Name: name, Vendor: in.Vendor, StorageID: in.StorageID, Status: model.ProcessorDraft,
		Config: raw, Version: 1, CreatedBy: actorID, UpdatedBy: actorID,
	}
	if err := s.repo.Create(ctx, p); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, errcode.ErrProcessorNameDup
		}
		return nil, err
	}
	return s.view(p, st, nil)
}

// Update 保存草稿（带乐观锁，成功后 version +1）。已发布的处理服务保存的也只是草稿：
// 线上配置（published_config）不变，发布之后才生效；之前的校验结果保留，但它的 version 会落后于新的 version，
// 前端据此知道需要重新校验。
func (s *ImageProcessorService) Update(ctx context.Context, actorID, id uint64, in ProcessorUpdateInput) (*ProcessorView, error) {
	// 1. 处理服务必须存在；校验名称与配置（厂商沿用创建时的，不可改）
	p, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	name, err := processorName(in.Name)
	if err != nil {
		return nil, err
	}
	cfg, err := imageproc.Normalize(p.Vendor, in.Config)
	if err != nil {
		return nil, errcode.ErrProcessorInvalid.WithMsg(err.Error())
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("序列化处理服务配置失败: %w", err)
	}

	// 2. 乐观锁更新：版本不一致说明别人改过了，让前端重新拉取
	if err := s.repo.Update(ctx, id, in.Version, map[string]any{"name": name, "config": model.JSONText(raw), "updated_by": actorID}); err != nil {
		return nil, mapProcessorErr(err)
	}
	return s.Get(ctx, id)
}

// Publish 发布当前草稿：必须有针对这一版草稿的、未发现 fail 的校验与试跑结果。
// 同一存储上已发布的其他处理服务会被自动停用；发布后本实例的解析缓存立刻失效。
func (s *ImageProcessorService) Publish(ctx context.Context, actorID, id uint64, version int) (*ProcessorView, error) {
	// 1. 加载并核对版本：前端拿到的草稿必须就是当前这一版
	p, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Version != version {
		return nil, errcode.ErrProcessorVersionConflict
	}

	// 2. 存储可能在创建之后被改过（比如去掉了公开域名），发布前重新核对绑定关系
	st, err := s.storages.GetByID(ctx, p.StorageID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrStorageNotFound
		}
		return nil, err
	}
	if err := checkProcessorBinding(p.Vendor, st); err != nil {
		return nil, err
	}

	// 3. 必须校验通过：check.Version 对不上说明保存草稿之后还没重新校验
	chk := decodeCheck(p.CheckResult)
	if chk == nil || !chk.OK || chk.Version != p.Version {
		return nil, errcode.ErrProcessorNotChecked
	}

	// 4. 事务内发布（停用同存储旧服务 + 写发布历史 + 切换线上配置）
	if _, err := s.repo.Publish(ctx, repository.PublishParams{ID: id, Version: version, TrialResult: p.CheckResult, By: actorID, At: s.now()}); err != nil {
		return nil, mapProcessorErr(err)
	}
	s.invalidate(p.StorageID)
	return s.Get(ctx, id)
}

// Rollback 把已发布的处理服务回滚到上一个发布版本，没有更早的版本返回 52009。
func (s *ImageProcessorService) Rollback(ctx context.Context, actorID, id uint64) (*ProcessorView, error) {
	p, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Status != model.ProcessorPublished {
		return nil, errcode.ErrProcessorNotPublished
	}
	if _, err := s.repo.Rollback(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrProcessorNoPrevious
		}
		return nil, err
	}
	s.invalidate(p.StorageID)
	return s.Get(ctx, id)
}

// Disable 停用已发布的处理服务，该存储回退原图 / 占位。
func (s *ImageProcessorService) Disable(ctx context.Context, actorID, id uint64) (*ProcessorView, error) {
	p, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Status != model.ProcessorPublished {
		return nil, errcode.ErrProcessorNotPublished
	}
	if err := s.repo.Disable(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrProcessorNotPublished
		}
		return nil, err
	}
	s.invalidate(p.StorageID)
	return s.Get(ctx, id)
}

// Delete 删除草稿或已停用的处理服务；已发布的必须先停用，避免删掉正在生效的配置。
func (s *ImageProcessorService) Delete(ctx context.Context, actorID, id uint64) error {
	p, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	if p.Status == model.ProcessorPublished {
		return errcode.ErrProcessorPublished
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return mapProcessorErr(err)
	}
	s.invalidate(p.StorageID)
	return nil
}

// load 按 id 读取处理服务，不存在转成 52001。
func (s *ImageProcessorService) load(ctx context.Context, id uint64) (*model.ImageProcessor, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, mapProcessorErr(err)
	}
	return p, nil
}

// mapProcessorErr 把 repository 的哨兵错误转成业务错误，其他错误原样返回。
func mapProcessorErr(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return errcode.ErrProcessorNotFound
	case errors.Is(err, repository.ErrRevisionConflict):
		return errcode.ErrProcessorVersionConflict
	case errors.Is(err, repository.ErrDuplicate):
		return errcode.ErrProcessorNameDup
	}
	return err
}

// processorName 清洗并校验名称：去掉首尾空白，1–64 个字符。
func processorName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > processorNameMaxRunes {
		return "", errcode.ErrProcessorInvalid.WithMsg("名称不能为空，且不超过 64 个字符")
	}
	return name, nil
}

// checkProcessorBinding 校验厂商与存储是否匹配，不匹配返回 52005，原因写进消息。
func checkProcessorBinding(vendor string, st *model.StorageConfig) error {
	pre, ok := imageproc.PresetOf(vendor)
	if !ok {
		return errcode.ErrProcessorInvalid.WithMsg(fmt.Sprintf("不支持的处理服务厂商 %q", vendor))
	}
	if st.Provider != pre.StorageProvider {
		return errcode.ErrProcessorStorageMismatch.WithMsg(
			fmt.Sprintf("这是「%s」存储；%s 只能绑定「%s」存储", storageProviderName(st.Provider), pre.Name, storageProviderName(pre.StorageProvider)))
	}
	if pre.RequiresPublicBase && st.PublicBaseURL == "" {
		return errcode.ErrProcessorStorageMismatch.WithMsg("这套存储还没有设置公开访问域名，请先到存储配置里填写")
	}
	return nil
}

// storageProviderName 返回存储服务商的中文名，未知的原样返回。
func storageProviderName(provider string) string {
	if provider == storage.ProviderLocal {
		return "本地磁盘"
	}
	if p, ok := storage.PresetOf(provider); ok {
		return p.Name
	}
	return provider
}

// viewOf 组装单个处理服务的视图（读存储与可回滚版本）。
func (s *ImageProcessorService) viewOf(ctx context.Context, p *model.ImageProcessor) (*ProcessorView, error) {
	st, err := s.storages.GetByID(ctx, p.StorageID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	prev, err := s.repo.PreviousVersions(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	return s.view(p, st, prev)
}

// view 把表行转成视图；st 为 nil（存储已被删除）时存储名留空。
func (s *ImageProcessorService) view(p *model.ImageProcessor, st *model.StorageConfig, prev map[uint64]int) (*ProcessorView, error) {
	v := &ProcessorView{
		ID: p.ID, Name: p.Name, Vendor: p.Vendor, StorageID: p.StorageID, Status: p.Status,
		PublishedVersion: p.PublishedVersion, Version: p.Version, Check: decodeCheck(p.CheckResult),
		UpdatedAt: p.UpdatedAt, CreatedAt: p.CreatedAt,
	}
	if st != nil {
		v.StorageName, v.StorageProvider = st.Name, st.Provider
	}
	if err := json.Unmarshal(p.Config, &v.Config); err != nil {
		return nil, fmt.Errorf("解析处理服务 %d 的配置失败: %w", p.ID, err)
	}
	if len(p.PublishedConfig) > 0 {
		var pub imageproc.Config
		if err := json.Unmarshal(p.PublishedConfig, &pub); err != nil {
			return nil, fmt.Errorf("解析处理服务 %d 的线上配置失败: %w", p.ID, err)
		}
		v.PublishedConfig = &pub
		v.HasDraft = p.Status == model.ProcessorPublished && !reflect.DeepEqual(v.Config, pub)
	}
	if n, ok := prev[p.ID]; ok && p.Status == model.ProcessorPublished {
		v.PreviousVersion = &n
	}
	return v, nil
}
