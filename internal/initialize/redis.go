package initialize

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"video-canvas/internal/config"
)

// NewRedis 未启用时返回 (nil, nil)，调用方据此降级。
func NewRedis(cfg config.Redis) (*redis.Client, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("ping redis %s: %w", cfg.Addr, err)
	}
	return rdb, nil
}
