package service

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"video-canvas/internal/canvasdoc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// TransferAssets 是画布导入导出依赖的素材能力，由 AssetService 实现。
type TransferAssets interface {
	// EnsureSha256 返回当前用户一份素材的内容哈希与素材行；素材不存在、不属于该用户或文件读不到时返回 ErrAssetUnreadable。
	EnsureSha256(ctx context.Context, userID, assetID uint64) (string, *model.Asset, error)
	// CopyBySha256 按内容哈希与大小复制一份已有素材文件，返回尚未入库的素材行；找不到返回 ErrAssetNotFoundByHash。
	CopyBySha256(ctx context.Context, userID uint64, sha string, byteSize int64) (*model.Asset, error)
	// DiscardCopies 删除 CopyBySha256 产生的副本对象，用于导入事务失败后的回收。
	DiscardCopies(ctx context.Context, assets []*model.Asset)
	// FileURL 返回素材存储 key 对应的稳定访问地址。
	FileURL(key string) string
}

// TransferCharacters 是画布导入导出依赖的角色卡读取能力，由角色卡仓库实现。
type TransferCharacters interface {
	// ListByIDs 返回该用户这些 ID 的角色卡，查不到的缺席，ids 为空返回 nil。
	ListByIDs(ctx context.Context, userID uint64, ids []uint64) ([]model.CharacterCard, error)
}

// TransferRepo 是画布导入需要的事务入口，由 repository 实现。
type TransferRepo interface {
	// WithTx 在一个数据库事务里执行 fn：fn 返回 nil 提交，返回错误回滚。
	WithTx(ctx context.Context, fn func(tx TransferTx) error) error
}

// TransferTx 是导入事务内可用的写入与查询操作，都只作用于当前事务。
type TransferTx interface {
	// CreateAsset 插入一条素材记录。
	CreateAsset(ctx context.Context, a *model.Asset) error
	// CountCharacters 返回该用户已有的角色卡数量，用于检查角色卡数量上限。
	CountCharacters(ctx context.Context, userID uint64) (int64, error)
	// CharacterNameExists 判断该用户是否已有同名（不区分大小写）的角色卡。
	CharacterNameExists(ctx context.Context, userID uint64, name string) (bool, error)
	// CreateCharacter 插入一张角色卡。
	CreateCharacter(ctx context.Context, c *model.CharacterCard) error
	// CreateCanvas 插入一张画布。
	CreateCanvas(ctx context.Context, p *model.CanvasProject) error
}

// CanvasTransferDeps 是 CanvasTransferService 的依赖。
type CanvasTransferDeps struct {
	Canvases   CanvasProjectRepo
	Assets     TransferAssets
	Characters TransferCharacters
	Tx         TransferRepo
	// AppVersion 写进导出文件，为空时记为 "unknown"。
	AppVersion string
	Limits     canvasdoc.Limits
	// Now 返回当前时间，测试里注入固定值；为空时用 time.Now。
	Now func() time.Time
}

// CanvasTransferService 负责画布与导出文件之间的转换：导出把画布改写成文档，导入把文档还原成新画布。
type CanvasTransferService struct {
	canvases   CanvasProjectRepo
	assets     TransferAssets
	characters TransferCharacters
	tx         TransferRepo
	appVersion string
	limits     canvasdoc.Limits
	now        func() time.Time
}

// NewCanvasTransferService 创建画布导入导出服务；AppVersion、Now 没传时使用 "unknown" 和 time.Now。
func NewCanvasTransferService(d CanvasTransferDeps) *CanvasTransferService {
	s := &CanvasTransferService{
		canvases:   d.Canvases,
		assets:     d.Assets,
		characters: d.Characters,
		tx:         d.Tx,
		appVersion: d.AppVersion,
		limits:     d.Limits,
		now:        d.Now,
	}
	if s.appVersion == "" {
		s.appVersion = "unknown"
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// ExportReport 是导出过程的情况汇总。
type ExportReport struct {
	SkippedAssets int `json:"skipped_assets"` // 读不到而被跳过的素材个数
}

// ExportResult 是一次导出的结果：文档本体和汇总。
type ExportResult struct {
	Document *canvasdoc.Document `json:"document"`
	Report   ExportReport        `json:"report"`
}

// Export 把当前用户的一张画布导出成文档：素材 ID 换成内容哈希，角色卡内联进文件。
// 个别素材读不到不会让导出失败，只计入 Report.SkippedAssets；画布不存在或不属于该用户返回 ErrCanvasNotFound。
func (s *CanvasTransferService) Export(ctx context.Context, userID, id uint64) (*ExportResult, error) {
	// 1. 带 user_id 取画布：别人的画布和不存在的画布统一返回「画布不存在」，不暴露画布是否存在
	p, err := s.canvases.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrCanvasNotFound
	}
	if err != nil {
		return nil, err
	}

	// 2. 扫描画布内容，找出会被导出的素材 ID 和角色卡 ID；内容不是合法 JSON 时原样返回错误
	refs, err := canvasdoc.CollectRefs(p.PayloadJSON)
	if err != nil {
		return nil, err
	}

	// 3. 批量查角色卡：十六进制 ID 用 idcodec 解码，解不开的是脏数据，直接忽略（等同于已删除的卡）；
	//    查不到的卡不会出现在结果里，Build 会据此去掉节点上的 characterId
	characters, cardAssetIDs, err := s.loadCharacters(ctx, userID, refs.CharacterIDs)
	if err != nil {
		return nil, err
	}

	// 4. 逐个素材取内容哈希：画布引用的素材，加上角色卡的主图、补充图和声音（Build 在同一张表里查卡的素材）
	assets, err := s.loadAssets(ctx, userID, slices.Concat(refs.AssetIDs, cardAssetIDs))
	if err != nil {
		return nil, err
	}

	// 5. 交给 Build 改写；payload 非法、改写后残留原环境字段等错误原样返回
	doc, report, err := canvasdoc.Build(canvasdoc.ExportInput{
		Title:      p.Title,
		Payload:    p.PayloadJSON,
		Assets:     assets,
		Characters: characters,
		AppVersion: s.appVersion,
		Now:        s.now(),
	})
	if err != nil {
		return nil, err
	}
	return &ExportResult{Document: doc, Report: ExportReport{SkippedAssets: report.SkippedAssets}}, nil
}

// loadCharacters 按画布里的十六进制角色卡 ID 批量查出该用户的角色卡，返回以十六进制 ID 为键的条目，
// 以及这些卡引用的素材 ID（十进制字符串，去重）。解不开的 ID 与查不到的卡都不出现在结果里。
func (s *CanvasTransferService) loadCharacters(ctx context.Context, userID uint64, hexIDs []string) (map[string]canvasdoc.CharacterEntry, []string, error) {
	// 同一个主键可能被写成不同的十六进制串（大小写），所以记下每个主键对应的所有原串
	hexByID := make(map[uint64][]string, len(hexIDs))
	ids := make([]uint64, 0, len(hexIDs))
	for _, h := range hexIDs {
		id, ok := idcodec.Decode(h)
		if !ok {
			continue
		}
		if _, seen := hexByID[id]; !seen {
			ids = append(ids, id)
		}
		hexByID[id] = append(hexByID[id], h)
	}
	entries := map[string]canvasdoc.CharacterEntry{}
	if len(ids) == 0 {
		return entries, nil, nil
	}
	cards, err := s.characters.ListByIDs(ctx, userID, ids)
	if err != nil {
		return nil, nil, err
	}

	var assetIDs []string
	for _, c := range cards {
		entry := canvasdoc.CharacterEntry{
			Name:          c.Name,
			Description:   c.Description,
			AvatarAssetID: strconv.FormatUint(c.AvatarAssetID, 10),
			ExtraAssetIDs: make([]string, 0, len(c.ExtraAssetIDs)),
		}
		assetIDs = append(assetIDs, entry.AvatarAssetID)
		for _, extra := range c.ExtraAssetIDs {
			extraID := strconv.FormatUint(extra, 10)
			entry.ExtraAssetIDs = append(entry.ExtraAssetIDs, extraID)
			assetIDs = append(assetIDs, extraID)
		}
		if c.VoiceAssetID != nil {
			entry.VoiceAssetID = strconv.FormatUint(*c.VoiceAssetID, 10)
			assetIDs = append(assetIDs, entry.VoiceAssetID)
		}
		for _, h := range hexByID[c.ID] {
			entries[h] = entry
		}
	}
	return entries, assetIDs, nil
}

// loadAssets 为这批素材 ID（十进制字符串，可重复）取内容哈希和元信息，返回以素材 ID 为键的条目。
// 读不到的素材（含 ID 不是合法数字的）不放进结果，Build 会把它们计为跳过。
func (s *CanvasTransferService) loadAssets(ctx context.Context, userID uint64, ids []string) (map[string]canvasdoc.AssetEntry, error) {
	entries := make(map[string]canvasdoc.AssetEntry, len(ids))
	tried := make(map[string]bool, len(ids))
	for _, raw := range ids {
		if tried[raw] {
			continue
		}
		tried[raw] = true
		assetID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || assetID == 0 {
			continue
		}
		hash, a, err := s.assets.EnsureSha256(ctx, userID, assetID)
		if errors.Is(err, ErrAssetUnreadable) {
			// 导出不能因个别素材丢失而整体失败：跳过它，文件里该素材的引用会被去掉并计入跳过数
			continue
		}
		if err != nil {
			// 其他错误（如回填哈希时数据库故障）不是素材本身的问题，不能悄悄丢素材后仍报导出成功
			return nil, err
		}
		entries[raw] = canvasdoc.AssetEntry{
			Hash: hash,
			Info: canvasdoc.AssetInfo{
				Kind:       a.Kind,
				MimeType:   a.MimeType,
				ByteSize:   a.ByteSize,
				Width:      a.Width,
				Height:     a.Height,
				DurationMs: a.DurationMs,
				FileName:   a.FileName,
			},
		}
	}
	return entries, nil
}
