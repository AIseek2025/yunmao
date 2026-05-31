package cache

import (
	"context"
	"time"
)

// RateLimiter 是固定窗口限流器（粗粒度，适用于反爬 / 防刷）。
// 高峰场景可由 Redis 端的 Lua 脚本升级为令牌桶；当前 PoC 不引入 Lua，避免与 mock store 偏离。
type RateLimiter struct {
	store  Store
	limit  int64
	window time.Duration
}

// NewRateLimiter 构造。
func NewRateLimiter(store Store, limit int64, window time.Duration) *RateLimiter {
	return &RateLimiter{store: store, limit: limit, window: window}
}

// Allow 返回 (是否允许, 当前计数, 错误)。
// key 通常组合 user_id + endpoint，比如 "feed:usr_x"。
func (r *RateLimiter) Allow(ctx context.Context, key string) (bool, int64, error) {
	bucket := K("rl", key, time.Now().UTC().Truncate(r.window).Format("150405"))
	cnt, err := r.store.IncrBy(ctx, bucket, 1, r.window+time.Second)
	if err != nil {
		return false, 0, err
	}
	return cnt <= r.limit, cnt, nil
}
