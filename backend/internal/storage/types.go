// Package storage 是素材的存储抽象：内置的本地磁盘，以及 S3 兼容的对象存储（阿里云 OSS / 腾讯云 COS / AWS S3 / Cloudflare R2）。
// 存储配置在数据库里，由 Registry 按 id 解析并缓存客户端。
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound 对象不存在。
var ErrNotFound = errors.New("storage: object not found")

// ErrTooLarge 声明的文件大小超过上限，不签发直传地址。
var ErrTooLarge = errors.New("storage: 文件超过大小上限")

// Storage 是对象存储的最小接口。
type Storage interface {
	// Put 写入对象。size 未知传 -1。
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Open 读取对象，不存在返回 ErrNotFound。
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// URL 返回可访问的地址：公开桶 / 本地静态路由返回固定地址，私有桶返回带过期时间的签名地址。
	URL(ctx context.Context, key string, ttl time.Duration) (string, error)
	// Delete 删除对象，不存在不算错误。
	Delete(ctx context.Context, key string) error
}

// ObjectInfo 是对象的元信息。
type ObjectInfo struct {
	Size        int64
	ContentType string
}

// Statter 是能查询对象元信息的存储，直传登记时用它复核大小。
type Statter interface {
	// Stat 返回对象元信息，不存在返回 ErrNotFound。
	Stat(ctx context.Context, key string) (ObjectInfo, error)
}

// RangeOpener 是能只读取对象一段字节的存储，直传登记时用它读文件头做类型嗅探。
type RangeOpener interface {
	// OpenRange 读取 [offset, offset+length) 这一段，不存在返回 ErrNotFound。
	OpenRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error)
}

// DirectUploadRequest 是申请一次浏览器直传所需的信息。
type DirectUploadRequest struct {
	Key         string        // 业务 key（不含路径前缀）
	ContentType string        // 嗅探后 / 客户端声明的类型，会被签进策略或地址
	Size        int64         // 声明的文件大小；预签名 PUT 必须提供，并会被签进地址
	MaxSize     int64         // 大小上限
	TTL         time.Duration // 地址有效期
}

// DirectUpload 是给浏览器的上传凭证。
type DirectUpload struct {
	Method  string            // post：multipart 表单 POST；put：直接 PUT 文件内容
	URL     string            // 上传地址
	Fields  map[string]string // Method=post 时必须原样放进表单的字段（file 字段放最后）
	Headers map[string]string // Method=put 时必须带上的请求头（Content-Length 由浏览器按文件自动设置）
}

// DirectUploader 是支持浏览器直传的存储。
type DirectUploader interface {
	// DirectMethod 返回直传方式：DirectPostPolicy 或 DirectPresignedPut。
	DirectMethod() string
	// DirectUpload 签发一次直传凭证。
	DirectUpload(ctx context.Context, req DirectUploadRequest) (*DirectUpload, error)
}

// BucketChecker 是能检查“桶是否存在且有权访问”的存储，测试连接的第一步用它。
//
// 不能靠“读一个不存在的对象”判断：对象级的 HEAD 请求没有响应体，桶不存在和对象不存在都是 404，无法区分。
type BucketChecker interface {
	// CheckBucket 桶存在且可访问返回 nil；桶不存在、无权访问等返回带原因的错误。
	CheckBucket(ctx context.Context) error
}
