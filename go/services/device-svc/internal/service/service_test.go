package service

import (
	"context"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
	yerr "yunmao.live/pkg/yunmao/errors"
)

func newSvc(t *testing.T) *DeviceService {
	t.Helper()
	// HS256 已下线（ADR-0019 第七轮）；测试改用 ephemeral RS256 KeyProvider。
	kp, err := authjwt.NewRSKeyProviderEphemeral("dev-key")
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{KeyProvider: kp, MqttCredentialTTL: time.Hour})
}

func TestDefaultDeviceOnline(t *testing.T) {
	s := newSvc(t)
	d, err := s.Get(context.Background(), "dev_demo")
	if err != nil {
		t.Fatal(err)
	}
	if d.OnlineStatus != "online" {
		t.Fatalf("got %s", d.OnlineStatus)
	}
}

func TestUnknownDevice(t *testing.T) {
	s := newSvc(t)
	_, err := s.Get(context.Background(), "dev_x")
	if app := yerr.AsAppError(err); app.Code != yerr.DeviceUnbound {
		t.Fatalf("expected unbound, got %s", app.Code)
	}
}

func TestMarkOnlineUpdatesRemaining(t *testing.T) {
	s := newSvc(t)
	s.MarkOnline(context.Background(), "dev_demo", 555)
	d, _ := s.Get(context.Background(), "dev_demo")
	if d.RemainingFoodGrams != 555 {
		t.Fatalf("got %d", d.RemainingFoodGrams)
	}
}

func TestRegisterUpdateBind(t *testing.T) {
	s := newSvc(t)
	d, err := s.Register(context.Background(), Device{HardwareModel: "v2"})
	if err != nil {
		t.Fatal(err)
	}
	if d.ID == "" {
		t.Fatal("expected id auto-gen")
	}
	upd, err := s.Update(context.Background(), Device{ID: d.ID, FirmwareVersion: "1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if upd.FirmwareVersion != "1.2.3" {
		t.Fatalf("update lost field")
	}
	if err := s.BindRoom(context.Background(), d.ID, "room_demo"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(context.Background(), d.ID)
	if got.RoomID != "room_demo" {
		t.Fatalf("bind failed: %+v", got)
	}
	if err := s.UnbindRoom(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(context.Background(), d.ID)
	if got.RoomID != "" {
		t.Fatalf("unbind failed: %+v", got)
	}
}

func TestSetStatus(t *testing.T) {
	s := newSvc(t)
	if err := s.SetStatus(context.Background(), "dev_demo", "error"); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Get(context.Background(), "dev_demo")
	if d.OnlineStatus != "error" {
		t.Fatalf("status not set: %+v", d)
	}
	if err := s.SetStatus(context.Background(), "dev_demo", "bogus"); err == nil {
		t.Fatal("expected invalid status err")
	}
}

func TestIssueMqttCredential(t *testing.T) {
	s := newSvc(t)
	cred, err := s.IssueMqttCredential(context.Background(), "dev_demo")
	if err != nil {
		t.Fatal(err)
	}
	if cred.Username == "" || cred.Password == "" {
		t.Fatalf("missing fields: %+v", cred)
	}
	if cred.ExpiresAt.Before(time.Now()) {
		t.Fatalf("already expired: %v", cred.ExpiresAt)
	}
	if cred.Algorithm != string(authjwt.AlgRS256) {
		t.Fatalf("expected RS256, got %s", cred.Algorithm)
	}
}

func TestUpdateFirmware(t *testing.T) {
	s := newSvc(t)
	if err := s.UpdateFirmware(context.Background(), "dev_demo", "2.0.0"); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Get(context.Background(), "dev_demo")
	if d.FirmwareTarget != "2.0.0" {
		t.Fatalf("firmware target not set: %+v", d)
	}
}
