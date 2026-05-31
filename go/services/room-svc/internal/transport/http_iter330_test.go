package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
	"yunmao.live/services/room-svc/internal/service"
	"yunmao.live/services/room-svc/internal/turn"
)

var turnTestSecret = []byte("iter330-turn-primary-secret-32B-pad")

func testKP(t *testing.T) authjwt.KeyProvider {
	t.Helper()
	kp, err := authjwt.NewRSKeyProviderEphemeral("room-svc-iter330")
	if err != nil {
		t.Fatal(err)
	}
	return kp
}

func newRoomSvcWithTURN(t *testing.T) (*service.RoomService, authjwt.KeyProvider, *turn.Signer) {
	t.Helper()
	kp := testKP(t)
	signer, _ := authjwt.NewSignerFromProvider(kp, "yunmao.room-svc")
	ver, _ := authjwt.NewVerifierFromProvider(kp)
	turnSigner, err := turn.NewSigner(turn.Config{PrimarySecret: []byte("iter330-turn-primary-secret-32B-pad")})
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(service.Config{
		Signer:     signer,
		Verifier:   ver,
		TokenTTL:   time.Minute,
		RegionID:   "cn-east-1",
		TurnSigner: turnSigner,
		TurnHosts:  []string{"turn.yunmao.test"},
		TurnPorts:  []int{3478, 5349},
		TurnTTL:    5 * time.Minute,
	})
	return svc, kp, turnSigner
}

func newRoomSvcSTUNOnly(t *testing.T) (*service.RoomService, authjwt.KeyProvider) {
	t.Helper()
	kp := testKP(t)
	signer, _ := authjwt.NewSignerFromProvider(kp, "yunmao.room-svc")
	ver, _ := authjwt.NewVerifierFromProvider(kp)
	svc := service.New(service.Config{
		Signer:    signer,
		Verifier:  ver,
		TokenTTL:  time.Minute,
		RegionID:  "cn-east-1",
		StunUrls:  []string{"stun:stun.l.google.com:19302"},
	})
	return svc, kp
}

// round2 fresh evidence: /v1/rooms/{id}/ice-servers with TURN signer issues
// time-limited HMAC-SHA1 credential that verifies round-trip.
func TestIceServersWithTURN_Iter330ProcessEvidence(t *testing.T) {
	svc, _, turnSigner := newRoomSvcWithTURN(t)
	h := New(svc, false)

	req := httptest.NewRequest(http.MethodGet, "/v1/rooms/room_demo/ice-servers?user_id=usr_iter330", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d body=%s", w.Code, w.Body.String())
	}
	var resp service.IceServersResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(resp.IceServers) != 2 {
		t.Fatalf("expected STUN + TURN entries, got %d: %+v", len(resp.IceServers), resp.IceServers)
	}
	turnEntry := resp.IceServers[1]
	if len(turnEntry.Urls) != 4 {
		t.Fatalf("expected 4 TURN URLs (2 hosts*2 transports), got %d: %v", len(turnEntry.Urls), turnEntry.Urls)
	}
	if turnEntry.Username == "" || turnEntry.Credential == "" {
		t.Fatalf("TURN credential missing: %+v", turnEntry)
	}
	if resp.TTLSeconds <= 0 || resp.ExpiresAt.IsZero() {
		t.Fatalf("expected positive TTL and non-zero expires_at, got ttl=%v expires=%v", resp.TTLSeconds, resp.ExpiresAt)
	}
	if err := turnSigner.Verify(turnEntry.Username, turnEntry.Credential); err != nil {
		t.Fatalf("TURN credential roundtrip verify failed: %v", err)
	}
	if resp.Username != turnEntry.Username || resp.Credential != turnEntry.Credential {
		t.Fatalf("top-level credential must mirror TURN entry: top=%s/%s entry=%s/%s",
			resp.Username, resp.Credential, turnEntry.Username, turnEntry.Credential)
	}
}

// round2 evidence: STUN-only path returns no TURN credential but still returns
// a well-formed response — proves failure-mode branch is correctly exercised.
func TestIceServersSTUNOnly_Iter330ProcessEvidence(t *testing.T) {
	svc, _ := newRoomSvcSTUNOnly(t)
	h := New(svc, false)

	req := httptest.NewRequest(http.MethodGet, "/v1/rooms/room_demo/ice-servers?user_id=usr_iter330", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d body=%s", w.Code, w.Body.String())
	}
	var resp service.IceServersResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.IceServers) != 1 {
		t.Fatalf("expected STUN-only, got %d entries: %+v", len(resp.IceServers), resp.IceServers)
	}
	if resp.Username != "" || resp.Credential != "" {
		t.Fatalf("STUN-only mode must not carry TURN credentials, got u=%s c=%s", resp.Username, resp.Credential)
	}
}

// round2 evidence: expired TURN credential must fail Verify (failure-mode).
func TestIceServersCredentialExpires_Iter330ProcessEvidence(t *testing.T) {
	turnSigner, err := turn.NewSigner(turn.Config{PrimarySecret: turnTestSecret})
	if err != nil {
		t.Fatal(err)
	}
	frozen := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	turnSigner.SetNow(func() time.Time { return frozen })
	cred, err := turnSigner.Issue("usr_iter330", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// advance simulated clock past TTL, then verify: must fail with ErrExpired.
	turnSigner.SetNow(func() time.Time { return frozen.Add(2 * time.Second) })
	if err := turnSigner.Verify(cred.Username, cred.Credential); err == nil {
		t.Fatal("expected expired error after TTL window, got nil")
	}
}

// round2 evidence: /jwks.json returns a well-formed JWK Set with at least one key.
func TestJWKS_Iter330ProcessEvidence(t *testing.T) {
	svc, _, _ := newRoomSvcWithTURN(t)
	h := New(svc, false)

	req := httptest.NewRequest(http.MethodGet, "/jwks.json", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d body=%s", w.Code, w.Body.String())
	}
	var jwks map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &jwks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	keys, ok := jwks["keys"].([]any)
	if !ok || len(keys) == 0 {
		t.Fatalf("jwks.keys missing or empty: %v", jwks)
	}
	first := keys[0].(map[string]any)
	for _, field := range []string{"kty", "alg", "kid", "use", "n", "e"} {
		if _, present := first[field]; !present {
			t.Fatalf("jwk missing field %s: %v", field, first)
		}
	}
}

// round2 evidence: /internal/keys/health reflects active key id and signing alg.
func TestKeysHealth_Iter330ProcessEvidence(t *testing.T) {
	svc, _, _ := newRoomSvcWithTURN(t)
	h := New(svc, false)

	req := httptest.NewRequest(http.MethodGet, "/internal/keys/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d body=%s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v, _ := out["service"].(string); v != "room-svc" {
		t.Fatalf("service tag missing: %v", out)
	}
	if _, ok := out["active"].(string); !ok {
		t.Fatalf("active kid missing: %v", out)
	}
	if _, ok := out["alg"].(string); !ok {
		t.Fatalf("alg missing: %v", out)
	}
}
