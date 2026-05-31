package observability

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestHealthzReturnsOK(t *testing.T) {
	r := chi.NewRouter()
	WireFull(r, Probes{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("healthz: want 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "ok" {
		t.Fatalf("healthz: want body 'ok', got %q", body)
	}
}

func TestReadyzLegacyNilReady(t *testing.T) {
	r := chi.NewRouter()
	WireFull(r, Probes{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("readyz: want 200, got %d", w.Code)
	}
}

func TestReadyzLegacyErrorReturnsServiceUnavailable(t *testing.T) {
	r := chi.NewRouter()
	WireFull(r, Probes{}, func() error { return errors.New("not ready") })

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz: want 503, got %d", w.Code)
	}
}

func TestInternalLivezReturnsOK(t *testing.T) {
	r := chi.NewRouter()
	WireFull(r, Probes{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/internal/livez", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("internal/livez: want 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "ok" {
		t.Fatalf("internal/livez: want body 'ok', got %q", body)
	}
}

func TestInternalReadyzAllOK(t *testing.T) {
	r := chi.NewRouter()
	WireFull(r, Probes{
		"pg":    func() error { return nil },
		"redis": func() error { return nil },
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/internal/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("internal/readyz: want 200, got %d", w.Code)
	}
	var out struct {
		Ready bool              `json:"ready"`
		Deps  map[string]string `json:"deps"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Ready {
		t.Fatalf("internal/readyz: want ready=true, got false")
	}
	if out.Deps["pg"] != "ok" {
		t.Fatalf("internal/readyz: pg want 'ok', got %q", out.Deps["pg"])
	}
	if out.Deps["redis"] != "ok" {
		t.Fatalf("internal/readyz: redis want 'ok', got %q", out.Deps["redis"])
	}
}

func TestInternalReadyzFailingDep(t *testing.T) {
	r := chi.NewRouter()
	WireFull(r, Probes{
		"pg":    func() error { return nil },
		"redis": func() error { return errors.New("connection refused") },
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/internal/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("internal/readyz: want 503, got %d", w.Code)
	}
	var out struct {
		Ready bool              `json:"ready"`
		Deps  map[string]string `json:"deps"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Ready {
		t.Fatalf("internal/readyz: want ready=false, got true")
	}
	if out.Deps["pg"] != "ok" {
		t.Fatalf("internal/readyz: pg want 'ok', got %q", out.Deps["pg"])
	}
	if got := out.Deps["redis"]; got == "ok" {
		t.Fatalf("internal/readyz: redis should not be ok")
	}
	if out.Deps["redis"] != "err: connection refused" {
		t.Fatalf("internal/readyz: redis want 'err: connection refused', got %q", out.Deps["redis"])
	}
}

func TestInternalReadyzNoProbes(t *testing.T) {
	r := chi.NewRouter()
	WireFull(r, Probes{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/internal/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("internal/readyz (no probes): want 200, got %d", w.Code)
	}
	var out struct {
		Ready bool              `json:"ready"`
		Deps  map[string]string `json:"deps"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Ready {
		t.Fatalf("internal/readyz (no probes): want ready=true")
	}
}

func TestInternalReadyzProbePanic(t *testing.T) {
	r := chi.NewRouter()
	WireFull(r, Probes{
		"bad": func() error { panic("boom") },
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/internal/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("internal/readyz (panic): want 503, got %d", w.Code)
	}
	var out struct {
		Ready bool              `json:"ready"`
		Deps  map[string]string `json:"deps"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Ready {
		t.Fatal("internal/readyz (panic): want ready=false")
	}
	if got := out.Deps["bad"]; got == "ok" {
		t.Fatalf("internal/readyz (panic): bad should not be ok, got %q", got)
	}
}
