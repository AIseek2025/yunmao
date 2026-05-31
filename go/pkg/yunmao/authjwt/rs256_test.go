package authjwt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRS256SignAndVerifyRoundtrip(t *testing.T) {
	kp, err := NewRSKeyProviderEphemeral("test-kid-1")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSignerFromProvider(kp, "yunmao.test")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := s.SignLogin("usr_rs", ScopeUser, "yunmao.gateway", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVerifierFromProvider(kp)
	if err != nil {
		t.Fatal(err)
	}
	c, err := v.Parse(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.Subject != "usr_rs" || c.Kind != KindLogin {
		t.Fatalf("claims mismatch: %+v", c)
	}
}

func TestJWKSClientFetchesRemotePublicKeys(t *testing.T) {
	kp, _ := NewRSKeyProviderEphemeral("test-kid-2")
	jwks := kp.PublicJWKS()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	defer srv.Close()

	s, _ := NewSignerFromProvider(kp, "yunmao.test")
	tok, _ := s.SignLogin("usr_remote", ScopeUser, "yunmao.gateway", time.Minute)

	client := NewJWKSClient([]string{srv.URL}, time.Minute)
	if err := client.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	v, _ := NewVerifierFromProvider(client)
	c, err := v.Parse(tok)
	if err != nil {
		t.Fatalf("parse via JWKSClient: %v", err)
	}
	if c.Subject != "usr_remote" {
		t.Fatalf("subject mismatch: %+v", c)
	}
}

func TestJWKSClientRejectsUnknownKid(t *testing.T) {
	kp, _ := NewRSKeyProviderEphemeral("kid-A")
	other, _ := NewRSKeyProviderEphemeral("kid-B")
	s, _ := NewSignerFromProvider(other, "yunmao.test")
	tok, _ := s.SignLogin("usr_x", ScopeUser, "yunmao.gateway", time.Minute)

	// 客户端只缓存 kp 的 kid，应该校验失败
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(kp.PublicJWKS())
	}))
	defer srv.Close()

	c := NewJWKSClient([]string{srv.URL}, time.Minute)
	_ = c.Refresh(context.Background())
	v, _ := NewVerifierFromProvider(c)
	if _, err := v.Parse(tok); err == nil {
		t.Fatal("expected unknown kid error")
	}
}
