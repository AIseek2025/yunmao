// Package service admin-svc：运营后台关键开关（PoC）。
package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/featureflags"
	"yunmao.live/pkg/yunmao/feedingsafety"
)

// FeedingPolicy 单房间投喂策略；运营后台直接改这里。
type FeedingPolicy struct {
	RoomID              string `json:"room_id"`
	FeedingOpen         bool   `json:"feeding_open"`
	FeedCooldownSeconds uint32 `json:"feed_cooldown_seconds"`
	UserRoomCooldownSec uint32 `json:"user_room_cooldown_seconds"`
	NoFeedWindowStart   string `json:"no_feed_window_start,omitempty"` // HH:MM
	NoFeedWindowEnd     string `json:"no_feed_window_end,omitempty"`
	DailyLimitGrams     uint32 `json:"daily_limit_grams"`
}

// AdminService 内存版运营后台。
type AdminService struct {
	mu       sync.Mutex
	policies map[string]FeedingPolicy
	safety   feedingsafety.Store
	flags    featureflags.Store
	// 第八轮（C）：词表 sink 用于 admin PUT → chat.wordlist.updated 事件。
	wordlistSink WordlistSink
}

// WordlistEntry 词表条目。
type WordlistEntry struct {
	Region   string `json:"region"`
	Language string `json:"language"`
	Word     string `json:"word"`
	Action   string `json:"action"`
}

// WordlistSink 词表写入抽象（生产 = PG + outbox；测试 = in-memory）。
type WordlistSink interface {
	UpsertBatch(ctx context.Context, entries []WordlistEntry, updatedBy string) (version int, err error)
	List(ctx context.Context, region, language string) ([]WordlistEntry, error)
	Version(ctx context.Context) (int, error)
}

// InMemoryWordlistSink 测试 / dev 用。
type InMemoryWordlistSink struct {
	mu      sync.Mutex
	entries []WordlistEntry
	version int
}

// NewInMemoryWordlistSink 构造。
func NewInMemoryWordlistSink() *InMemoryWordlistSink { return &InMemoryWordlistSink{} }

// UpsertBatch upsert by (region,language,word)；返回最新 version。
func (s *InMemoryWordlistSink) UpsertBatch(_ context.Context, entries []WordlistEntry, _ string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := func(e WordlistEntry) string { return e.Region + "/" + e.Language + "/" + e.Word }
	exist := map[string]int{}
	for i, e := range s.entries {
		exist[key(e)] = i
	}
	for _, ne := range entries {
		if ne.Action == "" {
			ne.Action = "hide"
		}
		if ne.Region == "" {
			ne.Region = "global"
		}
		if ne.Language == "" {
			ne.Language = "zh"
		}
		if idx, ok := exist[key(ne)]; ok {
			s.entries[idx] = ne
		} else {
			s.entries = append(s.entries, ne)
			exist[key(ne)] = len(s.entries) - 1
		}
	}
	s.version++
	return s.version, nil
}

// List 列出（按 region/language 过滤；空字符串 = 全部）。
func (s *InMemoryWordlistSink) List(_ context.Context, region, language string) ([]WordlistEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]WordlistEntry, 0, len(s.entries))
	for _, e := range s.entries {
		if region != "" && e.Region != region {
			continue
		}
		if language != "" && e.Language != language {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// Version 返回当前版本。
func (s *InMemoryWordlistSink) Version(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.version, nil
}

// New 构造服务。safety 可传入 feedingsafety.MemoryStore（单实例）或 feedingsafety.PgStore
// （多副本一致）。本服务对接口只读 / 写两个能力，不感知后端。
// flags 同理：feature flags 后端可注入 PgStore，admin 改写直接刷数据库。
func New(safety feedingsafety.Store, flags featureflags.Store) *AdminService {
	a := &AdminService{
		policies:     map[string]FeedingPolicy{},
		safety:       safety,
		flags:        flags,
		wordlistSink: NewInMemoryWordlistSink(),
	}
	a.policies["room_demo"] = FeedingPolicy{
		RoomID: "room_demo", FeedingOpen: true,
		FeedCooldownSeconds: 30, DailyLimitGrams: 12,
		UserRoomCooldownSec: 60,
	}
	return a
}

// SetWordlistSink 替换 sink（生产 PG 实现）。
func (s *AdminService) SetWordlistSink(sink WordlistSink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wordlistSink = sink
}

// ImportWordlist 批量 upsert 词条，返回最新版本号 + 生效条数。
//
// 调用方应在事务后发送 `chat.wordlist.updated` 事件（key=region/language）；
// chat-svc + gateway 订阅后刷本地缓存。
func (s *AdminService) ImportWordlist(ctx context.Context, entries []WordlistEntry, updatedBy string) (int, error) {
	if s.wordlistSink == nil {
		return 0, yerr.New(yerr.SystemInternal, "wordlist sink not configured")
	}
	if len(entries) == 0 {
		return 0, yerr.New(yerr.SystemInternal, "empty entries")
	}
	return s.wordlistSink.UpsertBatch(ctx, entries, updatedBy)
}

// ListWordlist 列出条目。
func (s *AdminService) ListWordlist(ctx context.Context, region, language string) ([]WordlistEntry, error) {
	if s.wordlistSink == nil {
		return nil, yerr.New(yerr.SystemInternal, "wordlist sink not configured")
	}
	return s.wordlistSink.List(ctx, region, language)
}

// WordlistVersion 当前版本（供 chat-svc 轮询 fallback）。
func (s *AdminService) WordlistVersion(ctx context.Context) (int, error) {
	if s.wordlistSink == nil {
		return 0, nil
	}
	return s.wordlistSink.Version(ctx)
}

// ---------------- Feature Flags ----------------

// ListFlags 返回全部灰度开关。
func (s *AdminService) ListFlags(ctx context.Context) ([]featureflags.Flag, error) {
	if s.flags == nil {
		return nil, yerr.New(yerr.SystemInternal, "feature flag store not configured")
	}
	return s.flags.List(ctx)
}

// GetFlag 返回单个开关。
func (s *AdminService) GetFlag(ctx context.Context, name string) (*featureflags.Flag, error) {
	if s.flags == nil {
		return nil, yerr.New(yerr.SystemInternal, "feature flag store not configured")
	}
	return s.flags.Get(ctx, name)
}

// SetFlag 写一个开关。
func (s *AdminService) SetFlag(ctx context.Context, f featureflags.Flag) error {
	if s.flags == nil {
		return yerr.New(yerr.SystemInternal, "feature flag store not configured")
	}
	if f.Name == "" {
		return yerr.New(yerr.SystemInternal, "missing flag name")
	}
	if f.Scope == "" {
		f.Scope = "global"
	}
	return s.flags.Set(ctx, f)
}

func (s *AdminService) GetPolicy(_ context.Context, roomID string) (FeedingPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.policies[roomID]
	if !ok {
		return FeedingPolicy{}, yerr.New(yerr.RoomNotFound, "policy not found")
	}
	return p, nil
}

func (s *AdminService) UpdatePolicy(ctx context.Context, p FeedingPolicy) error {
	if p.RoomID == "" {
		return yerr.New(yerr.SystemInternal, "missing room_id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies[p.RoomID] = p
	if s.safety != nil {
		return s.safety.Put(ctx, p.RoomID, p.toLimits())
	}
	return nil
}

func (p FeedingPolicy) toLimits() feedingsafety.Limits {
	return feedingsafety.Limits{
		RoomCooldown:     time.Duration(p.FeedCooldownSeconds) * time.Second,
		UserRoomCooldown: time.Duration(p.UserRoomCooldownSec) * time.Second,
		CatDailyLimit:    p.DailyLimitGrams,
	}
}

// SafetyGlobal 把全局阈值落到 safety 内存 store；GET /v1/admin/feeding-safety
func (s *AdminService) GetGlobalSafety(ctx context.Context) (feedingsafety.Limits, error) {
	if s.safety == nil {
		return feedingsafety.DefaultGlobal, nil
	}
	l, ok, err := s.safety.Get(ctx, "")
	if err != nil {
		return feedingsafety.Limits{}, err
	}
	if !ok {
		return feedingsafety.DefaultGlobal, nil
	}
	return l, nil
}

func (s *AdminService) PutGlobalSafety(ctx context.Context, l feedingsafety.Limits) error {
	if s.safety == nil {
		return yerr.New(yerr.SystemInternal, "safety store not configured")
	}
	return s.safety.Put(ctx, "", l)
}

// ---------------- WebRTC 灰度模拟 ----------------

// SimulateWebrtcGray 给运营 / SRE 调试用：
//
//   - 读取 flag `room.webrtc.enabled` 的 enabled + gray_percent；
//   - 模拟 N 个虚拟 room_id 计算命中率，返回 {samples, hit_webrtc, hit_pct, configured_gray_pct}。
//
// 路由：GET /v1/admin/webrtc/gray-sim?room_count=N（admin 自带，避免再绑 room-svc）。
func (s *AdminService) SimulateWebrtcGray(ctx context.Context, samples int) (map[string]any, error) {
	if samples <= 0 {
		samples = 1000
	}
	flag := "room.webrtc.enabled"
	f, err := s.flags.Get(ctx, flag)
	if err != nil {
		return nil, err
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
	hits := 0
	if f.Enabled {
		for i := 0; i < samples; i++ {
			rid := fmt.Sprintf("room_sim_%d", i)
			if pct >= 100 || (pct > 0 && featureflags.Hash100(rid) < pct) {
				hits++
			}
		}
	}
	pctOut := 0.0
	if samples > 0 {
		pctOut = float64(hits) / float64(samples) * 100
	}
	return map[string]any{
		"flag":                flag,
		"enabled":             f.Enabled,
		"samples":             samples,
		"hit_webrtc":          hits,
		"hit_pct":             pctOut,
		"configured_gray_pct": pct,
	}, nil
}

