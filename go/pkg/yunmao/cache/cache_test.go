package cache

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStoreSetGet(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	if err := s.Set(ctx, "k", "v", 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	v, ok, err := s.Get(ctx, "k")
	if err != nil || !ok || v != "v" {
		t.Fatalf("get %q %v %v", v, ok, err)
	}

	ok, err = s.SetNX(ctx, "k", "x", time.Second)
	if err != nil || ok {
		t.Fatalf("setnx should fail when key exists: %v %v", ok, err)
	}

	ok, err = s.SetNX(ctx, "new", "x", time.Second)
	if err != nil || !ok {
		t.Fatalf("setnx should succeed when key is new: %v %v", ok, err)
	}
}

func TestCooldownChecksAndConsumes(t *testing.T) {
	st := NewMemoryStore()
	cd := NewCooldown(st, 30*time.Second, 60*time.Second, 12)
	ctx := context.Background()

	out, _, err := cd.Check(ctx, "r1", "u1", "cat1", 5)
	if err != nil || out != OK {
		t.Fatalf("first check: %v %v", out, err)
	}
	if o, err := cd.Consume(ctx, "r1", "u1", "cat1", 5); err != nil || o != OK {
		t.Fatalf("first consume: %v %v", o, err)
	}

	out2, _, err := cd.Check(ctx, "r1", "u2", "cat1", 5)
	if err != nil || out2 != BlockedRoom {
		t.Fatalf("expected room block, got %v", out2)
	}

	// 不同 room 不再受 (user,room) 冷却影响（这是按 user+room 复合 key 设计），
	// 也不再受 r1 的 room cooldown 影响；预期 OK。
	out3, _, _ := cd.Check(ctx, "r2", "u1", "cat2", 5)
	if out3 != OK {
		t.Fatalf("expected ok in fresh (room=r2, user=u1, cat=cat2), got %v", out3)
	}
}

func TestCooldownDailyLimit(t *testing.T) {
	st := NewMemoryStore()
	cd := NewCooldown(st, 0, 0, 10)
	ctx := context.Background()

	if o, _ := cd.Consume(ctx, "r", "u", "c", 6); o != OK {
		t.Fatalf("first 6g: %v", o)
	}
	if out, _, _ := cd.Check(ctx, "r", "u", "c", 5); out != BlockedDaily {
		t.Fatalf("expected daily block on 6+5>10, got %v", out)
	}
	if out, _, _ := cd.Check(ctx, "r", "u", "c", 3); out != OK {
		t.Fatalf("expected ok on 6+3<=10, got %v", out)
	}
}

func TestIdempotentInsertReturnsFalseOnDup(t *testing.T) {
	st := NewMemoryStore()
	idem := NewIdempotent(st, time.Minute)
	ctx := context.Background()
	first, err := idem.Insert(ctx, "feed", "abc")
	if err != nil || !first {
		t.Fatalf("first insert: %v %v", first, err)
	}
	second, err := idem.Insert(ctx, "feed", "abc")
	if err != nil || second {
		t.Fatalf("second insert should be dup: %v %v", second, err)
	}
}

func TestRateLimiterAllowsThenBlocks(t *testing.T) {
	st := NewMemoryStore()
	rl := NewRateLimiter(st, 2, time.Second)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		ok, _, err := rl.Allow(ctx, "k")
		if err != nil || !ok {
			t.Fatalf("call %d: %v %v", i, ok, err)
		}
	}
	ok, cnt, err := rl.Allow(ctx, "k")
	if err != nil || ok {
		t.Fatalf("3rd: ok=%v cnt=%d err=%v", ok, cnt, err)
	}
}
