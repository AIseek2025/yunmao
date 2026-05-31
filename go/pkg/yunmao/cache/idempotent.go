package cache

import (
	"context"
	"time"
)

// Idempotent 是基于 SETNX 的幂等键缓存：首次写入返回 true，重复返回 false。
type Idempotent struct {
	store Store
	ttl   time.Duration
}

// NewIdempotent 构造幂等器；ttl 为单键有效期，建议 48h（>=客户端最大重试窗口）。
func NewIdempotent(store Store, ttl time.Duration) *Idempotent {
	return &Idempotent{store: store, ttl: ttl}
}

// Insert 把 namespace+key 写入；返回 (firstSeen, error)。
func (i *Idempotent) Insert(ctx context.Context, namespace, key string) (bool, error) {
	return i.store.SetNX(ctx, K("idem", namespace, key), "1", i.ttl)
}

// Has 不修改状态。
func (i *Idempotent) Has(ctx context.Context, namespace, key string) (bool, error) {
	_, ok, err := i.store.Get(ctx, K("idem", namespace, key))
	return ok, err
}
