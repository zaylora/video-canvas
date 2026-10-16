package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

const (
	// defaultLibraryPage / maxLibraryPage 是资产库与生成历史每页条数的默认值与上限。
	defaultLibraryPage = 40
	maxLibraryPage     = 100
)

// 库条目列表 folder_id 参数的特殊取值。
const (
	LibraryFolderRoot = "root" // 根目录（默认）
	LibraryFolderAll  = "all"  // 跨文件夹平铺
)

// AssetFolderRepo 是文件夹的数据访问接口（真实实现是 repository.AssetFolderRepository）。
type AssetFolderRepo interface {
	// ListWithCount 返回用户全部文件夹及各自的素材数。
	ListWithCount(ctx context.Context, userID uint64) ([]model.AssetFolderCount, error)
	// Count 统计用户的文件夹数。
	Count(ctx context.Context, userID uint64) (int64, error)
	// NameExists 判断用户名下是否已有同名（忽略大小写）的文件夹，excludeID 非 0 时排除它自己。
	NameExists(ctx context.Context, userID uint64, name string, excludeID uint64) (bool, error)
	// Create 新建文件夹，同名（忽略大小写）返回 repository.ErrDuplicate。
	Create(ctx context.Context, f *model.AssetFolder) error
	// GetByID 按 id + user_id 查询，不存在或不属于该用户返回 repository.ErrNotFound。
	GetByID(ctx context.Context, userID, id uint64) (*model.AssetFolder, error)
	// Rename 改名，不存在返回 repository.ErrNotFound，重名返回 repository.ErrDuplicate。
	Rename(ctx context.Context, userID, id uint64, name string) error
	// CountItems 统计文件夹里该用户的素材数。
	CountItems(ctx context.Context, userID, folderID uint64) (int64, error)
	// WithTx 在一个事务里执行 fn（删除文件夹用），fn 返回错误整体回滚。
	WithTx(ctx context.Context, fn func(tx repository.AssetFolderTx) error) error
}

// AssetLibraryItemRepo 是库归属的数据访问接口（真实实现是 repository.AssetLibraryRepository）。
type AssetLibraryItemRepo interface {
	// Create 新建归属，该用户已有这个素材的归属返回 repository.ErrDuplicate。
	Create(ctx context.Context, it *model.AssetLibraryItem) error
	// GetByID 按 id + user_id 查询，不存在或不属于该用户返回 repository.ErrNotFound。
	GetByID(ctx context.Context, userID, id uint64) (*model.AssetLibraryItem, error)
	// GetByAssetID 按 user_id + asset_id 查询，不存在返回 repository.ErrNotFound。
	GetByAssetID(ctx context.Context, userID, assetID uint64) (*model.AssetLibraryItem, error)
	// ListByAssetIDs 一次查询返回该用户这些素材的归属。
	ListByAssetIDs(ctx context.Context, userID uint64, assetIDs []uint64) ([]model.AssetLibraryItem, error)
	// SetFolder 移动归属，folderID 为 nil 表示根目录；不存在返回 repository.ErrNotFound。
	SetFolder(ctx context.Context, userID, id uint64, folderID *uint64) error
	// Delete 移出资产库，不存在返回 repository.ErrNotFound。
	Delete(ctx context.Context, userID, id uint64) error
	// List 按加入时间倒序返回库条目，最多 f.Limit 条。
	List(ctx context.Context, userID uint64, f repository.LibraryItemFilter) ([]model.AssetLibraryItem, error)
}

// AssetLibraryAssets 是资产库依赖的素材数据访问接口（真实实现是 repository.AssetRepository）。
type AssetLibraryAssets interface {
	// GetByID 按 id + user_id 查询，不存在或不属于该用户返回 repository.ErrNotFound。
	GetByID(ctx context.Context, userID, id uint64) (*model.Asset, error)
	// ListByIDs 按 id 批量查询，不存在的缺席。
	ListByIDs(ctx context.Context, ids []uint64) ([]model.Asset, error)
	// ListGeneratedByUser 返回该用户的生成历史（已排除试跑任务），最多 f.Limit 条。
	ListGeneratedByUser(ctx context.Context, userID uint64, f repository.GeneratedAssetFilter) ([]model.GeneratedAssetRow, error)
}

// AssetLibraryCanvases 是生成历史依赖的画布查询接口（真实实现是 repository.CanvasProjectRepository）。
type AssetLibraryCanvases interface {
	// BriefsByIDs 批量返回该用户这些画布的标题与是否已删除，查不到的缺席。
	BriefsByIDs(ctx context.Context, userID uint64, ids []uint64) ([]model.CanvasBrief, error)
}

// AssetLibraryViewer 把素材转成带稳定地址的视图（真实实现是 AssetService.ViewOf）。
type AssetLibraryViewer interface {
	// ViewOf 返回素材视图，调用方必须已确认素材归属当前用户。
	ViewOf(ctx context.Context, a *model.Asset) (*model.AssetView, error)
}

// AssetLibraryDeps 是资产库服务的依赖。
type AssetLibraryDeps struct {
	Folders  AssetFolderRepo
	Items    AssetLibraryItemRepo
	Assets   AssetLibraryAssets
	Canvases AssetLibraryCanvases
	Views    AssetLibraryViewer
}

// AssetLibraryService 是个人资产库与生成历史的业务：文件夹、库归属、按用户列出生成产物。
// 库只是同一条 assets 记录的归属，不复制文件，所以移出库、删文件夹都不动素材与存储。
type AssetLibraryService struct {
	folders  AssetFolderRepo
	items    AssetLibraryItemRepo
	assets   AssetLibraryAssets
	canvases AssetLibraryCanvases
	views    AssetLibraryViewer
}

// NewAssetLibraryService 创建资产库服务。
func NewAssetLibraryService(d AssetLibraryDeps) *AssetLibraryService {
	return &AssetLibraryService{folders: d.Folders, items: d.Items, assets: d.Assets, canvases: d.Canvases, views: d.Views}
}

// ListFolders 返回当前用户的全部文件夹（不分页，最多 100 个），带各自的素材数。
func (s *AssetLibraryService) ListFolders(ctx context.Context, userID uint64) (*model.AssetFolderListView, error) {
	rows, err := s.folders.ListWithCount(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := &model.AssetFolderListView{Items: make([]model.AssetFolderView, 0, len(rows))}
	for i := range rows {
		out.Items = append(out.Items, folderView(&rows[i].AssetFolder, rows[i].ItemCount))
	}
	return out, nil
}

// CreateFolder 新建文件夹并返回它。名称去首尾空白后 1–30 字，同一用户下忽略大小写不能重名，每用户最多 100 个。
func (s *AssetLibraryService) CreateFolder(ctx context.Context, userID uint64, name string) (*model.AssetFolderView, error) {
	// 1. 校验名称
	name, err := normalizeFolderName(name)
	if err != nil {
		return nil, err
	}

	// 2. 数量上限：先查再建，并发下可能略微超出，可接受（上限是防滥用，不是计费约束）
	n, err := s.folders.Count(ctx, userID)
	if err != nil {
		return nil, err
	}
	if n >= model.MaxAssetFolders {
		return nil, errcode.ErrAssetFolderLimit
	}

	// 3. 重名：先查给出友好的 409；并发撞上唯一索引时 repository 返回 ErrDuplicate，同样翻成 409
	if err := s.checkFolderNameFree(ctx, userID, name, 0); err != nil {
		return nil, err
	}
	f := &model.AssetFolder{UserID: userID, Name: name}
	if err := s.folders.Create(ctx, f); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, errcode.ErrAssetFolderDup
		}
		return nil, err
	}

	// 4. 新建的文件夹是空的
	v := folderView(f, 0)
	return &v, nil
}

// RenameFolder 重命名文件夹并返回它。规则同新建；文件夹不存在或不属于当前用户返回 404。
func (s *AssetLibraryService) RenameFolder(ctx context.Context, userID, id uint64, name string) (*model.AssetFolderView, error) {
	// 1. 校验名称
	name, err := normalizeFolderName(name)
	if err != nil {
		return nil, err
	}

	// 2. 文件夹归属：别人的和不存在的统一 404
	f, err := s.getFolder(ctx, userID, id)
	if err != nil {
		return nil, err
	}

	// 3. 重名检查排除它自己：只改大小写（Foo → foo）不算重名
	if err := s.checkFolderNameFree(ctx, userID, name, id); err != nil {
		return nil, err
	}
	if err := s.folders.Rename(ctx, userID, id, name); err != nil {
		switch {
		case errors.Is(err, repository.ErrDuplicate):
			return nil, errcode.ErrAssetFolderDup
		case errors.Is(err, repository.ErrNotFound):
			return nil, errcode.ErrAssetFolderNotFound
		}
		return nil, err
	}

	// 4. 返回带最新素材数的视图
	cnt, err := s.folders.CountItems(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	f.Name = name
	v := folderView(f, int(cnt))
	return &v, nil
}

// DeleteFolder 删除文件夹及其中的库归属，返回一并移出资产库的素材数。
// 不动 assets 与存储文件：画布里已引用的素材不受影响。
func (s *AssetLibraryService) DeleteFolder(ctx context.Context, userID, id uint64) (*model.AssetFolderRemovedView, error) {
	var removed int64
	// 1. 同一事务里先删归属行再删文件夹（没有外键，靠事务保证不留下指向已删文件夹的归属）
	err := s.folders.WithTx(ctx, func(tx repository.AssetFolderTx) error {
		if _, err := tx.GetByID(ctx, userID, id); err != nil {
			return err
		}
		n, err := tx.DeleteItems(ctx, userID, id)
		if err != nil {
			return err
		}
		removed = n
		return tx.Delete(ctx, userID, id)
	})

	// 2. 文件夹不存在或不属于该用户 → 404，事务已回滚
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrAssetFolderNotFound
		}
		return nil, err
	}
	return &model.AssetFolderRemovedView{Removed: removed}, nil
}

// LibraryListInput 是库条目列表的入参（已由 handler 做完格式校验）。
type LibraryListInput struct {
	FolderID string // root（空等同）/ 十六进制文件夹 id / all
	Kind     string // 素材类型，空不过滤
	Q        string // 文件名包含匹配，空不过滤
	Cursor   string // 上一页返回的 next_cursor
	Limit    int    // 每页条数，0 取默认 40
}

// ListItems 返回当前用户资产库的一页条目，按加入时间倒序。
func (s *AssetLibraryService) ListItems(ctx context.Context, userID uint64, in LibraryListInput) (*model.AssetLibraryItemPage, error) {
	// 1. 解析文件夹范围；指定的文件夹必须存在且属于该用户，否则 404
	f := repository.LibraryItemFilter{Kind: in.Kind, Q: strings.TrimSpace(in.Q)}
	switch in.FolderID {
	case "", LibraryFolderRoot:
		f.Scope = repository.LibraryScopeRoot
	case LibraryFolderAll:
		f.Scope = repository.LibraryScopeAll
	default:
		fid, ok := idcodec.Decode(in.FolderID)
		if !ok {
			return nil, errcode.ErrInvalidParams.WithMsg("folder_id 格式错误")
		}
		if _, err := s.getFolder(ctx, userID, fid); err != nil {
			return nil, err
		}
		f.Scope, f.FolderID = repository.LibraryScopeFolder, fid
	}

	// 2. 游标与条数：多取一条判断有没有下一页
	after, err := decodeTimeCursor(in.Cursor)
	if err != nil {
		return nil, err
	}
	limit := clampPageSize(in.Limit)
	f.After, f.Limit = after, limit+1
	rows, err := s.items.List(ctx, userID, f)
	if err != nil {
		return nil, err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeTimeCursor(rows[limit-1].CreatedAt, rows[limit-1].ID)
	}

	// 3. 一次查询取这页的素材；素材行不存在的归属（不应出现）直接跳过，不让一条脏数据拖垮整页
	assets, err := s.assetsByID(ctx, rows, func(it *model.AssetLibraryItem) uint64 { return it.AssetID })
	if err != nil {
		return nil, err
	}
	out := &model.AssetLibraryItemPage{Items: make([]model.AssetLibraryItemView, 0, len(rows)), NextCursor: next}
	for i := range rows {
		a, ok := assets[rows[i].AssetID]
		if !ok {
			continue
		}
		v, err := s.itemView(ctx, &rows[i], a)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *v)
	}
	return out, nil
}

// AddItem 把素材加入资产库，返回归属与是否新建。素材已在库里是幂等的：返回现有归属（created=false，不改它的文件夹）。
// 素材不存在或不属于当前用户、文件夹不存在或不属于当前用户都返回 404。folderID 为 0 表示根目录。
func (s *AssetLibraryService) AddItem(ctx context.Context, userID, assetID, folderID uint64) (*model.AssetLibraryItemView, bool, error) {
	// 1. 素材必须是自己的（别人的素材与不存在统一 404，避免暴露素材是否存在）
	asset, err := s.assets.GetByID(ctx, userID, assetID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, false, errcode.ErrAssetNotFound
		}
		return nil, false, err
	}

	// 2. 目标文件夹必须是自己的
	if folderID != 0 {
		if _, err := s.getFolder(ctx, userID, folderID); err != nil {
			return nil, false, err
		}
	}

	// 3. 已在库里：幂等返回现有归属
	if it, err := s.items.GetByAssetID(ctx, userID, assetID); err == nil {
		v, err := s.itemView(ctx, it, asset)
		return v, false, err
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, false, err
	}

	// 4. 新建归属；并发下另一个请求先插入时撞唯一索引，同样按幂等返回已存在的那条
	it := &model.AssetLibraryItem{UserID: userID, AssetID: assetID, FolderID: folderPtr(folderID)}
	if err := s.items.Create(ctx, it); err != nil {
		if !errors.Is(err, repository.ErrDuplicate) {
			return nil, false, err
		}
		existing, gerr := s.items.GetByAssetID(ctx, userID, assetID)
		if gerr != nil {
			return nil, false, gerr
		}
		v, verr := s.itemView(ctx, existing, asset)
		return v, false, verr
	}
	v, err := s.itemView(ctx, it, asset)
	return v, true, err
}

// MoveItem 把库条目移到另一个文件夹（folderID 为 0 表示根目录）并返回它。条目或文件夹不存在、不属于当前用户都返回 404。
func (s *AssetLibraryService) MoveItem(ctx context.Context, userID, id, folderID uint64) (*model.AssetLibraryItemView, error) {
	// 1. 条目归属
	it, err := s.getItem(ctx, userID, id)
	if err != nil {
		return nil, err
	}

	// 2. 目标文件夹归属
	if folderID != 0 {
		if _, err := s.getFolder(ctx, userID, folderID); err != nil {
			return nil, err
		}
	}

	// 3. 移动
	if err := s.items.SetFolder(ctx, userID, id, folderPtr(folderID)); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrLibraryItemNotFound
		}
		return nil, err
	}
	it.FolderID = folderPtr(folderID)

	// 4. 返回最新视图；素材行不见了说明归属已成脏数据，按条目不存在处理
	asset, err := s.assets.GetByID(ctx, userID, it.AssetID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrLibraryItemNotFound
		}
		return nil, err
	}
	return s.itemView(ctx, it, asset)
}

// RemoveItem 把条目移出资产库：只删归属行，不动素材与存储文件；不存在或不属于当前用户返回 404。
func (s *AssetLibraryService) RemoveItem(ctx context.Context, userID, id uint64) error {
	if err := s.items.Delete(ctx, userID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errcode.ErrLibraryItemNotFound
		}
		return err
	}
	return nil
}

// GeneratedListInput 是生成历史的入参（已由 handler 解析好 id 与时间）。
type GeneratedListInput struct {
	Kind        string     // 素材类型（必填）
	Source      string     // all（空等同）/ conversation / canvas
	CanvasID    uint64     // 非 0 时只看这张画布
	CreatedFrom *time.Time // 只看不早于这个时间的素材
	Asc         bool       // 最早优先
	Q           string     // 提示词包含匹配
	Cursor      string     // 上一页返回的 next_cursor
	Limit       int        // 每页条数，0 取默认 40
}

// ListGenerated 返回当前用户的生成历史一页：生成产物素材（不含上传来源与试跑任务），
// 每张卡带完整提示词、模型名、来源画布，以及是否已在资产库。
func (s *AssetLibraryService) ListGenerated(ctx context.Context, userID uint64, in GeneratedListInput) (*model.GeneratedItemPage, error) {
	// 1. 游标与条数：多取一条判断有没有下一页
	after, err := decodeTimeCursor(in.Cursor)
	if err != nil {
		return nil, err
	}
	limit := clampPageSize(in.Limit)
	rows, err := s.assets.ListGeneratedByUser(ctx, userID, repository.GeneratedAssetFilter{
		Kind: in.Kind, Source: in.Source, CanvasID: in.CanvasID, CreatedFrom: in.CreatedFrom,
		Asc: in.Asc, Q: strings.TrimSpace(in.Q), After: after, Limit: limit + 1,
	})
	if err != nil {
		return nil, err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeTimeCursor(rows[limit-1].CreatedAt, rows[limit-1].ID)
	}
	out := &model.GeneratedItemPage{Items: make([]model.GeneratedItemView, 0, len(rows)), NextCursor: next}
	if len(rows) == 0 {
		return out, nil
	}

	// 2. 这页用到的关联数据各一次查询：来源画布、已在库里的归属（避免 N+1）
	canvases, err := s.canvasBriefs(ctx, userID, rows)
	if err != nil {
		return nil, err
	}
	assetIDs := make([]uint64, 0, len(rows))
	for i := range rows {
		assetIDs = append(assetIDs, rows[i].ID)
	}
	inLib, err := s.items.ListByAssetIDs(ctx, userID, assetIDs)
	if err != nil {
		return nil, err
	}
	libItem := make(map[uint64]uint64, len(inLib))
	for _, it := range inLib {
		libItem[it.AssetID] = it.ID
	}

	// 3. 逐条组装：提示词取完整文本（历史里要看全文，前端自己省略），模型名取快照展示名
	for i := range rows {
		r := &rows[i]
		av, err := s.views.ViewOf(ctx, &r.Asset)
		if err != nil {
			return nil, err
		}
		item := model.GeneratedItemView{
			Asset:         av,
			Prompt:        taskPromptText(r.InputJSON),
			ModelLabel:    taskModelLabel(r.ConfigSnapshot, r.TaskModelKey),
			CreatedAt:     r.CreatedAt,
			LibraryItemID: idcodec.ID(libItem[r.ID]),
		}
		// 来源：任务没有画布 = 首页对话（canvas 为 null）；画布已被删除（或查不到）时标 deleted
		if r.CanvasProjectID != nil {
			c := &model.GeneratedCanvas{ID: idcodec.ID(*r.CanvasProjectID)}
			if b, ok := canvases[*r.CanvasProjectID]; ok && !b.Deleted {
				c.Title = b.Title
			} else {
				c.Deleted = true
			}
			item.Canvas = c
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// canvasBriefs 批量取这一页产物所属画布的简要信息，返回 画布 id -> 简要信息；没有画布来源的页不发查询。
func (s *AssetLibraryService) canvasBriefs(ctx context.Context, userID uint64, rows []model.GeneratedAssetRow) (map[uint64]model.CanvasBrief, error) {
	seen := map[uint64]struct{}{}
	var ids []uint64
	for i := range rows {
		if id := rows[i].CanvasProjectID; id != nil {
			if _, dup := seen[*id]; !dup {
				seen[*id] = struct{}{}
				ids = append(ids, *id)
			}
		}
	}
	m := make(map[uint64]model.CanvasBrief, len(ids))
	if len(ids) == 0 {
		return m, nil
	}
	briefs, err := s.canvases.BriefsByIDs(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	for _, b := range briefs {
		m[b.ID] = b
	}
	return m, nil
}

// assetsByID 批量取库条目对应的素材，返回 素材 id -> 素材；rows 为空不发查询。
func (s *AssetLibraryService) assetsByID(ctx context.Context, rows []model.AssetLibraryItem, idOf func(*model.AssetLibraryItem) uint64) (map[uint64]*model.Asset, error) {
	m := make(map[uint64]*model.Asset, len(rows))
	if len(rows) == 0 {
		return m, nil
	}
	ids := make([]uint64, 0, len(rows))
	for i := range rows {
		ids = append(ids, idOf(&rows[i]))
	}
	assets, err := s.assets.ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range assets {
		m[assets[i].ID] = &assets[i]
	}
	return m, nil
}

// itemView 组装库条目视图。
func (s *AssetLibraryService) itemView(ctx context.Context, it *model.AssetLibraryItem, a *model.Asset) (*model.AssetLibraryItemView, error) {
	av, err := s.views.ViewOf(ctx, a)
	if err != nil {
		return nil, err
	}
	v := &model.AssetLibraryItemView{ID: idcodec.ID(it.ID), Asset: av, CreatedAt: it.CreatedAt}
	if it.FolderID != nil {
		v.FolderID = idcodec.ID(*it.FolderID)
	}
	return v, nil
}

// getFolder 查询当前用户的文件夹，不存在或不属于该用户统一返回 64004。
func (s *AssetLibraryService) getFolder(ctx context.Context, userID, id uint64) (*model.AssetFolder, error) {
	f, err := s.folders.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrAssetFolderNotFound
		}
		return nil, err
	}
	return f, nil
}

// getItem 查询当前用户的库条目，不存在或不属于该用户统一返回 64005。
func (s *AssetLibraryService) getItem(ctx context.Context, userID, id uint64) (*model.AssetLibraryItem, error) {
	it, err := s.items.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrLibraryItemNotFound
		}
		return nil, err
	}
	return it, nil
}

// checkFolderNameFree 确认用户名下没有同名（忽略大小写）的文件夹，excludeID 非 0 时排除它自己；重名返回 64002。
func (s *AssetLibraryService) checkFolderNameFree(ctx context.Context, userID uint64, name string, excludeID uint64) error {
	exists, err := s.folders.NameExists(ctx, userID, name, excludeID)
	if err != nil {
		return err
	}
	if exists {
		return errcode.ErrAssetFolderDup
	}
	return nil
}

// normalizeFolderName 去首尾空白并校验长度（按字符数 1–30），不合法返回 64001。
func normalizeFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n == 0 {
		return "", errcode.ErrAssetFolderInvalid.WithMsg("文件夹名称不能为空")
	}
	if n > model.MaxAssetFolderNameLen {
		return "", errcode.ErrAssetFolderInvalid.WithMsg(fmt.Sprintf("文件夹名称最多 %d 个字", model.MaxAssetFolderNameLen))
	}
	return name, nil
}

// folderView 组装文件夹视图。
func folderView(f *model.AssetFolder, itemCount int) model.AssetFolderView {
	return model.AssetFolderView{ID: idcodec.ID(f.ID), Name: f.Name, ItemCount: itemCount, CreatedAt: f.CreatedAt}
}

// folderPtr 把 0（根目录）转成 nil，其余取地址。
func folderPtr(id uint64) *uint64 {
	if id == 0 {
		return nil
	}
	return &id
}

// clampPageSize 修正每页条数：没传取默认 40，超过 100 收敛到 100。
func clampPageSize(n int) int {
	if n < 1 {
		return defaultLibraryPage
	}
	return min(n, maxLibraryPage)
}

// encodeTimeCursor 把 (created_at, id) 编成不透明的游标串；时间取微秒，与 PostgreSQL timestamptz 精度一致，翻页不丢不重。
func encodeTimeCursor(at time.Time, id uint64) string {
	raw := strconv.FormatInt(at.UnixMicro(), 10) + ":" + strconv.FormatUint(id, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeTimeCursor 解码游标；空串表示从头开始（返回 nil），格式不对返回参数错误。
func decodeTimeCursor(s string) (*repository.TimeCursor, error) {
	if s == "" {
		return nil, nil
	}
	bad := errcode.ErrInvalidParams.WithMsg("cursor 格式错误")
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, bad
	}
	left, right, ok := strings.Cut(string(raw), ":")
	if !ok {
		return nil, bad
	}
	micro, err1 := strconv.ParseInt(left, 10, 64)
	id, err2 := strconv.ParseUint(right, 10, 64)
	if err1 != nil || err2 != nil {
		return nil, bad
	}
	return &repository.TimeCursor{At: time.UnixMicro(micro), ID: id}, nil
}
