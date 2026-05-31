package featureflags

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStoreCrud(t *testing.T) {
	s := NewMemoryStore(Flag{Name: "x", Enabled: true, Value: map[string]any{"k": 1.0}})
	all, err := s.List(context.Background())
	if err != nil || len(all) != 1 {
		t.Fatalf("expected 1, got %d", len(all))
	}
	f, err := s.Get(context.Background(), "x")
	if err != nil || !f.Enabled {
		t.Fatalf("get: %v", err)
	}
	if err := s.Set(context.Background(), Flag{Name: "y", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(context.Background(), "y"); err != nil {
		t.Fatalf("set lost: %v", err)
	}
}

func TestManagerInitialAndAccessors(t *testing.T) {
	s := NewMemoryStore(
		Flag{Name: "feeding.allow_new_rooms", Enabled: true, Value: map[string]any{}},
		Flag{Name: "feeding.timeout_seconds", Enabled: true, Value: map[string]any{"seconds": 12.0}},
		Flag{Name: "feeding.device_maintenance", Enabled: false, Value: map[string]any{"device_ids": []any{"dev_a", "dev_b"}}},
	)
	m := NewManager(Config{Store: s, RefreshEvery: 50 * time.Millisecond})
	if !m.Bool("feeding.allow_new_rooms", false) {
		t.Fatal("expected enabled")
	}
	if m.Int("feeding.timeout_seconds", "seconds", 30) != 12 {
		t.Fatalf("int access wrong")
	}
	ids := m.StringSlice("feeding.device_maintenance", "device_ids")
	if len(ids) != 2 || ids[0] != "dev_a" {
		t.Fatalf("string slice wrong: %v", ids)
	}
	if m.Bool("does.not.exist", true) != true {
		t.Fatalf("default not used")
	}
}

func TestIsRoomInGrayPercent_BoundsAndDistribution(t *testing.T) {
	s := NewMemoryStore(
		Flag{Name: "room.webrtc.enabled", Enabled: true, Value: map[string]any{"gray_percent": 20.0}},
	)
	m := NewManager(Config{Store: s})

	// 边界：=100 全量；<=0 全部 false。
	_ = m.Set(context.Background(), Flag{Name: "room.webrtc.enabled", Enabled: true, Value: map[string]any{"gray_percent": 100.0}})
	if !m.IsRoomInGrayPercent("room.webrtc.enabled", "room_xyz") {
		t.Fatalf("100%% should always be in")
	}
	_ = m.Set(context.Background(), Flag{Name: "room.webrtc.enabled", Enabled: true, Value: map[string]any{"gray_percent": 0.0}})
	if m.IsRoomInGrayPercent("room.webrtc.enabled", "room_xyz") {
		t.Fatalf("0%% should be out")
	}
	// disabled flag → false
	_ = m.Set(context.Background(), Flag{Name: "room.webrtc.enabled", Enabled: false, Value: map[string]any{"gray_percent": 100.0}})
	if m.IsRoomInGrayPercent("room.webrtc.enabled", "room_xyz") {
		t.Fatalf("disabled flag should not enroll")
	}

	// 20% gray：1000 个 room_xxx ID 大致命中 20% ±5%
	_ = m.Set(context.Background(), Flag{Name: "room.webrtc.enabled", Enabled: true, Value: map[string]any{"gray_percent": 20.0}})
	hits := 0
	for i := 0; i < 1000; i++ {
		if m.IsRoomInGrayPercent("room.webrtc.enabled", roomID(i)) {
			hits++
		}
	}
	if hits < 130 || hits > 270 {
		t.Fatalf("gray hit distribution out of bounds: hits=%d", hits)
	}
}

func roomID(i int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, 0, 16)
	for j := 0; j < 8; j++ {
		out = append(out, alphabet[(i*31+j*7)%len(alphabet)])
		i /= 3
	}
	return "room_" + string(out)
}

func TestManagerRefreshOnSet(t *testing.T) {
	s := NewMemoryStore()
	m := NewManager(Config{Store: s})
	if m.Bool("feeding.allow_new_rooms", true) {
		// default true
	}
	if err := m.Set(context.Background(), Flag{Name: "feeding.allow_new_rooms", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if m.Bool("feeding.allow_new_rooms", true) {
		t.Fatalf("expected disabled after set")
	}
}
