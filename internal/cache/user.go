package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"video-canvas/internal/model"
)

const userTTL = 30 * time.Minute

// UserCache 缓存用户详情。rdb 为 nil（未启用 Redis）时所有方法都是空操作。
type UserCache struct {
	rdb *redis.Client
}

func NewUserCache(rdb *redis.Client) *UserCache {
	return &UserCache{rdb: rdb}
}

func userKey(id uint64) string {
	return fmt.Sprintf("user:%d", id)
}

// Get 未命中或未启用时返回 (nil, nil)。
func (c *UserCache) Get(ctx context.Context, id uint64) (*model.User, error) {
	if c.rdb == nil {
		return nil, nil
	}
	data, err := c.rdb.Get(ctx, userKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var u model.User
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *UserCache) Set(ctx context.Context, u *model.User) error {
	if c.rdb == nil {
		return nil
	}
	data, err := json.Marshal(u)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, userKey(u.ID), data, userTTL).Err()
}

func (c *UserCache) Delete(ctx context.Context, id uint64) error {
	if c.rdb == nil {
		return nil
	}
	return c.rdb.Del(ctx, userKey(id)).Err()
}
