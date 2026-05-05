package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakePinger struct {
	err   error
	block bool
}

func (f *fakePinger) PingContext(ctx context.Context) error {
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.err
}

func newTestHealthHandler(p Pinger) *HealthHandler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewHealthHandler(p, logger)
}

func TestHealth_Healthy(t *testing.T) {
	h := newTestHealthHandler(&fakePinger{})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var body healthResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("expected status=ok, got %q", body.Status)
	}
	if body.Checks["database"] != "ok" {
		t.Errorf("expected checks.database=ok, got %q", body.Checks["database"])
	}
}

func TestHealth_Unhealthy_DBPingError(t *testing.T) {
	h := newTestHealthHandler(&fakePinger{err: errors.New("connection refused")})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}

	var body healthResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Status != "unhealthy" {
		t.Errorf("expected status=unhealthy, got %q", body.Status)
	}
	if body.Checks["database"] != "connection refused" {
		t.Errorf("expected checks.database=connection refused, got %q", body.Checks["database"])
	}
}

func TestHealth_Unhealthy_Timeout(t *testing.T) {
	// We don't override the package-level constant; instead we use a
	// blocking pinger that respects context cancellation. The handler
	// itself enforces the 2s deadline via context.WithTimeout. To keep the
	// test fast, cancel the request context before calling.
	h := newTestHealthHandler(&fakePinger{block: true})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	start := time.Now()
	mux.ServeHTTP(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}

	if elapsed > healthCheckTimeout {
		t.Errorf("handler took %v, expected ≤ %v (context was already cancelled)", elapsed, healthCheckTimeout)
	}

	var body healthResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Status != "unhealthy" {
		t.Errorf("expected status=unhealthy, got %q", body.Status)
	}
	if !strings.Contains(body.Checks["database"], "context") {
		t.Errorf("expected checks.database to mention context cancellation, got %q", body.Checks["database"])
	}
}

func TestHealth_MethodNotAllowed(t *testing.T) {
	h := newTestHealthHandler(&fakePinger{})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHealth_Stateless_RecoversAfterFailure(t *testing.T) {
	p := &fakePinger{err: errors.New("down")}
	h := newTestHealthHandler(p)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// First call: unhealthy.
	req1 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, req1)
	if w1.Code != http.StatusServiceUnavailable {
		t.Fatalf("first call: expected 503, got %d", w1.Code)
	}

	// DB recovers.
	p.err = nil

	// Second call: healthy.
	req2 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d", w2.Code)
	}
}
