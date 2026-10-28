package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// PrepareDramaVideo 将 ffmpeg 成片写入当前默认存储，返回尚未登记的素材以便和任务原子提交。
func (s *AssetService) PrepareDramaVideo(ctx context.Context, userID uint64, path string) (*model.Asset, error) {
	// 1. 本地文件来自合成器，仍限制成片大小，沿用既有视频元数据和哈希探测能力。
	f, err := os.Open(path) //nolint:gosec // path 由服务端合成临时目录产生，不接收用户路径。
	if err != nil {
		return nil, fmt.Errorf("读取合成成片失败：%w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	limit := s.cfg.MaxResult
	if limit <= 0 {
		limit = defaultMaxResult
	}
	if info.Size() == 0 || info.Size() > limit {
		return nil, errcode.ErrAssetInvalid.WithMsg("成片为空或超过大小限制")
	}
	meta := probeAssetMeta("video/mp4", f, info.Size())
	hash := sha256.New()
	if _, err := io.Copy(hash, io.NewSectionReader(f, 0, info.Size())); err != nil {
		return nil, fmt.Errorf("计算成片哈希失败：%w", err)
	}
	// 2. 先上传到不可猜测的独立对象；不创建生成任务或资产库归属，来源沿用 upload。
	h, err := s.stores.Default(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取成片存储失败：%w", err)
	}
	key := fmt.Sprintf("u%d/%s/%s.mp4", userID, s.now().Format("200601"), newAssetUUID())
	if err := h.Storage.Put(ctx, key, f, info.Size(), "video/mp4"); err != nil {
		s.cleanup(ctx, h.Storage, key)
		return nil, fmt.Errorf("写入成片存储失败：%w", err)
	}
	return &model.Asset{UserID: userID, Kind: "video", StorageID: h.ID, StorageKey: key, MimeType: "video/mp4", ByteSize: info.Size(), Width: meta.Width, Height: meta.Height, DurationMs: meta.DurationMs, Source: model.AssetSourceUpload, FileName: "成片.mp4", Sha256: hex.EncodeToString(hash.Sum(nil))}, nil
}
