package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	yerr "yunmao.live/pkg/yunmao/errors"
)

func TestTraceMiddlewareInjectsId(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if TraceID(r.Context()) == "" {
			t.Fatal("expected trace id in context")
		}
		w.WriteHeader(204)
	})
	srv := TraceMiddleware(next)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.ServeHTTP(rec, req)
	if rec.Header().Get("X-Trace-Id") == "" {
		t.Fatal("trace header not set")
	}
}

func TestTraceMiddlewarePassesThrough(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if TraceID(r.Context()) != "abc" {
			t.Fatalf("expected abc, got %q", TraceID(r.Context()))
		}
	})
	srv := TraceMiddleware(next)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Trace-Id", "abc")
	srv.ServeHTTP(rec, req)
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	WriteError(rec, req, yerr.New(yerr.FeedDeviceOffline, "device offline"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("got %d", rec.Code)
	}
	var env yerr.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "FEED.DEVICE_OFFLINE" {
		t.Fatalf("code: %s", env.Error.Code)
	}
}
