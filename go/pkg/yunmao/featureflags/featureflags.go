// Package featureflags 提供运行时灰度开关：
//
//   - `Store` 抽象支持内存（dev/test）与 PostgreSQL（feature_flags 表）。
//   - `Manager` 在 service 启动时拉一次全表，并按 TTL（默认 10s）后台轮询刷新。
//   - 开关三种取值：`enabled`(bool) + `scope`(string) + `value`(JSON)。
//
// 与 go/migrations/0004_fourth_iteration.sql `feature_flags` 表对齐。
package featureflags

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Flag 单条开关。
type Flag struct {
	Name      string         `json:"name"`
	Enabled   bool           `json:"enabled"`
	Scope     string         `json:"scope"`
	Value     map[string]any `json:"value"`
	UpdatedAt time.Time      `json:"updated_at"`
	UpdatedBy string         `json:"updated_by,omitempty"`
}

// Store 持久层抽象。
type Store interface {
	List(ctx context.Context) ([]Flag, error)
	Get(ctx context.Context, name string) (*Flag, error)
	Set(ctx context.Context, f Flag) error
}

// ErrNotFound 未找到。
var ErrNotFound = errors.New("featureflags: not found")

// ---------- MemoryStore ----------

// NewMemoryStore 进程内实现。
func NewMemoryStore(initial ...Flag) Store {
	m := &memoryStore{m: map[string]*Flag{}}
	for _, f := range initial {
		cp := f
		if cp.Value == nil {
			cp.Value = map[string]any{}
		}
		if cp.UpdatedAt.IsZero() {
			cp.UpdatedAt = time.Now().UTC()
		}
		m.m[cp.Name] = &cp
	}
	return m
}

type memoryStore struct {
	mu sync.RWMutex
	m  map[string]*Flag
}

func (s *memoryStore) List(_ context.Context) ([]Flag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Flag, 0, len(s.m))
	for _, v := range s.m {
		out = append(out, *v)
	}
	return out, nil
}

func (s *memoryStore) Get(_ context.Context, name string) (*Flag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.m[name]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *f
	return &cp, nil
}

func (s *memoryStore) Set(_ context.Context, f Flag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.Value == nil {
		f.Value = map[string]any{}
	}
	f.UpdatedAt = time.Now().UTC()
	cp := f
	s.m[f.Name] = &cp
	return nil
}

// ---------- PgStore ----------

// NewPgStore PostgreSQL 实现。
func NewPgStore(pool *pgxpool.Pool) Store { return &pgStore{pool: pool} }

type pgStore struct {
	pool *pgxpool.Pool
}

func (p *pgStore) List(ctx context.Context) ([]Flag, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT name, enabled, scope, COALESCE(value, '{}'::jsonb), updated_at, COALESCE(updated_by, '')
		FROM feature_flags
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Flag{}
	for rows.Next() {
		f, err := scanFlag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (p *pgStore) Get(ctx context.Context, name string) (*Flag, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT name, enabled, scope, COALESCE(value, '{}'::jsonb), updated_at, COALESCE(updated_by, '')
		FROM feature_flags WHERE name = $1
	`, name)
	f := &Flag{}
	var valJSON []byte
	if err := row.Scan(&f.Name, &f.Enabled, &f.Scope, &valJSON, &f.UpdatedAt, &f.UpdatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal(valJSON, &f.Value)
	if f.Value == nil {
		f.Value = map[string]any{}
	}
	return f, nil
}

func (p *pgStore) Set(ctx context.Context, f Flag) error {
	val, err := json.Marshal(f.Value)
	if err != nil {
		return err
	}
	if f.Scope == "" {
		f.Scope = "global"
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO feature_flags (name, enabled, scope, value, updated_at, updated_by)
		VALUES ($1, $2, $3, $4::jsonb, NOW(), $5)
		ON CONFLICT (name) DO UPDATE SET
		  enabled = EXCLUDED.enabled,
		  scope = EXCLUDED.scope,
		  value = EXCLUDED.value,
		  updated_at = NOW(),
		  updated_by = EXCLUDED.updated_by
	`, f.Name, f.Enabled, f.Scope, string(val), f.UpdatedBy)
	return err
}

func scanFlag(rows pgx.Rows) (*Flag, error) {
	f := &Flag{}
	var valJSON []byte
	if err := rows.Scan(&f.Name, &f.Enabled, &f.Scope, &valJSON, &f.UpdatedAt, &f.UpdatedBy); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(valJSON, &f.Value)
	if f.Value == nil {
		f.Value = map[string]any{}
	}
	return f, nil
}

// ---------- Manager（带缓存 + 后台刷新） ----------

// Config Manager 配置。
type Config struct {
	Store        Store
	RefreshEvery time.Duration // 默认 10s
}

// Manager 单实例多读消费者，按 TTL 后台刷新缓存。
type Manager struct {
	cfg   Config
	mu    sync.RWMutex
	cache map[string]*Flag
}

// NewManager 构造并启动后台轮询。
func NewManager(cfg Config) *Manager {
	if cfg.RefreshEvery == 0 {
		cfg.RefreshEvery = 10 * time.Second
	}
	if cfg.Store == nil {
		cfg.Store = NewMemoryStore()
	}
	m := &Manager{cfg: cfg, cache: map[string]*Flag{}}
	_ = m.refresh(context.Background())
	return m
}

// Start 启动后台 ticker；调用者负责 cancel。
func (m *Manager) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(m.cfg.RefreshEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = m.refresh(ctx)
			}
		}
	}()
}

func (m *Manager) refresh(ctx context.Context) error {
	flags, err := m.cfg.Store.List(ctx)
	if err != nil {
		return err
	}
	next := map[string]*Flag{}
	for i := range flags {
		cp := flags[i]
		next[cp.Name] = &cp
	}
	m.mu.Lock()
	m.cache = next
	m.mu.Unlock()
	return nil
}

// Get 查单个开关；不存在返回 defaultFlag。
func (m *Manager) Get(name string) Flag {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if f, ok := m.cache[name]; ok {
		return *f
	}
	return Flag{Name: name, Enabled: false, Scope: "global", Value: map[string]any{}}
}

// Bool 取 enabled 字段；未配置时返回 def。
func (m *Manager) Bool(name string, def bool) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if f, ok := m.cache[name]; ok {
		return f.Enabled
	}
	return def
}

// Int 从 value 中提取整数（key）；不存在返回 def。
func (m *Manager) Int(name, key string, def int) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if f, ok := m.cache[name]; ok {
		if v, ok := f.Value[key]; ok {
			switch t := v.(type) {
			case float64:
				return int(t)
			case int:
				return t
			case int64:
				return int(t)
			}
		}
	}
	return def
}

// IsRoomInGrayPercent 计算 hash(room_id) % 100 < gray_percent，且 flag.Enabled=true。
//
// 用于 WebRTC 灰度等基于房间维度的渐进发布（ADR-0023）：
//
//   - 当 flag 不存在或 enabled=false → 返回 false；
//   - 否则按 FNV-1a hash 取 [0,100)，与 value.gray_percent 比较；缺省 gray_percent=0；
//   - gray_percent>=100 → 全量；<=0 → 全部回滚。
func (m *Manager) IsRoomInGrayPercent(flag, roomID string) bool {
	m.mu.RLock()
	f, ok := m.cache[flag]
	m.mu.RUnlock()
	if !ok || !f.Enabled || roomID == "" {
		return false
	}
	pct := 0
	if v, ok := f.Value["gray_percent"]; ok {
		switch t := v.(type) {
		case float64:
			pct = int(t)
		case int:
			pct = t
		case int64:
			pct = int(t)
		}
	}
	if pct >= 100 {
		return true
	}
	if pct <= 0 {
		return false
	}
	return Hash100(roomID) < pct
}

// Hash100 计算 FNV-1a hash(room_id) % 100；纯函数便于灰度均匀性测试。
func Hash100(s string) int {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return int(h % 100)
}

// StringSlice 从 value 中提取 []string（key）；不存在返回空。
func (m *Manager) StringSlice(name, key string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if f, ok := m.cache[name]; ok {
		if v, ok := f.Value[key]; ok {
			if arr, ok := v.([]any); ok {
				out := make([]string, 0, len(arr))
				for _, e := range arr {
					if s, ok := e.(string); ok {
						out = append(out, s)
					}
				}
				return out
			}
		}
	}
	return nil
}

// Set 写回（PG 即写表，memory 即写 map），同步刷新缓存。
func (m *Manager) Set(ctx context.Context, f Flag) error {
	if err := m.cfg.Store.Set(ctx, f); err != nil {
		return err
	}
	return m.refresh(ctx)
}
