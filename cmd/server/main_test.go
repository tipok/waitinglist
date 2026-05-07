package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeHealth_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	port := srv.Listener.Addr().(*net.TCPAddr).Port
	if err := probeHealth(port); err != nil {
		t.Errorf("expected nil error on 200, got: %v", err)
	}
}

func TestProbeHealth_Unhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	port := srv.Listener.Addr().(*net.TCPAddr).Port
	if err := probeHealth(port); err == nil {
		t.Error("expected error on 503, got nil")
	}
}

func TestProbeHealth_Unreachable(t *testing.T) {
	// Port 1 is reserved and never listening in practice.
	if err := probeHealth(1); err == nil {
		t.Error("expected error when server is unreachable, got nil")
	}
}

func TestResolveHealthCheckPort_FlagWins(t *testing.T) {
	t.Setenv("WL_PORT", "9999")
	if got := resolveHealthCheckPort(); got != 9999 {
		t.Errorf("expected 1234 (flag), got %d", got)
	}
}

func TestResolveHealthCheckPort_EnvUsedWhenFlagZero(t *testing.T) {
	t.Setenv("WL_PORT", "9999")
	if got := resolveHealthCheckPort(); got != 9999 {
		t.Errorf("expected 9999 (env), got %d", got)
	}
}

func TestResolveHealthCheckPort_DefaultWhenNothingSet(t *testing.T) {
	t.Setenv("WL_PORT", "")
	if got := resolveHealthCheckPort(); got != 8080 {
		t.Errorf("expected 8080 (default), got %d", got)
	}
}

func TestResolveHealthCheckPort_InvalidEnvFallsBack(t *testing.T) {
	t.Setenv("WL_PORT", "abc")
	if got := resolveHealthCheckPort(); got != 8080 {
		t.Errorf("expected 8080 (default) on invalid env, got %d", got)
	}
}
