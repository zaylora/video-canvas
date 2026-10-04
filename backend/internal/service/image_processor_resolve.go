package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"video-canvas/internal/imageproc"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// VariantTarget 是一个素材变体的处理地址。
type VariantTarget struct {
	URL    string // 厂商的处理地址，直接 302
	Signed bool   // 地址带签名（私有读桶）：缓存时长必须小于签名有效期；公开读地址永久稳定
}

// resolvedEntry 是缓存的“某存储已发布的处理服务”，proc 为 nil 表示这套存储没有。
type resolvedEntry struct {
	proc    *resolvedProcessor
	expires time.Time
}

type resolvedProcessor struct {
	provider imageproc.Provider
	cfg      imageproc.Config
}

// Resolve 返回素材变体的处理地址：素材所在存储有已发布的处理服务，并且它支持这个变体时 ok=true。
// 没有处理服务、或不支持这个变体（比如腾讯云未开通媒体处理的封面）时 ok=false，调用方按“回退”处理。
// 生成地址失败（比如签名出错）返回 error，调用方也应回退并记录日志，不能让画布坏掉。
func (s *ImageProcessorService) Resolve(ctx context.Context, storageID uint64, h *storage.Handle, key, variant string) (VariantTarget, bool, error) {
	// 1. 取该存储已发布的处理服务（带短缓存，上百个同画布请求不会各查一次库）
	proc, err := s.published(ctx, storageID)
	if err != nil || proc == nil {
		return VariantTarget{}, false, err
	}
	// 2. 不支持这个变体：回退
	if !proc.provider.Supports(proc.cfg, variant) {
		return VariantTarget{}, false, nil
	}
	// 3. 生成处理地址；公开读（存储配置了公开访问域名）不带签名
	u, err := proc.provider.VariantURL(ctx, imageproc.URLInput{Spec: h.Spec, Config: proc.cfg, Key: key, Variant: variant, TTL: h.SignedTTL})
	if err != nil {
		if errors.Is(err, imageproc.ErrUnsupported) {
			return VariantTarget{}, false, nil
		}
		return VariantTarget{}, false, fmt.Errorf("生成处理地址失败: %w", err)
	}
	return VariantTarget{URL: u, Signed: h.Spec.PublicBaseURL == ""}, true, nil
}

// published 返回存储当前已发布的处理服务；没有返回 nil。
func (s *ImageProcessorService) published(ctx context.Context, storageID uint64) (*resolvedProcessor, error) {
	s.mu.Lock()
	e, ok := s.cache[storageID]
	s.mu.Unlock()
	if ok && s.now().Before(e.expires) {
		return e.proc, nil
	}

	var proc *resolvedProcessor
	p, err := s.repo.GetPublishedByStorage(ctx, storageID)
	switch {
	case err == nil:
		var cfg imageproc.Config
		if err := json.Unmarshal(p.PublishedConfig, &cfg); err != nil {
			return nil, fmt.Errorf("解析处理服务 %d 的线上配置失败: %w", p.ID, err)
		}
		prov, ok := imageproc.New(p.Vendor)
		if !ok {
			return nil, fmt.Errorf("处理服务 %d 的厂商 %q 没有适配器", p.ID, p.Vendor)
		}
		proc = &resolvedProcessor{provider: prov, cfg: cfg}
	case errors.Is(err, repository.ErrNotFound):
		// 没有处理服务也缓存下来：这是绝大多数存储的常态，不能每个请求都查库
	default:
		return nil, err
	}
	s.mu.Lock()
	s.cache[storageID] = resolvedEntry{proc: proc, expires: s.now().Add(processorCacheTTL)}
	s.mu.Unlock()
	return proc, nil
}

// invalidate 让某个存储的解析缓存立刻失效。
func (s *ImageProcessorService) invalidate(storageID uint64) {
	s.mu.Lock()
	delete(s.cache, storageID)
	s.mu.Unlock()
}
