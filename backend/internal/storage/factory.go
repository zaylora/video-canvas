package storage

import (
	"fmt"

	"video-canvas/internal/config"
)

// New 按 storage.driver 创建存储实现：local（仅开发，空值等同 local）或 s3（含阿里云 OSS）。
//
// 选用 local 时，调用方需要挂静态路由：
//
//	if ls, ok := st.(*storage.LocalStorage); ok { r.GET("/files/*filepath", storage.FileServer(ls)) }
func New(cfg config.Storage) (Storage, error) {
	switch cfg.Driver {
	case "", "local":
		return NewLocal(cfg.Local)
	case "s3":
		return NewS3(cfg.S3)
	default:
		return nil, fmt.Errorf("storage: 不支持的 driver %q（可选 local / s3）", cfg.Driver)
	}
}
