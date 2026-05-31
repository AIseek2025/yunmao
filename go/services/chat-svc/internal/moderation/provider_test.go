package moderation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLocalProviderHit(t *testing.T) {
	p := NewLocalProvider([]string{"shit"})
	d, err := p.Inspect(context.Background(), "this is shit content")
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != ActionHide {
		t.Fatalf("want hide got %s", d.Action)
	}
	if d.Provider != "local" {
		t.Fatalf("want local got %s", d.Provider)
	}
}

func TestLocalProviderPass(t *testing.T) {
	p := NewLocalProvider([]string{"shit"})
	d, _ := p.Inspect(context.Background(), "hello world")
	if d.Action != ActionPass {
		t.Fatalf("want pass got %s", d.Action)
	}
}

func TestLocalProviderHotReload(t *testing.T) {
	p := NewLocalProvider(nil)
	d, _ := p.Inspect(context.Background(), "xyz")
	if d.Action != ActionPass {
		t.Fatalf("want pass got %s", d.Action)
	}
	p.SetWords([]string{"xyz"})
	d2, _ := p.Inspect(context.Background(), "xyz here")
	if d2.Action != ActionHide {
		t.Fatalf("want hide after reload got %s", d2.Action)
	}
}

func TestAliyunGreenMockBlock(t *testing.T) {
	p, err := NewAliyunGreenProvider(AliyunGreenConfig{MockMode: true})
	if err != nil {
		t.Fatal(err)
	}
	d, err := p.Inspect(context.Background(), "aliyun-block-test boom")
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != ActionBlock {
		t.Fatalf("want block got %s", d.Action)
	}
}

func TestAliyunGreenMockPass(t *testing.T) {
	p, err := NewAliyunGreenProvider(AliyunGreenConfig{MockMode: true})
	if err != nil {
		t.Fatal(err)
	}
	d, err := p.Inspect(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != ActionPass {
		t.Fatalf("want pass got %s", d.Action)
	}
}

func TestAliyunGreenMissingCreds(t *testing.T) {
	_, err := NewAliyunGreenProvider(AliyunGreenConfig{MockMode: false})
	if err == nil {
		t.Fatal("want error on missing creds")
	}
}

// failingProvider used to verify fallback wiring.
type failingProvider struct{}

func (failingProvider) Name() string { return "failing" }
func (failingProvider) Inspect(_ context.Context, _ string) (Decision, error) {
	return Decision{}, errors.New("upstream boom")
}

func TestManagerFallback(t *testing.T) {
	local := NewLocalProvider([]string{"shit"})
	m := NewManager(failingProvider{}, local, 50*time.Millisecond)
	d := m.Inspect(context.Background(), "this is shit")
	if d.Action != ActionHide {
		t.Fatalf("want hide via fallback got %s reason=%s", d.Action, d.Reason)
	}
	if d.Provider != "local" {
		t.Fatalf("want provider=local got %s", d.Provider)
	}
}

func TestManagerPrimarySuccess(t *testing.T) {
	primary, _ := NewAliyunGreenProvider(AliyunGreenConfig{MockMode: true})
	local := NewLocalProvider(nil)
	m := NewManager(primary, local, 50*time.Millisecond)
	d := m.Inspect(context.Background(), "hello world")
	if d.Action != ActionPass {
		t.Fatalf("want pass got %s", d.Action)
	}
	if m.Active() != "aliyun_green" {
		t.Fatalf("want active aliyun_green got %s", m.Active())
	}
}

func TestManagerSetPrimary(t *testing.T) {
	primary, _ := NewAliyunGreenProvider(AliyunGreenConfig{MockMode: true})
	local := NewLocalProvider(nil)
	m := NewManager(primary, local, 50*time.Millisecond)
	if m.Active() != "aliyun_green" {
		t.Fatal("expect aliyun_green initially")
	}
	m.SetPrimary(local)
	if m.Active() != "local" {
		t.Fatalf("want local got %s", m.Active())
	}
}
