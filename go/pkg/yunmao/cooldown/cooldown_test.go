package cooldown

import (
	"testing"
	"time"
)

func TestRoomCooldownBlocksSecondAttempt(t *testing.T) {
	l := New(2*time.Second, time.Second, 100)
	now := time.Unix(1700000000, 0)
	l.now = func() time.Time { return now }

	if got := l.Consume("room", "u1", "cat", 5); got != OK {
		t.Fatalf("first consume: %s", got)
	}
	// 不同用户也应被房间冷却挡住
	if out, _ := l.Check("room", "u2", "cat", 5); out != BlockedRoom {
		t.Fatalf("expected room blocked, got %s", out)
	}
	now = now.Add(3 * time.Second)
	if out, _ := l.Check("room", "u2", "cat", 5); out != OK {
		t.Fatalf("expected ok after cooldown, got %s", out)
	}
}

func TestUserCooldownBlocksUserOnly(t *testing.T) {
	l := New(0, 5*time.Second, 100)
	now := time.Unix(1700000000, 0)
	l.now = func() time.Time { return now }

	l.Consume("room", "u1", "cat", 5)
	if out, _ := l.Check("room", "u1", "cat", 5); out != BlockedUser {
		t.Fatalf("expected user blocked, got %s", out)
	}
	if out, _ := l.Check("room", "u2", "cat", 5); out != OK {
		t.Fatalf("user2 should not be blocked")
	}
}

func TestDailyLimit(t *testing.T) {
	l := New(0, 0, 10)
	now := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }

	if got := l.Consume("r", "u1", "cat", 6); got != OK {
		t.Fatal(got)
	}
	if got := l.Consume("r", "u2", "cat", 5); got != BlockedDaily {
		t.Fatalf("expected daily blocked, got %s", got)
	}
	if got := l.Consume("r", "u2", "cat", 4); got != OK {
		t.Fatalf("4g should fit: %s", got)
	}
	// 跨天后应当重置
	now = now.Add(25 * time.Hour)
	if got := l.Consume("r", "u3", "cat", 8); got != OK {
		t.Fatalf("next day should reset, got %s", got)
	}
}
