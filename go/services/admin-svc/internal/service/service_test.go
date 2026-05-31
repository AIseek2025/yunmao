package service

import (
	"context"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/featureflags"
	"yunmao.live/pkg/yunmao/feedingsafety"
)

func newSvc() *AdminService {
	return New(feedingsafety.NewMemoryStore(), featureflags.NewMemoryStore())
}

func TestUpdateThenGet(t *testing.T) {
	s := newSvc()
	if err := s.UpdatePolicy(context.Background(), FeedingPolicy{
		RoomID: "room_x", FeedingOpen: true, FeedCooldownSeconds: 30, DailyLimitGrams: 80,
	}); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetPolicy(context.Background(), "room_x")
	if err != nil {
		t.Fatal(err)
	}
	if p.FeedCooldownSeconds != 30 || p.DailyLimitGrams != 80 {
		t.Fatalf("unexpected policy: %#v", p)
	}
}

func TestUpdateRequiresRoom(t *testing.T) {
	if err := newSvc().UpdatePolicy(context.Background(), FeedingPolicy{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestUpdatePolicyAlsoUpdatesSafety(t *testing.T) {
	store := feedingsafety.NewMemoryStore()
	s := New(store, featureflags.NewMemoryStore())
	if err := s.UpdatePolicy(context.Background(), FeedingPolicy{
		RoomID: "room_y", FeedingOpen: true,
		FeedCooldownSeconds: 90, UserRoomCooldownSec: 120, DailyLimitGrams: 5,
	}); err != nil {
		t.Fatal(err)
	}
	l, ok, err := store.Get(context.Background(), "room_y")
	if err != nil || !ok {
		t.Fatalf("expected stored: %v %v", ok, err)
	}
	if l.RoomCooldown != 90*time.Second {
		t.Fatalf("room cd: %v", l.RoomCooldown)
	}
	if l.CatDailyLimit != 5 {
		t.Fatalf("daily: %d", l.CatDailyLimit)
	}
}

func TestGlobalSafetyDefault(t *testing.T) {
	s := newSvc()
	l, err := s.GetGlobalSafety(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if l != feedingsafety.DefaultGlobal {
		t.Fatalf("expected default, got %+v", l)
	}
	if err := s.PutGlobalSafety(context.Background(), feedingsafety.Limits{
		RoomCooldown: 50 * time.Second, UserRoomCooldown: 100 * time.Second, CatDailyLimit: 7,
	}); err != nil {
		t.Fatal(err)
	}
	l2, _ := s.GetGlobalSafety(context.Background())
	if l2.RoomCooldown != 50*time.Second {
		t.Fatalf("expected put applied, got %v", l2)
	}
}

func TestFeatureFlagsCrud(t *testing.T) {
	s := newSvc()
	ctx := context.Background()
	if err := s.SetFlag(ctx, featureflags.Flag{
		Name:    "feed.global_kill_switch",
		Enabled: true,
		Scope:   "global",
		Value:   map[string]any{"reason": "maintenance"},
	}); err != nil {
		t.Fatal(err)
	}
	f, err := s.GetFlag(ctx, "feed.global_kill_switch")
	if err != nil {
		t.Fatal(err)
	}
	if !f.Enabled {
		t.Fatalf("expected enabled, got %+v", f)
	}
	if f.Value["reason"] != "maintenance" {
		t.Fatalf("expected reason=maintenance, got %+v", f.Value)
	}
	list, err := s.ListFlags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 1 {
		t.Fatalf("expected >=1 flag, got %d", len(list))
	}
}

func TestSetFlagRequiresName(t *testing.T) {
	s := newSvc()
	if err := s.SetFlag(context.Background(), featureflags.Flag{Enabled: true}); err == nil {
		t.Fatal("expected error")
	}
}

func TestImportWordlistAndList(t *testing.T) {
	s := newSvc()
	ctx := context.Background()
	v, err := s.ImportWordlist(ctx, []WordlistEntry{
		{Region: "global", Language: "zh", Word: "敏感词1", Action: "hide"},
		{Region: "global", Language: "zh", Word: "敏感词2", Action: "block"},
	}, "admin@yunmao.live")
	if err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("version got %d", v)
	}
	entries, _ := s.ListWordlist(ctx, "global", "zh")
	if len(entries) != 2 {
		t.Fatalf("got %d entries", len(entries))
	}
	// 重复 upsert：version 自增
	v2, _ := s.ImportWordlist(ctx, []WordlistEntry{
		{Region: "global", Language: "zh", Word: "敏感词1", Action: "block"},
	}, "admin")
	if v2 != 2 {
		t.Fatalf("version after upsert got %d", v2)
	}
	entries, _ = s.ListWordlist(ctx, "global", "zh")
	if len(entries) != 2 {
		t.Fatalf("expected merge, got %d", len(entries))
	}
}
