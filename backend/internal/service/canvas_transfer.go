package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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

// TransferRepo 是画布导入需要的事务入口，由 repository 实现。
type TransferRepo interface {
	// WithTx 在一个数据库事务里执行 fn：fn 返回 nil 提交，返回错误回滚。
	WithTx(ctx context.Context, fn func(tx TransferTx) error) error
}

// TransferTx 是导入事务内可用的写入与查询操作，都只作用于当前事务：
// CreateAsset、CreateCanvas。
// 定义在 repository 包（与 PaymentTx、GenerationTaskTx 同样的做法）：WithTx 的回调签名两边必须是同一个类型，
// 而 repository 不能反过来 import service。
type TransferTx = repository.CanvasTransferTx

// 编译期确认真实实现满足导入导出依赖的接口。
var (
	_ TransferAssets = (*AssetService)(nil)
	_ TransferRepo   = (*repository.CanvasTransferRepository)(nil)
)

// CanvasTransferDeps 是 CanvasTransferService 的依赖。
type CanvasTransferDeps struct {
	Canvases CanvasProjectRepo
	Assets   TransferAssets
	Tx       TransferRepo
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

// Export 把当前用户的一张画布导出成文档：素材 ID 换成内容哈希；角色数据内嵌在角色节点里，随节点一起导出。
// 个别素材读不到不会让导出失败，只计入 Report.SkippedAssets；画布不存在、不属于该用户或属于短剧返回 ErrCanvasNotFound；
// 导出结果超出导入上限（节点、素材数、素材总大小、文件大小）返回 ErrCanvasExportTooLarge。
func (s *CanvasTransferService) Export(ctx context.Context, userID, id uint64) (*ExportResult, error) {
	// 1. 带 user_id 取画布：别人的画布和不存在的画布统一返回「画布不存在」，不暴露画布是否存在
	p, err := s.canvases.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrCanvasNotFound
	}
	if err != nil {
		return nil, err
	}
	if p.DramaID != nil {
		return nil, errcode.ErrCanvasNotFound
	}

	// 2. 扫描画布内容，找出会被导出的素材 ID（含角色节点内嵌的补充图和音频音色）；内容不是合法 JSON 时原样返回错误
	refs, err := canvasdoc.CollectRefs(p.PayloadJSON)
	if err != nil {
		return nil, err
	}

	// 3. 逐个素材取内容哈希
	assets, err := s.loadAssets(ctx, userID, refs.AssetIDs)
	if err != nil {
		return nil, err
	}

	// 4. 交给 Build 改写；payload 非法、改写后残留原环境字段等错误原样返回
	doc, report, err := canvasdoc.Build(canvasdoc.ExportInput{
		Title:      p.Title,
		Payload:    p.PayloadJSON,
		Assets:     assets,
		AppVersion: s.appVersion,
		Now:        s.now(),
	})
	if err != nil {
		return nil, err
	}

	// 5. 按导入上限检查导出结果：超限的文件导出了也导不回来，不如现在就说清楚原因（30008）
	if err := s.checkExportLimits(doc); err != nil {
		return nil, err
	}
	return &ExportResult{Document: doc, Report: ExportReport{SkippedAssets: report.SkippedAssets}}, nil
}

// checkExportLimits 用导入的上限检查导出文档：节点数、素材数、素材总大小，以及文件大小。
// 文件大小按两格缩进序列化来量，因为前端下载时就是这样写文件的（JSON.stringify(data, null, 2)），
// 导入时比的是这个文件的字节数；Go 会把 <>& 转义得更长，量出来只会偏大，不会放过超限的文件。
func (s *CanvasTransferService) checkExportLimits(doc *canvasdoc.Document) error {
	if err := doc.CheckLimits(s.limits); err != nil {
		if errors.Is(err, canvasdoc.ErrLimit) {
			return errcode.ErrCanvasExportTooLarge.WithMsg(
				errcode.ErrCanvasExportTooLarge.Msg + "：" + strings.TrimPrefix(err.Error(), canvasdoc.ErrLimit.Error()+": "))
		}
		return err
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化导出文档: %w", err)
	}
	if int64(len(raw)) > s.limits.MaxBytes {
		return errcode.ErrCanvasExportTooLarge.WithMsg(
			errcode.ErrCanvasExportTooLarge.Msg + "：导出文件超过 " + formatBytes(s.limits.MaxBytes))
	}
	return nil
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

// 导入的默认值与长度规则。
const (
	importTitleSuffix      = "（导入）"
	importTitleFallback    = "未命名画布"
	maxImportTitleLen      = 60 // 导入画布标题（含后缀）最多 60 字
	importedCanvasRevision = 1
)

// ImportResult 是一次导入的结果：新画布和汇总。
type ImportResult struct {
	Canvas ImportedCanvas `json:"canvas"`
	Report ImportReport   `json:"report"`
}

// ImportedCanvas 是导入生成的新画布。
type ImportedCanvas struct {
	ID       idcodec.ID `json:"id"`       // 画布 ID（十六进制串）
	Title    string     `json:"title"`    // 标题，原标题加「（导入）」
	Revision uint64     `json:"revision"` // 版本号，新画布恒为 1
}

// ImportReport 是导入过程的情况汇总，前端据此决定直接打开画布还是先弹出详情。
type ImportReport struct {
	Assets       ImportAssets `json:"assets"`
	EdgesDropped int          `json:"edges_dropped"` // 两端节点不存在而丢弃的连线数
}

// ImportAssets 是素材复制的汇总。
type ImportAssets struct {
	Total   int                  `json:"total"`   // 画布节点（含角色节点内嵌的补充图和音频音色）用到的不同素材数
	Copied  int                  `json:"copied"`  // 成功复制的素材数
	Missing []ImportMissingAsset `json:"missing"` // 找不到的素材，按节点列出
}

// ImportMissingAsset 是某个节点里找不到的一个素材。
type ImportMissingAsset struct {
	NodeID   string `json:"node_id"`
	FileName string `json:"file_name"`
}

// copiedAsset 是一份已复制到导入者存储、尚未入库的素材，hash 是它在文档里的键。
type copiedAsset struct {
	hash  string
	asset *model.Asset
}

// Import 把导出文档还原成当前用户的一张新画布（revision 1），并复制节点用到的素材。角色数据内嵌在节点里，随画布一起还原。
// 个别素材找不到不会让导入失败，只记进报告；文件不合法返回 30005，版本过高返回 30006，
// 超出大小或数量上限返回 30007；存储或数据库故障原样返回（不是用户能修的问题）。
func (s *CanvasTransferService) Import(ctx context.Context, userID uint64, document []byte) (*ImportResult, error) {
	// 1. 原文大小先查：Parse 要把整份文件解码进内存，超大文件不该走到那一步；Validate 看不到原文，只能在这里查
	if int64(len(document)) > s.limits.MaxBytes {
		return nil, errcode.ErrCanvasImportTooLarge.WithMsg("导入文件超过 " + formatBytes(s.limits.MaxBytes))
	}

	// 2. 解析与校验：格式、版本、体量上限、引用完整；再扫描原环境字段，拒绝手改文件夹带进来的旧素材 / 任务 ID。
	//    canvasdoc 的哨兵错误翻译成导入错误码（版本过高 30006、超限 30007、其余 30005），具体原因写进提示
	doc, err := canvasdoc.Parse(document)
	if err == nil {
		err = doc.Validate(s.limits)
	}
	if err == nil {
		err = canvasdoc.CheckDocumentClean(doc)
	}
	if err != nil {
		return nil, importDocError(err)
	}

	// 3. 只处理节点真正用到的素材：assets 里没被引用的条目忽略（设计 §8.4），
	//    否则手改的文件能借哈希把任意文件悄悄认领进素材库
	hashes := canvasdoc.ReferencedSets(doc)

	// 4. 事务外复制素材文件：复制是存储网络 I/O，不能放进数据库事务（A5）。
	//    哈希已排序，逐个复制保证同一份文件的结果与报错稳定；同一哈希在画布里出现多少次都只复制一份
	copies, err := s.copyAssets(ctx, userID, doc, hashes)
	if err != nil {
		return nil, err
	}

	// 5. 一个事务内写入：素材行 → 还原画布内容 → 新画布；任一步失败整体回滚
	var (
		canvas  *model.CanvasProject
		restore canvasdoc.RestoreReport
	)
	err = s.tx.WithTx(ctx, func(tx TransferTx) error {
		restored, err := insertCopies(ctx, tx, copies, s.assets.FileURL)
		if err != nil {
			return err
		}
		payload, rep, err := canvasdoc.Restore(canvasdoc.RestoreInput{Doc: doc, Assets: restored})
		if err != nil {
			return err
		}
		restore = rep
		canvas = &model.CanvasProject{
			UserID: userID, Title: importTitle(doc.Canvas.Title), PayloadJSON: payload, Revision: importedCanvasRevision,
		}
		return tx.CreateCanvas(ctx, canvas)
	})

	// 6. 事务失败时数据库已回滚，但复制出的文件还在存储里，删掉它们，免得留下没有素材行的孤儿文件。
	//    用 WithoutCancel：失败常常就是因为请求被取消，而回收时找存储可能要查数据库，跟着取消就会留下孤儿文件
	if err != nil {
		s.assets.DiscardCopies(context.WithoutCancel(ctx), copyAssetsOf(copies))
		return nil, err
	}

	// 7. 汇总报告：素材总数只算被引用的；缺失素材以 Restore 按节点给出的为准
	report := ImportReport{
		Assets: ImportAssets{
			Total: len(hashes), Copied: len(copies),
			Missing: make([]ImportMissingAsset, 0, len(restore.MissingAssets)),
		},
		EdgesDropped: restore.EdgesDropped,
	}
	for _, m := range restore.MissingAssets {
		report.Assets.Missing = append(report.Assets.Missing, ImportMissingAsset{NodeID: m.NodeID, FileName: m.FileName})
	}
	return &ImportResult{
		Canvas: ImportedCanvas{ID: idcodec.ID(canvas.ID), Title: canvas.Title, Revision: canvas.Revision},
		Report: report,
	}, nil
}

// importDocError 把 canvasdoc 的解析 / 校验错误翻译成导入错误码，提示里只放去掉哨兵前缀的中文原因；
// 不是这三类的错误原样返回。
func importDocError(err error) error {
	reason := func(sentinel error) string {
		return strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
	}
	switch {
	case errors.Is(err, canvasdoc.ErrTooNew):
		return errcode.ErrCanvasImportTooNew.WithMsg(reason(canvasdoc.ErrTooNew))
	case errors.Is(err, canvasdoc.ErrLimit):
		return errcode.ErrCanvasImportTooLarge.WithMsg(reason(canvasdoc.ErrLimit))
	case errors.Is(err, canvasdoc.ErrInvalid):
		return errcode.ErrCanvasImportInvalid.WithMsg(reason(canvasdoc.ErrInvalid))
	}
	return err
}

// copyAssets 按给定顺序（调用方已按字典序排好）逐个复制这些哈希的素材，返回复制成功的副本（同样有序）。
// 找不到的哈希跳过（Restore 会按节点报告缺失）；其他错误回收已复制的副本后返回。
func (s *CanvasTransferService) copyAssets(ctx context.Context, userID uint64, doc *canvasdoc.Document, hashes []string) ([]copiedAsset, error) {
	copies := make([]copiedAsset, 0, len(hashes))
	for _, h := range hashes {
		a, err := s.assets.CopyBySha256(ctx, userID, h, doc.Assets[h].ByteSize)
		if errors.Is(err, ErrAssetNotFoundByHash) {
			continue
		}
		if err != nil {
			// 存储故障（含请求被取消）不是文件的问题，不能悄悄当成缺失后报导入成功；已复制的文件不会再有人引用，先回收。
			// 回收用 WithoutCancel：请求取消后仍要删干净，否则留下孤儿文件
			s.assets.DiscardCopies(context.WithoutCancel(ctx), copyAssetsOf(copies))
			return nil, fmt.Errorf("复制导入素材: %w", err)
		}
		copies = append(copies, copiedAsset{hash: h, asset: a})
	}
	return copies, nil
}

// copyAssetsOf 取出副本里的素材行，交给 DiscardCopies。
func copyAssetsOf(copies []copiedAsset) []*model.Asset {
	out := make([]*model.Asset, len(copies))
	for i, c := range copies {
		out[i] = c.asset
	}
	return out
}

// insertCopies 在事务里插入副本素材行（回填 ID），返回 Restore 需要的哈希 → 新素材 ID 与地址。
func insertCopies(ctx context.Context, tx TransferTx, copies []copiedAsset, fileURL func(string) string) (map[string]canvasdoc.RestoredAsset, error) {
	restored := make(map[string]canvasdoc.RestoredAsset, len(copies))
	for _, c := range copies {
		if err := tx.CreateAsset(ctx, c.asset); err != nil {
			return nil, err
		}
		restored[c.hash] = canvasdoc.RestoredAsset{ID: strconv.FormatUint(c.asset.ID, 10), URL: fileURL(c.asset.StorageKey)}
	}
	return restored, nil
}

// importTitle 生成导入画布的标题：原标题去首尾空白后加「（导入）」，空标题用「未命名画布」；
// 总长超过 60 字时截短原标题部分，保证后缀完整（用户靠它区分导入件）。
func importTitle(title string) string {
	base := strings.TrimSpace(title)
	if base == "" {
		base = importTitleFallback
	}
	limit := maxImportTitleLen - utf8.RuneCountInString(importTitleSuffix)
	return strings.TrimSpace(truncateRunes(base, limit)) + importTitleSuffix
}
