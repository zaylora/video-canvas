// Package storage 是素材的对象存储抽象：本地磁盘（仅开发）、S3 兼容（含阿里云 OSS）。
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound 对象不存在。
var ErrNotFound = errors.New("storage: object not found")

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
