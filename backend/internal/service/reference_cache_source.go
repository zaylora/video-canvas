package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
)

// referenceHash 用 NUL 分隔由存储规则限制过的字段，生成不会暴露用户路径的固定长度标识。
func referenceHash(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(hash[:])
}

// referenceSource 重用已有的不可变素材摘要；旧素材流式计算摘要并从临时文件上传。
func (s *ReferenceCache) referenceSource(ctx context.Context, task provider.TaskRef, file *provider.AssetFile) (referenceSource, error) {
	if file == nil || file.Asset == nil || file.Body == nil || file.Remote || file.Asset.UserID != task.UserID {
		return referenceSource{}, referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheMissing, "本地参考素材不存在")
	}
	a := file.Asset
	limit := int64(referenceCacheMaxFile)
	if s.cfg.MaxBytes > 0 {
		limit = min(limit, s.cfg.MaxBytes)
	}
	if a.ByteSize > limit {
		return referenceSource{}, referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheCapacity, "本地参考素材超过 R2 临时容量上限")
	}
	digest := strings.ToLower(a.Sha256)
	if validReferenceDigest(digest) && a.ByteSize > 0 {
		return referenceSource{asset: a, body: file.Body, size: a.ByteSize, digest: digest, close: func() {}}, nil
	}
	return spoolReferenceSource(ctx, file, limit)
}

// spoolReferenceSource 在已限制大小的临时文件上完成哈希，避免将大视频或音频读入内存。
func spoolReferenceSource(ctx context.Context, file *provider.AssetFile, limit int64) (referenceSource, error) {
	h := sha256.New()
	tmp, size, err := spoolAsset(ctx, io.TeeReader(file.Body, h), limit)
	if err != nil {
		var business *errcode.Error
		if errors.As(err, &business) {
			return referenceSource{}, referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheCapacity, "本地参考素材超过 R2 临时容量上限")
		}
		return referenceSource{}, referenceOperationError(ctx, err, "读取本地参考素材失败")
	}
	closeFile := func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}
	if size <= 0 || (file.Asset.ByteSize > 0 && file.Asset.ByteSize != size) {
		closeFile()
		return referenceSource{}, referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheMissing, "本地参考素材为空或大小异常")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		closeFile()
		return referenceSource{}, referenceOperationError(ctx, err, "无法读取临时准备文件")
	}
	return referenceSource{asset: file.Asset, body: tmp, size: size, digest: hex.EncodeToString(h.Sum(nil)), close: closeFile}, nil
}

// validReferenceDigest 验证摘要是 32 字节的十六进制。
func validReferenceDigest(digest string) bool {
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == sha256.Size
}
