package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/cache"
	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/featureflags"
	"yunmao.live/pkg/yunmao/feedingsafety"
	"yunmao.live/pkg/yunmao/feedstate"

	"yunmao.live/services/feeding-svc/publisher"
)

type stubPublisher struct {
	mu       sync.Mutex
	cmds     []publisher.FeedCommandRequested
	gateways []gatewayEvent
	failNext bool
}

type gatewayEvent struct {
	eventType string
	roomID    string
}

func (p *stubPublisher) PublishFeedCommandRequested(_ context.Context, evt publisher.FeedCommandRequested) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failNext {
		p.failNext = false
		return yerr.New(yerr.SystemDependencyUnavailable, "boom")
	}
	p.cmds = append(p.cmds, evt)
	return nil
}

func (p *stubPublisher) PublishGatewayEvent(_ context.Context, et, roomID string, _ any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gateways = append(p.gateways, gatewayEvent{eventType: et, roomID: roomID})
	return nil
}

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting condition")
}

func newTestSvc(p publisher.EventPublisher) *FeedingService {
	store := cache.NewMemoryStore()
	mgr := feedingsafety.NewManager(feedingsafety.NewMemoryStore())
	return New(p, store, mgr)
}

func TestCreateHappyPath(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)

	req, err := svc.Create(context.Background(), CreateInput{
		UserID: "u1", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if req.Status != feedstate.Accepted && req.Status != feedstate.Queued {
		t.Fatalf("unexpected initial status: %s", req.Status)
	}

	waitFor(t, func() bool {
		pub.mu.Lock()
		defer pub.mu.Unlock()
		return len(pub.cmds) == 1
	})
	waitFor(t, func() bool {
		got, _ := svc.Get(context.Background(), req.ID)
		return got != nil && got.Status == feedstate.Dispatched
	})
}

func TestCreateRejectedOnRoomMissing(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	_, err := svc.Create(context.Background(), CreateInput{
		UserID: "u1", RoomID: "room_unknown", AmountGrams: 5, IdempotencyKey: "k",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestIdempotencyReturnsExistingRequest(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	a, err := svc.Create(context.Background(), CreateInput{
		UserID: "u1", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "kx",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Create(context.Background(), CreateInput{
		UserID: "u1", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "kx",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Fatalf("expected same id, got %s != %s", a.ID, b.ID)
	}
}

func TestCooldownBlocksSecondAttempt(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	if _, err := svc.Create(context.Background(), CreateInput{
		UserID: "u1", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "k1",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(context.Background(), CreateInput{
		UserID: "u2", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "k2",
	})
	if err == nil {
		t.Fatal("expected cooldown")
	}
}

func TestHandleAckPromotesToSucceeded(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	req, _ := svc.Create(context.Background(), CreateInput{
		UserID: "u1", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "kx",
	})
	waitFor(t, func() bool {
		got, _ := svc.Get(context.Background(), req.ID)
		return got != nil && got.Status == feedstate.Dispatched
	})
	if err := svc.HandleAck(context.Background(), publisher.FeedCommandAcked{
		FeedRequestID: req.ID, DeviceCommandID: req.DeviceCommandID,
		DeviceID: req.DeviceID, RoomID: req.RoomID,
		Status: "succeeded", ActualAmountGrams: 5, ExecutedAt: "2026-05-25T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(context.Background(), req.ID)
	if got.Status != feedstate.Succeeded {
		t.Fatalf("expected succeeded, got %s", got.Status)
	}
	pub.mu.Lock()
	defer pub.mu.Unlock()
	var hasAck, hasDisp bool
	for _, g := range pub.gateways {
		if g.eventType == "feed.command.acked" {
			hasAck = true
		}
		if g.eventType == "feed.command.dispatched" {
			hasDisp = true
		}
	}
	if !hasAck || !hasDisp {
		t.Fatalf("expected dispatched + acked, got %#v", pub.gateways)
	}
}

func TestCancelOnDispatched(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	req, err := svc.Create(context.Background(), CreateInput{
		UserID: "u_cancel", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "kc1",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		got, _ := svc.Get(context.Background(), req.ID)
		return got.Status == feedstate.Dispatched
	})
	cancelled, err := svc.Cancel(context.Background(), req.ID, "user_cancel")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != feedstate.Rejected {
		t.Fatalf("expected rejected after cancel, got %s", cancelled.Status)
	}
	if cancelled.RejectReason != "user_cancel" {
		t.Fatalf("expected user_cancel reason, got %s", cancelled.RejectReason)
	}
	pub.mu.Lock()
	defer pub.mu.Unlock()
	var hasCancel bool
	for _, g := range pub.gateways {
		if g.eventType == "feed.command.cancelled" {
			hasCancel = true
		}
	}
	if !hasCancel {
		t.Fatalf("expected feed.command.cancelled, got %#v", pub.gateways)
	}
}

func TestCancelRejectsWrongState(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	req, _ := svc.Create(context.Background(), CreateInput{
		UserID: "u_cancel2", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "kc2",
	})
	waitFor(t, func() bool {
		got, _ := svc.Get(context.Background(), req.ID)
		return got.Status == feedstate.Dispatched
	})
	_ = svc.HandleAck(context.Background(), publisher.FeedCommandAcked{
		FeedRequestID: req.ID, DeviceCommandID: req.DeviceCommandID,
		DeviceID: req.DeviceID, RoomID: req.RoomID, Status: "succeeded",
	})
	if _, err := svc.Cancel(context.Background(), req.ID, "too late"); err == nil {
		t.Fatal("expected cancel rejected for succeeded request")
	}
}

func TestTimeoutScanFlipsStuckDispatched(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	req, _ := svc.Create(context.Background(), CreateInput{
		UserID: "u_t", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "kt1",
	})
	waitFor(t, func() bool {
		got, _ := svc.Get(context.Background(), req.ID)
		return got.Status == feedstate.Dispatched
	})
	// 故意把 updated_at 拉早，触发 timeout 扫描
	svc.mu.Lock()
	svc.requests[req.ID].UpdatedAt = time.Now().Add(-1 * time.Minute)
	svc.mu.Unlock()
	hits := svc.TimeoutScanRun(context.Background(), 30*time.Second)
	if hits == 0 {
		t.Fatalf("expected at least 1 timeout hit")
	}
	got, _ := svc.Get(context.Background(), req.ID)
	if got.Status != feedstate.Failed {
		t.Fatalf("expected failed after timeout, got %s", got.Status)
	}
	if got.RejectReason != "timeout" {
		t.Fatalf("expected reason=timeout, got %s", got.RejectReason)
	}
}

func TestFeatureFlagBlocksWhenDisabled(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	flagStore := featureflags.NewMemoryStore(
		featureflags.Flag{Name: "feeding.allow_new_rooms", Enabled: false, Value: map[string]any{}},
	)
	flags := featureflags.NewManager(featureflags.Config{Store: flagStore})
	svc.SetFlags(flags)
	if _, err := svc.Create(context.Background(), CreateInput{
		UserID: "u_ff", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "kff",
	}); err == nil {
		t.Fatal("expected flag blocked")
	}
}

func TestFeatureFlagDeviceMaintenance(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	flagStore := featureflags.NewMemoryStore(
		featureflags.Flag{Name: "feeding.allow_new_rooms", Enabled: true, Value: map[string]any{}},
		featureflags.Flag{Name: "feeding.device_maintenance", Enabled: true,
			Value: map[string]any{"device_ids": []any{"dev_demo"}}},
	)
	flags := featureflags.NewManager(featureflags.Config{Store: flagStore})
	svc.SetFlags(flags)
	_, err := svc.Create(context.Background(), CreateInput{
		UserID: "u_mtn", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "kmtn",
	})
	if err == nil {
		t.Fatal("expected device maintenance block")
	}
}

// 第五轮：billing saga 占位行为 + cat daily limit 硬约束。

type stubBilling struct {
	mu           sync.Mutex
	reserves     []string
	confirms     []string
	cancels      []string
	failReserve  bool
}

func (s *stubBilling) Reserve(_ context.Context, in BillingReserveInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failReserve {
		return "", yerr.New(yerr.PayChannelFailed, "stub fail")
	}
	id := "rcpt_" + in.IdempotencyKey
	s.reserves = append(s.reserves, id)
	return id, nil
}

func (s *stubBilling) Confirm(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.confirms = append(s.confirms, id)
	return nil
}

func (s *stubBilling) Cancel(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancels = append(s.cancels, id)
	return nil
}

func TestBillingSagaConfirmOnSucceeded(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	bill := &stubBilling{}
	svc.SetBilling(bill)

	req, err := svc.Create(context.Background(), CreateInput{
		UserID: "u_bill_ok", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "bill_ok",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		got, _ := svc.Get(context.Background(), req.ID)
		return got.Status == feedstate.Dispatched
	})
	_ = svc.HandleAck(context.Background(), publisher.FeedCommandAcked{
		FeedRequestID: req.ID, DeviceCommandID: req.DeviceCommandID,
		DeviceID: req.DeviceID, RoomID: req.RoomID, Status: "succeeded",
	})
	bill.mu.Lock()
	defer bill.mu.Unlock()
	if len(bill.reserves) != 1 || len(bill.confirms) != 1 {
		t.Fatalf("expected 1 reserve + 1 confirm, got %d/%d", len(bill.reserves), len(bill.confirms))
	}
	if len(bill.cancels) != 0 {
		t.Fatalf("expected no cancel, got %d", len(bill.cancels))
	}
}

func TestBillingSagaCancelOnReject(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	bill := &stubBilling{}
	svc.SetBilling(bill)
	req, err := svc.Create(context.Background(), CreateInput{
		UserID: "u_bill_cancel", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "bill_cancel",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		got, _ := svc.Get(context.Background(), req.ID)
		return got.Status == feedstate.Dispatched
	})
	if _, err := svc.Cancel(context.Background(), req.ID, "test"); err != nil {
		t.Fatal(err)
	}
	bill.mu.Lock()
	defer bill.mu.Unlock()
	if len(bill.cancels) != 1 {
		t.Fatalf("expected 1 cancel, got %d", len(bill.cancels))
	}
}

func TestBillingReserveFailureBlocksCreate(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)
	bill := &stubBilling{failReserve: true}
	svc.SetBilling(bill)
	if _, err := svc.Create(context.Background(), CreateInput{
		UserID: "u_bill_fail", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "bill_fail",
	}); err == nil {
		t.Fatal("expected billing reserve failure to block")
	}
}

func TestCatDailyLimitBlocksOverDose(t *testing.T) {
	// CatDailyLimit=12 → 第二次 8g 应被 BlockedDaily 拦住。
	store := cache.NewMemoryStore()
	mem := feedingsafety.NewMemoryStore()
	_ = mem.Put(context.Background(), "", feedingsafety.Limits{
		RoomCooldown: 1 * time.Millisecond, UserRoomCooldown: 1 * time.Millisecond,
		CatDailyLimit: 12,
	})
	mgr := feedingsafety.NewManager(mem)
	pub := &stubPublisher{}
	svc := New(pub, store, mgr)

	if _, err := svc.Create(context.Background(), CreateInput{
		UserID: "u_cat_d1", RoomID: "room_demo", AmountGrams: 8, IdempotencyKey: "kc_d1",
	}); err != nil {
		t.Fatalf("first feed: %v", err)
	}
	time.Sleep(10 * time.Millisecond) // 让 room cooldown 过期
	_, err := svc.Create(context.Background(), CreateInput{
		UserID: "u_cat_d2", RoomID: "room_demo", AmountGrams: 8, IdempotencyKey: "kc_d2",
	})
	if err == nil {
		t.Fatal("expected cat daily limit block (8+8 > 12)")
	}
}

func TestEventListenerCapturesTransitions(t *testing.T) {
	pub := &stubPublisher{}
	svc := newTestSvc(pub)

	var mu sync.Mutex
	var events []StateChangeEvent
	svc.AddEventListener(func(_ context.Context, ev StateChangeEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
	})

	req, err := svc.Create(context.Background(), CreateInput{
		UserID: "u1", RoomID: "room_demo", AmountGrams: 5, IdempotencyKey: "ke",
	})
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		// 至少观察到 created->accepted、accepted->queued、queued->dispatched
		return len(events) >= 3
	})

	if _, err := svc.Get(context.Background(), req.ID); err != nil {
		t.Fatal(err)
	}
}
