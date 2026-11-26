package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"video-canvas/internal/imageproc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/outbound"
	"video-canvas/internal/provider"
)

const canvasPosterMaxBytes = 8 << 20

var canvasPosterClient = outbound.Client("asset.canvas_poster", &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
})

// OpenCanvasPoster 按视频素材归属读取同一处理服务生成的封面。
func (s *AssetService) OpenCanvasPoster(ctx context.Context, userID, assetID uint64) (*provider.AssetFile, error) {
	asset, err := s.Get(ctx, userID, assetID)
	if err != nil {
		return nil, err
	}
	if s.variants == nil || asset.Kind != "video" || asset.StorageKey == "" {
		return nil, provider.ErrAssetNotFound
	}
	handle, err := s.stores.Get(ctx, asset.StorageID)
	if err != nil {
		return nil, provider.ErrAssetNotFound
	}
	target, ok, err := s.variants.Resolve(ctx, asset.StorageID, handle, asset.StorageKey, imageproc.VariantPoster)
	if err != nil || !ok {
		return nil, provider.ErrAssetNotFound
	}
	return readCanvasPoster(ctx, asset, target.URL)
}

func readCanvasPoster(ctx context.Context, asset *model.Asset, url string) (*provider.AssetFile, error) {
	request, err := http.NewRequestWithContext(outbound.WithPrivateURL(ctx), http.MethodGet, url, nil)
	if err != nil {
		return nil, provider.ErrAssetNotFound
	}
	response, err := canvasPosterClient.Do(request)
	if err != nil {
		return nil, provider.ErrAssetNotFound
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > canvasPosterMaxBytes {
		return nil, provider.ErrAssetNotFound
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, canvasPosterMaxBytes+1))
	if err != nil || len(data) > canvasPosterMaxBytes {
		return nil, provider.ErrAssetNotFound
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		cover := *asset
		cover.MimeType, cover.ByteSize = mime, int64(len(data))
		return &provider.AssetFile{Asset: &cover, Body: io.NopCloser(bytes.NewReader(data))}, nil
	default:
		return nil, provider.ErrAssetNotFound
	}
}
