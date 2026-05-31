package turn

import (
	"errors"
	"testing"
	"time"
)

func TestIssueAndVerifyRoundtrip(t *testing.T) {
	s, err := NewSigner(Config{PrimarySecret: []byte("yunmao-turn-secret-32-bytes-please")})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := s.Issue("usr_demo", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if cred.Username == "" || cred.Credential == "" {
		t.Fatalf("missing fields: %+v", cred)
	}
	if err := s.Verify(cred.Username, cred.Credential); err != nil {
		t.Fatalf("verify roundtrip failed: %v", err)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	s, _ := NewSigner(Config{PrimarySecret: []byte("yunmao-turn-secret-32-bytes-please")})
	cred, _ := s.Issue("usr_demo", 1*time.Second)
	s.SetNow(func() time.Time { return time.Now().Add(10 * time.Second) })
	if err := s.Verify(cred.Username, cred.Credential); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
}

func TestVerifyRejectsBadSignature(t *testing.T) {
	s, _ := NewSigner(Config{PrimarySecret: []byte("yunmao-turn-secret-32-bytes-please")})
	cred, _ := s.Issue("usr_demo", 5*time.Minute)
	if err := s.Verify(cred.Username, cred.Credential+"x"); !errors.Is(err, ErrSignature) {
		t.Fatalf("expected ErrSignature, got %v", err)
	}
}

func TestRotationAcceptsLegacy(t *testing.T) {
	old, _ := NewSigner(Config{PrimarySecret: []byte("old-secret-32-byte-stretched-aaa")})
	rotated, _ := NewSigner(Config{
		PrimarySecret: []byte("new-secret-32-byte-stretched-bbb"),
		LegacySecret:  []byte("old-secret-32-byte-stretched-aaa"),
	})
	cred, _ := old.Issue("usr_demo", 5*time.Minute)
	if err := rotated.Verify(cred.Username, cred.Credential); err != nil {
		t.Fatalf("rotation overlap should accept legacy secret, got %v", err)
	}
	// 反过来：用 rotated 签发的 token，旧 signer 不应再认可
	newCred, _ := rotated.Issue("usr_demo", 5*time.Minute)
	if err := old.Verify(newCred.Username, newCred.Credential); err == nil {
		t.Fatal("expected old signer to reject new credential")
	}
}

func TestVerifyRejectsBadUsername(t *testing.T) {
	s, _ := NewSigner(Config{PrimarySecret: []byte("yunmao-turn-secret-32-bytes-please")})
	if err := s.Verify("nope", "x"); err == nil {
		t.Fatal("expected error on malformed username")
	}
}

func TestICEEndpointsHelper(t *testing.T) {
	urls := ICEEndpoints([]string{"turn.yunmao.live"}, []int{3478, 5349})
	if len(urls) != 4 {
		t.Fatalf("expected 4 urls (2 hosts × 2 transports), got %d: %v", len(urls), urls)
	}
}
