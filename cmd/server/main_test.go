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
