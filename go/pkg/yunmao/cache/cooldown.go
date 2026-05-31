package cache

import (
	"context"
	"time"
)

// Outcome 与 `pkg/yunmao/cooldown.Outcome` 同义。复制定义避免循环引用。
type Outcome string

const (
	OK           Outcome = "ok"
	BlockedRoom  Outcome = "room_cooldown"
	BlockedUser  Outcome = "user_cooldown"
	BlockedDaily Outcome = "daily_limit"
)

// Cooldown 是基于 Store 的冷却 + 每日上限组合。
type Cooldown struct {
	store          Store
	roomCooldown   time.Duration
	userCooldown   time.Duration
	dailyLimitGram uint32
	clock          func() time.Time
}

// NewCooldown 构造冷却器。
func NewCooldown(store Store, room, user time.Duration, dailyGram uint32) *Cooldown {
	return &Cooldown{
		store:          store,
		roomCooldown:   room,
		userCooldown:   user,
		dailyLimitGram: dailyGram,
		clock:          time.Now,
	}
}

// Check 不修改状态，仅检查；用于 dry-run / 预校验。
func (c *Cooldown) Check(ctx context.Context, roomID, userID, catID string, amountGram uint32) (Outcome, time.Duration, error) {
	now := c.clock().UTC()
	if v, ok, err := c.store.Get(ctx, K("cool", "room", roomID)); err != nil {
		return OK, 0, err
	} else if ok && v != "" {
		ts, _ := time.Parse(time.RFC3339Nano, v)
		if now.Before(ts) {
			return BlockedRoom, ts.Sub(now), nil
		}
	}
	if v, ok, err := c.store.Get(ctx, K("cool", "ur", userID, roomID)); err != nil {
		return OK, 0, err
	} else if ok && v != "" {
		ts, _ := time.Parse(time.RFC3339Nano, v)
		if now.Before(ts) {
			return BlockedUser, ts.Sub(now), nil
		}
	}
	if c.dailyLimitGram > 0 {
		if v, ok, err := c.store.Get(ctx, K("cool", "cat", catID, now.Format("20060102"))); err != nil {
			return OK, 0, err
		} else if ok && v != "" {
			cur, _ := parseInt(v)
			if uint32(cur)+amountGram > c.dailyLimitGram {
				return BlockedDaily, 0, nil
			}
		}
	}
	return OK, 0, nil
}

// Consume 记录冷却 / 累计；通过后才会调用。
func (c *Cooldown) Consume(ctx context.Context, roomID, userID, catID string, amountGram uint32) (Outcome, error) {
	now := c.clock().UTC()
	if v, ok, err := c.store.Get(ctx, K("cool", "room", roomID)); err != nil {
		return OK, err
	} else if ok && v != "" {
		ts, _ := time.Parse(time.RFC3339Nano, v)
		if now.Before(ts) {
			return BlockedRoom, nil
		}
	}
	if v, ok, err := c.store.Get(ctx, K("cool", "ur", userID, roomID)); err != nil {
		return OK, err
	} else if ok && v != "" {
		ts, _ := time.Parse(time.RFC3339Nano, v)
		if now.Before(ts) {
			return BlockedUser, nil
		}
	}
	if c.dailyLimitGram > 0 {
		v, _, err := c.store.Get(ctx, K("cool", "cat", catID, now.Format("20060102")))
		if err != nil {
			return OK, err
		}
		cur, _ := parseInt(v)
		if uint32(cur)+amountGram > c.dailyLimitGram {
			return BlockedDaily, nil
		}
	}

	if err := c.store.Set(ctx, K("cool", "room", roomID), now.Add(c.roomCooldown).Format(time.RFC3339Nano), c.roomCooldown+time.Second); err != nil {
		return OK, err
	}
	if err := c.store.Set(ctx, K("cool", "ur", userID, roomID), now.Add(c.userCooldown).Format(time.RFC3339Nano), c.userCooldown+time.Second); err != nil {
		return OK, err
	}
	if c.dailyLimitGram > 0 {
		ttl := time.Until(time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC))
		if _, err := c.store.IncrBy(ctx, K("cool", "cat", catID, now.Format("20060102")), int64(amountGram), ttl); err != nil {
			return OK, err
		}
	}
	return OK, nil
}

func parseInt(s string) (int64, error) {
	var n int64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return n, nil
		}
		n = n*10 + int64(ch-'0')
	}
	return n, nil
}
