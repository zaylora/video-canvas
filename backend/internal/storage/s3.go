package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"video-canvas/internal/config"
)

const (
	// defaultSignedTTL 调用方没给 ttl 时的签名 URL 有效期。
	defaultSignedTTL = time.Hour
	// s3PartSize 未知大小上传时的分片大小；不设置的话 minio-go 会按 5TiB/10000 预分配约 500MB 缓冲。
	s3PartSize = 16 << 20
)

// S3Storage 是 S3 兼容存储（AWS S3 / MinIO / 阿里云 OSS 等，OSS 填 endpoint 即可）。
type S3Storage struct {
	client     *minio.Client
	bucket     string
	prefix     string // 对象 key 前缀，已规范成 "" 或 "a/b/"
	publicBase string // 公开桶 / CDN 前缀（已去掉末尾 /），为空则用签名 URL
}

// NewS3 创建 S3 存储。minio.New 不会发起网络请求；
// 建议配置 region，否则首次签名时 minio-go 会先请求一次 bucket location。
func NewS3(cfg config.S3Storage) (*S3Storage, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, errors.New("storage: s3.endpoint 与 s3.bucket 不能为空")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("storage: s3.access_key 与 s3.secret_key 不能为空（请通过环境变量 APP_STORAGE_S3_ACCESS_KEY / APP_STORAGE_S3_SECRET_KEY 提供）")
	}
	// endpoint 允许误带协议头，minio-go 要求只写 host[:port]
	endpoint := strings.TrimPrefix(strings.TrimPrefix(cfg.Endpoint, "https://"), "http://")
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: 创建 s3 客户端失败: %w", err)
	}
	return &S3Storage{
		client:     client,
		bucket:     cfg.Bucket,
		prefix:     normalizePrefix(cfg.PathPrefix),
		publicBase: strings.TrimRight(cfg.PublicBaseURL, "/"),
	}, nil
}

// normalizePrefix 把 path_prefix 规范成 "" 或 "a/b/"（去掉首尾 /）。
func normalizePrefix(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

// objectKey 拼上前缀得到桶内真实 key，同时校验业务 key。
func (s *S3Storage) objectKey(key string) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	return s.prefix + key, nil
}

// Put 上传对象。size 未知（-1）时走分片流式上传。
func (s *S3Storage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	k, err := s.objectKey(key)
	if err != nil {
		return err
	}
	opts := minio.PutObjectOptions{ContentType: contentType}
	if size < 0 {
		opts.PartSize = s3PartSize
	}
	if _, err := s.client.PutObject(ctx, s.bucket, k, r, size, opts); err != nil {
		return fmt.Errorf("storage: 上传对象失败: %w", err)
	}
	return nil
}

// Open 读取对象。GetObject 是惰性的，这里 Stat 一次以便在对象不存在时立刻返回 ErrNotFound。
func (s *S3Storage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	k, err := s.objectKey(key)
	if err != nil {
		return nil, ErrNotFound
	}
	obj, err := s.client.GetObject(ctx, s.bucket, k, minio.GetObjectOptions{})
	if err != nil {
		if isS3NotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: 读取对象失败: %w", err)
	}
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if isS3NotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: 读取对象信息失败: %w", err)
	}
	return obj, nil
}

// URL 配置了 PublicBaseURL 时返回公开地址（不过期），否则返回带 ttl 的预签名 GET URL。
func (s *S3Storage) URL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	k, err := s.objectKey(key)
	if err != nil {
		return "", err
	}
	if s.publicBase != "" {
		return s.publicBase + "/" + escapePath(k), nil
	}
	if ttl <= 0 {
		ttl = defaultSignedTTL
	}
	u, err := s.client.PresignedGetObject(ctx, s.bucket, k, ttl, nil)
	if err != nil {
		return "", fmt.Errorf("storage: 生成签名 URL 失败: %w", err)
	}
	return u.String(), nil
}

// Delete 删除对象，不存在不算错误。
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	k, err := s.objectKey(key)
	if err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, k, minio.RemoveObjectOptions{}); err != nil {
		if isS3NotFound(err) {
			return nil
		}
		return fmt.Errorf("storage: 删除对象失败: %w", err)
	}
	return nil
}

// escapePath 按段转义路径（保留 /），用于拼公开地址。
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return strings.Join(segs, "/")
}

func isS3NotFound(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.Code == "NoSuchKey" || resp.Code == "NotFound" || resp.StatusCode == 404
}
