package eventbus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/cloudevents"
)

func TestMemoryBusPublishSubscribe(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var got atomic.Int32
	wg := sync.WaitGroup{}
	wg.Add(1)
	err := bus.Subscribe(ctx, "grp1", []Topic{TopicFeedCommandRequested}, func(_ context.Context, env Envelope) error {
		if env.Topic == TopicFeedCommandRequested {
			got.Add(1)
			wg.Done()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	evt := cloudevents.New[any]("feed.command.requested", "test", "feed_x", map[string]any{"k": "v"})
	env, err := NewEnvelope(TopicFeedCommandRequested, "dev_x", evt)
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(ctx, env); err != nil {
		t.Fatal(err)
	}

	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber didn't receive event")
	}
	if got.Load() != 1 {
		t.Fatalf("expected 1, got %d", got.Load())
	}
}

func TestOpenFactory(t *testing.T) {
	b, err := Open(Config{Backend: BackendMemory})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b == nil {
		t.Fatal("nil bus")
	}

	if _, err := Open(Config{Backend: "redis"}); err == nil {
		t.Fatal("expected ErrUnknownBackend")
	}
	if _, err := Open(Config{Backend: BackendKafka}); err == nil {
		t.Fatal("expected brokers required err")
	}
}

func TestTopicDLQ(t *testing.T) {
	if TopicFeedCommandRequested.DLQ() != "feed.command.requested.dlq" {
		t.Fatalf("dlq suffix wrong: %s", TopicFeedCommandRequested.DLQ())
	}
}

func TestNewEnvelopeIncludesHeaders(t *testing.T) {
	evt := cloudevents.New[any]("feed.command.requested", "test", "feed_x", map[string]any{"a": 1})
	env, err := NewEnvelope(TopicFeedCommandRequested, "rk", evt)
	if err != nil {
		t.Fatal(err)
	}
	if env.Headers["content-type"] != "application/cloudevents+json" {
		t.Fatalf("missing CE content-type: %#v", env.Headers)
	}
	if env.Key != "rk" {
		t.Fatalf("partition key not set")
	}
}

func TestDispatchRetriesUntilSuccess(t *testing.T) {
	calls := atomic.Int32{}
	handler := func(_ context.Context, _ Envelope) error {
		c := calls.Add(1)
		if c < 3 {
			return errBoom
		}
		return nil
	}
	rp := retryPolicy{MaxAttempts: 5, BaseBackoff: time.Millisecond}
	if err := dispatch(context.Background(), handler, Envelope{}, rp); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls.Load())
	}
}

func TestDispatchRetriesExhausted(t *testing.T) {
	handler := func(_ context.Context, _ Envelope) error { return errBoom }
	rp := retryPolicy{MaxAttempts: 2, BaseBackoff: time.Millisecond}
	if err := dispatch(context.Background(), handler, Envelope{}, rp); err == nil {
		t.Fatal("expected error after retries exhausted")
	}
}

var errBoom = boomErr{}

type boomErr struct{}

func (boomErr) Error() string { return "boom" }
