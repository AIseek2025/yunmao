package mqttx

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestMemoryBrokerPubSub(t *testing.T) {
	broker := NewMemoryBroker()
	sub := broker.NewClient("device-svc")
	pub := broker.NewClient("device-edge")

	if err := sub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := pub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = sub.Disconnect(0)
		_ = pub.Disconnect(0)
	}()

	var hits int32
	if err := sub.Subscribe(context.Background(), "device/+/event/+", QoS1, func(_ context.Context, m Message) error {
		atomic.AddInt32(&hits, 1)
		if m.Topic != "device/dev_demo/event/feed_ack" {
			t.Errorf("unexpected topic: %s", m.Topic)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := pub.Publish(context.Background(),
		DeviceEventTopic("dev_demo", "feed_ack"), QoS1, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}

	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", hits)
	}
}

func TestMemoryBrokerWildcardHash(t *testing.T) {
	broker := NewMemoryBroker()
	sub := broker.NewClient("s")
	pub := broker.NewClient("p")
	_ = sub.Connect(context.Background())
	_ = pub.Connect(context.Background())

	var hits int32
	_ = sub.Subscribe(context.Background(), "device/#", QoS1, func(_ context.Context, _ Message) error {
		atomic.AddInt32(&hits, 1)
		return nil
	})
	_ = pub.Publish(context.Background(), "device/abc/event/online", QoS1, nil)
	_ = pub.Publish(context.Background(), "device/xyz/cmd/feed", QoS1, nil)

	if atomic.LoadInt32(&hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", hits)
	}
}

func TestMatchFilter(t *testing.T) {
	tests := []struct {
		filter string
		topic  string
		want   bool
	}{
		{"a/b/c", "a/b/c", true},
		{"a/b/c", "a/b/d", false},
		{"a/+/c", "a/x/c", true},
		{"a/+/c", "a/b/d", false},
		{"a/#", "a/b/c/d", true},
		{"a/#", "x/b/c", false},
		{"device/+/event/+", "device/dev/event/online", true},
		{"device/+/event/+", "device/dev/cmd/feed", false},
	}
	for _, tt := range tests {
		got := matchFilter(splitTopic(tt.filter), splitTopic(tt.topic))
		if got != tt.want {
			t.Errorf("matchFilter(%s, %s) = %t want %t", tt.filter, tt.topic, got, tt.want)
		}
	}
}
