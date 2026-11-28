package service

import (
	"context"

	"video-canvas/internal/model"
	"video-canvas/internal/storage"
)

// assetReadRouter 允许只为素材读取选择已准备的本地镜像；写入及定向删除仍使用原存储。
type assetReadRouter interface {
	// ForAsset 返回当前策略可读取此素材的存储句柄。
	ForAsset(ctx context.Context, asset *model.Asset) (*storage.Handle, error)
}

// storageForAsset 在应用组装了混合策略时把文件读取交给策略。
func (s *AssetService) storageForAsset(ctx context.Context, a *model.Asset) (*storage.Handle, error) {
	if router, ok := s.stores.(assetReadRouter); ok {
		return router.ForAsset(ctx, a)
	}
	return s.stores.Get(ctx, a.StorageID)
}
