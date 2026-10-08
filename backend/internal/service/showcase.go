package service

import (
	"context"
	"strconv"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// 登录页展示的设置范围与内置缺省。
const (
	// minShowcaseClipSeconds / maxShowcaseClipSeconds 是每段视频播放秒数的合法范围：
	// 太短来不及看清文案，太长轮播就失去意义。
	minShowcaseClipSeconds = 4
	maxShowcaseClipSeconds = 15

	// DefaultShowcaseClipSeconds 是每段视频的默认播放秒数。
	// 后台保存的库值优先，这个常量只是库里没有（或库值非法）时的兜底。
	DefaultShowcaseClipSeconds = 7
)

// ShowcaseRepo 是登录页展示条目的数据访问接口（真实实现是 repository.ShowcaseRepository）。
type ShowcaseRepo interface {
	// ListAll 返回全部条目（含禁用），按 sort、id 升序。
	ListAll(ctx context.Context) ([]model.ShowcaseItem, error)
	// ListEnabled 返回已启用的条目，按 sort、id 升序。
	ListEnabled(ctx context.Context) ([]model.ShowcaseItem, error)
	// GetByID 按 id 查询，不存在返回 repository.ErrNotFound。
	GetByID(ctx context.Context, id uint64) (*model.ShowcaseItem, error)
	// MaxSort 返回当前最大的 sort，没有条目时返回 -1。
	MaxSort(ctx context.Context) (int, error)
	// Create 插入条目，成功后 ID 已回填。
	Create(ctx context.Context, it *model.ShowcaseItem) error
	// Update 按列名更新指定字段（值为 nil 置 NULL），条目不存在返回 repository.ErrNotFound。
	Update(ctx context.Context, id uint64, fields map[string]any) error
	// Delete 删除条目（不动素材），条目不存在返回 repository.ErrNotFound。
	Delete(ctx context.Context, id uint64) error
	// ReferencedAssetIDs 返回 assetIDs 里已被未删除条目作为视频引用的素材 id（去重，顺序不保证）。
	ReferencedAssetIDs(ctx context.Context, assetIDs []uint64) ([]uint64, error)
	// WithTx 在一个事务里执行 fn，fn 返回错误则整体回滚。
	WithTx(ctx context.Context, fn func(tx repository.ShowcaseTx) error) error
}

// ShowcaseAssets 是展示服务读取素材的依赖（真实实现是 repository.AssetRepository）。
type ShowcaseAssets interface {
	// GetByID 按 id 且 user_id 查询，不存在或不属于该用户返回 repository.ErrNotFound。
	GetByID(ctx context.Context, userID, id uint64) (*model.Asset, error)
	// ListByIDs 按 id 批量查询，不带用户条件；不存在的 id 缺席，顺序不保证。
	ListByIDs(ctx context.Context, ids []uint64) ([]model.Asset, error)
	// ListByKindSource 按 kind + source 分页查询素材（不限用户），created_at 倒序、id 倒序，同时返回总数。
	ListByKindSource(ctx context.Context, kind, source string, offset, limit int) ([]model.Asset, int64, error)
}

// ShowcaseTasks 是素材库列表按素材的 task_id 带出提示词与模型名的依赖（真实实现是 repository.GenerationTaskRepository）。
type ShowcaseTasks interface {
	// ListBriefByIDs 按 id 批量查询任务（不限用户），只保证 id、model_id、input_json、config_snapshot 有值；不存在的 id 缺席。
	ListBriefByIDs(ctx context.Context, ids []uint64) ([]model.GenerationTask, error)
}

// ShowcaseUsers 是素材库列表显示素材作者的依赖（真实实现是 repository.UserRepository）。
type ShowcaseUsers interface {
	// ListUsernames 按 id 批量取用户名，返回 用户 id -> 用户名；不存在的 id 缺席。
	ListUsernames(ctx context.Context, ids []uint64) (map[uint64]string, error)
}

// ShowcaseAssetViewer 把素材转成带稳定地址的视图（真实实现是 AssetService.ViewOf），
// 这样 URL 的构造规则只有素材服务一处，不在这里重复一份。
type ShowcaseAssetViewer interface {
	// ViewOf 返回素材视图，URL 是稳定地址 {站点前缀}/files/<key>。
	ViewOf(ctx context.Context, a *model.Asset) (*model.AssetView, error)
}

// ShowcaseDeps 是 ShowcaseService 的依赖。
type ShowcaseDeps struct {
	Items    ShowcaseRepo        // 展示条目
	Assets   ShowcaseAssets      // 素材读取
	Views    ShowcaseAssetViewer // 素材地址
	Tasks    ShowcaseTasks       // 生成任务（素材库列表带出提示词与模型名）
	Users    ShowcaseUsers       // 用户名（素材库列表显示作者）
	Settings SystemSettingRepo   // 系统设置键值表
	Audit    AdminAuditWriter    // 后台审计；可为 nil
}

// ShowcaseService 管理登录页展示：后台配置轮播视频与设置，登录页（未登录）公开读取。
type ShowcaseService struct {
	items    ShowcaseRepo
	assets   ShowcaseAssets
	views    ShowcaseAssetViewer
	tasks    ShowcaseTasks
	users    ShowcaseUsers
	settings SystemSettingRepo
	audit    AdminAuditWriter
}

// NewShowcaseService 创建登录页展示服务。
func NewShowcaseService(d ShowcaseDeps) *ShowcaseService {
	return &ShowcaseService{items: d.Items, assets: d.Assets, views: d.Views, tasks: d.Tasks, users: d.Users, settings: d.Settings, audit: d.Audit}
}

// Settings 返回当前生效的展示设置：库里有合法值用库值，否则回落默认（7 秒、展示、省流量只显示封面）。
func (s *ShowcaseService) Settings(ctx context.Context) (*model.ShowcaseSettingsView, error) {
	// 1. 读取全部设置键值
	kv, err := s.settings.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	// 2. 解析：两个开关缺省或任何非 "false" 的值都按开；
	//    秒数库值损坏（非数字、越界）时回落默认而不是报错——这是登录页的公开读取，不能因为一行脏数据让登录页打不开
	v := &model.ShowcaseSettingsView{
		ClipSeconds:          DefaultShowcaseClipSeconds,
		ShowOnLogin:          kv[model.SettingShowcaseShowOnLogin] != "false",
		PosterOnlyOnSaveData: kv[model.SettingShowcasePosterOnSaveData] != "false",
	}
	if n, err := strconv.Atoi(kv[model.SettingShowcaseClipSeconds]); err == nil && validShowcaseClipSeconds(n) {
		v.ClipSeconds = n
	}
	return v, nil
}

// validShowcaseClipSeconds 判断播放秒数是否在合法范围内。
func validShowcaseClipSeconds(n int) bool {
	return n >= minShowcaseClipSeconds && n <= maxShowcaseClipSeconds
}

// Public 返回登录页公开读取的数据：设置 + 已启用条目（按 sort 升序，sort 相同按 id 升序）。
// 只暴露登录页播放所需的字段；show_on_login 关闭时 items 为空数组，设置照常返回，前端据此决定是否展示。
func (s *ShowcaseService) Public(ctx context.Context) (*model.ShowcasePublicView, error) {
	// 1. 读设置；关闭展示时不必再查条目和素材，直接返回空列表
	settings, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	out := &model.ShowcasePublicView{Settings: *settings, Items: []model.ShowcasePublicItem{}}
	if !settings.ShowOnLogin {
		return out, nil
	}

	// 2. 读已启用的条目，并一次性批量取出它们引用的视频与封面素材（避免每条一次查询）
	items, err := s.items.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	assets, err := s.assetMap(ctx, items)
	if err != nil {
		return nil, err
	}

	// 3. 逐条组装。视频素材已被删除的条目直接跳过：登录页没有视频可播，留着只会出现黑屏；
	//    封面素材被删除则 poster_url 退化为空串，前端按“没有封面”处理
	for i := range items {
		video, ok := assets[items[i].AssetID]
		if !ok {
			continue
		}
		item, err := s.publicItem(ctx, &items[i], video, assets)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *item)
	}
	return out, nil
}

// AdminView 返回后台读取的数据：设置 + 全部条目（含禁用），按 sort 升序。
// 视频素材已被删除的条目也会列出（视频地址为空串），否则管理员看不到它、也就删不掉它。
func (s *ShowcaseService) AdminView(ctx context.Context) (*model.ShowcaseAdminView, error) {
	// 1. 读设置与全部条目
	settings, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.items.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	// 2. 批量取素材并逐条组装后台视图
	assets, err := s.assetMap(ctx, items)
	if err != nil {
		return nil, err
	}
	out := &model.ShowcaseAdminView{Settings: *settings, Items: make([]model.ShowcaseAdminItem, 0, len(items))}
	for i := range items {
		item, err := s.adminItem(ctx, &items[i], assets)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *item)
	}
	return out, nil
}

// assetMap 批量取出条目引用的视频与封面素材，返回 素材 id -> 素材。已被删除的素材不在 map 里。
func (s *ShowcaseService) assetMap(ctx context.Context, items []model.ShowcaseItem) (map[uint64]*model.Asset, error) {
	ids := make([]uint64, 0, len(items)*2)
	for _, it := range items {
		ids = append(ids, it.AssetID)
		if it.PosterAssetID != nil {
			ids = append(ids, *it.PosterAssetID)
		}
	}
	rows, err := s.assets.ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	m := make(map[uint64]*model.Asset, len(rows))
	for i := range rows {
		m[rows[i].ID] = &rows[i]
	}
	return m, nil
}

// posterURL 返回封面的稳定地址；条目没有封面或封面素材已被删除时返回空串。
func (s *ShowcaseService) posterURL(ctx context.Context, it *model.ShowcaseItem, assets map[uint64]*model.Asset) (string, error) {
	if it.PosterAssetID == nil {
		return "", nil
	}
	poster, ok := assets[*it.PosterAssetID]
	if !ok {
		return "", nil
	}
	v, err := s.views.ViewOf(ctx, poster)
	if err != nil {
		return "", err
	}
	return v.URL, nil
}

// publicItem 组装一个公开条目：视频地址与宽高、大小取自视频素材，只带登录页需要的字段。
func (s *ShowcaseService) publicItem(ctx context.Context, it *model.ShowcaseItem, video *model.Asset, assets map[uint64]*model.Asset) (*model.ShowcasePublicItem, error) {
	vv, err := s.views.ViewOf(ctx, video)
	if err != nil {
		return nil, err
	}
	poster, err := s.posterURL(ctx, it, assets)
	if err != nil {
		return nil, err
	}
	return &model.ShowcasePublicItem{
		ID: it.ID, VideoURL: vv.URL, PosterURL: poster, Prompt: it.Prompt, ModelLabel: it.ModelLabel,
		StartSec: it.StartSec, Width: vv.Width, Height: vv.Height, ByteSize: vv.ByteSize,
	}, nil
}

// adminItem 组装一个后台条目。视频素材不在 assets 里（已被删除）时，视频相关字段留零值。
func (s *ShowcaseService) adminItem(ctx context.Context, it *model.ShowcaseItem, assets map[uint64]*model.Asset) (*model.ShowcaseAdminItem, error) {
	out := &model.ShowcaseAdminItem{
		ID: it.ID, AssetID: it.AssetID, PosterAssetID: it.PosterAssetID, Prompt: it.Prompt, ModelLabel: it.ModelLabel,
		StartSec: it.StartSec, Enabled: it.Enabled, CreatedAt: it.CreatedAt,
	}
	if video, ok := assets[it.AssetID]; ok {
		vv, err := s.views.ViewOf(ctx, video)
		if err != nil {
			return nil, err
		}
		out.VideoURL, out.Width, out.Height, out.ByteSize, out.DurationMs, out.FileName = vv.URL, vv.Width, vv.Height, vv.ByteSize, vv.DurationMs, vv.FileName
	}
	poster, err := s.posterURL(ctx, it, assets)
	if err != nil {
		return nil, err
	}
	out.PosterURL = poster
	return out, nil
}
