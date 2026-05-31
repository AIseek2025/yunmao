package service

import (
	"context"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
	yerr "yunmao.live/pkg/yunmao/errors"
)

// testKP 为每个测试生成一份 ephemeral RSA keypair（HS256 已下线，ADR-0019 第七轮）。
func testKP(t *testing.T) authjwt.KeyProvider {
	t.Helper()
	kp, err := authjwt.NewRSKeyProviderEphemeral("room-svc-test")
	if err != nil {
		t.Fatal(err)
	}
	return kp
}

func newSvc(t *testing.T) (*RoomService, authjwt.KeyProvider) {
	t.Helper()
	kp := testKP(t)
	signer, _ := authjwt.NewSignerFromProvider(kp, "yunmao.room-svc")
	ver, _ := authjwt.NewVerifierFromProvider(kp)
	return New(Config{Signer: signer, Verifier: ver, TokenTTL: time.Minute, RegionID: "cn-east-1"}), kp
}

func TestDefaultRoomExists(t *testing.T) {
	s, _ := newSvc(t)
	r, err := s.Get(context.Background(), "room_demo")
	if err != nil {
		t.Fatal(err)
	}
	if r.FeedingStatus != "open" {
		t.Fatalf("expected open, got %s", r.FeedingStatus)
	}
}

func TestUnknownRoomReturnsErr(t *testing.T) {
	s, _ := newSvc(t)
	_, err := s.Get(context.Background(), "room_x")
	app := yerr.AsAppError(err)
	if app.Code != yerr.RoomNotFound {
		t.Fatalf("expected room not found, got %s", app.Code)
	}
}

func TestCreateAutoFillsDefaults(t *testing.T) {
	s, _ := newSvc(t)
	r, err := s.Create(context.Background(), Room{DisplayName: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == "" || r.FeedCooldownSeconds == 0 || r.StreamKey == "" {
		t.Fatalf("defaults not filled: %#v", r)
	}
	if r.Status != "offline" {
		t.Fatalf("status not initialized: %s", r.Status)
	}
}

func TestUpdateMergesFields(t *testing.T) {
	s, _ := newSvc(t)
	created, _ := s.Create(context.Background(), Room{DisplayName: "x", City: "上海"})
	upd, err := s.Update(context.Background(), Room{ID: created.ID, DisplayName: "y"})
	if err != nil {
		t.Fatal(err)
	}
	if upd.DisplayName != "y" || upd.City != "上海" {
		t.Fatalf("update did not merge: %+v", upd)
	}
}

func TestListFilters(t *testing.T) {
	s, _ := newSvc(t)
	_, _ = s.Create(context.Background(), Room{DisplayName: "a", OwnerID: "usr_1", RegionID: "cn"})
	_, _ = s.Create(context.Background(), Room{DisplayName: "b", OwnerID: "usr_2", RegionID: "us"})
	rooms, err := s.List(context.Background(), ListFilter{OwnerID: "usr_1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rooms {
		if r.OwnerID != "" && r.OwnerID != "usr_1" {
			t.Fatalf("filter leaked: %s", r.OwnerID)
		}
	}
}

func TestSetStatus(t *testing.T) {
	s, _ := newSvc(t)
	if err := s.SetStatus(context.Background(), "room_demo", "live"); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Get(context.Background(), "room_demo")
	if r.Status != "live" || r.LiveStatus != "online" {
		t.Fatalf("status not live: %+v", r)
	}
	if err := s.SetStatus(context.Background(), "room_demo", "banned"); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Get(context.Background(), "room_demo")
	if r.Status != "banned" || r.LiveStatus != "offline" {
		t.Fatalf("status not banned: %+v", r)
	}
	if err := s.SetStatus(context.Background(), "room_demo", "garbage"); err == nil {
		t.Fatal("expected invalid status err")
	}
}

func TestRotateStreamKey(t *testing.T) {
	s, _ := newSvc(t)
	sk1, err := s.RotateStreamKey(context.Background(), "room_demo")
	if err != nil {
		t.Fatal(err)
	}
	// 微小延迟以确保 hmac 入参 nanos 不同
	time.Sleep(time.Millisecond)
	sk2, _ := s.RotateStreamKey(context.Background(), "room_demo")
	if sk1 == sk2 {
		t.Fatalf("expected stream key to change between rotations")
	}
	if sk1[:len("cn-east-1_")] != "cn-east-1_" {
		t.Fatalf("expected region prefix, got %s", sk1)
	}
}

func TestIssueSubscriptionWithLoginToken(t *testing.T) {
	s, kp := newSvc(t)
	signer, _ := authjwt.NewSignerFromProvider(kp, "yunmao.user-svc")
	loginTok, _ := signer.SignLogin("usr_x", authjwt.ScopeUser, "yunmao.gateway", time.Minute)

	resp, err := s.IssueSubscription(context.Background(), SubscriptionRequest{
		RoomID:    "room_demo",
		UserToken: loginTok,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Token == "" {
		t.Fatal("expected token")
	}
	v, _ := authjwt.NewVerifierFromProvider(kp)
	cl, err := v.Parse(resp.Token)
	if err != nil {
		t.Fatal(err)
	}
	if cl.Room != "room_demo" || cl.Kind != authjwt.KindRoomSubscription {
		t.Fatalf("claims wrong: %+v", cl)
	}
	if cl.Scope != authjwt.ScopeUser {
		t.Fatalf("expected scope=user, got %v", cl.Scope)
	}
}

func TestIssueSubscriptionGuestNeedsFlag(t *testing.T) {
	s, kp := newSvc(t)
	if _, err := s.IssueSubscription(context.Background(), SubscriptionRequest{RoomID: "room_demo"}, false); err == nil {
		t.Fatal("expected login required")
	}
	resp, err := s.IssueSubscription(context.Background(), SubscriptionRequest{RoomID: "room_demo"}, true)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := authjwt.NewVerifierFromProvider(kp)
	cl, _ := v.Parse(resp.Token)
	if cl.Scope != authjwt.ScopeGuest {
		t.Fatalf("expected guest, got %v", cl.Scope)
	}
}

func TestIssueSubscriptionRejectsExpiredLogin(t *testing.T) {
	s, kp := newSvc(t)
	signer, _ := authjwt.NewSignerFromProvider(kp, "yunmao.user-svc")
	loginTok, _ := signer.SignLogin("usr_x", authjwt.ScopeUser, "yunmao.gateway", time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	if _, err := s.IssueSubscription(context.Background(), SubscriptionRequest{
		RoomID:    "room_demo",
		UserToken: loginTok,
	}, false); err == nil {
		t.Fatal("expected expired error")
	}
}
