// Package feedingsafety 抽出投喂安全阈值的配置 / 解析 / 覆盖优先级。
//
// 与 ADR-0006 对齐：room 30s，user-room 60s，cat-daily 12（克 / 次数，由调用方语义解释）。
// 加载顺序（每一项独立解析；后者覆盖前者）：
//
//  1. 全局默认（[`DefaultGlobal`]）
//  2. env：YUNMAO_FEED_ROOM_COOLDOWN_SEC / YUNMAO_FEED_USER_ROOM_COOLDOWN_SEC / YUNMAO_FEED_CAT_DAILY
//  3. 数据库 / admin-svc 推送（[`Store`] 接口）
//
// feeding-svc 在每次校验时再读一遍 store，使运营可在线热调而无需重启。
package feedingsafety

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"
)

// Limits 是一组阈值。零值表示采用全局默认。
type Limits struct {
	RoomCooldown     time.Duration
	UserRoomCooldown time.Duration
	CatDailyLimit    uint32
}

// DefaultGlobal MVP 默认值（ADR-0006）。
var DefaultGlobal = Limits{
	RoomCooldown:     30 * time.Second,
	UserRoomCooldown: 60 * time.Second,
	CatDailyLimit:    12,
}

// Store 提供覆盖值（如来自 PostgreSQL feeding_policies 表 / admin-svc API）。
// roomID == "" 表示读全局默认。
type Store interface {
	Get(ctx context.Context, roomID string) (Limits, bool, error)
	Put(ctx context.Context, roomID string, l Limits) error
}

// Manager 按 roomID 解析最终生效阈值，优先级：room 覆盖 > 全局覆盖 > env > 默认。
type Manager struct {
	store Store
	mu    sync.RWMutex
	env   Limits // env 解析一次，运行期不变
}

// NewManager 构造管理器；env 立刻读一次。
func NewManager(store Store) *Manager {
	return &Manager{store: store, env: ParseEnv()}
}

// Resolve 按 roomID 返回最终生效阈值。
func (m *Manager) Resolve(ctx context.Context, roomID string) (Limits, error) {
	cur := DefaultGlobal
	cur = merge(cur, m.env)
	if m.store != nil {
		if g, ok, err := m.store.Get(ctx, ""); err != nil {
			return Limits{}, err
		} else if ok {
			cur = merge(cur, g)
		}
		if roomID != "" {
			if r, ok, err := m.store.Get(ctx, roomID); err != nil {
				return Limits{}, err
			} else if ok {
				cur = merge(cur, r)
			}
		}
	}
	return cur, nil
}

// ParseEnv 从环境变量读取覆盖值。
func ParseEnv() Limits {
	var l Limits
	if v := os.Getenv("YUNMAO_FEED_ROOM_COOLDOWN_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			l.RoomCooldown = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("YUNMAO_FEED_USER_ROOM_COOLDOWN_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			l.UserRoomCooldown = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("YUNMAO_FEED_CAT_DAILY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			l.CatDailyLimit = uint32(n)
		}
	}
	return l
}

// merge 把 override 中非零字段叠加到 base 上。
func merge(base, override Limits) Limits {
	if override.RoomCooldown > 0 {
		base.RoomCooldown = override.RoomCooldown
	}
	if override.UserRoomCooldown > 0 {
		base.UserRoomCooldown = override.UserRoomCooldown
	}
	if override.CatDailyLimit > 0 {
		base.CatDailyLimit = override.CatDailyLimit
	}
	return base
}

// MemoryStore 进程内 store；admin-svc 与 feeding-svc 内置使用。
type MemoryStore struct {
	mu sync.RWMutex
	m  map[string]Limits
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{m: map[string]Limits{}} }

func (s *MemoryStore) Get(_ context.Context, roomID string) (Limits, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.m[roomID]
	return l, ok, nil
}

func (s *MemoryStore) Put(_ context.Context, roomID string, l Limits) error {
	if l.RoomCooldown < 0 || l.UserRoomCooldown < 0 {
		return errors.New("feedingsafety: durations must be >=0")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[roomID] = l
	return nil
}
