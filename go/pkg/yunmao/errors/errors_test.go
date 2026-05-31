package errors

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHTTPStatusMapping(t *testing.T) {
	cases := map[Code]int{
		AuthLoginRequired:       http.StatusUnauthorized,
		FeedCooldownNotFinished: http.StatusConflict,
		MediaStreamOffline:      http.StatusServiceUnavailable,
		SystemRateLimited:       http.StatusTooManyRequests,
		SystemInternal:          http.StatusInternalServerError,
	}
	for code, want := range cases {
		if got := code.HTTPStatus(); got != want {
			t.Fatalf("%s: want %d got %d", code, want, got)
		}
	}
}

func TestEnvelopeJSON(t *testing.T) {
	e := New(FeedCooldownNotFinished, "等 42s").WithTrace("01HX")
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var env Envelope
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "FEED.COOLDOWN_NOT_FINISHED" {
		t.Fatalf("got code %q", env.Error.Code)
	}
	if env.Error.TraceID != "01HX" {
		t.Fatalf("trace not propagated")
	}
}

func TestAsAppError(t *testing.T) {
	if got := AsAppError(nil); got != nil {
		t.Fatal("expected nil")
	}
	plain := errorString("boom")
	if got := AsAppError(plain); got.Code != SystemInternal {
		t.Fatalf("expected SYSTEM.INTERNAL, got %s", got.Code)
	}
	custom := New(FeedDeviceOffline, "x")
	if got := AsAppError(custom); got != custom {
		t.Fatalf("expected pass-through")
	}
}

type errorString string

func (s errorString) Error() string { return string(s) }
