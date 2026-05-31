package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
	"yunmao.live/pkg/yunmao/featureflags"
	"yunmao.live/pkg/yunmao/feedingsafety"

	"yunmao.live/services/admin-svc/internal/service"
)

func newTestMux(verifier *authjwt.Verifier) http.Handler {
	svc := service.New(feedingsafety.NewMemoryStore(), featureflags.NewMemoryStore())
	return New(svc, ProxyConfig{Verifier: verifier}, nil)
}

func TestAuthGate_NoVerifier_PassesThrough(t *testing.T) {
	mux := newTestMux(nil)
	r := httptest.NewRequest(http.MethodGet, "/v1/admin/feature-flags", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without verifier, got %d", w.Code)
	}
}

func TestAuthGate_RequiresBearer(t *testing.T) {
	kp, err := authjwt.NewRSKeyProviderEphemeral("kid-test-1")
	if err != nil {
		t.Fatal(err)
	}
	v, err := authjwt.NewVerifierFromProvider(kp)
	if err != nil {
		t.Fatal(err)
	}
	mux := newTestMux(v)

	r := httptest.NewRequest(http.MethodGet, "/v1/admin/feature-flags", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthGate_RejectsUserScope(t *testing.T) {
	kp, err := authjwt.NewRSKeyProviderEphemeral("kid-test-2")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authjwt.NewSignerFromProvider(kp, "yunmao.user-svc")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := signer.SignLogin("usr_abc", authjwt.ScopeUser, "yunmao.gateway", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	v, err := authjwt.NewVerifierFromProvider(kp)
	if err != nil {
		t.Fatal(err)
	}
	mux := newTestMux(v)

	r := httptest.NewRequest(http.MethodGet, "/v1/admin/feature-flags", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for user scope, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthGate_AdminScopeAccepted(t *testing.T) {
	kp, err := authjwt.NewRSKeyProviderEphemeral("kid-test-3")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authjwt.NewSignerFromProvider(kp, "yunmao.user-svc")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := signer.SignLogin("usr_admin", authjwt.ScopeAdmin, "yunmao.gateway", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	v, err := authjwt.NewVerifierFromProvider(kp)
	if err != nil {
		t.Fatal(err)
	}
	mux := newTestMux(v)

	r := httptest.NewRequest(http.MethodGet, "/v1/admin/feature-flags", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin scope, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRoomsProxy_NoUpstream_ReturnsUnavailable(t *testing.T) {
	svc := service.New(feedingsafety.NewMemoryStore(), featureflags.NewMemoryStore())
	mux := New(svc, ProxyConfig{}, nil)

	r := httptest.NewRequest(http.MethodGet, "/v1/admin/rooms", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("rooms proxy absent: got %d (expected 404 or 405)", w.Code)
	}
}

func TestRoomsProxy_ForwardsToRoomSvc(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rooms" {
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rooms": []map[string]any{{"id": "room_demo", "name": "demo", "status": "live"}},
		})
	}))
	defer upstream.Close()

	svc := service.New(feedingsafety.NewMemoryStore(), featureflags.NewMemoryStore())
	mux := New(svc, ProxyConfig{RoomBaseURL: upstream.URL}, nil)

	r := httptest.NewRequest(http.MethodGet, "/v1/admin/rooms", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rooms, ok := body["rooms"].([]any)
	if !ok || len(rooms) == 0 {
		t.Fatalf("expected rooms array, got %+v", body)
	}
}

func TestWalletProxy_ForwardsToBillingSvc(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/wallets/u_demo" {
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user_id": "u_demo", "balance_fen": 10000, "coins": 100,
		})
	}))
	defer upstream.Close()

	svc := service.New(feedingsafety.NewMemoryStore(), featureflags.NewMemoryStore())
	mux := New(svc, ProxyConfig{BillingBaseURL: upstream.URL}, nil)

	r := httptest.NewRequest(http.MethodGet, "/v1/admin/wallets/u_demo", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestClaimsFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), claimsKey{}, &authjwt.Claims{
		Scope: authjwt.ScopeAdmin,
	})
	c := AdminClaims(ctx)
	if c == nil || c.Scope != authjwt.ScopeAdmin {
		t.Fatalf("expected admin claims, got %+v", c)
	}
	if AdminClaims(context.Background()) != nil {
		t.Fatal("expected nil claims in empty context")
	}
}

func TestAdminLogin_NoSigner_Unavailable(t *testing.T) {
	svc := service.New(feedingsafety.NewMemoryStore(), featureflags.NewMemoryStore())
	mux := New(svc, ProxyConfig{}, nil)

	r := httptest.NewRequest(http.MethodPost, "/v1/auth/admin/login",
		strings.NewReader(`{"password":"s3cret"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when signer absent, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminLogin_WrongPassword(t *testing.T) {
	kp, err := authjwt.NewRSKeyProviderEphemeral("kid-login-1")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authjwt.NewSignerFromProvider(kp, "yunmao.admin-svc")
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(feedingsafety.NewMemoryStore(), featureflags.NewMemoryStore())
	mux := New(svc, ProxyConfig{Signer: signer, AdminPassword: "correct"}, nil)

	r := httptest.NewRequest(http.MethodPost, "/v1/auth/admin/login",
		strings.NewReader(`{"password":"wrong"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong password, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminLogin_CorrectPassword(t *testing.T) {
	kp, err := authjwt.NewRSKeyProviderEphemeral("kid-login-2")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authjwt.NewSignerFromProvider(kp, "yunmao.admin-svc")
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := authjwt.NewVerifierFromProvider(kp)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(feedingsafety.NewMemoryStore(), featureflags.NewMemoryStore())
	mux := New(svc, ProxyConfig{Signer: signer, AdminPassword: "s3cret", Verifier: verifier}, nil)

	r := httptest.NewRequest(http.MethodPost, "/v1/auth/admin/login",
		strings.NewReader(`{"password":"s3cret"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on correct password, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken == "" || resp.TokenType != "Bearer" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	claims, err := verifier.Parse(resp.AccessToken)
	if err != nil {
		t.Fatalf("issued token unparsable: %v", err)
	}
	if claims.Scope != authjwt.ScopeAdmin {
		t.Fatalf("expected admin scope, got %q", claims.Scope)
	}
	if claims.Subject != "admin" {
		t.Fatalf("expected subject 'admin', got %q", claims.Subject)
	}

	r2 := httptest.NewRequest(http.MethodGet, "/v1/admin/feature-flags", nil)
	r2.Header.Set("Authorization", "Bearer "+resp.AccessToken)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("issued token should access admin routes: got %d", w2.Code)
	}
}

func TestAdminLogin_InvalidJSON(t *testing.T) {
	kp, err := authjwt.NewRSKeyProviderEphemeral("kid-login-3")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authjwt.NewSignerFromProvider(kp, "yunmao.admin-svc")
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(feedingsafety.NewMemoryStore(), featureflags.NewMemoryStore())
	mux := New(svc, ProxyConfig{Signer: signer, AdminPassword: "s3cret"}, nil)

	r := httptest.NewRequest(http.MethodPost, "/v1/auth/admin/login",
		bytes.NewReader([]byte(`{invalid`)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code == http.StatusOK {
		t.Fatal("expected error on invalid JSON")
	}
}

func TestAdminLogin_EmptyPassword_Returns401(t *testing.T) {
	kp, err := authjwt.NewRSKeyProviderEphemeral("kid-login-4")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authjwt.NewSignerFromProvider(kp, "yunmao.admin-svc")
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(feedingsafety.NewMemoryStore(), featureflags.NewMemoryStore())
	mux := New(svc, ProxyConfig{Signer: signer, AdminPassword: "s3cret"}, nil)

	r := httptest.NewRequest(http.MethodPost, "/v1/auth/admin/login",
		strings.NewReader(`{"password":""}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on empty password, got %d: %s", w.Code, w.Body.String())
	}
}
