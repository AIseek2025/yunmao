package service

import (
	"context"
	"errors"
	"testing"

	"yunmao.live/services/billing-svc/internal/store"
)

func TestWalletSaga_ReserveConfirmHappyPath(t *testing.T) {
	svc := New(store.NewMemoryStore())
	ctx := context.Background()
	if err := svc.TopUp(ctx, "u1", 1000); err != nil {
		t.Fatalf("topup: %v", err)
	}
	h, err := svc.Reserve(ctx, WalletReserveInput{
		UserID:         "u1",
		RoomID:         "r1",
		CatID:          "c1",
		AmountGrams:    20, // ConvertGramsToFen=100
		IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if h.Status != "reserved" || h.AmountFen != 100 {
		t.Fatalf("unexpected hold: %+v", h)
	}
	w, _ := svc.Wallet(ctx, "u1")
	if w.ReservedFen != 100 || w.BalanceFen != 1000 {
		t.Fatalf("wallet after reserve: %+v", w)
	}

	c, err := svc.Confirm(ctx, h.ID)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if c.Status != "confirmed" {
		t.Fatalf("expected confirmed, got %s", c.Status)
	}
	w, _ = svc.Wallet(ctx, "u1")
	if w.BalanceFen != 900 || w.ReservedFen != 0 {
		t.Fatalf("wallet after confirm: %+v", w)
	}
}

func TestWalletSaga_ReserveCancelReleasesFunds(t *testing.T) {
	svc := New(store.NewMemoryStore())
	ctx := context.Background()
	_ = svc.TopUp(ctx, "u2", 500)
	h, _ := svc.Reserve(ctx, WalletReserveInput{
		UserID: "u2", AmountGrams: 50, IdempotencyKey: "k2",
	})
	if _, err := svc.Cancel(ctx, h.ID, "user_cancelled"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	w, _ := svc.Wallet(ctx, "u2")
	if w.ReservedFen != 0 || w.BalanceFen != 500 {
		t.Fatalf("wallet after cancel: %+v", w)
	}
}

func TestWalletSaga_InsufficientFundsRejects(t *testing.T) {
	svc := New(store.NewMemoryStore())
	ctx := context.Background()
	_ = svc.TopUp(ctx, "u3", 10)
	_, err := svc.Reserve(ctx, WalletReserveInput{
		UserID: "u3", AmountGrams: 100, IdempotencyKey: "k3",
	})
	if err == nil {
		t.Fatalf("expected insufficient funds error")
	}
}

func TestWalletSaga_IdempotentReserve(t *testing.T) {
	svc := New(store.NewMemoryStore())
	ctx := context.Background()
	_ = svc.TopUp(ctx, "u4", 1000)
	h1, err := svc.Reserve(ctx, WalletReserveInput{
		UserID: "u4", AmountGrams: 20, IdempotencyKey: "k4",
	})
	if err != nil {
		t.Fatalf("reserve1: %v", err)
	}
	h2, err := svc.Reserve(ctx, WalletReserveInput{
		UserID: "u4", AmountGrams: 20, IdempotencyKey: "k4",
	})
	if err != nil {
		t.Fatalf("reserve2: %v", err)
	}
	if h1.ID != h2.ID {
		t.Fatalf("expected same hold id on idempotent reserve, got %s vs %s", h1.ID, h2.ID)
	}
	w, _ := svc.Wallet(ctx, "u4")
	// 只应冻结一次
	if w.ReservedFen != 100 {
		t.Fatalf("expected reserved=100, got %d", w.ReservedFen)
	}
}

func TestWalletSaga_ConfirmTwiceReturnsCurrent(t *testing.T) {
	svc := New(store.NewMemoryStore())
	ctx := context.Background()
	_ = svc.TopUp(ctx, "u5", 1000)
	h, _ := svc.Reserve(ctx, WalletReserveInput{UserID: "u5", AmountGrams: 10, IdempotencyKey: "k5"})
	_, err := svc.Confirm(ctx, h.ID)
	if err != nil {
		t.Fatalf("confirm1: %v", err)
	}
	c2, err := svc.Confirm(ctx, h.ID)
	if err != nil {
		t.Fatalf("confirm2: %v", err)
	}
	if c2.Status != "confirmed" {
		t.Fatalf("expected idempotent confirm")
	}
}

func TestWalletSaga_CancelAfterConfirmFails(t *testing.T) {
	svc := New(store.NewMemoryStore())
	ctx := context.Background()
	_ = svc.TopUp(ctx, "u6", 1000)
	h, _ := svc.Reserve(ctx, WalletReserveInput{UserID: "u6", AmountGrams: 10, IdempotencyKey: "k6"})
	_, _ = svc.Confirm(ctx, h.ID)
	_, err := svc.Cancel(ctx, h.ID, "late")
	if err == nil || !errStringContains(err, "terminal") {
		t.Fatalf("expected terminal status err, got %v", err)
	}
}

func errStringContains(err error, sub string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, store.ErrAlreadyTerminal) {
		return true
	}
	return contains(err.Error(), sub)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
