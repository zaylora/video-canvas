package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	// defaultSignedTTL 调用方没给 ttl 时的签名 URL 有效期。
	defaultSignedTTL = time.Hour
	// s3PartSize 未知大小上传时的分片大小；不设置的话 minio-go 会按 5TiB/10000 预分配约 500MB 缓冲。
	s3PartSize = 16 << 20
)

// S3Storage 是 S3 兼容存储（阿里云 OSS / 腾讯云 COS / AWS S3 / Cloudflare R2），用 NewFromSpec 按服务商预设创建。
type S3Storage struct {
	client     *minio.Client
	bucket     string
	prefix     string // 对象 key 前缀，已规范成 "" 或 "a/b/"
	publicBase string // 公开桶 / CDN 前缀（已去掉末尾 /），为空则用签名 URL
	direct     string // 浏览器直传方式，空值按 post_policy 处理
}

var (
	_ Statter        = (*S3Storage)(nil)
	_ RangeOpener    = (*S3Storage)(nil)
	_ DirectUploader = (*S3Storage)(nil)
	_ BucketChecker  = (*S3Storage)(nil)
)

// NewFromSpec 按服务商预设创建 S3 兼容存储：先校验并规范化配置，再按寻址方式创建客户端。
// OSS / COS 必须用虚拟主机寻址，R2 用 path 寻址，这些都由预设决定，调用方不用关心。
func NewFromSpec(spec Spec) (*S3Storage, error) {
	n, err := spec.Normalize()
	if err != nil {
		return nil, err
	}
	if n.AccessKey == "" || n.SecretKey == "" {
		return nil, errors.New("storage: AccessKey 与 Secret（密钥）不能为空")
	}
	lookup := minio.BucketLookupAuto
	switch n.Addressing {
	case AddressingVirtual:
		lookup = minio.BucketLookupDNS
	case AddressingPath:
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(n.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(n.AccessKey, n.SecretKey, ""),
		Secure:       n.UseSSL,
		Region:       n.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: 创建 s3 客户端失败: %w", err)
	}
	p, _ := PresetOf(n.Provider)
	return &S3Storage{
		client:     client,
		bucket:     n.Bucket,
		prefix:     normalizePrefix(n.PathPrefix),
		publicBase: n.PublicBaseURL,
		direct:     p.DirectMethod,
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

// isS3NotFound 判断是不是“对象不存在”。桶不存在（NoSuchBucket）也是 404，但它是配置错误而不是对象缺失，
// 必须当成真实错误返回，否则探针会把填错的桶名误判为通过。
func isS3NotFound(err error) bool {
	resp := minio.ToErrorResponse(err)
	if resp.Code == "NoSuchBucket" {
		return false
	}
	return resp.Code == "NoSuchKey" || resp.Code == "NotFound" || resp.StatusCode == 404
}

// Stat 返回对象元信息，不存在返回 ErrNotFound。
func (s *S3Storage) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	k, err := s.objectKey(key)
	if err != nil {
		return ObjectInfo{}, ErrNotFound
	}
	st, err := s.client.StatObject(ctx, s.bucket, k, minio.StatObjectOptions{})
	if err != nil {
		if isS3NotFound(err) {
			return ObjectInfo{}, ErrNotFound
		}
		return ObjectInfo{}, fmt.Errorf("storage: 读取对象信息失败: %w", err)
	}
	return ObjectInfo{Size: st.Size, ContentType: st.ContentType}, nil
}

// OpenRange 读取 [offset, offset+length) 这一段字节；length 必须大于 0。
func (s *S3Storage) OpenRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	k, err := s.objectKey(key)
	if err != nil {
		return nil, ErrNotFound
	}
	if offset < 0 || length <= 0 {
		return nil, errors.New("storage: 读取范围不合法")
	}
	opts := minio.GetObjectOptions{}
	if err := opts.SetRange(offset, offset+length-1); err != nil {
		return nil, fmt.Errorf("storage: 设置读取范围失败: %w", err)
	}
	obj, err := s.client.GetObject(ctx, s.bucket, k, opts)
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

// DirectMethod 返回浏览器直传方式，由服务商预设决定；预设没指定时按 POST Policy 处理。
func (s *S3Storage) DirectMethod() string {
	if s.direct == "" {
		return DirectPostPolicy
	}
	return s.direct
}

// DirectUpload 签发一次直传凭证，方式由服务商预设决定。
//
// 声明的大小一旦超过上限就不签发（两种方式都一样）。POST Policy 额外在桶侧用 content-length-range 强制上限；
// 预签名 PUT 没有这层保护，所以要把 Content-Length 和 Content-Type 一起签进地址，登记时再 Stat 复核。
func (s *S3Storage) DirectUpload(ctx context.Context, req DirectUploadRequest) (*DirectUpload, error) {
	k, err := s.objectKey(req.Key)
	if err != nil {
		return nil, err
	}
	if req.MaxSize > 0 && req.Size > req.MaxSize {
		return nil, ErrTooLarge
	}
	ttl := req.TTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}

	if s.DirectMethod() == DirectPresignedPut {
		if req.Size <= 0 || req.ContentType == "" {
			return nil, errors.New("storage: 预签名 PUT 必须声明文件大小与类型")
		}
		hdr := http.Header{}
		hdr.Set("Content-Type", req.ContentType)
		hdr.Set("Content-Length", strconv.FormatInt(req.Size, 10))
		u, err := s.client.PresignHeader(ctx, http.MethodPut, s.bucket, k, ttl, nil, hdr)
		if err != nil {
			return nil, fmt.Errorf("storage: 生成预签名 PUT 地址失败: %w", err)
		}
		return &DirectUpload{Method: "put", URL: u.String(), Headers: map[string]string{"Content-Type": req.ContentType}}, nil
	}

	pp := minio.NewPostPolicy()
	if err := pp.SetBucket(s.bucket); err != nil {
		return nil, err
	}
	if err := pp.SetKey(k); err != nil {
		return nil, err
	}
	if err := pp.SetExpires(time.Now().UTC().Add(ttl)); err != nil {
		return nil, err
	}
	if req.ContentType != "" {
		if err := pp.SetContentType(req.ContentType); err != nil {
			return nil, err
		}
	}
	if req.MaxSize > 0 {
		if err := pp.SetContentLengthRange(1, req.MaxSize); err != nil {
			return nil, err
		}
	}
	u, fields, err := s.client.PresignedPostPolicy(ctx, pp)
	if err != nil {
		return nil, fmt.Errorf("storage: 生成 POST Policy 失败: %w", err)
	}
	return &DirectUpload{Method: "post", URL: u.String(), Fields: fields}, nil
}

// CheckBucket 用 HEAD 桶检查桶是否存在且当前密钥可访问：404 明确表示桶不存在，403 表示无权访问。
func (s *S3Storage) CheckBucket(ctx context.Context) error {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("storage: 检查桶失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("storage: NoSuchBucket: 桶 %s 不存在", s.bucket)
	}
	return nil
}
