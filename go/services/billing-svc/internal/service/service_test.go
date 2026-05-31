package service

import (
	"context"
	"testing"

	yerr "yunmao.live/pkg/yunmao/errors"
)

func TestCreateAndPay(t *testing.T) {
	s := New(nil)
	o, err := s.Create(context.Background(), CreateInput{UserID: "u", Channel: "wechat", BizType: "feed_ticket", AmountCny: 100})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != "created" {
		t.Fatalf("got %s", o.Status)
	}
	paid, err := s.MarkPaid(context.Background(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paid.Status != "paid" {
		t.Fatalf("got %s", paid.Status)
	}
	if _, err := s.MarkPaid(context.Background(), o.ID); err == nil {
		t.Fatal("expected double-pay rejected")
	}
}

func TestCreateRequiresFields(t *testing.T) {
	s := New(nil)
	_, err := s.Create(context.Background(), CreateInput{})
	if err == nil {
		t.Fatal("expected error")
	}
	app := yerr.AsAppError(err)
	if app == nil {
		t.Fatal("not app error")
	}
}

func TestRefundFlow(t *testing.T) {
	s := New(nil)
	o, err := s.Create(context.Background(), CreateInput{UserID: "u", Channel: "wechat", BizType: "feed_ticket", AmountCny: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkPaid(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Refund(context.Background(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "refunded" {
		t.Fatalf("got %s", got.Status)
	}
}
