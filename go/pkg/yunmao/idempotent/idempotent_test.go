package idempotent

import (
	"testing"
	"time"
)

func TestInsertDeduplicates(t *testing.T) {
	c := NewCache(0, time.Second)
	if !c.Insert("a") {
		t.Fatal("first insert should be true")
	}
	if c.Insert("a") {
		t.Fatal("duplicate should be false")
	}
}

func TestCapacityEvicts(t *testing.T) {
	c := NewCache(2, 0)
	c.Insert("a")
	c.Insert("b")
	c.Insert("c")
	if c.Has("a") {
		t.Fatal("a should have been evicted")
	}
	if !c.Has("b") || !c.Has("c") {
		t.Fatal("b/c should still be present")
	}
}

func TestTTLExpires(t *testing.T) {
	now := time.Unix(0, 0)
	c := NewCache(0, 100*time.Millisecond)
	c.now = func() time.Time { return now }
	c.Insert("x")
	if !c.Has("x") {
		t.Fatal("expected present")
	}
	now = now.Add(200 * time.Millisecond)
	if c.Has("x") {
		t.Fatal("should be expired")
	}
	// re-insert after expiry should be true
	if !c.Insert("x") {
		t.Fatal("expected re-insert true after expiry")
	}
}
