package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

// ErrObjectExists 表示条件上传期间另一个调用已提交同一对象。
var ErrObjectExists = errors.New("storage: 临时对象已存在")

// ErrReferenceAccessDenied 表示对象存储拒绝访问临时前缀。
var ErrReferenceAccessDenied = errors.New("storage: 临时对象访问被拒绝")

// ErrReferenceBucketMissing 表示 R2 明确报告目标桶不存在。
var ErrReferenceBucketMissing = errors.New("storage: 临时对象桶不存在")

// ReferenceObjectInfo 是复用前所需的对象元数据。
type ReferenceObjectInfo struct {
	Size   int64
	SHA256 string
}

// ReferenceStorage 是临时素材所需的 S3 条件写入和前缀枚举能力。
type ReferenceStorage interface {
	Storage
	StatReference(ctx context.Context, key string) (ReferenceObjectInfo, error)
	PutReference(ctx context.Context, key string, body io.Reader, size int64, contentType, digest string) error
	ListReferences(ctx context.Context, prefix string, visit func(key string, size int64, modified time.Time) error) error
}

var _ ReferenceStorage = (*S3Storage)(nil)

// StatReference 查询大小及上传时登记的摘要；只把对象级 404 作为缺失。
func (s *S3Storage) StatReference(ctx context.Context, key string) (ReferenceObjectInfo, error) {
	k, err := s.objectKey(key)
	if err != nil {
		return ReferenceObjectInfo{}, err
	}
	info, err := s.client.StatObject(ctx, s.bucket, k, minio.StatObjectOptions{})
	if err != nil {
		response := minio.ToErrorResponse(err)
		if response.StatusCode == http.StatusNotFound && response.Code != "NoSuchBucket" {
			return ReferenceObjectInfo{}, ErrNotFound
		}
		return ReferenceObjectInfo{}, fmt.Errorf("查询临时对象失败: %w", referenceStorageError(err))
	}
	return ReferenceObjectInfo{Size: info.Size, SHA256: info.Metadata.Get("X-Amz-Meta-Sha256")}, nil
}

// PutReference 使用单次条件 PUT 创建对象；并发请求不能覆盖已提交的同名对象。
func (s *S3Storage) PutReference(ctx context.Context, key string, body io.Reader, size int64, contentType, digest string) error {
	k, err := s.objectKey(key)
	if err != nil {
		return err
	}
	opts := minio.PutObjectOptions{
		ContentType: contentType, DisableMultipart: true, UserMetadata: map[string]string{"sha256": digest},
	}
	opts.SetMatchETagExcept("*")
	if _, err := s.client.PutObject(ctx, s.bucket, k, body, size, opts); err != nil {
		if minio.ToErrorResponse(err).StatusCode == http.StatusPreconditionFailed {
			return ErrObjectExists
		}
		return fmt.Errorf("上传临时对象失败: %w", referenceStorageError(err))
	}
	return nil
}

// ListReferences 枚举指定前缀，回调拿到的 key 与 PutReference 使用相同的业务路径。
func (s *S3Storage) ListReferences(ctx context.Context, prefix string, visit func(string, int64, time.Time) error) error {
	if prefix != "" {
		if err := ValidateKey(strings.TrimSuffix(prefix, "/")); err != nil {
			return err
		}
	}
	listCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for info := range s.client.ListObjects(listCtx, s.bucket, minio.ListObjectsOptions{Prefix: s.prefix + prefix, Recursive: true}) {
		if info.Err != nil {
			return fmt.Errorf("列举对象失败: %w", referenceStorageError(info.Err))
		}
		if err := visit(strings.TrimPrefix(info.Key, s.prefix), info.Size, info.LastModified); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func referenceStorageError(err error) error {
	response := minio.ToErrorResponse(err)
	switch {
	case response.Code == "NoSuchBucket":
		return ErrReferenceBucketMissing
	case response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized:
		return ErrReferenceAccessDenied
	default:
		return err
	}
}
