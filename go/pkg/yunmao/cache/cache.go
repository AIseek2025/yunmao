// Package cache 封装 Redis 客户端 + 业务原语：冷却、幂等、令牌桶限流、计数。
//
// 设计目标：
//
//   - 不暴露 *redis.Client；调用方使用 [`Cooldown`]、[`Idempotent`]、[`RateLimiter`] 这些
//     业务接口，便于把 RedisStore 切换为 MemoryStore（单测）。
//   - 与 `cooldown` / `idempotent` 子包语义保持一致：Outcome / Insert 返回值含义不变。
//   - 所有 Redis 操作都加 `yunmao:` 前缀，避免与其它系统共享 redis 时发生 key 冲突。
package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "yunmao:"

// Backend 后端类型。
type Backend string

const (
	BackendMemory Backend = "memory"
	BackendRedis  Backend = "redis"
)

// Config 缓存配置。
type Config struct {
	Backend Backend
	URL     string // redis://host:6379/0
}

// Store 是统一 KV 抽象。MemoryStore / RedisStore 都实现它。
type Store interface {
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	Get(ctx context.Context, key string) (string, bool, error)
	IncrBy(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Del(ctx context.Context, key string) error
	Close() error
}

// Open 工厂；按 cfg.Backend 选择实现。
func Open(ctx context.Context, cfg Config) (Store, error) {
	switch cfg.Backend {
	case BackendMemory, "":
		return NewMemoryStore(), nil
	case BackendRedis:
		if cfg.URL == "" {
			return nil, errors.New("cache: redis URL required")
		}
		opt, err := redis.ParseURL(cfg.URL)
		if err != nil {
			return nil, fmt.Errorf("cache: parse url: %w", err)
		}
		c := redis.NewClient(opt)
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := c.Ping(pingCtx).Err(); err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("cache: ping: %w", err)
		}
		return &RedisStore{client: c}, nil
	default:
		return nil, fmt.Errorf("cache: unknown backend %q", cfg.Backend)
	}
}

func K(parts ...string) string {
	b := keyPrefix
	for i, p := range parts {
		if i > 0 {
			b += ":"
		}
		b += p
	}
	return b
}

// --------------------------------------------------------------------------------------
// RedisStore
// --------------------------------------------------------------------------------------

// RedisStore 是 redis 实现。
type RedisStore struct{ client *redis.Client }

func NewRedisStore(c *redis.Client) *RedisStore { return &RedisStore{client: c} }

func (s *RedisStore) Client() *redis.Client { return s.client }

func (s *RedisStore) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return s.client.Set(ctx, key, value, ttl).Err()
}

func (s *RedisStore) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	ok, err := s.client.SetNX(ctx, key, value, ttl).Result()
	return ok, err
}

func (s *RedisStore) Get(ctx context.Context, key string) (string, bool, error) {
	v, err := s.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *RedisStore) IncrBy(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	pipe := s.client.TxPipeline()
	incr := pipe.IncrBy(ctx, key, delta)
	if ttl > 0 {
		pipe.Expire(ctx, key, ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

func (s *RedisStore) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return s.client.Expire(ctx, key, ttl).Err()
}

func (s *RedisStore) Del(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}

func (s *RedisStore) Close() error { return s.client.Close() }

// --------------------------------------------------------------------------------------
// MemoryStore —— 单测 / EVENT_BUS=memory 模式时使用。
// --------------------------------------------------------------------------------------

type entry struct {
	val     string
	counter int64
	expire  time.Time
}

// MemoryStore 进程内 KV。
type MemoryStore struct {
	mu  sync.Mutex
	m   map[string]*entry
	now func() time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{m: map[string]*entry{}, now: time.Now}
}

func (s *MemoryStore) gc(now time.Time) {
	for k, e := range s.m {
		if !e.expire.IsZero() && now.After(e.expire) {
			delete(s.m, k)
		}
	}
}

func (s *MemoryStore) Set(_ context.Context, key string, value string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp := time.Time{}
	if ttl > 0 {
		exp = s.now().Add(ttl)
	}
	s.m[key] = &entry{val: value, expire: exp}
	return nil
}

func (s *MemoryStore) SetNX(_ context.Context, key string, value string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.gc(now)
	if e, ok := s.m[key]; ok && (e.expire.IsZero() || now.Before(e.expire)) {
		return false, nil
	}
	exp := time.Time{}
	if ttl > 0 {
		exp = now.Add(ttl)
	}
	s.m[key] = &entry{val: value, expire: exp}
	return true, nil
}

func (s *MemoryStore) Get(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.gc(now)
	e, ok := s.m[key]
	if !ok {
		return "", false, nil
	}
	return e.val, true, nil
}

func (s *MemoryStore) IncrBy(_ context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.gc(now)
	e, ok := s.m[key]
	if !ok {
		exp := time.Time{}
		if ttl > 0 {
			exp = now.Add(ttl)
		}
		e = &entry{expire: exp}
		s.m[key] = e
	}
	e.counter += delta
	// 保持 val 与 counter 同步，使 Get 返回字符串形式（与 Redis IncrBy + Get 一致）。
	e.val = formatInt(e.counter)
	return e.counter, nil
}

func formatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func (s *MemoryStore) Expire(_ context.Context, key string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.m[key]; ok {
		e.expire = s.now().Add(ttl)
	}
	return nil
}

func (s *MemoryStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func (s *MemoryStore) Close() error { return nil }
