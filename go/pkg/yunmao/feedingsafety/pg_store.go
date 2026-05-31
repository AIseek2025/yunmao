package feedingsafety

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgStore 把投喂安全策略持久化到 `feeding_safety_policies` 表。
//
// 多副本一致性：admin-svc 写入 PG，feeding-svc 从 PG 读；本结构带 30s 缓存避免每次都查 DB。
// 调用方可在写入后 `Invalidate("")` 立即触发 reload。
//
// 与 ADR-0006 / migration 0002 对齐。
type PgStore struct {
	pool *pgxpool.Pool

	mu     sync.RWMutex
	cache  map[string]cachedLimits
	ttl    time.Duration
	now    func() time.Time
	region string
}

type cachedLimits struct {
	limits  Limits
	expires time.Time
	exists  bool
}

// NewPgStore 构造；TTL=0 用默认 30s。
func NewPgStore(pool *pgxpool.Pool, ttl time.Duration) *PgStore {
	if ttl == 0 {
		ttl = 30 * time.Second
	}
	return &PgStore{
		pool:   pool,
		cache:  map[string]cachedLimits{},
		ttl:    ttl,
		now:    time.Now,
		region: "global",
	}
}

// Get 从缓存或 PG 读取；roomID="" 取全局。
func (s *PgStore) Get(ctx context.Context, roomID string) (Limits, bool, error) {
	now := s.now()
	s.mu.RLock()
	if c, ok := s.cache[roomID]; ok && now.Before(c.expires) {
		s.mu.RUnlock()
		return c.limits, c.exists, nil
	}
	s.mu.RUnlock()

	var (
		roomCD, userCD, daily int
		wStart, wEnd          *string
	)
	key := roomID
	if key == "" {
		key = "__global__"
	}
	err := s.pool.QueryRow(ctx, `
		SELECT room_cooldown_sec, user_room_cooldown_sec, cat_daily_limit,
		       no_feed_window_start, no_feed_window_end
		FROM feeding_safety_policies
		WHERE room_id = $1
	`, key).Scan(&roomCD, &userCD, &daily, &wStart, &wEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		s.mu.Lock()
		s.cache[roomID] = cachedLimits{expires: now.Add(s.ttl), exists: false}
		s.mu.Unlock()
		return Limits{}, false, nil
	}
	if err != nil {
		return Limits{}, false, err
	}
	l := Limits{
		RoomCooldown:     time.Duration(roomCD) * time.Second,
		UserRoomCooldown: time.Duration(userCD) * time.Second,
		CatDailyLimit:    uint32(daily),
	}
	s.mu.Lock()
	s.cache[roomID] = cachedLimits{limits: l, expires: now.Add(s.ttl), exists: true}
	s.mu.Unlock()
	return l, true, nil
}

// Put upsert 一行。
func (s *PgStore) Put(ctx context.Context, roomID string, l Limits) error {
	if l.RoomCooldown < 0 || l.UserRoomCooldown < 0 {
		return errors.New("feedingsafety: durations must be >=0")
	}
	key := roomID
	if key == "" {
		key = "__global__"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO feeding_safety_policies
		(room_id, room_cooldown_sec, user_room_cooldown_sec, cat_daily_limit, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (room_id) DO UPDATE
		SET room_cooldown_sec = EXCLUDED.room_cooldown_sec,
		    user_room_cooldown_sec = EXCLUDED.user_room_cooldown_sec,
		    cat_daily_limit = EXCLUDED.cat_daily_limit,
		    updated_at = NOW()
	`, key, int(l.RoomCooldown.Seconds()), int(l.UserRoomCooldown.Seconds()), int(l.CatDailyLimit))
	if err != nil {
		return err
	}
	// 立即失效缓存，让下一次 Get 走 DB。
	s.Invalidate(roomID)
	return nil
}

// Invalidate 主动清缓存。
func (s *PgStore) Invalidate(roomID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, roomID)
}
