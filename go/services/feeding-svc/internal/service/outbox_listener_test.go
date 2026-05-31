package service

import (
	"context"
	"sync"
	"testing"

	"yunmao.live/pkg/yunmao/eventbus"
	"yunmao.live/pkg/yunmao/feedstate"

	"yunmao.live/services/feeding-svc/internal/store"
)

// recordingStore captures every Create / SaveTransition with outbox events for assertion.
type recordingStore struct {
	mu      sync.Mutex
	creates []store.Transition
	saves   []store.Transition
}

func (r *recordingStore) Create(_ context.Context, t store.Transition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.creates = append(r.creates, t)
	return nil
}

func (r *recordingStore) SaveTransition(_ context.Context, t store.Transition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saves = append(r.saves, t)
	return nil
}

func (r *recordingStore) LoadByID(_ context.Context, _ string) (*store.FeedRequest, error) {
	return nil, store.ErrNotFound
}

func (r *recordingStore) ListByRoom(_ context.Context, _ string, _ int) ([]store.FeedRequest, error) {
	return nil, nil
}

func (r *recordingStore) ListByDevice(_ context.Context, _ string, _ int) ([]store.FeedRequest, error) {
	return nil, nil
}

func TestOutboxListener_CreateThenTransitions(t *testing.T) {
	rec := &recordingStore{}
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	svc.SetOutboxMode(true)
	svc.AddEventListener((&OutboxListener{Store: rec, Source: "feeding-svc@test"}).Handle)

	req, err := svc.Create(context.Background(), CreateInput{
		UserID: "u1", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "kox",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if req.Status != feedstate.Dispatched {
		t.Fatalf("expected dispatched in outbox mode, got %s", req.Status)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.creates) != 1 {
		t.Fatalf("expected 1 create, got %d", len(rec.creates))
	}
	if len(rec.saves) != 2 {
		t.Fatalf("expected 2 saves (queued, dispatched), got %d", len(rec.saves))
	}

	// queued save should include feed.command.requested outbox event
	saveQueued := rec.saves[0]
	if saveQueued.To != feedstate.Queued {
		t.Fatalf("first save not queued: %s", saveQueued.To)
	}
	if len(saveQueued.OutboxEvents) != 1 || saveQueued.OutboxEvents[0].Topic != eventbus.TopicFeedCommandRequested {
		t.Fatalf("expected feed.command.requested outbox row; got %+v", saveQueued.OutboxEvents)
	}
	if saveQueued.OutboxEvents[0].PartitionKey != "dev_demo" {
		t.Fatalf("expected partition key = device id, got %q", saveQueued.OutboxEvents[0].PartitionKey)
	}

	saveDispatched := rec.saves[1]
	if saveDispatched.To != feedstate.Dispatched {
		t.Fatalf("second save not dispatched: %s", saveDispatched.To)
	}
	if len(saveDispatched.OutboxEvents) != 1 || saveDispatched.OutboxEvents[0].Topic != eventbus.TopicFeedCommandDispatched {
		t.Fatalf("expected feed.command.dispatched outbox row")
	}
	if saveDispatched.OutboxEvents[0].PartitionKey != "room_demo" {
		t.Fatalf("expected partition key = room id, got %q", saveDispatched.OutboxEvents[0].PartitionKey)
	}

	// 在 outbox 模式下 publisher 不应被直接调用
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.cmds) != 0 {
		t.Fatalf("expected no direct PublishFeedCommandRequested in outbox mode, got %d", len(pub.cmds))
	}
	if len(pub.gateways) != 0 {
		t.Fatalf("expected no direct PublishGatewayEvent in outbox mode, got %d", len(pub.gateways))
	}
}

func TestOutboxListener_CancelEmitsCancelledEvent(t *testing.T) {
	rec := &recordingStore{}
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	svc.SetOutboxMode(true)
	svc.AddEventListener((&OutboxListener{Store: rec, Source: "feeding-svc@test"}).Handle)

	req, err := svc.Create(context.Background(), CreateInput{
		UserID: "u_cancel_ob", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "kcob",
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Status != feedstate.Dispatched {
		t.Fatalf("expected dispatched, got %s", req.Status)
	}
	if _, err := svc.Cancel(context.Background(), req.ID, "user_abort"); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	// 最后一条 save 应该是 dispatched -> rejected 且带 cancelled 事件
	last := rec.saves[len(rec.saves)-1]
	if last.From != feedstate.Dispatched || last.To != feedstate.Rejected {
		t.Fatalf("expected dispatched->rejected, got %s->%s", last.From, last.To)
	}
	if len(last.OutboxEvents) != 1 || last.OutboxEvents[0].Topic != eventbus.TopicFeedCommandCancelled {
		t.Fatalf("expected cancelled outbox row, got %+v", last.OutboxEvents)
	}
}
