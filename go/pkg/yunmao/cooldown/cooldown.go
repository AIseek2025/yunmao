// Package cooldown 提供进程内冷却（与 04 章动物福利硬约束一致）：
//
// - 房间级冷却（key = room_id）
// - 用户级冷却（key = user_id+room_id）
// - 单猫每日总量上限（按"calendar day in UTC" 累计）
//
// 真实生产应当用 Redis 原子操作，本实现只服务 MVP / 单进程；接口与 Redis 行为对齐，
// 后续可平滑替换。
package cooldown

import (
	"sync"
	"time"
)

// Limiter 冷却 + 每日上限组合。
type Limiter struct {
	mu sync.Mutex

	roomCooldown   time.Duration
	userCooldown   time.Duration
	dailyLimitGram uint32

	roomNext   map[string]time.Time
	userNext   map[string]time.Time
	dailyTotal map[string]uint32 // key = cat_id + ":" + date(YYYYMMDD)

	now func() time.Time
}

// New 构造 Limiter。
//
// - roomCooldown: 房间级冷却（同一房间下次允许投喂的最早时间）
// - userCooldown: 用户级冷却
// - dailyLimitGram: 单猫每日上限（克）；0 = 无限制
func New(roomCooldown, userCooldown time.Duration, dailyLimitGram uint32) *Limiter {
	return &Limiter{
		roomCooldown:   roomCooldown,
		userCooldown:   userCooldown,
		dailyLimitGram: dailyLimitGram,
		roomNext:       make(map[string]time.Time),
		userNext:       make(map[string]time.Time),
		dailyTotal:     make(map[string]uint32),
		now:            time.Now,
	}
}

// Outcome 表示尝试结果。
type Outcome string

const (
	OK              Outcome = "ok"
	BlockedRoom     Outcome = "room_cooldown"
	BlockedUser     Outcome = "user_cooldown"
	BlockedDaily    Outcome = "daily_limit"
)

// Check 不消耗冷却，仅检查；用于上层决定是否允许进入；不会修改状态。
// 返回 (outcome, retryAfter)。
func (l *Limiter) Check(roomID, userID, catID string, amountGram uint32) (Outcome, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now().UTC()

	if next, ok := l.roomNext[roomID]; ok && now.Before(next) {
		return BlockedRoom, next.Sub(now)
	}
	uk := userID + "|" + roomID
	if next, ok := l.userNext[uk]; ok && now.Before(next) {
		return BlockedUser, next.Sub(now)
	}
	if l.dailyLimitGram > 0 {
		dk := catID + ":" + now.Format("20060102")
		if l.dailyTotal[dk]+amountGram > l.dailyLimitGram {
			return BlockedDaily, 0
		}
	}
	return OK, 0
}

// Consume 真正记入冷却 / 累计；只在通过校验后调用。
func (l *Limiter) Consume(roomID, userID, catID string, amountGram uint32) Outcome {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now().UTC()

	if next, ok := l.roomNext[roomID]; ok && now.Before(next) {
		return BlockedRoom
	}
	uk := userID + "|" + roomID
	if next, ok := l.userNext[uk]; ok && now.Before(next) {
		return BlockedUser
	}
	dk := catID + ":" + now.Format("20060102")
	if l.dailyLimitGram > 0 && l.dailyTotal[dk]+amountGram > l.dailyLimitGram {
		return BlockedDaily
	}

	l.roomNext[roomID] = now.Add(l.roomCooldown)
	l.userNext[uk] = now.Add(l.userCooldown)
	l.dailyTotal[dk] += amountGram
	return OK
}
