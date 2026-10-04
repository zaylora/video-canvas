package service

import (
	"context"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/imageproc"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/storage"
)

const (
	// variantPublicMaxAge 是公开读处理地址（永久稳定、不带签名）允许浏览器缓存跳转的时长。
	variantPublicMaxAge = 24 * time.Hour
	// variantFallbackMaxAge 是回退到原图时跳转的缓存上限：处理服务发布后要尽快生效，所以不能缓存太久。
	variantFallbackMaxAge = 60 * time.Second
)

// AssetVariantResolver 为素材变体（缩略图 / 封面）解析处理地址，由 ImageProcessorService 实现。
type AssetVariantResolver interface {
	// Resolve 返回素材变体的处理地址：存储有已发布的处理服务且支持该变体时 ok=true；
	// 没有处理服务或不支持时 ok=false，调用方按“回退”处理；生成地址失败返回 error，调用方也应回退。
	Resolve(ctx context.Context, storageID uint64, h *storage.Handle, key, variant string) (VariantTarget, bool, error)
}

var _ AssetVariantResolver = (*ImageProcessorService)(nil)

// SetVariantResolver 设置缩略图 / 封面的处理服务解析器；不设置时所有变体都按“没有处理服务”回退。
func (s *AssetService) SetVariantResolver(r AssetVariantResolver) { s.variants = r }

// ResolveVariant 决定 /files/<key>?v=thumb|poster 怎么响应：
// 素材所在存储有已发布的处理服务时跳转到厂商的处理地址；否则缩略图回退到原图，封面返回 404
// （视频没有“原图可回退”：回退到几十 MB 的原视频会让低缩放下的大画布更慢，前端显示占位即可）。
func (s *AssetService) ResolveVariant(ctx context.Context, key, variant string) (*FileTarget, error) {
	// 1. 变体只有两种，取值不对按不存在处理
	if variant != imageproc.VariantThumb && variant != imageproc.VariantPoster {
		return nil, errcode.ErrAssetNotFound
	}

	// 2. 反查素材与所在存储
	a, h, err := s.lookupFile(ctx, key)
	if err != nil {
		return nil, err
	}

	// 3. 变体必须与素材种类对应：缩略图只给图片，封面只给视频，音频没有变体。
	//    先于解析器判断，避免对不可能有变体的素材白白去生成地址
	if (variant == imageproc.VariantThumb && a.Kind != "image") || (variant == imageproc.VariantPoster && a.Kind != "video") {
		return nil, errcode.ErrAssetNotFound
	}

	// 4. 有处理服务就跳转到处理地址；生成地址出错只记日志并回退，不能让画布因为处理服务出问题而坏掉
	if s.variants != nil {
		tgt, ok, err := s.variants.Resolve(ctx, a.StorageID, h, key, variant)
		switch {
		case err != nil:
			logger.Warn("生成素材变体地址失败，已回退", zap.Uint64("storage_id", a.StorageID), zap.String("variant", variant), zap.Error(err))
		case ok:
			return &FileTarget{RedirectURL: tgt.URL, MaxAge: s.variantMaxAge(h, tgt)}, nil
		}
	}

	// 5. 回退：封面没有可回退的原图；缩略图回到原图，并把缓存压短
	if variant == imageproc.VariantPoster {
		return nil, errcode.ErrAssetNotFound
	}
	t, err := s.targetOf(ctx, h, key, a)
	if err != nil {
		return nil, err
	}
	if t.MaxAge > variantFallbackMaxAge {
		t.MaxAge = variantFallbackMaxAge
	}
	return t, nil
}

// variantMaxAge 决定处理地址的跳转缓存时长：公开读地址永久稳定，可以缓存一天；
// 带签名的地址不能超过签名有效期的 1/4，保证浏览器拿到的地址一定还没过期。
func (s *AssetService) variantMaxAge(h *storage.Handle, t VariantTarget) time.Duration {
	if !t.Signed {
		return variantPublicMaxAge
	}
	ttl := s.signedTTL(h)
	if ttl <= 0 {
		ttl = time.Hour
	}
	return ttl / 4
}
