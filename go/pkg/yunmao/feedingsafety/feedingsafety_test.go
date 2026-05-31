package feedingsafety

import (
	"context"
	"testing"
	"time"
)

func TestResolveUsesDefaults(t *testing.T) {
	m := NewManager(NewMemoryStore())
	l, err := m.Resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if l != DefaultGlobal {
		t.Fatalf("expected defaults: %+v", l)
	}
}

func TestResolveAppliesGlobalThenRoomOverride(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Put(context.Background(), "", Limits{RoomCooldown: 10 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "room_demo", Limits{CatDailyLimit: 99}); err != nil {
		t.Fatal(err)
	}

	m := NewManager(store)
	got, err := m.Resolve(context.Background(), "room_demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.RoomCooldown != 10*time.Second {
		t.Fatalf("expected room cooldown=10s, got %v", got.RoomCooldown)
	}
	if got.UserRoomCooldown != DefaultGlobal.UserRoomCooldown {
		t.Fatalf("expected user cooldown to fall back to default, got %v", got.UserRoomCooldown)
	}
	if got.CatDailyLimit != 99 {
		t.Fatalf("expected daily=99, got %d", got.CatDailyLimit)
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("YUNMAO_FEED_ROOM_COOLDOWN_SEC", "7")
	t.Setenv("YUNMAO_FEED_CAT_DAILY", "20")
	m := NewManager(nil)
	got, err := m.Resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.RoomCooldown != 7*time.Second {
		t.Fatalf("expected env override 7s, got %v", got.RoomCooldown)
	}
	if got.CatDailyLimit != 20 {
		t.Fatalf("expected env cat=20, got %d", got.CatDailyLimit)
	}
}
