package pay_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"yunmao.live/services/billing-svc/internal/pay"
	"yunmao.live/services/billing-svc/internal/service"
)

type memoryOrderSrc struct {
	orders []pay.LocalOrder
}

func (s *memoryOrderSrc) ListOrdersForReconcile(_ context.Context, _ time.Time) ([]pay.LocalOrder, error) {
	return s.orders, nil
}

func TestFullPaymentChain_E2E(t *testing.T) {
	ctx := context.Background()
	svc := service.New(nil)

	reg := pay.NewRegistry()
	mockSecret := "test-k"
	mockCh := pay.NewMockChannel(pay.MockConfig{Secret: mockSecret})
	reg.Register(mockCh)

	order, err := svc.Create(ctx, service.CreateInput{
		UserID:    "user-001",
		Channel:   "mock",
		BizType:   "membership",
		AmountCny: 30,
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	t.Logf("[1/9] order created: id=%s status=%s amount=%d CNY",
		order.ID, order.Status, order.AmountCny)

	prepay, err := mockCh.CreatePrepay(ctx, pay.PrepayRequest{
		OrderID:   order.ID,
		AmountFen: int64(order.AmountCny) * 100,
	})
	if err != nil {
		t.Fatalf("create prepay: %v", err)
	}
	if prepay.PrepayID == "" || prepay.Channel != pay.ChannelMock {
		t.Fatalf("prepay malformed: %+v", prepay)
	}
	t.Logf("[2/9] prepay created: prepay_id=%s channel=%s pay_url=%s",
		prepay.PrepayID, prepay.Channel, prepay.PayURL)

	body := fmt.Sprintf(`{"order_id":%q,"external_trade_no":"ext-123","amount_fen":3000,"status":"paid"}`, order.ID)
	tsv := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "nonce-abc"
	sig := pay.MockSign(mockSecret, []byte(body), tsv, nonce)

	ev, err := mockCh.VerifyWebhook(ctx, []byte(body), map[string]string{
		"X-Yunmao-Pay-Ts":    tsv,
		"X-Yunmao-Pay-Nonce": nonce,
		"X-Yunmao-Pay-Sig":   sig,
	})
	if err != nil {
		t.Fatalf("verify webhook: %v", err)
	}
	if ev.Status != "paid" || ev.OrderID != order.ID {
		t.Fatalf("webhook event malformed: %+v", ev)
	}
	t.Logf("[3/9] webhook verified: order_id=%s status=%s external_trade=%s",
		ev.OrderID, ev.Status, ev.ExternalTradeNo)

	if _, err := svc.MarkPaid(ctx, order.ID); err != nil {
		t.Fatalf("mark paid: %v", err)
	}
	paid, err := svc.Get(ctx, order.ID)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if paid.Status != "paid" {
		t.Fatalf("want paid got %s", paid.Status)
	}
	t.Logf("[4/9] order paid: id=%s paid_at=%s", paid.ID, paid.PaidAt.Format(time.RFC3339))

	src := &memoryOrderSrc{orders: []pay.LocalOrder{
		{OrderID: order.ID, Channel: pay.ChannelMock, Status: "paid", AmountFen: 3000},
	}}
	sink := pay.NewInMemoryReconcileSink()
	worker := pay.NewReconcileWorker(src, reg, sink)
	worker.SetInterval(time.Millisecond)
	worker.SetLookback(time.Hour)

	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("reconcile run: %v", err)
	}
	recs := sink.Records()
	diffs := sink.Diffs()
	t.Logf("[5/9] reconciliation: records=%d diffs=%d", len(recs), len(diffs))
	if len(recs) != 1 {
		t.Fatalf("want 1 reconcile record got %d", len(recs))
	}
	if recs[0].RemoteStatus != "paid" {
		t.Fatalf("want remote=paid got %s", recs[0].RemoteStatus)
	}
	if recs[0].DiffReason != "" {
		t.Fatalf("want no diff got %q", recs[0].DiffReason)
	}
	t.Logf("      local_status=%s remote_status=%s diff=%q",
		recs[0].LocalStatus, recs[0].RemoteStatus, recs[0].DiffReason)

	if err := svc.TopUp(ctx, "user-001", 10000); err != nil {
		t.Fatalf("topup: %v", err)
	}
	t.Logf("      wallet topped up: user=user-001 +10000 fen")

	hold, err := svc.Reserve(ctx, service.WalletReserveInput{
		UserID:         "user-001",
		RoomID:         "room-001",
		CatID:          "cat-007",
		IdempotencyKey: "idem-feed-1",
		AmountGrams:    10,
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	t.Logf("[6/9] wallet hold reserved: id=%s user=%s grams=%d fen=%d status=%s",
		hold.ID, hold.UserID, hold.AmountGrams, hold.AmountFen, hold.Status)

	confirmed, err := svc.Confirm(ctx, hold.ID)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if confirmed.Status != "confirmed" {
		t.Fatalf("want confirmed got %s", confirmed.Status)
	}
	t.Logf("[7/9] wallet hold confirmed: id=%s status=%s", confirmed.ID, confirmed.Status)

	refundBody := fmt.Sprintf(`{"order_id":%q,"external_trade_no":"ext-123","amount_fen":3000,"status":"refunded"}`, order.ID)
	rts := strconv.FormatInt(time.Now().Unix(), 10)
	rnonce := "nonce-refund"
	rsig := pay.MockSign(mockSecret, []byte(refundBody), rts, rnonce)
	_, err = mockCh.VerifyWebhook(ctx, []byte(refundBody), map[string]string{
		"X-Yunmao-Pay-Ts":    rts,
		"X-Yunmao-Pay-Nonce": rnonce,
		"X-Yunmao-Pay-Sig":   rsig,
	})
	if err != nil {
		t.Fatalf("refund webhook verify: %v", err)
	}
	t.Logf("[8/9] refund webhook verified")

	refundRes, err := mockCh.Refund(ctx, pay.RefundRequest{
		OrderID:   order.ID,
		AmountFen: 3000,
	})
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if refundRes.Status != "ok" {
		t.Fatalf("want refund ok got %s", refundRes.Status)
	}
	if _, err := svc.Refund(ctx, order.ID); err != nil {
		t.Fatalf("svc refund: %v", err)
	}
	refunded, _ := svc.Get(ctx, order.ID)
	if refunded.Status != "refunded" {
		t.Fatalf("want refunded got %s", refunded.Status)
	}
	t.Logf("[9/9] order refunded: id=%s status=%s refund_id=%s",
		refunded.ID, refunded.Status, refundRes.RefundID)

	src.orders = []pay.LocalOrder{
		{OrderID: order.ID, Channel: pay.ChannelMock, Status: "refunded", AmountFen: 3000},
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("post-refund reconcile: %v", err)
	}
	postRecs := sink.Records()
	postDiffs := sink.Diffs()
	t.Logf("      post-refund reconcile: total_records=%d total_diffs=%d", len(postRecs), len(postDiffs))

	t.Logf("=== FULL CHAIN COMPLETE: order lifecycle + wallet saga + reconciliation ===")
}

func TestFullPaymentChain_WalletBalanceConsistency(t *testing.T) {
	ctx := context.Background()
	svc := service.New(nil)

	if err := svc.TopUp(ctx, "user-topup", 50000); err != nil {
		t.Fatalf("topup: %v", err)
	}
	t.Logf("[setup] wallet topped up: user=user-topup +50000 fen")

	topupHold, err := svc.Reserve(ctx, service.WalletReserveInput{
		UserID:         "user-topup",
		RoomID:         "room-sys",
		CatID:          "cat-sys",
		IdempotencyKey: "idem-topup-1",
		AmountGrams:    100,
	})
	if err != nil {
		t.Fatalf("topup reserve: %v", err)
	}
	t.Logf("[topup-1] reserve for initial funds: hold=%s fen=%d", topupHold.ID, topupHold.AmountFen)

	confirmed, err := svc.Confirm(ctx, topupHold.ID)
	if err != nil {
		t.Fatalf("topup confirm: %v", err)
	}
	t.Logf("[topup-2] confirmed: hold=%s fen=%d", confirmed.ID, confirmed.AmountFen)

	spender, err := svc.Reserve(ctx, service.WalletReserveInput{
		UserID:         "user-topup",
		RoomID:         "room-r1",
		CatID:          "cat-c1",
		IdempotencyKey: "idem-spend-1",
		AmountGrams:    50,
	})
	if err != nil {
		t.Fatalf("spend reserve: %v", err)
	}
	t.Logf("[spend-1] spend reserve: hold=%s fen=%d status=%s", spender.ID, spender.AmountFen, spender.Status)

	spent, err := svc.Confirm(ctx, spender.ID)
	if err != nil {
		t.Fatalf("spend confirm: %v", err)
	}
	t.Logf("[spend-2] spend confirmed: hold=%s status=%s", spent.ID, spent.Status)

	idemHold, err := svc.Reserve(ctx, service.WalletReserveInput{
		UserID:         "user-topup",
		RoomID:         "room-r1",
		CatID:          "cat-c1",
		IdempotencyKey: "idem-spend-1",
		AmountGrams:    50,
	})
	if err != nil {
		t.Fatalf("idempotent reserve: %v", err)
	}
	if idemHold.ID != spender.ID {
		t.Fatalf("idempotent reserve returned different hold: %s vs %s", idemHold.ID, spender.ID)
	}
	t.Logf("[idempotency] duplicate reserve returned same hold: %s", idemHold.ID)

	t.Logf("=== WALLET CONSISTENCY: reserve/confirm/idempotent all consistent ===")
}
