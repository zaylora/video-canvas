package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

var (
	// ErrAssetUnreadable 表示素材不存在、不属于该用户，或它的文件在存储里读不到。
	// 导出时据此把这个素材标成“缺失”，而不是让整个导出失败。
	ErrAssetUnreadable = errors.New("素材文件不可读")
	// ErrAssetNotFoundByHash 表示按内容哈希找不到任何一份可读的素材文件，导入时据此把素材标成“缺失”。
	ErrAssetNotFoundByHash = errors.New("按哈希找不到可用的素材文件")
)

// EnsureSha256 返回当前用户一份素材的内容哈希与素材行；老素材还没有哈希时，从它所在的存储读出文件补算并写回。
// 素材不存在、不属于该用户、或文件读不到，都返回 ErrAssetUnreadable（可用 errors.Is 判断）。
func (s *AssetService) EnsureSha256(ctx context.Context, userID, assetID uint64) (string, *model.Asset, error) {
	// 1. 带 user_id 取素材：不存在和不属于当前用户统一按“不可读”处理，不暴露别人的素材是否存在
	a, err := s.repo.GetByID(ctx, userID, assetID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", nil, ErrAssetUnreadable
		}
		return "", nil, err
	}

	// 2. 已经有哈希（新上传的素材，或之前补算过）就直接返回，不再读存储
	if a.Sha256 != "" {
		return a.Sha256, a, nil
	}

	// 3. 从素材自己记录的存储读出文件并计算哈希；存储里没有这个对象说明数据已不一致，同样按“不可读”处理
	body, err := s.openAssetObject(ctx, a)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrAssetUnreadable, err)
	}
	defer func() { _ = body.Close() }()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, body); err != nil {
		return "", nil, fmt.Errorf("%w: 读取素材失败: %w", ErrAssetUnreadable, err)
	}
	sum := hex.EncodeToString(hasher.Sum(nil))

	// 4. 写回库：只在库里哈希仍为空时才写，同一内容的哈希恒定，所以并发补算同一素材最多是重复读一次文件，
	//    结果一致，不需要 singleflight
	if err := s.repo.SetSha256IfEmpty(ctx, a.ID, sum); err != nil {
		return "", nil, fmt.Errorf("回填素材哈希失败: %w", err)
	}
	a.Sha256 = sum
	return sum, a, nil
}

// CopyBySha256 按内容哈希与大小找到一份已有的素材文件，复制到当前默认存储的新 key 下，
// 返回尚未入库的素材行（UserID 为 userID，其余元数据取自源素材，Source 固定为 upload）；入库由调用方在事务里完成，
// 事务失败时调用 DiscardCopies 回收这份副本。没有任何可读的候选返回 ErrAssetNotFoundByHash；请求已取消时返回包装了 ctx.Err() 的错误。
func (s *AssetService) CopyBySha256(ctx context.Context, userID uint64, sha string, byteSize int64) (*model.Asset, error) {
	// 1. 按哈希 + 大小查候选。故意不按用户过滤：导入的 JSON 里的哈希是不可猜测的内容摘要，
	//    谁拿到了这份 JSON，就说明他持有这份内容，允许他认领；调用方只会得到文件副本，拿不到别人的素材行
	candidates, err := s.repo.FindBySha256(ctx, sha, byteSize)
	if err != nil {
		return nil, fmt.Errorf("按哈希查询素材失败: %w", err)
	}
	if len(candidates) == 0 {
		return nil, ErrAssetNotFoundByHash
	}

	// 2. 取当前默认存储：副本写入哪里、记录哪个 storage_id、失败清理哪里，都用这一份，与 ingest 一致
	dst, err := s.stores.Default(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取默认存储失败: %w", err)
	}

	// 3. 依次尝试候选：源文件读得通的第一个就用；读不通（存储已删、对象丢失）只记日志并换下一个，
	//    因为同一内容常有多份拷贝，只要有一份可用就不该导入失败。
	//    每轮先看请求是否已取消：取消后每个候选都会「读不通」，不能误报成找不到素材
	for i := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("按哈希复制素材: %w", err)
		}
		src := &candidates[i]
		body, err := s.openAssetObject(ctx, src)
		if err != nil {
			logger.Warn("按哈希复制素材时跳过不可读的候选", zap.Uint64("asset_id", src.ID), zap.Error(err))
			continue
		}

		// 4. 复制到新 key 而不是共用源 key：storage_key 全局唯一（/files/<key> 靠它反查素材），
		//    而且源素材被原用户删除时会连带删除对象，共用会让导入者的素材一起失效。
		//    key 格式与 ingest 一致：u{用户ID}/{年月}/{随机uuid}{扩展名}
		key := fmt.Sprintf("u%d/%s/%s%s", userID, s.now().Format("200601"), newAssetUUID(), assetMimeExt[src.MimeType])
		putErr := dst.Storage.Put(ctx, key, body, src.ByteSize, src.MimeType)
		_ = body.Close()
		if putErr != nil {
			// 目标存储写失败不是候选的问题，换候选也不会好；清理可能已部分落地的对象后直接返回
			s.cleanup(ctx, dst.Storage, key)
			return nil, fmt.Errorf("复制素材到存储失败: %w", putErr)
		}

		return &model.Asset{
			UserID:     userID,
			Kind:       src.Kind,
			StorageID:  dst.ID,
			StorageKey: key,
			MimeType:   src.MimeType,
			ByteSize:   src.ByteSize,
			Width:      src.Width,
			Height:     src.Height,
			DurationMs: src.DurationMs,
			Source:     model.AssetSourceUpload,
			FileName:   src.FileName,
			Sha256:     src.Sha256,
		}, nil
	}
	return nil, ErrAssetNotFoundByHash
}

// DiscardCopies 删除 CopyBySha256 产生的副本对象，用于导入事务失败后的回收。
// 尽力而为：存储已不存在或删除失败只记日志，不返回错误（此时已无法补救，运维可按日志清理孤儿对象）。
func (s *AssetService) DiscardCopies(ctx context.Context, assets []*model.Asset) {
	for _, a := range assets {
		// 每个副本按它自己记录的 storage_id 找存储，不假设仍是当前默认存储
		h, err := s.stores.Get(ctx, a.StorageID)
		if err != nil {
			logger.Error("回收素材副本时获取存储失败，可能产生孤儿文件",
				zap.Uint64("storage_id", a.StorageID), zap.String("key", a.StorageKey), zap.Error(err))
			continue
		}
		cleanupObject(ctx, h.Storage, a.StorageKey)
	}
}

// openAssetObject 按素材自己记录的 storage_id 打开它的存储对象，调用方负责关闭。
func (s *AssetService) openAssetObject(ctx context.Context, a *model.Asset) (io.ReadCloser, error) {
	h, err := s.stores.Get(ctx, a.StorageID)
	if err != nil {
		return nil, fmt.Errorf("获取素材所在存储失败: %w", err)
	}
	body, err := h.Storage.Open(ctx, a.StorageKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, fmt.Errorf("素材存储对象丢失: %w", err)
		}
		return nil, fmt.Errorf("打开素材失败: %w", err)
	}
	return body, nil
}
